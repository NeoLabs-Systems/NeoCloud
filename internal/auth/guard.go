package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

// accountIdentity admits what may manage an account: a browser session or the
// phone app's token. Desktop and lite tokens and WebDAV app passwords never do.
func (h *Handler) accountIdentity(w http.ResponseWriter, r *http.Request) (RequestIdentity, bool) {
	u, identity, ok := h.accountRequestUser(w, r)
	if !ok {
		return RequestIdentity{}, false
	}
	identity.UserID = u.ID
	return identity, true
}

// accountUser is accountIdentity for handlers that need the account row.
func (h *Handler) accountUser(w http.ResponseWriter, r *http.Request) (*user, bool) {
	u, _, ok := h.accountRequestUser(w, r)
	return u, ok
}

func (h *Handler) accountRequestUser(w http.ResponseWriter, r *http.Request) (*user, RequestIdentity, bool) {
	identity, found := IdentityFromContext(r.Context())
	var u *user
	var err error
	switch {
	case found && identity.IsDevice && identity.Platform != "android" && identity.Platform != "ios":
		writeError(w, http.StatusForbidden, "Use a browser or the NeoCloud phone app for this.", "PHONE_REQUIRED")
		return nil, identity, false
	case found:
		u, err = h.loadUser(r.Context(), identity.UserID)
		if u != nil && u.DisabledAt.Valid {
			u = nil
		}
	case sessionx.FromContext(r.Context()) != nil:
		u, _, err = h.authenticatedUser(r)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return nil, identity, false
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return nil, identity, false
	}
	return u, identity, true
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// issueDeviceToken is the one way a bearer token comes to exist. The caller has
// checked assertSignInAllowed; the token is shown once and only its digest kept.
func issueDeviceToken(ctx context.Context, q execer, userID int64, name, platform string) (int64, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return 0, "", err
	}
	token := "nc_" + base64.RawURLEncoding.EncodeToString(raw)
	result, err := q.ExecContext(ctx, `INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, userID, name, platform, passhash.DigestToken(token))
	if err != nil {
		return 0, "", err
	}
	id, err := result.LastInsertId()
	return id, token, err
}

func (h *Handler) notify(userID int64, event string, data map[string]any) {
	if h.notifier != nil {
		h.notifier(userID, event, data)
	}
}

// RevokeCredentials ends everything that could still act for the account: other
// browser sessions, device tokens, WebDAV app passwords, sign-ins approved but
// not yet picked up, and connected apps.
func RevokeCredentials(ctx context.Context, tx *sql.Tx, userID int64, keepSID, keepHash string, keepDeviceID int64) error {
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM sessions WHERE CAST(json_extract(sess, '$.userId') AS TEXT) = CAST(? AS TEXT) AND sid != ?`, []any{userID, keepSID}},
		{`UPDATE user_sessions SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL AND session_hash != ?`, []any{userID, keepHash}},
		{`UPDATE device_tokens SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL AND id != ?`, []any{userID, keepDeviceID}},
		{`UPDATE device_authorizations SET status = 'denied' WHERE user_id = ? AND status = 'approved'`, []any{userID}},
		{`UPDATE user_qr_login_challenges SET status = 'declined' WHERE approved_by_user_id = ? AND status = 'approved'`, []any{userID}},
		{`UPDATE device_pairings SET expires_at = datetime('now', '-1 second') WHERE user_id = ? AND status = 'pending'`, []any{userID}},
		{`UPDATE oauth_access_tokens SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL`, []any{userID}},
		{`UPDATE oauth_refresh_tokens SET revoked_at = datetime('now') WHERE user_id = ? AND revoked_at IS NULL`, []any{userID}},
		{`UPDATE oauth_codes SET consumed_at = datetime('now') WHERE user_id = ? AND consumed_at IS NULL`, []any{userID}},
	} {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

// signInAccount is the account a sign-in is about to hand a token to, refused
// when it is disabled or still owes its sign-up confirmation.
func (h *Handler) signInAccount(w http.ResponseWriter, r *http.Request, q queryRower, userID int64) (*user, bool) {
	u := &user{ID: userID}
	err := q.QueryRowContext(r.Context(), `SELECT username,email,email_verified_at,disabled_at FROM users WHERE id=?`, userID).Scan(&u.Username, &u.Email, &u.EmailVerified, &u.DisabledAt)
	if err == nil {
		err = h.signInRefusal(r.Context(), q, u)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return nil, false
	}
	if err != nil {
		writeSignInError(w, err)
		return nil, false
	}
	return u, true
}
