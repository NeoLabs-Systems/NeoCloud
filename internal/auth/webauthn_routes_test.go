package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

func TestSecurityKeyRegistrationUsesReauthenticatedSingleUseSessionChallenge(t *testing.T) {
	_, handler := testAuthServer(t)
	unauthenticated := serveJSON(t, handler, http.MethodGet, "/api/account/security-keys", nil, nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated key list status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login=%d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	wrongPassword := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/options", map[string]string{"currentPassword": "wrong"}, cookie)
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status=%d body=%s", wrongPassword.Code, wrongPassword.Body.String())
	}
	cookie = wrongPassword.Result().Cookies()[0]
	options := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/options", map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ"}, cookie)
	if options.Code != http.StatusOK || !contains(options.Body.String(), `"options"`) || !contains(options.Body.String(), `"challenge"`) {
		t.Fatalf("registration options=%d %s", options.Code, options.Body.String())
	}
	cookie = options.Result().Cookies()[0]
	invalid := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/verify", map[string]any{"response": map[string]any{}}, cookie)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid registration status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	cookie = invalid.Result().Cookies()[0]
	reused := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/verify", map[string]any{"response": map[string]any{}}, cookie)
	if reused.Code != http.StatusBadRequest || !contains(reused.Body.String(), "challenge") {
		t.Fatalf("reused registration challenge=%d %s", reused.Code, reused.Body.String())
	}
	noKeys := serveJSON(t, handler, http.MethodPost, "/api/devices/webauthn/options", map[string]string{}, nil)
	if noKeys.Code != http.StatusConflict || !contains(noKeys.Body.String(), "NO_SECURITY_KEYS") {
		t.Fatalf("device security-key options without registered key=%d %s", noKeys.Code, noKeys.Body.String())
	}
}

func TestSecurityKeyManagementIsAccountScopedAndAudited(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	cookie := login.Result().Cookies()[0]
	if err := (webAuthnStore{db: runtime.DB.W}).Insert(context.Background(), webauthnx.Credential{UserID: "1", CredentialID: "fixture-credential", PublicKey: "fixture-key", RPID: "cloud.example.test", Label: "Fixture key"}); err != nil {
		t.Fatal(err)
	}
	list := serveJSON(t, handler, http.MethodGet, "/api/account/security-keys", nil, cookie)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"label":"Fixture key"`) {
		t.Fatalf("list keys=%d %s", list.Code, list.Body.String())
	}
	cookie = list.Result().Cookies()[0]
	rename := serveJSON(t, handler, http.MethodPut, "/api/account/security-keys/1", map[string]string{"label": "Renamed key"}, cookie)
	if rename.Code != http.StatusOK || !strings.Contains(rename.Body.String(), `"label":"Renamed key"`) {
		t.Fatalf("rename key=%d %s", rename.Code, rename.Body.String())
	}
	var otherUserID int64
	if err := runtime.DB.W.QueryRowContext(context.Background(), `INSERT INTO users(username,password) VALUES('other-user','hash') RETURNING id`).Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	other := httptest.NewRequest(http.MethodPut, "/api/account/security-keys/1", strings.NewReader(`{"label":"Changed by other"}`))
	other = other.WithContext(context.WithValue(other.Context(), identityKey{}, RequestIdentity{UserID: otherUserID}))
	otherResponse := httptest.NewRecorder()
	handler.ServeHTTP(otherResponse, other)
	if otherResponse.Code != http.StatusNotFound {
		t.Fatalf("other-account rename=%d %s", otherResponse.Code, otherResponse.Body.String())
	}
	deleted := serveJSON(t, handler, http.MethodDelete, "/api/account/security-keys/1", nil, cookie)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"credentials":[]`) {
		t.Fatalf("delete key=%d %s", deleted.Code, deleted.Body.String())
	}
	var added, removed int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.security_key_added'`).Scan(&added); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.security_key_removed'`).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if added != 1 || removed != 1 {
		t.Fatalf("key audit counts added=%d removed=%d, want one of each", added, removed)
	}
}

func TestBrowserSecurityKeyLoginUsesSingleUseSessionChallenge(t *testing.T) {
	runtime, handler := testAuthServerWithOptions(t, Options{AllowedOrigins: []string{"http://example.com"}})
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_webauthn_credentials
		(user_id,credential_id,public_key,counter,rp_id,transports_json,device_type,backed_up,user_verified,label)
		VALUES(1,'browser-key','not-a-valid-public-key',0,'example.com','[]','singleDevice',0,0,'Browser key')`); err != nil {
		t.Fatal(err)
	}
	options := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/options", map[string]string{}, nil)
	if options.Code != http.StatusOK || !contains(options.Body.String(), `"options"`) || contains(options.Body.String(), `"challengeId"`) {
		t.Fatalf("browser WebAuthn options=%d %s", options.Code, options.Body.String())
	}
	cookies := options.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("options did not issue the session cookie: %s", options.Body.String())
	}
	invalid := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/verify", map[string]any{"response": map[string]string{"id": "browser-key"}}, cookies[0])
	if invalid.Code != http.StatusBadRequest && invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid assertion status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	verifyCookies := invalid.Result().Cookies()
	if len(verifyCookies) == 0 {
		verifyCookies = cookies
	}
	replay := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/verify", map[string]any{"response": map[string]string{"id": "browser-key"}}, verifyCookies[0])
	if replay.Code != http.StatusBadRequest || !contains(replay.Body.String(), "SECURITY_KEY_CHALLENGE_EXPIRED") {
		t.Fatalf("replayed challenge status=%d body=%s", replay.Code, replay.Body.String())
	}
}
