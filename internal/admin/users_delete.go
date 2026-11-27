package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/accountdelete"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	if h.blobDir == "" || h.previewDir == "" || h.uploadTmpDir == "" {
		writeError(w, http.StatusInternalServerError, "Account storage is not configured", "INTERNAL_ERROR")
		return
	}
	var isAdminAccount bool
	if err := h.db.QueryRowContext(r.Context(), `SELECT is_admin=1 FROM users WHERE id=?`, userID).Scan(&isAdminAccount); err == nil && !isAdminAccount && h.stopAccount != nil {
		if err := h.stopAccount(r.Context(), userID); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
			return
		}
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var isAdmin int
	err = tx.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=?`, userID).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	if isAdmin == 1 {
		writeError(w, http.StatusForbidden, "Remove admin rights before deleting this account.", "ADMIN_ACCOUNT")
		return
	}
	partners, err := accountdelete.SharePartners(r.Context(), tx, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not prepare account deletion", "INTERNAL_ERROR")
		return
	}
	uploads, err := accountdelete.ListUploadIDs(r.Context(), tx, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not prepare account deletion", "INTERNAL_ERROR")
		return
	}
	if err = accountdelete.DeleteRows(r.Context(), tx, userID, actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
		return
	}
	// Disk cleanup is post-commit so a failed transaction never destroys data.
	accountdelete.CleanupStorage(h.blobDir, h.previewDir, h.uploadTmpDir, userID, uploads)
	accountdelete.NotifySharePartners(h.notify, partners)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
