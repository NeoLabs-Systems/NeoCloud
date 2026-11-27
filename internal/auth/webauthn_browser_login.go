package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

const pendingWebAuthnLogin = "pendingWebAuthnLogin"

func (h *Handler) browserWebAuthnLoginOptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	session := sessionx.FromContext(r.Context())
	if session == nil {
		writeError(w, http.StatusInternalServerError, "Session middleware is not configured", "INTERNAL_ERROR")
		return
	}
	options, pending, err := h.webauthn.BeginLogin(r.Context(), h.webAuthnRequest(r), body.Username)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start security key sign-in", "INTERNAL_ERROR")
		return
	}
	session.Set(pendingWebAuthnLogin, string(encoded))
	writeJSON(w, http.StatusOK, map[string]any{"options": options})
}

func (h *Handler) browserWebAuthnLoginVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Response json.RawMessage `json:"response"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	session := sessionx.FromContext(r.Context())
	if session == nil {
		writeError(w, http.StatusInternalServerError, "Session middleware is not configured", "INTERNAL_ERROR")
		return
	}
	pending := takeBrowserWebAuthnPending(session)
	if pending == nil {
		writeError(w, http.StatusBadRequest, "Security key challenge expired, start again.", "SECURITY_KEY_CHALLENGE_EXPIRED")
		return
	}
	// Apply the same account lockout accounting as password sign-in before
	// verifying a known credential. Unknown credential IDs remain generic and
	// do not reveal an account or affect one account's lockout budget.
	var response struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body.Response, &response)
	var ownerID int64
	if response.ID != "" {
		err := h.db.QueryRowContext(r.Context(), `SELECT user_id FROM user_webauthn_credentials WHERE credential_id=? LIMIT 1`, response.ID).Scan(&ownerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "Could not sign in", "INTERNAL_ERROR")
			return
		}
	}
	key := ""
	if ownerID > 0 {
		key = "user:" + strconv.FormatInt(ownerID, 10)
		if err := h.lockout.Check(r.Context(), key); err != nil {
			writeLockout(w, err)
			return
		}
		if err := h.recordFailure(r.Context(), key, "password", ""); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not sign in", "INTERNAL_ERROR")
			return
		}
	}
	result, err := h.webauthn.CompleteLogin(r.Context(), h.webAuthnRequest(r), body.Response, pending)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	userID, err := strconv.ParseInt(result.UserID, 10, 64)
	if err != nil || userID < 1 {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return
	}
	u, err := h.loadUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign in", "INTERNAL_ERROR")
		return
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}
	if err := h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	if key == "" {
		key = accountKey(u, "")
	}
	if err := h.lockout.Clear(r.Context(), key, "password"); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign in", "INTERNAL_ERROR")
		return
	}
	if !result.UserVerified {
		var enabled int
		err = h.db.QueryRowContext(r.Context(), `SELECT enabled FROM user_two_factor WHERE user_id=?`, userID).Scan(&enabled)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "Could not sign in", "INTERNAL_ERROR")
			return
		}
		if enabled == 1 {
			h.establishPendingTwoFactor(w, r, u)
			return
		}
	}
	h.establishSession(w, r, u, key)
}

func takeBrowserWebAuthnPending(session *sessionx.Session) *webauthnx.Pending {
	raw := session.String(pendingWebAuthnLogin)
	session.Delete(pendingWebAuthnLogin)
	if raw == "" {
		return nil
	}
	var pending webauthnx.Pending
	if json.Unmarshal([]byte(raw), &pending) != nil || pending.RPID == "" || pending.ExpiresAt == 0 {
		return nil
	}
	return &pending
}
