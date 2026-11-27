package auth

import (
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/accountdelete"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

type deleteAccountRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
	ConfirmUsername string `json:"confirmUsername"`
}

// deleteAccountSelf mirrors the deliberate reauthentication and typed-name
// confirmation required by the account erasure flow.
func (h *Handler) deleteAccountSelf(w http.ResponseWriter, r *http.Request) {
	u, identity, ok := h.accountRequestUser(w, r)
	if !ok {
		return
	}
	if h.blobDir == "" || h.previewDir == "" || h.uploadTmpDir == "" {
		writeError(w, http.StatusInternalServerError, "Account storage is not configured", "INTERNAL_ERROR")
		return
	}
	var body deleteAccountRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(body.ConfirmUsername), u.Username) {
		writeError(w, http.StatusBadRequest, "Type your username to confirm.", "CONFIRMATION_MISMATCH")
		return
	}

	// The last admin is refused below, before anything of theirs is stopped.
	var lastAdmin bool
	if err := h.db.QueryRowContext(r.Context(), `SELECT is_admin=1 AND NOT EXISTS(SELECT 1 FROM users WHERE is_admin=1 AND disabled_at IS NULL AND id<>?1) FROM users WHERE id=?1`, u.ID).Scan(&lastAdmin); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not verify administrator status", "INTERNAL_ERROR")
		return
	}
	if h.stopAccount != nil && !lastAdmin {
		if err := h.stopAccount(r.Context(), u.ID); err != nil {
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
	var currentAdmin int
	if err = tx.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=? AND disabled_at IS NULL`, u.ID).Scan(&currentAdmin); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusUnauthorized, "Not authenticated", "UNAUTHORIZED")
		return
	}
	if currentAdmin == 1 {
		var others int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin=1 AND disabled_at IS NULL AND id<>?`, u.ID).Scan(&others); err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "Could not verify administrator status", "INTERNAL_ERROR")
			return
		}
		if others == 0 {
			_ = tx.Rollback()
			writeError(w, http.StatusConflict, "You are the only admin. Make someone else an admin first.", "LAST_ADMIN")
			return
		}
	}
	partners, err := accountdelete.SharePartners(r.Context(), tx, u.ID)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not prepare account deletion", "INTERNAL_ERROR")
		return
	}
	uploads, err := accountdelete.ListUploadIDs(r.Context(), tx, u.ID)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not prepare account deletion", "INTERNAL_ERROR")
		return
	}
	if err = accountdelete.DeleteRows(r.Context(), tx, u.ID, u.ID); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not delete account", "INTERNAL_ERROR")
		return
	}
	accountdelete.CleanupStorage(h.blobDir, h.previewDir, h.uploadTmpDir, u.ID, uploads)
	accountdelete.NotifySharePartners(h.notify, partners)
	if !identity.IsDevice {
		if session := sessionx.FromContext(r.Context()); session != nil {
			session.Destroy()
		}
		http.SetCookie(w, &http.Cookie{Name: "neocloud.sid", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.requestSecure(r), MaxAge: -1})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
