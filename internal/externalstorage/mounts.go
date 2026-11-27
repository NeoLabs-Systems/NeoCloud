package externalstorage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// The connections of each account. The settings, secrets included, are one JSON
// document sealed with the account's data key and bound to the account and the
// mount, so a row cannot be moved to another.

const (
	maxMounts     = 10
	maxNameLength = 60
)

type mount struct {
	ID        string
	UserID    int64
	Name      string
	Provider  string
	ReadOnly  bool
	CreatedAt string
	Config    settings
	sealed    string
}

func (m mount) public() map[string]any {
	return map[string]any{
		"id": m.ID, "name": m.Name, "provider": m.Provider, "readOnly": m.ReadOnly,
		"config": publicConfig(m.Provider, m.Config), "createdAt": m.CreatedAt,
	}
}

const mountColumns = `SELECT id,user_id,name,provider,config_enc,read_only,created_at FROM external_mounts`

func scanMount(row interface{ Scan(...any) error }) (mount, error) {
	var m mount
	err := row.Scan(&m.ID, &m.UserID, &m.Name, &m.Provider, &m.sealed, &m.ReadOnly, &m.CreatedAt)
	return m, err
}

func mountAAD(userID int64, mountID string) []byte {
	return []byte(fmt.Sprintf("external-mount:%d:%s", userID, mountID))
}

func (h *Handler) seal(ctx context.Context, userID int64, mountID string, config settings) (string, error) {
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	sealed, err := cryptox.SealBytes(dek, plain, mountAAD(userID, mountID))
	return base64.StdEncoding.EncodeToString(sealed), err
}

func (h *Handler) open(ctx context.Context, m *mount) error {
	dek, err := h.keys.GetUserDEK(ctx, h.db, m.UserID)
	if err != nil {
		return err
	}
	sealed, err := base64.StdEncoding.DecodeString(m.sealed)
	if err != nil {
		return err
	}
	plain, err := cryptox.OpenBytes(dek, sealed, mountAAD(m.UserID, m.ID))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, &m.Config)
}

func (h *Handler) mount(ctx context.Context, userID int64, id string) (mount, error) {
	m, err := scanMount(h.db.QueryRowContext(ctx, mountColumns+` WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, apperr.New(http.StatusNotFound, "Cloud storage not found.", "NOT_FOUND")
	}
	if err != nil {
		return m, err
	}
	return m, h.open(ctx, &m)
}

func (h *Handler) mounts(ctx context.Context, userID int64) ([]mount, error) {
	rows, err := h.db.QueryContext(ctx, mountColumns+` WHERE user_id=? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []mount{}
	for rows.Next() {
		m, err := scanMount(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Opening needs the database connection the rows hold.
	rows.Close()
	for i := range found {
		if err = h.open(ctx, &found[i]); err != nil {
			return nil, err
		}
	}
	return found, nil
}

func cleanName(value any) (string, error) {
	raw, _ := value.(string)
	name := strings.TrimSpace(norm.NFC.String(raw))
	if name == "" || utf8.RuneCountInString(name) > maxNameLength || strings.ContainsAny(name, `/\`) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", apperr.New(http.StatusBadRequest, "Give the connection a name of up to 60 characters, without slashes.", "NAME_INVALID")
	}
	return name, nil
}

// draft validates the fields of a new or edited connection before it is tested.
func draft(body map[string]any, previous *mount) (mount, error) {
	var d mount
	name := body["name"]
	if previous != nil {
		d = *previous
		if name == nil {
			name = previous.Name
		}
	} else {
		d.Provider, _ = body["provider"].(string)
	}
	if _, err := providerFor(d.Provider); err != nil {
		return d, err
	}
	var err error
	if d.Name, err = cleanName(name); err != nil {
		return d, err
	}
	switch {
	case previous == nil:
		d.Config, err = cleanConfig(d.Provider, body["config"], nil)
	case body["config"] != nil:
		d.Config, err = cleanConfig(d.Provider, body["config"], previous.Config)
	}
	if value, ok := body["readOnly"]; ok {
		d.ReadOnly = value == true
	}
	return d, err
}

func assertNameFree(ctx context.Context, tx *sql.Tx, userID int64, name, exceptID string) error {
	var clashes int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_mounts WHERE user_id=? AND name=? COLLATE NOCASE AND id!=?`, userID, name, exceptID).Scan(&clashes); err != nil {
		return err
	}
	if clashes > 0 {
		return apperr.New(http.StatusConflict, "You already have a connection with that name.", "NAME_EXISTS")
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func audit(ctx context.Context, db execer, action string, userID int64, detail map[string]string) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES(?,?,?,?)`, action, userID, userID, string(encoded))
	return err
}

// save writes a draft: a new connection when it has no ID yet.
func (h *Handler) save(ctx context.Context, userID int64, d mount) (mount, error) {
	action, isNew := "external_storage.update", d.ID == ""
	if isNew {
		action, d.ID = "external_storage.connect", uuid.NewString()
	}
	sealed, err := h.seal(ctx, userID, d.ID, d.Config)
	if err != nil {
		return d, err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	if isNew {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_mounts WHERE user_id=?`, userID).Scan(&count); err != nil {
			return d, err
		}
		if count >= maxMounts {
			return d, apperr.New(http.StatusConflict, fmt.Sprintf("You can connect up to %d cloud storages.", maxMounts), "EXTERNAL_MOUNT_LIMIT")
		}
	}
	if err = assertNameFree(ctx, tx, userID, d.Name, d.ID); err != nil {
		return d, err
	}
	if isNew {
		_, err = tx.ExecContext(ctx, `INSERT INTO external_mounts(id,user_id,name,provider,config_enc,read_only) VALUES(?,?,?,?,?,?)`, d.ID, userID, d.Name, d.Provider, sealed, d.ReadOnly)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE external_mounts SET name=?,config_enc=?,read_only=?,updated_at=datetime('now') WHERE id=? AND user_id=?`, d.Name, sealed, d.ReadOnly, d.ID, userID)
	}
	if err != nil {
		return d, err
	}
	if err = audit(ctx, tx, action, userID, map[string]string{"mountId": d.ID, "provider": d.Provider}); err != nil {
		return d, err
	}
	if err = tx.Commit(); err != nil {
		return d, err
	}
	return h.mount(ctx, userID, d.ID)
}

func (h *Handler) list(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	found, err := h.mounts(r.Context(), userID)
	if err != nil {
		return 0, nil, err
	}
	items := make([]map[string]any, 0, len(found))
	for _, m := range found {
		items = append(items, m.public())
	}
	return http.StatusOK, map[string]any{"items": items}, nil
}

// test tries the settings of the body; secrets left blank come from the
// connection named by its id.
func (h *Handler) test(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	var previous *mount
	if id, _ := body["id"].(string); id != "" {
		found, err := h.mount(r.Context(), userID, id)
		if err != nil {
			return 0, nil, err
		}
		previous = &found
	}
	if body["name"] == nil && previous == nil {
		body["name"] = "test"
	}
	d, err := draft(body, previous)
	if err != nil {
		return 0, nil, err
	}
	result, err := h.probe(r.Context(), userID, d)
	if err != nil {
		known := apijson.Public(err)
		if known == nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]any{"ok": false, "error": known.Message, "code": known.Code}, nil
	}
	return http.StatusOK, map[string]any{"ok": true, "items": result.items}, nil
}

// verify saves a connection only after it was opened and its root listed. The
// server's identity (SFTP host key) is remembered the first time it is seen.
func (h *Handler) verify(ctx context.Context, userID int64, d *mount) error {
	result, err := h.probe(ctx, userID, *d)
	if err != nil {
		if known := apijson.Public(err); known != nil {
			return apperr.New(http.StatusUnprocessableEntity, known.Message, "EXTERNAL_TEST_FAILED")
		}
		return err
	}
	if result.hostKey != "" && d.Config.text("hostKey") == "" {
		d.Config["hostKey"] = result.hostKey
	}
	return nil
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	d, err := draft(body, nil)
	if err != nil {
		return 0, nil, err
	}
	if err = h.verify(r.Context(), userID, &d); err != nil {
		return 0, nil, err
	}
	saved, err := h.save(r.Context(), userID, d)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, saved.public(), nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	previous, err := h.mount(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	d, err := draft(body, &previous)
	if err != nil {
		return 0, nil, err
	}
	if body["config"] != nil {
		if err = h.verify(r.Context(), userID, &d); err != nil {
			return 0, nil, err
		}
	}
	saved, err := h.save(r.Context(), userID, d)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, saved.public(), nil
}

func (h *Handler) disconnect(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	m, err := h.mount(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	if _, err = h.db.ExecContext(r.Context(), `DELETE FROM external_mounts WHERE id=? AND user_id=?`, m.ID, userID); err != nil {
		return 0, nil, err
	}
	err = audit(r.Context(), h.db, "external_storage.disconnect", userID, map[string]string{"mountId": m.ID, "provider": m.Provider})
	return http.StatusOK, map[string]bool{"success": true}, err
}
