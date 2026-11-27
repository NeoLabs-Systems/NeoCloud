package auth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

const pendingWebAuthnRegistration = "pendingWebAuthnRegistration"

type webAuthnStore struct{ db *sql.DB }
type webAuthnAccounts struct{ db *sql.DB }

func newWebAuthnService(db *sql.DB, allowedOrigins []string, trustProxy bool) *webauthnx.Service {
	return &webauthnx.Service{RPName: "NeoCloud", Origins: httpx.ParseOrigins(strings.Join(allowedOrigins, ",")), TrustProxy: trustProxy, Store: webAuthnStore{db}, Accounts: webAuthnAccounts{db}}
}

const credentialSelect = `SELECT id,user_id,credential_id,public_key,counter,rp_id,transports_json,COALESCE(device_type,''),backed_up,user_verified,label,created_at,COALESCE(last_used_at,'') FROM user_webauthn_credentials`

func (s webAuthnStore) ListForUser(ctx context.Context, userID string) ([]webauthnx.Credential, error) {
	return s.list(ctx, credentialSelect+` WHERE user_id=? ORDER BY created_at DESC,id DESC`, userID)
}
func (s webAuthnStore) ForUserAndRP(ctx context.Context, userID, rpID string) ([]webauthnx.Credential, error) {
	return s.list(ctx, credentialSelect+` WHERE user_id=? AND rp_id=? ORDER BY created_at DESC,id DESC`, userID, rpID)
}
func (s webAuthnStore) list(ctx context.Context, query string, args ...any) ([]webauthnx.Credential, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]webauthnx.Credential, 0)
	for rows.Next() {
		var c webauthnx.Credential
		var userID, rawTransports string
		var backed, verified int
		if err = rows.Scan(&c.RowID, &userID, &c.CredentialID, &c.PublicKey, &c.Counter, &c.RPID, &rawTransports, &c.DeviceType, &backed, &verified, &c.Label, &c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		c.UserID, c.BackedUp, c.UserVerified = userID, backed == 1, verified == 1
		if json.Unmarshal([]byte(rawTransports), &c.Transports) != nil || c.Transports == nil {
			c.Transports = []string{}
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (s webAuthnStore) CountForRP(ctx context.Context, rp string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_webauthn_credentials WHERE rp_id=?`, rp).Scan(&n)
	return n, err
}
func (s webAuthnStore) FindByCredentialID(ctx context.Context, credential, rp string) (*webauthnx.Credential, error) {
	items, err := s.list(ctx, credentialSelect+` WHERE credential_id=? AND rp_id=? LIMIT 1`, credential, rp)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}
func (s webAuthnStore) Exists(ctx context.Context, credential string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_webauthn_credentials WHERE credential_id=?)`, credential).Scan(&n)
	return n != 0, err
}
func (s webAuthnStore) Insert(ctx context.Context, c webauthnx.Credential) error {
	transports, err := json.Marshal(c.Transports)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO user_webauthn_credentials(user_id,credential_id,public_key,counter,rp_id,transports_json,device_type,backed_up,user_verified,label) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.UserID, c.CredentialID, c.PublicKey, c.Counter, c.RPID, string(transports), c.DeviceType, boolInt(c.BackedUp), boolInt(c.UserVerified), c.Label)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]int64{"securityKeyId": id})
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.security_key_added',?,?,?)`, c.UserID, c.UserID, string(detail)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s webAuthnStore) UpdateAfterLogin(ctx context.Context, id int64, counter uint32, verified bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_webauthn_credentials SET counter=?,user_verified=?,last_used_at=datetime('now') WHERE id=?`, counter, boolInt(verified), id)
	return err
}
func (s webAuthnStore) Rename(ctx context.Context, userID string, id int64, label string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE user_webauthn_credentials SET label=? WHERE id=? AND user_id=?`, label, id, userID)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}
func (s webAuthnStore) Delete(ctx context.Context, userID string, id int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `DELETE FROM user_webauthn_credentials WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return false, err
	}
	detail, _ := json.Marshal(map[string]int64{"securityKeyId": id})
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.security_key_removed',?,?,?)`, uid, uid, string(detail)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
func (a webAuthnAccounts) ByID(ctx context.Context, id string) (webauthnx.Identity, bool, error) {
	var v webauthnx.Identity
	err := a.db.QueryRowContext(ctx, `SELECT id,username,COALESCE(display_name,''),COALESCE(email,'') FROM users WHERE id=?`, id).Scan(&v.ID, &v.Username, &v.DisplayName, &v.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}
func (a webAuthnAccounts) ForSignIn(ctx context.Context, name string) (webauthnx.Identity, bool, error) {
	var v webauthnx.Identity
	err := a.db.QueryRowContext(ctx, `SELECT id,username,COALESCE(display_name,''),COALESCE(email,'') FROM users WHERE lower(username)=lower(?) OR lower(email)=lower(?) ORDER BY lower(username)=lower(?) DESC,id LIMIT 1`, name, name, name).Scan(&v.ID, &v.Username, &v.DisplayName, &v.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}
func (h *Handler) webAuthnRequest(r *http.Request) *http.Request {
	if strings.TrimSpace(h.publicURL) == "" {
		return r
	}
	u, err := url.Parse(h.publicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return r
	}
	clone := r.Clone(r.Context())
	clone.Host = u.Host
	clone.Header = clone.Header.Clone()
	clone.Header.Del("Origin")
	if u.Scheme == "https" {
		clone.TLS = &tls.ConnectionState{}
	} else {
		clone.TLS = nil
	}
	return clone
}

func (h *Handler) listSecurityKeys(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	keys, err := h.webauthn.List(r.Context(), strconv.FormatInt(id.UserID, 10))
	if err != nil {
		writeError(w, 500, "Could not read security keys", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, 200, map[string]any{"securityKeys": keys})
}
func (h *Handler) securityKeyRegistrationOptions(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	if id.IsDevice {
		writeError(w, 403, "Add security keys and passkeys in a browser.", "BROWSER_REQUIRED")
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		Code            string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	u, err := h.loadUser(r.Context(), id.UserID)
	if err != nil {
		writeError(w, 500, "Could not verify identity", "INTERNAL_ERROR")
		return
	}
	if u == nil {
		writeError(w, 401, "Not authenticated", "UNAUTHORIZED")
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	options, pending, err := h.webauthn.BeginRegistration(r.Context(), h.webAuthnRequest(r), strconv.FormatInt(id.UserID, 10))
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	s := sessionx.FromContext(r.Context())
	if s == nil {
		writeError(w, 500, "Session middleware is not configured", "INTERNAL_ERROR")
		return
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		writeError(w, 500, "Could not start security key registration", "INTERNAL_ERROR")
		return
	}
	s.Set(pendingWebAuthnRegistration, string(encoded))
	writeJSON(w, 200, map[string]any{"options": options})
}
func pendingWebAuthn(s *sessionx.Session) *webauthnx.Pending {
	if s == nil {
		return nil
	}
	raw := s.String(pendingWebAuthnRegistration)
	s.Delete(pendingWebAuthnRegistration)
	var p webauthnx.Pending
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil {
		return nil
	}
	return &p
}
func (h *Handler) securityKeyRegistrationVerify(w http.ResponseWriter, r *http.Request) {
	id, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	if id.IsDevice {
		writeError(w, 403, "Add security keys and passkeys in a browser.", "BROWSER_REQUIRED")
		return
	}
	var body struct {
		Response json.RawMessage `json:"response"`
		Label    string          `json:"label"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.webauthn.CompleteRegistration(r.Context(), h.webAuthnRequest(r), strconv.FormatInt(id.UserID, 10), body.Response, body.Label, pendingWebAuthn(sessionx.FromContext(r.Context()))); err != nil {
		writeWebAuthnError(w, err)
		return
	}
	keys, err := h.webauthn.List(r.Context(), strconv.FormatInt(id.UserID, 10))
	if err != nil {
		writeError(w, 500, "Could not read security keys", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "credentials": keys})
}
func (h *Handler) securityKeyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "Invalid security key id.", "INVALID_VALUE")
		return 0, false
	}
	return id, true
}
func (h *Handler) renameSecurityKey(w http.ResponseWriter, r *http.Request) {
	i, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	id, ok := h.securityKeyID(w, r)
	if !ok {
		return
	}
	var b struct {
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	keys, err := h.webauthn.Rename(r.Context(), strconv.FormatInt(i.UserID, 10), id, b.Label)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "credentials": keys})
}
func (h *Handler) deleteSecurityKey(w http.ResponseWriter, r *http.Request) {
	i, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	id, ok := h.securityKeyID(w, r)
	if !ok {
		return
	}
	keys, err := h.webauthn.Delete(r.Context(), strconv.FormatInt(i.UserID, 10), id)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "credentials": keys})
}

type securityKeyChallenge struct {
	Stage    string
	Pending  *webauthnx.Pending
	UserID   int64
	Name     string
	Platform string
	Expires  time.Time
	Failures int
}

// origin is where people reach this server: PUBLIC_URL when configured, else
// the address the request came in on.
func (h *Handler) origin(r *http.Request) string {
	if h.publicOrigin != nil {
		return h.publicOrigin(r)
	}
	if origin, err := ParseOrigin(h.publicURL); err == nil {
		return origin
	}
	if h.requestSecure(r) {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func (h *Handler) requestForPublicOrigin(r *http.Request) *http.Request {
	u, err := url.Parse(h.origin(r))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return r
	}
	clone := r.Clone(r.Context())
	clone.Host = u.Host
	clone.Header = clone.Header.Clone()
	clone.Header.Set("Origin", u.Scheme+"://"+u.Host)
	if u.Scheme == "https" {
		clone.TLS = &tls.ConnectionState{}
	} else {
		clone.TLS = nil
	}
	return clone
}

func (h *Handler) securityKeySignInOptions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	options, pending, err := h.webauthn.BeginLogin(r.Context(), h.requestForPublicOrigin(r), body.Username)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		writeError(w, 500, "Could not start security key sign-in", "INTERNAL_ERROR")
		return
	}
	challengeID := base64.RawURLEncoding.EncodeToString(raw)
	now := h.now()
	if err := h.storeSecurityKeyChallenge(r.Context(), challengeID, securityKeyChallenge{Stage: "assertion", Pending: pending, Expires: now.Add(5 * time.Minute)}); err != nil {
		if errors.Is(err, errSecurityKeyChallengeLimit) {
			writeError(w, 429, "Too many security key sign-ins in progress, try again shortly.", "RATE_LIMITED")
		} else {
			writeError(w, 500, "Could not start security key sign-in", "INTERNAL_ERROR")
		}
		return
	}
	writeJSON(w, 200, map[string]any{"challengeId": challengeID, "options": options, "origin": h.origin(r)})
}

func (h *Handler) securityKeySignInVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChallengeID string          `json:"challengeId"`
		Response    json.RawMessage `json:"response"`
		Name        string          `json:"name"`
		Platform    string          `json:"platform"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name, platform, code := cleanAuthorizationDevice(body.Name, body.Platform)
	if code != "" {
		message := "Unknown platform."
		if code == "NAME_REQUIRED" {
			message = "Give the device a name."
		}
		writeError(w, 400, message, code)
		return
	}
	challenge, ok, challengeErr := h.takeSecurityKeyChallenge(r.Context(), body.ChallengeID, "assertion")
	if challengeErr != nil {
		writeError(w, 500, "Could not verify security key sign-in", "INTERNAL_ERROR")
		return
	}
	if !ok {
		writeError(w, 400, "Security key challenge expired, start again.", "SECURITY_KEY_CHALLENGE_EXPIRED")
		return
	}
	result, err := h.webauthn.CompleteLogin(r.Context(), h.requestForPublicOrigin(r), body.Response, challenge.Pending)
	if err != nil {
		writeWebAuthnError(w, err)
		return
	}
	userID, err := strconv.ParseInt(result.UserID, 10, 64)
	if err != nil {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	u, err := h.loadUser(r.Context(), userID)
	if err != nil || u == nil {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	if err = h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	var enabled int
	err = h.db.QueryRowContext(r.Context(), `SELECT enabled FROM user_two_factor WHERE user_id=?`, userID).Scan(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, 500, "Could not sign in", "INTERNAL_ERROR")
		return
	}
	if !result.UserVerified && enabled == 1 {
		challenge.Stage, challenge.UserID, challenge.Name, challenge.Platform = "second_factor", userID, name, platform
		challenge.Expires = h.now().Add(10 * time.Minute)
		if err := h.storeSecurityKeyChallenge(r.Context(), body.ChallengeID, challenge); err != nil {
			writeError(w, 500, "Could not continue security key sign-in", "INTERNAL_ERROR")
			return
		}
		writeJSON(w, 200, map[string]any{"requiresTwoFactor": true, "challengeId": body.ChallengeID})
		return
	}
	h.issueSecurityKeyDeviceToken(w, r, u, name, platform)
}

func (h *Handler) securityKeySignInTwoFactor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChallengeID string `json:"challengeId"`
		Code        string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Code) == "" {
		writeError(w, 400, "Two-factor code required", "MISSING_FIELDS")
		return
	}
	challenge, ok, challengeErr := h.takeSecurityKeyChallenge(r.Context(), body.ChallengeID, "second_factor")
	if challengeErr != nil {
		writeError(w, 500, "Could not verify code", "INTERNAL_ERROR")
		return
	}
	if !ok {
		writeError(w, 401, "Two-factor challenge expired", "TWO_FACTOR_EXPIRED")
		return
	}
	key := "user:" + strconv.FormatInt(challenge.UserID, 10)
	if err := h.lockout.Check(r.Context(), key); err != nil {
		writeLockout(w, err)
		return
	}
	if err := h.lockout.Record(r.Context(), key, "second_factor"); err != nil {
		writeError(w, 500, "Could not verify code", "INTERNAL_ERROR")
		return
	}
	valid, err := h.verifySecondFactor(r.Context(), challenge.UserID, body.Code)
	if err != nil {
		writeError(w, 500, "Could not verify code", "INTERNAL_ERROR")
		return
	}
	if !valid {
		challenge.Failures++
		if challenge.Failures < 5 {
			if err := h.storeSecurityKeyChallenge(r.Context(), body.ChallengeID, challenge); err != nil {
				writeError(w, 500, "Could not verify code", "INTERNAL_ERROR")
				return
			}
		}
		writeError(w, 401, "Invalid 2FA code", "INVALID_CODE")
		return
	}
	if err := h.lockout.Clear(r.Context(), key, "second_factor"); err != nil {
		writeError(w, 500, "Could not verify code", "INTERNAL_ERROR")
		return
	}
	u, err := h.loadUser(r.Context(), challenge.UserID)
	if err != nil || u == nil {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	h.issueSecurityKeyDeviceToken(w, r, u, challenge.Name, challenge.Platform)
}

func (h *Handler) issueSecurityKeyDeviceToken(w http.ResponseWriter, r *http.Request, u *user, name, platform string) {
	if err := h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	id, token, err := issueDeviceToken(r.Context(), h.db, u.ID, name, platform)
	if err != nil {
		writeError(w, 500, "Could not create device token", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "device": map[string]any{"id": id, "name": name, "platform": platform}, "user": map[string]any{"id": u.ID, "username": u.Username}})
}

func writeWebAuthnError(w http.ResponseWriter, err error) {
	var e *webauthnx.Error
	if !errors.As(err, &e) {
		writeError(w, 500, "Could not process security key request", "INTERNAL_ERROR")
		return
	}
	message := e.Message
	if e.Code == "NO_SECURITY_KEYS" {
		message = "No security keys are registered for this server yet. Add one under Settings › Security first."
	}
	writeError(w, e.Status, message, e.Code)
}
