package signing

import (
	"context"
	"database/sql"
	"encoding/base64"
	"log/slog"
	"maps"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

// Quick sign: the owner signs a draft of their own in one request, without
// parties to set up or links to hand out. It runs the steps of the normal
// flow (the owner's own party, fields, send, opened, consent, values,
// complete), so the signed file is sealed, saved beside the original and
// audited like any other.

const maxQuickFields = 50

var quickTypes = []string{"signature", "initials", "date", "text"}

// quickEntry is one field of a quick sign with the value it gets.
type quickEntry struct {
	spec    map[string]any // the field as the field list takes it, without its party
	value   any            // a date or text
	imageID string         // a saved picture of the signing profile
	png     []byte         // or a picture sent along, re-encoded
	meta    map[string]any
}

// parseQuick checks every entry before anything is stored.
func (h *Handler) parseQuick(ctx context.Context, userID int64, input any) ([]quickEntry, error) {
	list, ok := input.([]any)
	if !ok || len(list) < 1 || len(list) > maxQuickFields {
		return nil, invalidField("Quick sign takes 1 to " + strconv.Itoa(maxQuickFields) + " fields.")
	}
	entries := make([]quickEntry, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, invalidField("Each field must be an object.")
		}
		kind, _ := item["type"].(string)
		if !contains(quickTypes, kind) {
			return nil, invalidField("Quick sign takes signature, initials, date and text fields.")
		}
		prefs, err := normalizePrefs(kind, item["prefs"])
		if err != nil {
			return nil, err
		}
		e := quickEntry{spec: map[string]any{"type": kind, "areas": item["areas"], "prefs": prefs}}
		if fieldTypes[kind].storage == storeBlob {
			err = h.quickImage(ctx, userID, kind, item, &e)
		} else if e.value, err = normalizeValue(kind, prefs, item["value"]); err == nil && emptyValue(e.value) {
			err = invalidValue("Every date and text field needs its value.")
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// quickImage takes a saved picture of the caller's profile or a PNG sent as
// base64 with how it was made.
func (h *Handler) quickImage(ctx context.Context, userID int64, kind string, item map[string]any, e *quickEntry) error {
	if id, _ := item["imageId"].(string); id != "" {
		image, err := ownImage(ctx, h.db, userID, id)
		if err != nil {
			return err
		}
		if image.kind != kind {
			return invalidImage("This saved picture is for another kind of field.")
		}
		e.imageID = id
		return nil
	}
	encoded, _ := item["image"].(string)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return invalidImage("A signature needs a saved picture or a PNG.")
	}
	t := fieldTypes[kind]
	png, width, height, err := signpdf.PNG(ctx, data, t.maxW, t.maxH)
	if err != nil {
		return invalidImage("This image could not be read. Use a PNG or JPEG picture.")
	}
	method, _ := item["method"].(string)
	typed, _ := item["text"].(string)
	e.png, e.meta = png, map[string]any{"kind": "image", "mime": "image/png", "width": width, "height": height}
	return signatureMeta(field{Type: kind}, method, typed, e.meta)
}

// quickSign signs one of the owner's drafts as the owner's only party.
func (h *Handler) quickSign(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	documentID := r.PathValue("id")
	raw, _ := input["signerName"].(string)
	name := singleLine(raw)
	if name == "" || length(name) > maxSignerName {
		return nil, badRequest("Enter your name (up to " + strconv.Itoa(maxSignerName) + " characters).")
	}
	entries, err := h.parseQuick(ctx, userID, input["fields"])
	if err != nil {
		return nil, err
	}
	partyID, err := h.prepareQuick(ctx, userID, documentID, name, entries)
	if err != nil {
		return nil, err
	}
	if err := h.signQuick(r, userID, documentID, partyID, name, entries, input); err != nil {
		h.discardQuick(context.WithoutCancel(ctx), userID, documentID)
		return nil, err
	}
	return h.describe(ctx, userID, documentID)
}

// prepareQuick makes the owner the draft's only party and the entries its
// fields. A refusal here leaves the draft as it was.
func (h *Handler) prepareQuick(ctx context.Context, userID int64, documentID, name string, entries []quickEntry) (string, error) {
	partyID := ""
	_, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if d.Status != statusDraft {
			return httpError(409, "Only a draft can be signed this way.", "ALREADY_SENT")
		}
		at := h.stampNow()
		if err := replaceParties(ctx, tx, d, []any{map[string]any{"name": name, "isSelf": true}}, at); err != nil {
			return err
		}
		parties, err := listParties(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		partyID = parties[0].ID
		specs := make([]any, len(entries))
		for i, e := range entries {
			spec := maps.Clone(e.spec)
			spec["partyId"] = partyID
			specs[i] = spec
		}
		if err := replaceFields(ctx, tx, d, specs); err != nil {
			return err
		}
		return touch(ctx, tx, d.ID, at)
	})
	return partyID, err
}

// signQuick sends the draft without mail and signs it as its owner.
func (h *Handler) signQuick(r *http.Request, userID int64, documentID, partyID, name string, entries []quickEntry, input body) error {
	ctx := r.Context()
	if _, err := h.send(r, userID, documentID, body{"deliverEmail": false}); err != nil {
		return err
	}
	d, err := documentByID(ctx, h.db, documentID)
	if err != nil {
		return err
	}
	p, err := partyByID(ctx, h.db, partyID)
	if err != nil {
		return err
	}
	c := signerContext{document: d, party: p, parties: []party{p}, state: publicState(d.Status, p.Status)}
	if err := h.recordOpened(r, c); err != nil {
		return err
	}
	consent := body{"accepted": true, "signerName": name, "timezone": input["timezone"], "tzOffsetMinutes": input["tzOffsetMinutes"]}
	if _, err := h.recordConsent(r, c, consent); err != nil {
		return err
	}
	fields, err := listFields(ctx, h.db, documentID)
	if err != nil {
		return err
	}
	for i, f := range fields {
		if err := h.fillQuick(ctx, userID, c, f, entries[i]); err != nil {
			return err
		}
	}
	_, err = h.completeParty(r, c, nil)
	return err
}

func (h *Handler) fillQuick(ctx context.Context, userID int64, c signerContext, f field, e quickEntry) error {
	switch {
	case e.imageID != "":
		return h.applyImage(ctx, userID, c, f, e.imageID)
	case e.png != nil:
		blob, err := h.storeBlob(ctx, c.document.OwnerID, e.png)
		if err != nil {
			return err
		}
		meta := maps.Clone(e.meta)
		meta["sha256"] = blob.sha256
		if err := h.saveBlobValue(ctx, c, f, blob, meta, true); err != nil {
			blob.discard()
			return err
		}
		return nil
	default:
		_, err := h.saveTypedValue(ctx, c, f, e.value, true)
		return err
	}
}

// discardQuick removes a quick sign that failed after it was sent, so no
// request is left that nobody will sign.
func (h *Handler) discardQuick(ctx context.Context, userID int64, documentID string) {
	if d, err := ownedDocument(ctx, h.db, userID, documentID); err == nil && isLive(d.Status) {
		if err := h.cancel(ctx, userID, documentID, client{}); err != nil {
			slog.Warn("signing: a failed quick sign was not canceled", "document", documentID, "error", err)
		}
	}
	if err := h.remove(ctx, userID, documentID); err != nil {
		slog.Warn("signing: a failed quick sign was not removed", "document", documentID, "error", err)
	}
}
