package files

import (
	"database/sql"
	"errors"
	"net/http"
)

func (h *Handler) uploadOwner(r *http.Request, viewer int64, id string) (int64, error) {
	var owner int64
	var creator sql.NullInt64
	var parentID string
	err := h.db.QueryRowContext(r.Context(), `SELECT user_id,created_by,parent_id FROM upload_sessions WHERE id=? AND datetime(expires_at)>datetime('now')`, id).Scan(&owner, &creator, &parentID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner != viewer && (!creator.Valid || creator.Int64 != viewer) {
		return 0, apiError{404, "Upload not found or expired", "UPLOAD_NOT_FOUND"}
	}
	if err != nil {
		return 0, err
	}
	if owner == viewer {
		return viewer, nil
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, access, err := h.resolveReadNode(tx, viewer, parentID)
	if err != nil || access == nil || access.ownerID != owner || access.permission != "write" {
		return 0, apiError{404, "Upload not found or expired", "UPLOAD_NOT_FOUND"}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return owner, nil
}
