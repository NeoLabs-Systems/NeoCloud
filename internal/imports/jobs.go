package imports

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/google/uuid"
)

var defaultFolders = map[string]string{nextcloud: "Nextcloud", plainWebDAV: "WebDAV import"}

func vaultRefused() error {
	return apperr.New(http.StatusConflict, "Files can’t be imported into a vault.", "VAULT_BOUNDARY")
}

// resolveTarget is the folder given, or a reused folder in the root, so running
// the same import again lands on the same files.
func (h *Handler) resolveTarget(ctx context.Context, userID int64, source string, given any) (files.Node, error) {
	var folder files.Node
	var err error
	if given == nil || given == "" {
		folder, err = h.files.EnsureFolderPath(ctx, userID, "root", defaultFolders[source])
	} else {
		id, _ := given.(string)
		folder, err = h.files.OwnFolder(ctx, userID, id)
	}
	if err == nil && folder.VaultID != nil {
		err = vaultRefused()
	}
	return folder, err
}

func (h *Handler) answer(ctx context.Context, userID int64, id string, status int) (int, any, error) {
	j, err := h.owned(ctx, userID, id)
	if err != nil {
		return 0, nil, err
	}
	value, err := h.serialize(ctx, j)
	return status, value, err
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	c, err := parseConnection(body)
	if err != nil {
		return 0, nil, err
	}
	includeFavorites := true
	if given, ok := body["includeFavorites"]; ok {
		if includeFavorites, ok = given.(bool); !ok {
			return 0, nil, invalid("includeFavorites must be true or false.", "INVALID_PARAM")
		}
	}
	target, err := h.resolveTarget(r.Context(), userID, c.source, body["targetFolderId"])
	if err != nil {
		return 0, nil, err
	}
	password, err := h.keys.EncryptValue(c.password)
	if err != nil {
		return 0, nil, err
	}
	id := uuid.NewString()
	if _, err = h.db.ExecContext(r.Context(), `INSERT INTO import_jobs(id,user_id,source,url,username,password_enc,source_path,target_folder_id,include_favorites) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, userID, c.source, c.url, c.username, password, c.sourcePath, target.ID, includeFavorites); err != nil {
		return 0, nil, err
	}
	h.wake()
	return h.answer(r.Context(), userID, id, http.StatusCreated)
}

// test signs in and lists the source folder once, writing nothing. files counts
// the entries directly in the folder; bytes is the folder's total size as the
// server reports it, else the sum of those entries.
func (h *Handler) test(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	c, err := parseConnection(body)
	if err != nil {
		return 0, nil, err
	}
	remote, err := h.clientFor(r.Context(), userID, c, 1)
	if err != nil {
		return 0, nil, err
	}
	defer remote.close()
	var size int64
	var children []remoteItem
	if err = remote.connect(r.Context()); err == nil {
		size, children, err = remote.list(r.Context(), "")
	}
	if err != nil {
		result := map[string]any{"ok": false, "files": 0, "bytes": 0, "error": "The server could not be read."}
		if known := apijson.Public(err); known != nil && known.Code != "" {
			result["error"], result["code"] = known.Message, known.Code
		}
		return http.StatusOK, result, nil
	}
	return http.StatusOK, map[string]any{"ok": true, "files": len(children), "bytes": size}, nil
}

func (h *Handler) list(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	rows, err := h.db.QueryContext(r.Context(), jobColumns+` WHERE user_id=? ORDER BY created_at DESC,rowid DESC`, userID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var jobs []job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return 0, nil, err
		}
		jobs = append(jobs, j)
	}
	if err = rows.Err(); err != nil {
		return 0, nil, err
	}
	// Serialising needs the database connection the rows hold.
	rows.Close()
	items := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		value, err := h.serialize(r.Context(), j)
		if err != nil {
			return 0, nil, err
		}
		items = append(items, value)
	}
	return http.StatusOK, map[string]any{"items": items}, nil
}

func (h *Handler) get(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	return h.answer(r.Context(), userID, r.PathValue("id"), http.StatusOK)
}

func invalidState(message string) error {
	return apperr.New(http.StatusConflict, message, "IMPORT_STATE")
}

// end moves a job out of the queue and stops its downloads.
func (h *Handler) end(r *http.Request, userID int64, status, refusal string, from ...string) (int, any, error) {
	j, err := h.owned(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	changed, err := h.setStatus(r.Context(), j.id, status, from...)
	if err != nil {
		return 0, nil, err
	}
	if !changed {
		return 0, nil, invalidState(refusal)
	}
	h.interrupt(j.id)
	return h.answer(r.Context(), userID, j.id, http.StatusOK)
}

func (h *Handler) pause(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	return h.end(r, userID, paused, "Only a queued or running import can be paused.", queued, scanning, running)
}

func (h *Handler) cancel(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	return h.end(r, userID, cancelled, "This import has already ended.", queued, scanning, running, paused)
}

// resume continues a paused, failed or cancelled import. An ended one no longer
// holds the password, so it must come again. Failed files are tried once more;
// finished ones are not fetched again.
func (h *Handler) resume(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return 0, nil, err
	}
	j, err := h.owned(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	refused := invalidState("Only a paused, failed or cancelled import can be resumed.")
	if j.status != paused && j.status != failed && j.status != cancelled {
		return 0, nil, refused
	}
	var encrypted string
	if password, _ := body["password"].(string); password != "" {
		if encrypted, err = h.keys.EncryptValue(password); err != nil {
			return 0, nil, err
		}
	} else if j.password.String == "" {
		return 0, nil, passwordRequired()
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	if encrypted != "" {
		if _, err = tx.ExecContext(r.Context(), `UPDATE import_jobs SET password_enc=? WHERE id=?`, encrypted, j.id); err != nil {
			return 0, nil, err
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE import_items SET state='pending' WHERE job_id=? AND state='failed'`, j.id); err != nil {
		return 0, nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM import_errors WHERE job_id=?`, j.id); err != nil {
		return 0, nil, err
	}
	changed, err := setStatus(r.Context(), tx, j.id, queued, j.status)
	if err != nil {
		return 0, nil, err
	}
	if !changed {
		return 0, nil, refused
	}
	if err = tx.Commit(); err != nil {
		return 0, nil, err
	}
	h.Announce(userID, j.id, true)
	h.wake()
	return h.answer(r.Context(), userID, j.id, http.StatusOK)
}

func passwordRequired() error {
	return invalid("Enter the app password again to continue.", "PASSWORD_REQUIRED")
}

func (h *Handler) delete(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	j, err := h.owned(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	result, err := h.db.ExecContext(r.Context(), `DELETE FROM import_jobs WHERE id=? AND status IN ('done','failed','cancelled')`, j.id)
	if err != nil {
		return 0, nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, nil, err
	}
	if count == 0 {
		return 0, nil, invalidState("Cancel the import before deleting it.")
	}
	return http.StatusOK, map[string]bool{"success": true}, nil
}
