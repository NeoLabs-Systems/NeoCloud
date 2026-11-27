package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// UserActionRoutes registers account-scoped administrative changes separately
// from the listing and quota API.
func (h *Handler) UserActionRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /api/admin/users/{id}/admin", h.settingsLimiter.Middleware(http.HandlerFunc(h.setUserAdmin)))
	mux.Handle("PUT /api/admin/users/{id}/disabled", h.settingsLimiter.Middleware(http.HandlerFunc(h.setUserDisabled)))
	mux.Handle("POST /api/admin/users/{id}/password", h.settingsLimiter.Middleware(http.HandlerFunc(h.resetUserPassword)))
	mux.Handle("POST /api/admin/users/{id}/email/verify", h.settingsLimiter.Middleware(http.HandlerFunc(h.verifyUserEmail)))
	mux.Handle("DELETE /api/admin/users/{id}/sessions", h.settingsLimiter.Middleware(http.HandlerFunc(h.revokeUserSessions)))
	mux.HandleFunc("GET /api/admin/users/{id}/allowed-models", h.getUserModelGrants)
	mux.Handle("PUT /api/admin/users/{id}/allowed-models", h.settingsLimiter.Middleware(http.HandlerFunc(h.setUserModelGrants)))
	mux.Handle("PUT /api/admin/users/{id}/ai-budget", h.settingsLimiter.Middleware(http.HandlerFunc(h.setUserAIBudget)))
	mux.Handle("DELETE /api/admin/users/{id}", h.settingsLimiter.Middleware(http.HandlerFunc(h.deleteUser)))
}

func (h *Handler) actionUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "Invalid user id", "INVALID_ID")
		return 0, false
	}
	return id, true
}

func readActionBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(dst) != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return false
	}
	return true
}

func strictBool(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func (h *Handler) writeUser(w http.ResponseWriter, r *http.Request, userID int64) {
	var u userRow
	err := h.db.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.display_name,u.email,u.email_verified_at,u.is_admin,u.disabled_at,u.used_bytes,u.quota_bytes,
		(SELECT COUNT(*) FROM nodes n WHERE n.user_id=u.id AND n.type='file'),u.ai_monthly_tokens,u.created_at,u.last_login,
		EXISTS(SELECT 1 FROM user_email_tokens t WHERE t.user_id=u.id AND t.type='signup_confirmation' AND t.consumed_at IS NULL)
		FROM users u WHERE u.id=?`, userID).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.DisabledAt, &u.UsedBytes, &u.QuotaBytes, &u.FileCount, &u.AIMonthlyTokens, &u.CreatedAt, &u.LastLogin, &u.EmailConfirmationPending)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read updated user", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.serializeUser(u))
}

func (h *Handler) setUserAdmin(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		IsAdmin json.RawMessage `json:"isAdmin"`
	}
	if !readActionBody(w, r, &body) {
		return
	}
	requested, valid := strictBool(body.IsAdmin)
	if !valid {
		writeError(w, http.StatusBadRequest, "isAdmin must be true or false.", "INVALID_VALUE")
		return
	}
	if userID == actorID && !requested {
		writeError(w, http.StatusBadRequest, "You can’t remove your own admin rights.", "SELF_DEMOTE")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update admin access", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=?`, userID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	if (current == 1) == requested {
		if err = tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not update admin access", "INTERNAL_ERROR")
			return
		}
		h.writeUser(w, r, userID)
		return
	}
	if !requested {
		var admins int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin=1`).Scan(&admins); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not check admin access", "INTERNAL_ERROR")
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusConflict, "The last admin can’t be removed.", "LAST_ADMIN")
			return
		}
	}
	detail, _ := json.Marshal(map[string]string{"source": "app"})
	action := "admin.grant"
	adminValue := 1
	if !requested {
		action = "admin.revoke"
		adminValue = 0
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET is_admin=? WHERE id=?`, adminValue, userID); err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES(?,?,?,?)`, action, actorID, userID, string(detail))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update admin access", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update admin access", "INTERNAL_ERROR")
		return
	}
	h.writeUser(w, r, userID)
}

func (h *Handler) setUserDisabled(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		Disabled json.RawMessage `json:"disabled"`
	}
	if !readActionBody(w, r, &body) {
		return
	}
	disable, valid := strictBool(body.Disabled)
	if !valid {
		writeError(w, http.StatusBadRequest, "disabled must be true or false.", "INVALID_VALUE")
		return
	}
	if disable && userID == actorID {
		writeError(w, http.StatusBadRequest, "You can’t disable your own account.", "CANNOT_DISABLE_SELF")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account access", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var admin int
	var disabled sql.NullString
	err = tx.QueryRowContext(r.Context(), `SELECT is_admin,disabled_at FROM users WHERE id=?`, userID).Scan(&admin, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	if disabled.Valid == disable {
		if err = tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not update account access", "INTERNAL_ERROR")
			return
		}
		h.writeUser(w, r, userID)
		return
	}
	if disable && admin == 1 {
		var activeAdmins int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin=1 AND disabled_at IS NULL AND id<>?`, userID).Scan(&activeAdmins); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not check admin access", "INTERNAL_ERROR")
			return
		}
		if activeAdmins == 0 {
			writeError(w, http.StatusConflict, "The last admin can’t be disabled.", "LAST_ADMIN")
			return
		}
	}
	action := "account.enable"
	if disable {
		action = "account.disable"
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET disabled_at=CASE WHEN ? THEN datetime('now') ELSE NULL END WHERE id=?`, disable, userID); err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES(?,?,?,'{}')`, action, actorID, userID)
	}
	if err == nil && disable {
		err = requestauth.RevokeCredentials(r.Context(), tx, userID, "", "", 0)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `DELETE FROM external_mounts WHERE user_id=?`, userID)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account access", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account access", "INTERNAL_ERROR")
		return
	}
	if disable {
		if h.stopAccount != nil {
			_ = h.stopAccount(r.Context(), userID)
		}
		h.cancelRevokedDeviceHandoffs(r.Context(), userID, 0)
	}
	h.writeUser(w, r, userID)
}
