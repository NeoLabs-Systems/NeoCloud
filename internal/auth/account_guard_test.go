package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

func bearerJSON(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	// Each token gets its own rate limit budget.
	digest := passhash.DigestToken(token)
	request.RemoteAddr = fmt.Sprintf("10.0.%d.%d:1", digest[0], digest[1])
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	return w
}

// Only a browser session or the phone app manages the account: a desktop token
// or a WebDAV app password must not read, change or delete it.
func TestAccountRoutesAdmitOnlyBrowserAndPhoneTokens(t *testing.T) {
	runtime, handler := testAuthServer(t)
	tokens := map[string]string{"android": "nc_phone-token", "lite": "nc_lite-token", "linux": "nc_desktop-token", "webdav": "nc_dav-password"}
	for platform, token := range tokens {
		if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,?,?,?)`, platform, platform, passhash.DigestToken(token)); err != nil {
			t.Fatal(err)
		}
	}
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/account", ""},
		{http.MethodGet, "/api/account/usage", ""},
		{http.MethodGet, "/api/account/sessions", ""},
		{http.MethodDelete, "/api/account/sessions/1", ""},
		{http.MethodPut, "/api/account/display-name", `{"displayName":"x"}`},
		{http.MethodPut, "/api/account/password", `{"currentPassword":"x","newPassword":"y"}`},
		{http.MethodPut, "/api/account/email", `{"email":"x@example.test"}`},
		{http.MethodPost, "/api/account/email/resend", ""},
		{http.MethodPost, "/api/account/2fa/setup", `{}`},
		{http.MethodPost, "/api/account/2fa/disable", `{}`},
		{http.MethodPost, "/api/account/2fa/recovery-codes", `{}`},
		{http.MethodPost, "/api/account/export", `{}`},
		{http.MethodPost, "/api/account/delete", `{}`},
		{http.MethodGet, "/api/account/devices", ""},
		{http.MethodPost, "/api/account/devices", `{"name":"x"}`},
		{http.MethodPost, "/api/account/devices/pairing", `{}`},
		{http.MethodPost, "/api/account/devices/1/wipe", `{}`},
		{http.MethodDelete, "/api/account/devices/1", ""},
		{http.MethodGet, "/api/account/security-keys", ""},
		{http.MethodDelete, "/api/account/security-keys/1", ""},
		{http.MethodPost, "/api/account/qr-login/approve", `{}`},
		{http.MethodPost, "/api/account/devices/authorize/ABCD-EFGH", `{"approve":true}`},
	}
	for _, platform := range []string{"lite", "linux", "webdav"} {
		for _, route := range routes {
			response := bearerJSON(handler, route.method, route.path, tokens[platform], route.body)
			// A WebDAV app password is not an identity here at all.
			want := http.StatusForbidden
			if platform == "webdav" {
				want = http.StatusUnauthorized
			}
			if response.Code != want && response.Code != http.StatusTooManyRequests {
				t.Errorf("%s %s with %s token = %d: %s", route.method, route.path, platform, response.Code, response.Body.String())
			}
		}
	}
	for _, path := range []string{"/api/account", "/api/account/usage", "/api/account/sessions", "/api/account/devices", "/api/account/security-keys"} {
		if response := bearerJSON(handler, http.MethodGet, path, tokens["android"], ""); response.Code != http.StatusOK {
			t.Errorf("phone GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
	changed := bearerJSON(handler, http.MethodPut, "/api/account/password", tokens["android"], `{"currentPassword":"G7!mQv#2xL9rT4pZ","newPassword":"Granite-Lantern-58!river"}`)
	if changed.Code != http.StatusOK {
		t.Fatalf("phone password change = %d: %s", changed.Code, changed.Body.String())
	}
	var live string
	if err := runtime.DB.W.QueryRowContext(context.Background(), `SELECT group_concat(platform) FROM device_tokens WHERE revoked_at IS NULL`).Scan(&live); err != nil || live != "android" {
		t.Fatalf("devices left signed in = %q, %v; only the asking phone stays", live, err)
	}
}

// An account that never had to confirm an address (no mail server) must still
// be able to sign devices in.
func TestUnconfirmedAddressWithoutOwedConfirmationGetsDeviceTokens(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email_verified_at=NULL`); err != nil {
		t.Fatal(err)
	}
	created := serveJSON(t, handler, http.MethodPost, "/api/account/devices", map[string]string{"name": "Laptop", "platform": "linux"}, login.Result().Cookies()[0])
	if created.Code != http.StatusCreated {
		t.Fatalf("device token = %d: %s", created.Code, created.Body.String())
	}
}

// The export names preferences only: stored provider keys and the 2FA replay
// guard live in the same table and must not leave with it.
func TestAccountExportLeavesOutStoredSecrets(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(1,'theme','"dark"'),(1,'ai_provider:openai','"sealed-provider-secret"'),(1,'two_factor_last_step','12345678')`); err != nil {
		t.Fatal(err)
	}
	export := serveJSON(t, handler, http.MethodPost, "/api/account/export", map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ"}, login.Result().Cookies()[0])
	body := export.Body.String()
	if export.Code != http.StatusOK || !strings.Contains(body, `"kind":"neocloud-account"`) {
		t.Fatalf("export = %d: %s", export.Code, body)
	}
	if strings.Contains(body, "sealed-provider-secret") || strings.Contains(body, "two_factor_last_step") {
		t.Fatalf("export contains a stored secret: %s", body)
	}
}

func TestSignInFromSomewhereNewTellsTheOwner(t *testing.T) {
	notices := make(chan string, 4)
	_, handler := testAuthServerWithOptions(t, Options{SendNewSignInNotice: func(_ context.Context, username, email, ip, userAgent, _ string) error {
		notices <- username + " " + email + " " + ip + " " + userAgent
		return nil
	}})
	signIn := func(agent string) {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"cloud-user","password":"G7!mQv#2xL9rT4pZ"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", agent)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if w.Code != http.StatusOK {
			t.Fatalf("sign in = %d: %s", w.Code, w.Body.String())
		}
	}
	signIn("First browser")
	signIn("First browser")
	signIn("Second browser")
	signIn("Third browser")
	select {
	case got := <-notices:
		if got != "cloud-user cloud@example.test 192.0.2.1 Second browser" {
			t.Fatalf("notice = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notice for a sign-in from a new browser")
	}
	select {
	case got := <-notices:
		t.Fatalf("a second notice within the cooldown: %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// The reset page's one script must match the hash its policy allows, and the
// page carries the strength meter, show/hide and mismatch hint.
func TestResetPageScriptMatchesItsPolicy(t *testing.T) {
	handler := &Handler{brand: "NeoCloud"}
	w := httptest.NewRecorder()
	handler.writeResetPage(w, http.StatusOK, "de", "form", `tok"en<`, "")
	body := w.Body.String()
	_, rest, _ := strings.Cut(body, "<script>")
	script, _, found := strings.Cut(rest, "</script>")
	if !found || script != resetScript {
		t.Fatalf("emitted script differs from the hashed one: %q", script)
	}
	digest := sha256.Sum256([]byte(script))
	if want := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"; !strings.Contains(w.Header().Get("Content-Security-Policy"), want) {
		t.Fatalf("policy %q lacks %q", w.Header().Get("Content-Security-Policy"), want)
	}
	for _, want := range []string{`id="meter"`, `id="mismatch"`, `id="toggle"`, "Passwort ändern", `value="tok&#34;en&lt;"`} {
		if !strings.Contains(body, want) {
			t.Errorf("reset page lacks %q", want)
		}
	}
}
