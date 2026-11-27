package signing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// An account's signing profile: the details used to fill in signing pages,
// the switches, the folder signed documents are saved to and the saved
// signatures. Details and presets are one text encrypted with the server key.

const (
	maxPresets      = 30
	maxPresetLabel  = 60
	maxPresetValue  = 500
	maxProfileName  = 120
	maxProfilePhone = 40

	maxImagesPerKind = 5
	maxImageLabel    = 60
)

type preset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type details struct {
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Presets     []preset `json:"presets"`
}

type profile struct {
	details
	autoFill, autoSave, notify bool
	saveFolder                 text
}

func (h *Handler) profileOf(ctx context.Context, q querier, userID int64) (profile, error) {
	p := profile{autoFill: true, autoSave: true}
	var data text
	var autoFill, autoSave, notify int
	err := q.QueryRowContext(ctx, `SELECT data_enc,auto_fill,auto_save,save_folder_id,notify_each_signature FROM sign_profiles WHERE user_id=?`, userID).
		Scan(&data, &autoFill, &autoSave, &p.saveFolder, &notify)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if err == nil {
		p.autoFill, p.autoSave, p.notify = autoFill == 1, autoSave == 1, notify == 1
	}
	if data.String != "" {
		plain, err := h.keys.DecryptValue(data.String)
		if err != nil {
			return p, err
		}
		_ = json.Unmarshal([]byte(plain), &p.details)
	} else {
		// What the account itself says, until the person saves their own.
		var username string
		var display, email text
		if err := q.QueryRowContext(ctx, `SELECT username,display_name,email FROM users WHERE id=?`, userID).Scan(&username, &display, &email); err != nil {
			return p, err
		}
		p.DisplayName, p.Email = display.String, email.String
		if p.DisplayName == "" {
			p.DisplayName = username
		}
	}
	if p.Presets == nil {
		p.Presets = []preset{}
	}
	return p, nil
}

func imageView(id, kind, method, label string, isDefault bool, created string) map[string]any {
	return map[string]any{"id": id, "kind": kind, "method": method, "label": label, "isDefault": isDefault, "createdAt": created}
}

type savedImage struct {
	id, kind, method, label, blobID, created string
	typed                                    text
	isDefault                                bool
	size                                     int64
}

func (i savedImage) view() map[string]any {
	return imageView(i.id, i.kind, i.method, i.label, i.isDefault, i.created)
}

func queryImages(ctx context.Context, q querier, where string, args ...any) ([]savedImage, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,kind,method,label,blob_id,created_at,typed_text,is_default,size FROM sign_profile_images `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []savedImage
	for rows.Next() {
		var i savedImage
		var label text
		if err := rows.Scan(&i.id, &i.kind, &i.method, &label, &i.blobID, &i.created, &i.typed, &i.isDefault, &i.size); err != nil {
			return nil, err
		}
		i.label = label.String
		out = append(out, i)
	}
	return out, rows.Err()
}

func (h *Handler) readProfile(ctx context.Context, userID int64) (map[string]any, error) {
	p, err := h.profileOf(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	images, err := queryImages(ctx, h.db, `WHERE user_id=? ORDER BY kind DESC,created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(images))
	for _, image := range images {
		views = append(views, image.view())
	}
	return map[string]any{"displayName": p.DisplayName, "email": p.Email, "phone": p.Phone, "presets": p.Presets,
		"autoFill": p.autoFill, "autoSave": p.autoSave, "saveFolderId": nullable(p.saveFolder), "notifyOnEachSignature": p.notify,
		"signatures": views}, nil
}

func (h *Handler) getProfile(r *http.Request, userID int64, _ body) (any, error) {
	return h.readProfile(r.Context(), userID)
}

func invalidProfile(message string) *apiError { return httpError(400, message, "INVALID_PROFILE") }

func profileText(value any, what string, max int) (string, error) {
	s, ok := value.(string)
	s = singleLine(s)
	if !ok || length(s) > max {
		return "", invalidProfile(what + " must be text of at most " + strconv.Itoa(max) + " characters.")
	}
	return s, nil
}

// comparable folds case and accents.
func comparable(value string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(value)))
}

func cleanPresets(input any) ([]preset, error) {
	list, ok := input.([]any)
	if !ok || len(list) > maxPresets {
		return nil, invalidProfile("Up to " + strconv.Itoa(maxPresets) + " presets are possible.")
	}
	seen := map[string]bool{}
	out := make([]preset, 0, len(list))
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, invalidProfile("Each preset must be an object.")
		}
		label, err := profileText(entry["label"], "A preset label", maxPresetLabel)
		if err != nil {
			return nil, err
		}
		if label == "" {
			return nil, invalidProfile("A preset needs a label.")
		}
		if seen[comparable(label)] {
			return nil, invalidProfile("Preset labels must be different.")
		}
		seen[comparable(label)] = true
		content, ok := entry["value"].(string)
		if !ok {
			return nil, invalidProfile("A preset value must be text.")
		}
		content = strings.TrimSpace(plainText(content))
		if length(content) > maxPresetValue {
			return nil, invalidProfile("A preset value has at most " + strconv.Itoa(maxPresetValue) + " characters.")
		}
		id, _ := entry["id"].(string)
		if !uuidPattern.MatchString(id) {
			id = uuid.NewString()
		}
		out = append(out, preset{ID: id, Label: label, Value: content})
	}
	return out, nil
}

// putProfile replaces what the body names; the rest stays.
func (h *Handler) putProfile(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	err := h.tx(ctx, func(tx *sql.Tx) error {
		p, err := h.profileOf(ctx, tx, userID)
		if err != nil {
			return err
		}
		if v, ok := input["displayName"]; ok {
			if p.DisplayName, err = profileText(v, "The name", maxProfileName); err != nil {
				return err
			}
		}
		if v, ok := input["email"]; ok {
			if p.Email, err = profileText(v, "The email", maxEmailLength); err != nil {
				return err
			}
			if p.Email != "" && !emailPattern.MatchString(p.Email) {
				return invalidProfile("That email address is not valid.")
			}
		}
		if v, ok := input["phone"]; ok {
			if p.Phone, err = profileText(v, "The phone number", maxProfilePhone); err != nil {
				return err
			}
		}
		if v, ok := input["presets"]; ok {
			if p.Presets, err = cleanPresets(v); err != nil {
				return err
			}
		}
		for name, target := range map[string]*bool{"autoFill": &p.autoFill, "autoSave": &p.autoSave, "notifyOnEachSignature": &p.notify} {
			if v, ok := input[name]; ok {
				b, isBool := v.(bool)
				if !isBool {
					return invalidProfile(name + " must be true or false.")
				}
				*target = b
			}
		}
		if v, ok := input["saveFolderId"]; ok {
			p.saveFolder = text{}
			if v != nil {
				id, _ := v.(string)
				folder := usableFolder(ctx, tx, userID, id)
				if folder == "" {
					return httpError(400, "Choose a folder of your own that is not a vault.", "INVALID_FOLDER")
				}
				p.saveFolder = text{String: folder, Valid: true}
			}
		}
		data, err := h.keys.EncryptValue(encodeJSON(p.details))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sign_profiles(user_id,data_enc,auto_fill,auto_save,save_folder_id,notify_each_signature,updated_at) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET data_enc=excluded.data_enc,auto_fill=excluded.auto_fill,auto_save=excluded.auto_save,
			save_folder_id=excluded.save_folder_id,notify_each_signature=excluded.notify_each_signature,updated_at=excluded.updated_at`,
			userID, data, flag(p.autoFill), flag(p.autoSave), nullable(p.saveFolder), flag(p.notify), h.stampNow())
		return err
	})
	if err != nil {
		return nil, err
	}
	return h.readProfile(ctx, userID)
}

// ownImage is the account's own image; another account's id answers the same.
func ownImage(ctx context.Context, q querier, userID int64, id string) (savedImage, error) {
	images, err := queryImages(ctx, q, `WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return savedImage{}, err
	}
	if len(images) == 0 {
		return savedImage{}, httpError(404, "Image not found", "NOT_FOUND")
	}
	return images[0], nil
}

func invalidImage(message string) *apiError { return httpError(400, message, "INVALID_IMAGE") }

func makeDefault(ctx context.Context, q querier, userID int64, kind, id string) error {
	_, err := q.ExecContext(ctx, `UPDATE sign_profile_images SET is_default=(id=?) WHERE user_id=? AND kind=?`, id, userID, kind)
	return err
}

// addProfileImage saves the request body as a new signature or initials.
func (h *Handler) addProfileImage(w http.ResponseWriter, r *http.Request, userID int64) {
	view, err := func() (map[string]any, error) {
		ctx := r.Context()
		query := r.URL.Query()
		kind := query.Get("kind")
		if kind != "signature" && kind != "initials" {
			return nil, invalidImage("kind is signature or initials.")
		}
		method := "draw"
		if query.Has("method") {
			method = query.Get("method")
		}
		if !contains(signingMethods, method) {
			return nil, invalidImage("method is one of: " + strings.Join(signingMethods, ", ") + ".")
		}
		var typed any
		if method == "type" {
			typed = nullIfEmpty(truncate(singleLine(query.Get("text")), maxNameLength))
		}
		label := singleLine(query.Get("label"))
		if length(label) > maxImageLabel {
			return nil, invalidImage("A label has at most " + strconv.Itoa(maxImageLabel) + " characters.")
		}
		full := func(q querier) (int, error) {
			var count int
			err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sign_profile_images WHERE user_id=? AND kind=?`, userID, kind).Scan(&count)
			if err == nil && count >= maxImagesPerKind {
				name := "signatures"
				if kind == "initials" {
					name = "initials"
				}
				err = httpError(409, "You can save "+strconv.Itoa(maxImagesPerKind)+" "+name+".", "IMAGE_LIMIT")
			}
			return count, err
		}
		if _, err := full(h.db); err != nil {
			return nil, err
		}
		png, _, _, err := uploadPNG(r, kind)
		if err != nil {
			return nil, err
		}
		if err := h.checkRoom(ctx, h.db, userID, int64(len(png)), "Not enough storage left for this."); err != nil {
			return nil, err
		}
		blob, err := h.storeBlob(ctx, userID, png)
		if err != nil {
			return nil, err
		}
		id := uuid.NewString()
		var image savedImage
		err = h.tx(ctx, func(tx *sql.Tx) error {
			count, err := full(tx)
			if err != nil {
				return err
			}
			if err := adoptBlob(ctx, tx, userID, blob); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO sign_profile_images(id,user_id,kind,method,label,typed_text,is_default,blob_id,size,created_at) VALUES(?,?,?,?,?,?,0,?,?,?)`,
				id, userID, kind, method, label, typed, blob.id, blob.size, h.stampNow()); err != nil {
				return err
			}
			if query.Get("default") == "1" || count == 0 {
				if err := makeDefault(ctx, tx, userID, kind, id); err != nil {
					return err
				}
			}
			image, err = ownImage(ctx, tx, userID, id)
			return err
		})
		if err != nil {
			blob.discard()
			return nil, err
		}
		return image.view(), nil
	}()
	respond(w, 201, view, err)
}

func (h *Handler) profileImage(w http.ResponseWriter, r *http.Request, userID int64) {
	image, err := ownImage(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendPNG(w, r, userID, image.blobID, image.size)
}

func (h *Handler) updateProfileImage(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	var image savedImage
	err := h.tx(ctx, func(tx *sql.Tx) error {
		current, err := ownImage(ctx, tx, userID, r.PathValue("id"))
		if err != nil {
			return err
		}
		isDefault, hasDefault := input["isDefault"]
		if _, ok := isDefault.(bool); hasDefault && !ok {
			return invalidImage("isDefault must be true or false.")
		}
		if v, ok := input["label"]; ok {
			s, _ := v.(string)
			label := singleLine(s)
			if length(label) > maxImageLabel {
				return invalidImage("A label has at most " + strconv.Itoa(maxImageLabel) + " characters.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE sign_profile_images SET label=? WHERE id=?`, label, current.id); err != nil {
				return err
			}
		}
		if isDefault == true {
			if err := makeDefault(ctx, tx, userID, current.kind, current.id); err != nil {
				return err
			}
		} else if isDefault == false {
			if _, err := tx.ExecContext(ctx, `UPDATE sign_profile_images SET is_default=0 WHERE id=?`, current.id); err != nil {
				return err
			}
		}
		image, err = ownImage(ctx, tx, userID, current.id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return image.view(), nil
}

// deleteProfileImage removes an image; the newest of its kind becomes the
// default when it was one.
func (h *Handler) deleteProfileImage(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	var orphaned []string
	err := h.tx(ctx, func(tx *sql.Tx) error {
		image, err := ownImage(ctx, tx, userID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sign_profile_images WHERE id=?`, image.id); err != nil {
			return err
		}
		if image.isDefault {
			next, err := queryImages(ctx, tx, `WHERE user_id=? AND kind=? ORDER BY created_at DESC,id DESC LIMIT 1`, userID, image.kind)
			if err != nil {
				return err
			}
			if len(next) > 0 {
				if err := makeDefault(ctx, tx, userID, image.kind, next[0].id); err != nil {
					return err
				}
			}
		}
		orphaned, err = releaseBlobs(ctx, tx, image.blobID)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.removeBlobs(userID, orphaned)
	return map[string]bool{"success": true}, nil
}
