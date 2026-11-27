package signing

import (
	"bytes"
	"context"
	"database/sql"
	"image/png"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// Filling a party's fields from the signer's profile. A field is matched by
// its name (case, accents and punctuation do not matter; English and German
// synonyms are known) or, for signatures and initials, by its type. Nothing
// with a value, a default of the sender or marked read-only is touched.

type builtinName struct {
	key     string
	phrases []string
	exact   []string // phrases that must be the whole name
}

var builtinNames = []builtinName{
	{"email", []string{"email", "e mail", "email address", "e mail address", "e mail adresse", "email adresse", "emailadresse", "mailadresse", "mail"}, []string{"mail"}},
	{"phone", []string{"phone", "phone number", "telephone", "mobile", "mobile number", "telefon", "telefonnummer", "mobil", "mobilnummer", "handy", "handynummer", "rufnummer", "tel"}, []string{"tel", "mobil", "handy"}},
	{"firstName", []string{"first name", "firstname", "given name", "forename", "vorname"}, nil},
	{"lastName", []string{"last name", "lastname", "surname", "family name", "nachname", "familienname"}, nil},
	{"fullName", []string{"full name", "fullname", "your name", "your full name", "signer", "signer name", "name", "vollstandiger name", "vor und nachname", "vorname und nachname", "vorname nachname", "name vorname", "nachname vorname", "unterzeichner", "ihr name", "dein name"}, []string{"name", "signer", "unterzeichner"}},
}

// A saved preset label matches every phrase of its group.
var presetGroups = [][]string{
	{"address", "street address", "postal address", "home address", "adresse", "anschrift", "postanschrift", "wohnadresse", "strasse", "street"},
	{"company", "company name", "organization", "organisation", "employer", "firma", "firmenname", "unternehmen", "arbeitgeber"},
	{"title", "job title", "position", "role", "titel", "berufsbezeichnung", "funktion"},
	{"city", "town", "ort", "stadt", "wohnort"},
	{"zip", "zip code", "postal code", "postcode", "plz", "postleitzahl"},
	{"country", "land"},
	{"date of birth", "birth date", "birthday", "dob", "geburtsdatum", "geboren am"},
	{"iban"},
	{"tax id", "tax number", "steuer id", "steuernummer", "steuer identifikationsnummer"},
	{"vat id", "vat number", "ust id", "ustid", "umsatzsteuer id"},
}

const minPartialLength = 5

var nonName = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeName: lower case, no accents, "ß" as "ss", other characters one
// space.
func normalizeName(value string) string {
	return strings.TrimSpace(nonName.ReplaceAllString(strings.ReplaceAll(comparable(value), "ß", "ss"), " "))
}

// fit rates how well a phrase fits a field name; a whole-name match beats any
// partial one.
func fit(name, phrase string, exactOnly bool) int {
	if name == phrase {
		return 1000 + len(phrase)
	}
	if exactOnly || len(phrase) < minPartialLength || !strings.Contains(" "+name+" ", " "+phrase+" ") {
		return 0
	}
	return len(phrase)
}

// matchName is the text of the profile a field name stands for. Presets win a
// tie.
func matchName(fieldName string, d details) string {
	name := normalizeName(fieldName)
	if name == "" {
		return ""
	}
	best, found := 0, ""
	for _, p := range d.Presets {
		label := normalizeName(p.Label)
		phrases := []string{label}
		for _, group := range presetGroups {
			if slices.Contains(group, label) {
				phrases = group
				break
			}
		}
		for _, phrase := range phrases {
			if score := fit(name, phrase, false); score > best {
				best, found = score, p.Value
			}
		}
	}
	parts := strings.Fields(d.DisplayName)
	first, last := strings.Join(parts, " "), ""
	if len(parts) > 1 {
		first, last = strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
	}
	values := map[string]string{"email": d.Email, "phone": d.Phone, "fullName": d.DisplayName, "firstName": first, "lastName": last}
	for _, builtin := range builtinNames {
		for _, phrase := range builtin.phrases {
			if score := fit(name, phrase, slices.Contains(builtin.exact, phrase)); score > best {
				best, found = score, values[builtin.key]
			}
		}
	}
	return found
}

// fillKind is "image", "today", "named" or "".
func fillKind(f field) string {
	switch f.Type {
	case "signature", "initials":
		return "image"
	case "date":
		if f.prefs()["autoToday"] == true {
			return "today"
		}
		return ""
	case "text", "phone", "number", "select", "radio":
		return "named"
	default:
		return ""
	}
}

type prefillItem struct {
	FieldID string `json:"fieldId"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Source  string `json:"source"`
	Value   any    `json:"value,omitempty"`
	ImageID string `json:"imageId,omitempty"`
}

// openFields are the party's fields that can still take a value.
func openFields(ctx context.Context, q querier, documentID, partyID string) ([]field, error) {
	return queryFields(ctx, q, `WHERE document_id=? AND party_id=? AND readonly=0 AND default_value IS NULL
		AND id NOT IN (SELECT field_id FROM sign_values WHERE document_id=sign_fields.document_id) ORDER BY position`, documentID, partyID)
}

func (h *Handler) computePrefill(ctx context.Context, userID int64, documentID, partyID, today string) ([]prefillItem, error) {
	items := []prefillItem{}
	p, err := h.profileOf(ctx, h.db, userID)
	if err != nil || !p.autoFill {
		return items, err
	}
	fields, err := openFields(ctx, h.db, documentID, partyID)
	if err != nil {
		return nil, err
	}
	if today == "" {
		today = h.now().UTC().Format("2006-01-02")
	}
	for _, f := range fields {
		item := prefillItem{FieldID: f.ID, Name: f.Name, Type: f.Type, Source: "profile"}
		prefs := f.prefs()
		switch kind := fillKind(f); kind {
		case "image":
			images, err := queryImages(ctx, h.db, `WHERE user_id=? AND kind=? ORDER BY is_default DESC,created_at DESC,id DESC`, userID, f.Type)
			if err != nil {
				return nil, err
			}
			methods := prefStrings(prefs, "methods")
			index := slices.IndexFunc(images, func(i savedImage) bool { return len(methods) == 0 || slices.Contains(methods, i.method) })
			if index < 0 {
				continue
			}
			item.ImageID = images[index].id
		case "today", "named":
			raw := today
			if kind == "named" {
				raw = matchName(f.Name, p.details)
			}
			if raw == "" {
				continue
			}
			normalized, err := normalizeValue(f.Type, prefs, raw)
			if err != nil || normalized == nil || normalized == "" {
				continue
			}
			item.Value = normalized
		default:
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// applyImage copies a saved picture from the signer's storage into the field,
// stored in the document owner's.
func (h *Handler) applyImage(ctx context.Context, userID int64, c signerContext, f field, imageID string) error {
	image, err := ownImage(ctx, h.db, userID, imageID)
	if err != nil {
		return err
	}
	if err := h.checkRoom(ctx, h.db, c.document.OwnerID, image.size, "The sender has no storage left for this."); err != nil {
		return err
	}
	data, err := h.readBlob(ctx, userID, image.blobID, image.size)
	if err != nil {
		return err
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	blob, err := h.storeBlob(ctx, c.document.OwnerID, data)
	if err != nil {
		return err
	}
	meta := map[string]any{"kind": "image", "mime": "image/png", "width": config.Width, "height": config.Height, "method": image.method, "sha256": blob.sha256}
	if image.typed.String != "" {
		meta["text"] = image.typed.String
	}
	if err := h.saveBlobValue(ctx, c, f, blob, meta, false); err != nil {
		blob.discard()
		return err
	}
	return nil
}

// applyPrefill writes the items; one that fails is skipped.
func (h *Handler) applyPrefill(ctx context.Context, userID int64, c signerContext, items []prefillItem) ([]string, error) {
	fields, err := openFields(ctx, h.db, c.document.ID, c.party.ID)
	if err != nil {
		return nil, err
	}
	applied := []string{}
	for _, item := range items {
		index := slices.IndexFunc(fields, func(f field) bool { return f.ID == item.FieldID })
		if index < 0 {
			continue
		}
		f := fields[index]
		if fieldTypes[f.Type].storage == storeBlob {
			err = h.applyImage(ctx, userID, c, f, item.ImageID)
		} else {
			_, err = h.saveTypedValue(ctx, c, f, item.Value, false)
		}
		if err == nil {
			applied = append(applied, f.ID)
		}
	}
	return applied, nil
}

func alreadyClaimed() *apiError {
	return httpError(409, "This request belongs to another account.", "ALREADY_CLAIMED")
}

// bindParty binds the party to the account unless another account holds it.
func (h *Handler) bindParty(ctx context.Context, userID int64, c signerContext, from client) error {
	if c.party.UserID.Valid {
		if c.party.UserID.Int64 == userID {
			return nil
		}
		return alreadyClaimed()
	}
	return h.tx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE sign_parties SET user_id=? WHERE id=? AND user_id IS NULL`, userID, c.party.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return alreadyClaimed()
		}
		return appendEvent(ctx, tx, c.document.ID, event{kind: "claimed", partyID: c.party.ID, at: h.stampNow(), client: from, data: map[string]any{"via": "link"}})
	})
}

var todayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// claimParty lets a signed-in account take a signing link as its own. With
// apply the prefill is written; with preview it is only listed.
func (h *Handler) claimParty(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	token, ok := input["token"].(string)
	if !ok {
		return nil, badRequest("The body needs the link token.")
	}
	today, _ := input["today"].(string)
	if !todayPattern.MatchString(today) {
		today = ""
	}
	c, err := h.openContext(ctx, token, clientOf(r))
	if err != nil {
		return nil, err
	}
	preview := input["preview"] == true
	if preview {
		if c.party.UserID.Valid && c.party.UserID.Int64 != userID {
			return nil, alreadyClaimed()
		}
	} else if err := h.bindParty(ctx, userID, c, clientOf(r)); err != nil {
		return nil, err
	}
	prefill := []prefillItem{}
	if c.state == "ready" {
		if prefill, err = h.computePrefill(ctx, userID, c.document.ID, c.party.ID, today); err != nil {
			return nil, err
		}
	}
	applied := []string{}
	if !preview && input["apply"] == true && len(prefill) > 0 {
		if applied, err = h.applyPrefill(ctx, userID, c, prefill); err != nil {
			return nil, err
		}
	}
	return map[string]any{"partyId": c.party.ID, "documentTitle": c.document.Title, "prefill": prefill, "canPrefill": len(prefill) > 0, "applied": applied}, nil
}
