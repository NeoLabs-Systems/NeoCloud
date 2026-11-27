package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func accountKey(u *user, typed string) string {
	if u != nil {
		return "user:" + strconv.FormatInt(u.ID, 10)
	}
	value := strings.ToLower(strings.TrimSpace(typed))
	if len(value) > 254 {
		value = value[:254]
	}
	return "name:" + value
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	u, secondFactor, err := h.verifyPassword(r.Context(), body.Username, body.Password)
	if err != nil {
		writeSignInError(w, err)
		return
	}
	if secondFactor {
		h.establishPendingTwoFactor(w, r, u)
		return
	}
	h.establishSession(w, r, u, accountKey(u, ""))
}

func (h *Handler) establishPendingTwoFactor(w http.ResponseWriter, r *http.Request, u *user) {
	if err := h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	s := sessionx.FromContext(r.Context())
	s.Regenerate()
	challenge, err := passhash.RandomToken(24)
	if err != nil {
		writeError(w, 500, "Could not start two-factor sign-in", "INTERNAL_ERROR")
		return
	}
	s.Set("pendingTwoFactorUserId", u.ID)
	s.Set("pendingTwoFactorStartedAt", h.now().UnixMilli())
	s.Set("pendingTwoFactorChallenge", challenge)
	writeJSON(w, http.StatusOK, map[string]any{"success": false, "requiresTwoFactor": true, "user": map[string]string{"username": u.Username}})
}

func (h *Handler) assertSignInAllowed(ctx context.Context, u *user) error {
	return h.signInRefusal(ctx, h.db, u)
}

// signInRefusal reads through q so that a sign-in holding a transaction does
// not wait for a second connection.
func (h *Handler) signInRefusal(ctx context.Context, q queryRower, u *user) error {
	if u.DisabledAt.Valid {
		return authError{status: 403, message: "This account is disabled.", code: "ACCOUNT_DISABLED"}
	}
	if h.emailSettings().SignupConfirmationRequired && u.Email.Valid && !u.EmailVerified.Valid {
		var pending int
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_email_tokens WHERE user_id = ?
			AND type = 'signup_confirmation' AND consumed_at IS NULL`, u.ID).Scan(&pending)
		if err != nil {
			return err
		}
		if pending > 0 {
			return authError{status: 403, message: "Confirm your email address before signing in.", code: "EMAIL_NOT_CONFIRMED", requiresEmailConfirmation: true}
		}
	}
	return nil
}

func (h *Handler) establishSession(w http.ResponseWriter, r *http.Request, u *user, key string) {
	if err := h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	s := sessionx.FromContext(r.Context())
	s.Regenerate()
	s.Set("userId", u.ID)
	s.Set("username", u.Username)
	h.noticeNewSignIn(r, u)
	hash := sessionx.HashID(h.secret, s.ID())
	if err := h.recordSession(r, u.ID, s); err != nil {
		s.Destroy()
		writeError(w, 500, "Could not establish session", "INTERNAL_ERROR")
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE users SET last_login = datetime('now') WHERE id = ?`, u.ID); err != nil {
		s.Destroy()
		_, _ = h.db.ExecContext(context.WithoutCancel(r.Context()), `UPDATE user_sessions SET revoked_at = datetime('now') WHERE session_hash = ?`, hash)
		writeError(w, 500, "Could not establish session", "INTERNAL_ERROR")
		return
	}
	if err := h.lockout.Clear(r.Context(), key, ""); err != nil {
		s.Destroy()
		_, _ = h.db.ExecContext(context.WithoutCancel(r.Context()), `UPDATE user_sessions SET revoked_at = datetime('now') WHERE session_hash = ?`, hash)
		writeError(w, 500, "Could not establish session", "INTERNAL_ERROR")
		return
	}
	value, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, 500, "Could not establish session", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "redirect": "/app", "user": value})
}

type twoFactorRequest struct {
	Code string `json:"code"`
}

func (h *Handler) loginTwoFactor(w http.ResponseWriter, r *http.Request) {
	var body twoFactorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	s := sessionx.FromContext(r.Context())
	if s == nil {
		writeError(w, 500, "Session middleware is not configured", "INTERNAL_ERROR")
		return
	}
	id, ok := s.Int64("pendingTwoFactorUserId")
	started, startedOK := s.Int64("pendingTwoFactorStartedAt")
	challenge := s.String("pendingTwoFactorChallenge")
	if !ok || !startedOK || challenge == "" || h.now().Sub(time.UnixMilli(started)) > twoFactorLifetime {
		writeError(w, 401, "Two-factor challenge expired", "TWO_FACTOR_EXPIRED")
		return
	}
	if strings.TrimSpace(body.Code) == "" {
		writeError(w, 400, "Two-factor code required", "MISSING_FIELDS")
		return
	}
	u, err := h.loadUser(r.Context(), id)
	if err != nil || u == nil {
		writeError(w, 401, "User not found", "UNAUTHORIZED")
		return
	}
	exhausted, err := h.VerifySignInCode(r.Context(), id, challenge, body.Code)
	if exhausted {
		clearPendingTwoFactor(s)
	}
	if err != nil {
		writeSignInError(w, err)
		return
	}
	clearPendingTwoFactor(s)
	h.establishSession(w, r, u, accountKey(u, ""))
}

func (h *Handler) verifySecondFactor(ctx context.Context, userID int64, value string) (bool, error) {
	var secret sql.NullString
	var enabled int
	err := h.db.QueryRowContext(ctx, `SELECT secret, enabled FROM user_two_factor WHERE user_id = ?`, userID).Scan(&secret, &enabled)
	if errors.Is(err, sql.ErrNoRows) || enabled != 1 || !secret.Valid {
		return err == nil || errors.Is(err, sql.ErrNoRows), err
	}
	plain, err := h.keys.DecryptValue(secret.String)
	if err != nil {
		return false, err
	}
	normalized := totp.NormalizeCode(value)
	now := h.now()
	var matchedStep int64 = -1
	for _, delta := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		at := now.Add(delta)
		code, err := totp.Code(plain, at)
		if err != nil {
			return false, err
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(normalized)) == 1 {
			matchedStep = at.Unix() / 30
		}
	}
	if matchedStep >= 0 {
		result, err := h.db.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value, updated_at)
			VALUES (?, 'two_factor_last_step', ?, datetime('now')) ON CONFLICT(user_id, key) DO UPDATE SET
			value = excluded.value, updated_at = datetime('now')
			WHERE CAST(user_settings.value AS INTEGER) < CAST(excluded.value AS INTEGER)`, userID, matchedStep)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		return changed == 1, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id, code_hash FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID)
	if err != nil {
		return false, err
	}
	type recovery struct {
		id   int64
		hash string
	}
	var candidates []recovery
	for rows.Next() {
		var item recovery
		if err := rows.Scan(&item.id, &item.hash); err != nil {
			rows.Close()
			return false, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	code := totp.NormalizeRecoveryCode(value)
	for _, item := range candidates {
		if passhash.Verify(item.hash, code) {
			result, err := h.db.ExecContext(ctx, `UPDATE user_recovery_codes SET used_at = datetime('now') WHERE id = ? AND user_id = ? AND used_at IS NULL`, item.id, userID)
			if err != nil {
				return false, err
			}
			changed, err := result.RowsAffected()
			return changed == 1, err
		}
	}
	return false, nil
}

func clearPendingTwoFactor(s *sessionx.Session) {
	s.Delete("pendingTwoFactorUserId")
	s.Delete("pendingTwoFactorStartedAt")
	s.Delete("pendingTwoFactorChallenge")
}

func (h *Handler) recordFailure(ctx context.Context, key, kind, challenge string) error {
	now := h.now().UnixMilli()
	if _, err := h.db.ExecContext(ctx, `DELETE FROM login_failures WHERE failed_at_ms <= ?`, now-loginWindow.Milliseconds()); err != nil {
		return err
	}
	_, err := h.db.ExecContext(ctx, `INSERT INTO login_failures (account_key, kind, challenge_id, failed_at_ms) VALUES (?, ?, ?, ?)`, key, kind, nullable(challenge), now)
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
