package auth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// changePassword signs out every other session and device. The caller stays: a
// browser session gets a new id, a phone keeps its token.
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	u, identity, ok := h.accountRequestUser(w, r)
	if !ok {
		return
	}
	var body changePasswordRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "A new password is required.", "MISSING_FIELDS")
		return
	}
	if err := h.confirmPassword(r.Context(), u.ID, body.CurrentPassword); err != nil {
		writeConfirmError(w, err)
		return
	}
	if err := h.validatePassword(u.Username, u.Email.String, body.NewPassword); err != nil {
		writeAuthError(w, err)
		return
	}
	if body.NewPassword == body.CurrentPassword {
		writeError(w, http.StatusBadRequest, "Choose a password different from the current one.", "PASSWORD_UNCHANGED")
		return
	}
	hash, err := passhash.Hash(body.NewPassword, passhash.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not change password", "INTERNAL_ERROR")
		return
	}
	var session *sessionx.Session
	keepSID, keepHash := "", ""
	if !identity.IsDevice {
		session = sessionx.FromContext(r.Context())
		keepSID = session.ID()
		keepHash = sessionx.HashID(h.secret, keepSID)
	}
	if err := h.applyPasswordChange(r.Context(), u.ID, hash, keepSID, keepHash, identity.DeviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not change password", "INTERNAL_ERROR")
		return
	}
	h.cancelDeviceHandoffs(r.Context(), u.ID, 0, identity.DeviceID)
	if session != nil {
		session.Rotate()
		session.Set("userId", u.ID)
		session.Set("username", u.Username)
		if err := h.recordSession(r, u.ID, session); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not change password", "INTERNAL_ERROR")
			return
		}
		if _, err := h.db.ExecContext(r.Context(), `UPDATE user_sessions SET revoked_at = datetime('now') WHERE user_id = ? AND session_hash = ? AND revoked_at IS NULL`, u.ID, keepHash); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not change password", "INTERNAL_ERROR")
			return
		}
	}
	h.noticePasswordChanged(u.Username, u.Email.String, requestLanguage(r))
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) applyPasswordChange(ctx context.Context, userID int64, hash, keepSID, keepHash string, keepDeviceID int64) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password = ? WHERE id = ?`, hash, userID); err != nil {
		return err
	}
	if err := RevokeCredentials(ctx, tx, userID, keepSID, keepHash, keepDeviceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *Handler) noticePasswordChanged(username, email, language string) {
	if h.sendPasswordChangedNotice == nil {
		return
	}
	go func() {
		if err := h.sendPasswordChangedNotice(context.Background(), username, email, language); err != nil {
			// The credential change is complete; email delivery cannot undo it.
			slog.Warn("password change notice failed", "error", err)
		}
	}()
}
