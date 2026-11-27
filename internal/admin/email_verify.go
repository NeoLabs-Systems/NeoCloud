package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// verifyUserEmail lets an administrator vouch for an address when automated
// confirmation cannot be completed. Consume pending signup tokens in the
// same transaction so an old link cannot alter the account afterwards.
func (h *Handler) verifyUserEmail(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not verify email address", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var email sql.NullString
	err = tx.QueryRowContext(r.Context(), `SELECT email FROM users WHERE id=?`, userID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	if !email.Valid || email.String == "" {
		writeError(w, http.StatusConflict, "This account has no email address.", "EMAIL_REQUIRED")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET email_verified_at=COALESCE(email_verified_at,datetime('now')) WHERE id=?`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not verify email address", "INTERNAL_ERROR")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE user_email_tokens SET consumed_at=COALESCE(consumed_at,datetime('now')) WHERE user_id=? AND type='signup_confirmation' AND consumed_at IS NULL`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not verify email address", "INTERNAL_ERROR")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.email_verify',?,?, '{}')`, actorID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record email verification", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not verify email address", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
