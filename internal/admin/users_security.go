package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

func (h *Handler) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		Password json.RawMessage `json:"password"`
	}
	if !readActionBody(w, r, &body) {
		return
	}
	var password string
	if len(body.Password) == 0 || json.Unmarshal(body.Password, &password) != nil || password == "" {
		writeError(w, http.StatusBadRequest, "A new password is required.", "MISSING_FIELDS")
		return
	}
	var username string
	var email sql.NullString
	err := h.db.QueryRowContext(r.Context(), `SELECT username,email FROM users WHERE id=?`, userID).Scan(&username, &email)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	status, message, code, valid := requestauth.ValidateNewPassword(username, email.String, password)
	if !valid {
		writeError(w, status, message, code)
		return
	}
	hash, err := passhash.Hash(password, passhash.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not reset user password", "INTERNAL_ERROR")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not reset user password", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var result sql.Result
	if result, err = tx.ExecContext(r.Context(), `UPDATE users SET password=? WHERE id=?`, hash, userID); err == nil {
		var changed int64
		changed, err = result.RowsAffected()
		if err == nil && changed != 1 {
			err = sql.ErrNoRows
		}
	}
	if err == nil {
		err = requestauth.RevokeCredentials(r.Context(), tx, userID, "", "", 0)
		if err == nil {
			// The new password starts with a clean slate of failed attempts.
			uid := strconv.FormatInt(userID, 10)
			_, err = tx.ExecContext(r.Context(), `DELETE FROM login_failures WHERE account_key IN (?, ?)`, "user:"+uid, "confirm:user:"+uid)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.password_reset',?,?, '{}')`, actorID, userID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not reset user password", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not reset user password", "INTERNAL_ERROR")
		return
	}
	h.cancelRevokedDeviceHandoffs(r.Context(), userID, 0)
	// An administrator may reset their own password. Do not let rolling session
	// middleware write that just-revoked browser session back after this reply.
	if actorID == userID {
		if session := sessionx.FromContext(r.Context()); session != nil {
			session.Destroy()
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	if _, err := h.userByID(r, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		}
		return
	}
	keepSID, keepHash, keepDeviceID := "", "", int64(0)
	if userID == actorID {
		if identity, identified := requestauth.IdentityFromContext(r.Context()); identified && identity.IsDevice {
			keepDeviceID = identity.DeviceID
		} else if session := sessionx.FromContext(r.Context()); session != nil && h.sessionSecret != "" {
			keepSID = session.ID()
			keepHash = sessionx.HashID(h.sessionSecret, keepSID)
		}
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign out user", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var sessions, devices int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM user_sessions WHERE user_id=? AND revoked_at IS NULL AND (?='' OR session_hash<>?)`, userID, keepHash, keepHash).Scan(&sessions); err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM device_tokens WHERE user_id=? AND revoked_at IS NULL AND id IS NOT ?`, userID, nullableID(keepDeviceID)).Scan(&devices)
	}
	if err == nil {
		err = requestauth.RevokeCredentials(r.Context(), tx, userID, keepSID, keepHash, keepDeviceID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.sign_out_everywhere',?,?, '{}')`, actorID, userID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign out user", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign out user", "INTERNAL_ERROR")
		return
	}
	h.cancelRevokedDeviceHandoffs(r.Context(), userID, keepDeviceID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "sessions": sessions, "devices": devices})
}

func (h *Handler) userByID(r *http.Request, userID int64) (bool, error) {
	var found int
	err := h.db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id=?`, userID).Scan(&found)
	return found == 1, err
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
