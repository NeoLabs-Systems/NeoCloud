package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

func testAuthServer(t *testing.T) (*runtimeapp.Runtime, http.Handler) {
	t.Helper()
	// The settings package owns which rows are preferences; this stands in for it.
	return testAuthServerWithOptions(t, Options{ReadPreferences: func(context.Context, int64) (map[string]any, error) {
		return map[string]any{"theme": "dark"}, nil
	}})
}

func testAuthServerWithOptions(t *testing.T, options Options) (*runtimeapp.Runtime, http.Handler) {
	t.Helper()
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := runtimeapp.Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	hash, err := passhash.Hash("G7!mQv#2xL9rT4pZ", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES (?, ?, ?)`, "cloud-user", "cloud@example.test", hash); err != nil {
		t.Fatal(err)
	}
	options.DB, options.Keys, options.SessionSecret = runtime.DB.W, runtime.Keys, runtime.Config.SessionSecret
	if options.DefaultQuotaBytes == 0 {
		options.DefaultQuotaBytes = 10 << 30
	}
	options.SignupOpen = true
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Routes(mux)
	return runtime, runtime.SessionMiddleware(IdentityMiddleware(runtime.DB.W, mux))
}

func serveJSON(t *testing.T, handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func serveForm(t *testing.T, handler http.Handler, method, path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAnonymousPasswordRecoveryDoesNotRevealAccountOrDeliveryFailures(t *testing.T) {
	mailAttempts := make(chan string, 4)
	runtime, handler := testAuthServerWithOptions(t, Options{
		EmailStatus: EmailStatus{Enabled: true, Configured: true},
		SendPasswordReset: func(_ context.Context, username, email, token, language string) error {
			mailAttempts <- username + ":" + email
			return errors.New("mail transport unavailable")
		},
	})
	_ = runtime

	requests := []any{
		map[string]any{"account": "cloud-user"},
		map[string]any{"account": "nobody@example.test"},
		map[string]any{"account": map[string]string{"unexpected": "shape"}},
	}
	var baseline string
	for i, body := range requests {
		started := time.Now()
		response := serveJSON(t, handler, http.MethodPost, "/api/auth/password/forgot", body, nil)
		elapsed := time.Since(started)
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status = %d: %s", i, response.Code, response.Body.String())
		}
		if elapsed < anonymousMailMin-50*time.Millisecond {
			t.Errorf("request %d completed before anonymous latency floor: %v", i, elapsed)
		}
		if i == 0 {
			baseline = response.Body.String()
		} else if response.Body.String() != baseline {
			t.Errorf("request %d response differs from generic response: %s != %s", i, response.Body.String(), baseline)
		}
	}
	select {
	case attempted := <-mailAttempts:
		if attempted != "cloud-user:cloud@example.test" {
			t.Errorf("mail attempt used unexpected account: %q", attempted)
		}
	case <-time.After(time.Second):
		t.Fatal("known account did not trigger password reset delivery")
	}
	select {
	case extra := <-mailAttempts:
		t.Errorf("unexpected extra delivery attempt: %q", extra)
	default:
	}
}

func TestAnonymousPasswordRecoveryRateLimitAppliesAcrossConcurrentRequests(t *testing.T) {
	_, handler := testAuthServer(t)
	const count = 11 // the shared anonymous mail limiter permits ten per window
	responses := make([]*httptest.ResponseRecorder, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses[i] = serveJSON(t, handler, http.MethodPost, "/api/auth/password/forgot", map[string]string{"account": "nobody@example.test"}, nil)
		}(i)
	}
	wg.Wait()
	var success, limited int
	for _, response := range responses {
		switch response.Code {
		case http.StatusOK:
			success++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
		}
	}
	if success != 10 || limited != 1 {
		t.Fatalf("rate limit outcomes: success=%d limited=%d; want 10 and 1", success, limited)
	}
}

func TestLoginCreatesSessionAndMeReadsIt(t *testing.T) {
	_, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "CLOUD-USER", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status %d: %s", login.Code, login.Body.String())
	}
	var result struct {
		Success  bool   `json:"success"`
		Redirect string `json:"redirect"`
		User     struct {
			Username string `json:"username"`
			IsAdmin  bool   `json:"isAdmin"`
		} `json:"user"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Redirect != "/app" || result.User.Username != "cloud-user" || !result.User.IsAdmin {
		t.Fatalf("unexpected login result: %+v", result)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "neocloud.sid" {
		t.Fatalf("login cookies: %#v", cookies)
	}
	me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, cookies[0])
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"username":"cloud-user"`) {
		t.Fatalf("me response %d: %s", me.Code, me.Body.String())
	}
}

func TestDisabledBrowserSessionCannotFallBackToRawSessionIdentity(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status %d: %s", login.Code, login.Body.String())
	}
	if _, err := runtime.DB.W.Exec(`UPDATE users SET disabled_at=datetime('now') WHERE username='cloud-user'`); err != nil {
		t.Fatal(err)
	}
	response := serveJSON(t, handler, http.MethodGet, "/api/account/devices", nil, login.Result().Cookies()[0])
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled account request status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestStringValuedDisabledSessionIdentityIsRejected(t *testing.T) {
	runtime, _ := testAuthServer(t)
	if _, err := runtime.DB.W.Exec(`UPDATE users SET disabled_at=datetime('now') WHERE username='cloud-user'`); err != nil {
		t.Fatal(err)
	}
	identityCheck := IdentityMiddleware(runtime.DB.W, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := CurrentIdentity(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	handler := runtime.SessionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionx.FromContext(r.Context()).Set("userId", "1")
		identityCheck.ServeHTTP(w, r)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("string-valued disabled session status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAccountProfileUsageAndDisplayNameMatchClientContract(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (1, 'hashed-recovery')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_webauthn_credentials
		(user_id, credential_id, public_key, rp_id, transports_json, backed_up, label)
		VALUES (1, 'credential-id', 'public-key', 'cloud.example.test', '["internal"]', 1, 'MacBook')`); err != nil {
		t.Fatal(err)
	}
	profile := serveJSON(t, handler, http.MethodGet, "/api/account", nil, cookie)
	if profile.Code != http.StatusOK || !strings.Contains(profile.Body.String(), `"recoveryCodesRemaining":1`) ||
		!strings.Contains(profile.Body.String(), `"transports":["internal"]`) || !strings.Contains(profile.Body.String(), `"backedUp":true`) {
		t.Fatalf("account profile = %d: %s", profile.Code, profile.Body.String())
	}
	if strings.Contains(profile.Body.String(), "public-key") || strings.Contains(profile.Body.String(), "hashed-recovery") {
		t.Fatal("account profile exposed credential material")
	}
	usage := serveJSON(t, handler, http.MethodGet, "/api/account/usage", nil, cookie)
	if usage.Code != http.StatusOK || !strings.Contains(usage.Body.String(), `"quotaBytes":10737418240`) || !strings.Contains(usage.Body.String(), `"customQuota":false`) {
		t.Fatalf("account usage = %d: %s", usage.Code, usage.Body.String())
	}
	updated := serveJSON(t, handler, http.MethodPut, "/api/account/display-name", map[string]string{"displayName": "  Neo  "}, cookie)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"displayName":"Neo"`) {
		t.Fatalf("display name update = %d: %s", updated.Code, updated.Body.String())
	}
	if unauthorized := serveJSON(t, handler, http.MethodGet, "/api/account", nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous account read = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
}

func TestAccountExportRequiresReauthenticationAndOmitsSecrets(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_settings (user_id, key, value) VALUES (1, 'theme', '"dark"')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO nodes (id, user_id, type, name, size, etag) VALUES ('export-file', 1, 'file', 'notes.txt', 10, 'etag')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username,email,password,display_name) VALUES ('export-collaborator','collaborator@example.test','hash','Collaborator')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO nodes (id,user_id,type,name,size,etag) VALUES ('foreign-export-file',2,'file','private-name.txt',4,'foreign-etag')`); err != nil {
		t.Fatal(err)
	}
	for _, comment := range []struct {
		nodeID string
		userID int64
		body   string
	}{
		{nodeID: "export-file", userID: 2, body: "comment on my file"},
		{nodeID: "foreign-export-file", userID: 1, body: "my comment on shared file"},
	} {
		sealed, err := runtime.Keys.EncryptValue(comment.body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.DB.W.Exec(`INSERT INTO node_comments(node_id,user_id,body) VALUES(?,?,?)`, comment.nodeID, comment.userID, sealed); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO activity(owner_id,actor_id,kind,node_id,node_type,name,detail,count) VALUES(1,2,'rename','export-file','file','notes.txt','{"oldName":"before.txt"}',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_two_factor(user_id,enabled,enabled_at) VALUES(1,0,'2026-10-05 12:00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO shares (user_id, node_id, token, password_hash) VALUES (1, 'export-file', 'private-token', 'private-hash')`); err != nil {
		t.Fatal(err)
	}
	dek, err := runtime.Keys.GetUserDEK(context.Background(), runtime.DB.W, 1)
	if err != nil {
		t.Fatal(err)
	}
	sealedMount, err := cryptox.SealBytes(dek, []byte(`{"url":"https://files.example.test/dav","username":"neo","password":"remote-secret"}`), []byte("external-mount:1:export-mount"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO external_mounts (id,user_id,name,provider,config_enc) VALUES ('export-mount',1,'Work drive','webdav',?)`, base64.StdEncoding.EncodeToString(sealedMount)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO import_jobs (id,user_id,source,url,username,password_enc,target_folder_id,status) VALUES ('export-import',1,'webdav','https://files.example.test/dav','neo','sealed-import-credential','export-file','paused')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO access_audit_log (action,actor_user_id,subject_user_id,detail_json) VALUES ('account.export-test',1,1,'{"source":"test"}')`); err != nil {
		t.Fatal(err)
	}
	bad := serveJSON(t, handler, http.MethodPost, "/api/account/export", map[string]string{"currentPassword": "wrong"}, cookie)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("export with bad password = %d: %s", bad.Code, bad.Body.String())
	}
	response := serveJSON(t, handler, http.MethodPost, "/api/account/export", map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ"}, cookie)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{`"kind":"neocloud-account"`, `"theme":"dark"`, `"notes.txt"`, `"hasPassword":true`, `"versions":[]`, `"externalStorage":[{`, `"id":"export-mount"`, `"config":{"passwordSet":true,"url":"https://files.example.test/dav","username":"neo"}`, `"imports":[{`, `"id":"export-import"`, `"audit":[{`, `"detail":{"source":"test"}`, `"signing":[]`, `"signingProfile":{"autoFill":true,"autoSave":true`} {
		if !strings.Contains(body, expected) {
			t.Errorf("export missing %s: %s", expected, body)
		}
	}
	if strings.Contains(body, "remote-secret") {
		t.Fatal("export disclosed an external storage credential")
	}
	var exported map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	profile := exported["profile"].(map[string]any)
	if _, found := profile["emailVerifiedAt"]; found {
		t.Errorf("profile export includes verification metadata outside the Node contract: %#v", profile)
	}
	if _, found := profile["pendingEmail"]; found {
		t.Errorf("profile export includes pending-address metadata outside the Node contract: %#v", profile)
	}
	security := exported["security"].(map[string]any)
	twoFactor := security["twoFactor"].(map[string]any)
	if twoFactor["enabled"] != false || twoFactor["enabledAt"] != "2026-10-05 12:00:00" {
		t.Errorf("two-factor export = %#v", twoFactor)
	}
	activity := exported["activity"].(map[string]any)
	entries := activity["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("activity entries = %#v", entries)
	}
	entry := entries[0].(map[string]any)
	if entry["actor"] != "Collaborator" || entry["byMe"] != false || entry["oldName"] != "before.txt" {
		t.Errorf("activity entry does not match portable export contract: %#v", entry)
	}
	comments := activity["comments"].([]any)
	if len(comments) != 2 {
		t.Fatalf("exported comments = %#v", comments)
	}
	foundOwnFileComment, foundForeignFileComment := false, false
	for _, raw := range comments {
		comment := raw.(map[string]any)
		if comment["nodeId"] == "export-file" {
			foundOwnFileComment = comment["body"] == "comment on my file" && comment["nodeName"] == "notes.txt" && comment["onMyFile"] == true && comment["byMe"] == false
		}
		if comment["nodeId"] == "foreign-export-file" {
			foundForeignFileComment = comment["body"] == "my comment on shared file" && comment["nodeName"] == nil && comment["onMyFile"] == false && comment["byMe"] == true
		}
	}
	if !foundOwnFileComment || !foundForeignFileComment {
		t.Errorf("comments missing or expose foreign file metadata: %#v", comments)
	}
	for _, secret := range []string{"private-token", "private-hash", "credential-id", "public-key", "wrapped_key", "password\":", "sealed-remote-credential", "sealed-import-credential"} {
		if strings.Contains(body, secret) {
			t.Errorf("export leaked %q: %s", secret, body)
		}
	}
}

func TestPublicExternalStorageConfigPreservesSettingsAndRedactsCredentials(t *testing.T) {
	tests := []struct {
		provider string
		config   map[string]any
		want     map[string]any
		secret   []string
	}{
		{"s3", map[string]any{"endpoint": "https://s3.example.test", "bucket": "archive", "accessKeyId": "public-id", "secretAccessKey": "secret", "forcePathStyle": true}, map[string]any{"endpoint": "https://s3.example.test", "bucket": "archive", "accessKeyId": "public-id", "secretAccessKeySet": true, "forcePathStyle": true}, []string{"secretAccessKey"}},
		{"sftp", map[string]any{"host": "files.example.test", "username": "neo", "privateKey": "private", "passphrase": "secret", "hostKey": "sha256-private"}, map[string]any{"host": "files.example.test", "username": "neo", "privateKeySet": true, "passphraseSet": true}, []string{"privateKey", "passphrase", "hostKey"}},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			got, err := publicExternalStorageConfig(test.provider, test.config)
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range test.want {
				if got[key] != value {
					t.Errorf("config[%q]=%#v, want %#v", key, got[key], value)
				}
			}
			for _, key := range test.secret {
				if _, exists := got[key]; exists {
					t.Errorf("export disclosed private %q setting", key)
				}
			}
		})
	}
	if _, err := publicExternalStorageConfig("unknown", map[string]any{"credential": "secret"}); err == nil {
		t.Fatal("unknown provider was exported without a compatible public schema")
	}
}

func TestAccountSessionsCanListAndRevokeOtherSessionOnly(t *testing.T) {
	_, handler := testAuthServer(t)
	login := func() *http.Cookie {
		response := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
			"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
		}, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("login = %d: %s", response.Code, response.Body.String())
		}
		return response.Result().Cookies()[0]
	}
	current, other := login(), login()
	listed := serveJSON(t, handler, http.MethodGet, "/api/account/sessions", nil, current)
	if listed.Code != http.StatusOK || strings.Count(listed.Body.String(), `"id":`) != 2 || !strings.Contains(listed.Body.String(), `"current":true`) {
		t.Fatalf("sessions = %d: %s", listed.Code, listed.Body.String())
	}
	var result struct {
		Sessions []accountSession `json:"sessions"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var currentID, otherID int64
	for _, item := range result.Sessions {
		if item.Current {
			currentID = item.ID
		} else {
			otherID = item.ID
		}
	}
	if currentID == 0 || otherID == 0 {
		t.Fatalf("missing current or other session ids: %+v", result.Sessions)
	}
	refuseCurrent := httptest.NewRequest(http.MethodDelete, "/api/account/sessions/"+strconv.FormatInt(currentID, 10), nil)
	refuseCurrent.AddCookie(current)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, refuseCurrent)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), `"code":"CURRENT_SESSION"`) {
		t.Fatalf("current session revocation = %d: %s", refused.Code, refused.Body.String())
	}
	revoke := httptest.NewRequest(http.MethodDelete, "/api/account/sessions/"+strconv.FormatInt(otherID, 10), nil)
	revoke.AddCookie(current)
	revoked := httptest.NewRecorder()
	handler.ServeHTTP(revoked, revoke)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), `"success":true`) {
		t.Fatalf("revoke session = %d: %s", revoked.Code, revoked.Body.String())
	}
	if response := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, other); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie still works: %d %s", response.Code, response.Body.String())
	}
}

func TestAccountTwoFactorLifecycleAndRecoveryCodeRotation(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	setup := serveJSON(t, handler, http.MethodPost, "/api/account/2fa/setup", map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ"}, cookie)
	if setup.Code != http.StatusOK || !strings.Contains(setup.Body.String(), `"pending":true`) {
		t.Fatalf("2FA setup = %d: %s", setup.Code, setup.Body.String())
	}
	var setupResult struct {
		ManualKey string `json:"manualKey"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &setupResult); err != nil || setupResult.ManualKey == "" {
		t.Fatalf("manual TOTP key: %+v, %v", setupResult, err)
	}
	code, err := totp.Code(setupResult.ManualKey, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	enabled := serveJSON(t, handler, http.MethodPost, "/api/account/2fa/enable", map[string]string{"code": code}, cookie)
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"enabled":true`) || !strings.Contains(enabled.Body.String(), `"recoveryCodes":[`) {
		t.Fatalf("2FA enable = %d: %s", enabled.Code, enabled.Body.String())
	}
	var enabledResult struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := json.Unmarshal(enabled.Body.Bytes(), &enabledResult); err != nil || len(enabledResult.RecoveryCodes) != 10 {
		t.Fatalf("initial recovery codes: %d, %v", len(enabledResult.RecoveryCodes), err)
	}
	var storedHash string
	if err := runtime.DB.W.QueryRow(`SELECT code_hash FROM user_recovery_codes WHERE user_id = 1 ORDER BY id LIMIT 1`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == totp.NormalizeRecoveryCode(enabledResult.RecoveryCodes[0]) || !passhash.Verify(storedHash, totp.NormalizeRecoveryCode(enabledResult.RecoveryCodes[0])) {
		t.Fatal("recovery code was not stored as a compatible bcrypt hash")
	}
	rotated := serveJSON(t, handler, http.MethodPost, "/api/account/2fa/recovery-codes", map[string]string{
		"currentPassword": "G7!mQv#2xL9rT4pZ", "code": enabledResult.RecoveryCodes[0],
	}, cookie)
	if rotated.Code != http.StatusOK || !strings.Contains(rotated.Body.String(), `"recoveryCodes":[`) {
		t.Fatalf("recovery-code rotation = %d: %s", rotated.Code, rotated.Body.String())
	}
	var rotatedResult struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &rotatedResult); err != nil || len(rotatedResult.RecoveryCodes) != 10 {
		t.Fatalf("rotated recovery codes: %d, %v", len(rotatedResult.RecoveryCodes), err)
	}
	disabled := serveJSON(t, handler, http.MethodPost, "/api/account/2fa/disable", map[string]string{
		"currentPassword": "G7!mQv#2xL9rT4pZ", "code": rotatedResult.RecoveryCodes[0],
	}, cookie)
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"enabled":false`) {
		t.Fatalf("2FA disable = %d: %s", disabled.Code, disabled.Body.String())
	}
	var remaining int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = 1`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("recovery codes after disable = %d, %v", remaining, err)
	}
}

func TestEmailChangeWaitsForConfirmationAndResendInvalidatesOlderLink(t *testing.T) {
	var tokensSent []string
	runtime, handler := testAuthServerWithOptions(t, Options{
		EmailStatus:    EmailStatus{Enabled: true, Configured: true, EmailChangeConfirmationRequired: true},
		SignupTokenTTL: time.Hour,
		SendEmailConfirmation: func(_ context.Context, username, email, token, language string, changing bool) error {
			if username != "cloud-user" || email != "new@example.test" || language != "en" || !changing {
				t.Errorf("unexpected change confirmation: user=%q email=%q language=%q changing=%v", username, email, language, changing)
			}
			tokensSent = append(tokensSent, token)
			return nil
		},
	})
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	cookie := login.Result().Cookies()[0]
	changed := serveJSON(t, handler, http.MethodPut, "/api/account/email", map[string]string{
		"email": " NEW@example.test ", "currentPassword": "G7!mQv#2xL9rT4pZ",
	}, cookie)
	if changed.Code != http.StatusOK || !strings.Contains(changed.Body.String(), `"emailChangePending":true`) || !strings.Contains(changed.Body.String(), `"pendingEmail":"new@example.test"`) {
		t.Fatalf("email change request = %d: %s", changed.Code, changed.Body.String())
	}
	var email, pending string
	if err := runtime.DB.W.QueryRow(`SELECT email, (SELECT email FROM user_email_tokens WHERE user_id = users.id AND type = 'email_change_confirmation' AND consumed_at IS NULL ORDER BY id DESC LIMIT 1) FROM users WHERE id = 1`).Scan(&email, &pending); err != nil {
		t.Fatal(err)
	}
	if email != "cloud@example.test" || pending != "new@example.test" {
		t.Fatalf("request changed the address early: email=%q pending=%q", email, pending)
	}
	if len(tokensSent) != 1 {
		t.Fatalf("confirmation sends before resend = %d", len(tokensSent))
	}
	resend := serveJSON(t, handler, http.MethodPost, "/api/account/email/resend", map[string]string{}, cookie)
	if resend.Code != http.StatusOK || len(tokensSent) != 2 {
		t.Fatalf("resend = %d: %s (sends=%d)", resend.Code, resend.Body.String(), len(tokensSent))
	}
	oldLink := serveForm(t, handler, http.MethodPost, "/api/auth/email/confirm", url.Values{"token": {tokensSent[0]}}, nil)
	if oldLink.Code != http.StatusBadRequest {
		t.Fatalf("superseded link status = %d: %s", oldLink.Code, oldLink.Body.String())
	}
	preview := httptest.NewRequest(http.MethodGet, "/api/auth/email/confirm?token="+url.QueryEscape(tokensSent[1]), nil)
	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, preview)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("confirmation preview = %d", previewResponse.Code)
	}
	confirmed := serveForm(t, handler, http.MethodPost, "/api/auth/email/confirm", url.Values{"token": {tokensSent[1]}}, nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), "Your new NeoCloud email is confirmed") {
		t.Fatalf("confirm email change = %d: %s", confirmed.Code, confirmed.Body.String())
	}
	var verified sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT email, email_verified_at FROM users WHERE id = 1`).Scan(&email, &verified); err != nil || email != "new@example.test" || !verified.Valid {
		t.Fatalf("confirmed account email = %q verified=%v err=%v", email, verified, err)
	}
}

func TestEmailChangesImmediatelyWhenConfirmationMailIsNotConfigured(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	response := serveJSON(t, handler, http.MethodPut, "/api/account/email", map[string]string{
		"email": "new@example.test", "currentPassword": "G7!mQv#2xL9rT4pZ",
	}, login.Result().Cookies()[0])
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "emailChangePending") {
		t.Fatalf("immediate email change = %d: %s", response.Code, response.Body.String())
	}
	var email string
	var verified sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT email, email_verified_at FROM users WHERE id = 1`).Scan(&email, &verified); err != nil || email != "new@example.test" || verified.Valid {
		t.Fatalf("immediate update = %q verified=%v err=%v", email, verified, err)
	}
}

func TestPasswordChangeKeepsRotatedSessionAndRevokesEveryOtherCredential(t *testing.T) {
	runtime, handler := testAuthServer(t)
	oldPassword, newPassword := "G7!mQv#2xL9rT4pZ", "Granite-Lantern-58!river"
	login := func() *http.Cookie {
		response := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": oldPassword}, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("login = %d: %s", response.Code, response.Body.String())
		}
		return response.Result().Cookies()[0]
	}
	currentCookie, otherCookie := login(), login()
	if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens (user_id, name, platform, token_hash) VALUES (1, 'Desktop', 'linux', 'change-password-device')`); err != nil {
		t.Fatal(err)
	}
	response := serveJSON(t, handler, http.MethodPut, "/api/account/password", map[string]string{
		"currentPassword": oldPassword, "newPassword": newPassword,
	}, currentCookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatalf("password change = %d: %s", response.Code, response.Body.String())
	}
	rotated := response.Result().Cookies()
	if len(rotated) != 1 || rotated[0].Value == currentCookie.Value {
		t.Fatalf("current session cookie was not rotated: %#v", rotated)
	}
	if me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, rotated[0]); me.Code != http.StatusOK {
		t.Fatalf("rotated session stopped working: %d %s", me.Code, me.Body.String())
	}
	if me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, currentCookie); me.Code != http.StatusUnauthorized {
		t.Fatalf("old current cookie remains valid: %d %s", me.Code, me.Body.String())
	}
	if me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, otherCookie); me.Code != http.StatusUnauthorized {
		t.Fatalf("other session remains valid: %d %s", me.Code, me.Body.String())
	}
	var revoked sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT revoked_at FROM device_tokens WHERE token_hash = 'change-password-device'`).Scan(&revoked); err != nil || !revoked.Valid {
		t.Fatalf("device token not revoked: %v, %v", revoked, err)
	}
	oldLogin := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": oldPassword}, nil)
	newLogin := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": newPassword}, nil)
	if oldLogin.Code != http.StatusUnauthorized || newLogin.Code != http.StatusOK {
		t.Fatalf("post-change login statuses: old=%d new=%d", oldLogin.Code, newLogin.Code)
	}
}

func TestPasswordChangeWithTwoFactorAsksOnlyForThePassword(t *testing.T) {
	runtime, handler := testAuthServer(t)
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	encrypted, err := runtime.Keys.EncryptValue(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_two_factor (user_id, secret, enabled) VALUES (1, ?, 1)`, encrypted); err != nil {
		t.Fatal(err)
	}
	recovery := "ZXCVB23456"
	recoveryHash, err := passhash.Hash(recovery, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (1, ?)`, recoveryHash); err != nil {
		t.Fatal(err)
	}
	start := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"requiresTwoFactor":true`) {
		t.Fatalf("password step = %d: %s", start.Code, start.Body.String())
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	finish := serveJSON(t, handler, http.MethodPost, "/api/auth/login/2fa", map[string]string{"code": code}, start.Result().Cookies()[0])
	if finish.Code != http.StatusOK {
		t.Fatalf("two-factor sign-in = %d: %s", finish.Code, finish.Body.String())
	}
	cookie := finish.Result().Cookies()[0]
	// As the apps send it: the current password is the confirmation.
	password := map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ", "newPassword": "Granite-Lantern-58!river"}
	changed := serveJSON(t, handler, http.MethodPut, "/api/account/password", password, cookie)
	if changed.Code != http.StatusOK {
		t.Fatalf("password change with 2FA on = %d: %s", changed.Code, changed.Body.String())
	}
	var unused int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = 1 AND used_at IS NULL`).Scan(&unused); err != nil || unused != 1 {
		t.Fatalf("password change consumed a recovery code: unused=%d, %v", unused, err)
	}
}

func TestFirstAccountCanRegisterWhenLaterSignupIsClosed(t *testing.T) {
	runtime, handler := emptyAuthServer(t, Options{})
	response := serveJSON(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "first-cloud-user", "email": "first@example.test", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatalf("first account registration = %d: %s", response.Code, response.Body.String())
	}
	var admin int
	if err := runtime.DB.W.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, "first-cloud-user").Scan(&admin); err != nil || admin != 1 {
		t.Fatalf("first account admin flag %d, error %v", admin, err)
	}
}

func TestSignupConfirmationIsPersistedBeforeSending(t *testing.T) {
	runtime, handler := emptyAuthServer(t, Options{
		SignupOpen: true, RequireSignupEmailConfirmation: true,
		SendSignupConfirmation: func(_ context.Context, username, email, token, language string) error {
			if username != "second-cloud-user" || email != "second@example.test" || token == "" {
				t.Errorf("confirmation callback arguments: %q %q %q", username, email, token)
			}
			if language != "en" {
				t.Errorf("confirmation language = %q, want en", language)
			}
			return nil
		},
	})
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES ('existing-user', 'existing@example.test', 'hash')`); err != nil {
		t.Fatal(err)
	}
	response := serveJSON(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "second-cloud-user", "email": "second@example.test", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"requiresEmailConfirmation":true`) {
		t.Fatalf("email-confirmed registration = %d: %s", response.Code, response.Body.String())
	}
	var tokenHash, email string
	if err := runtime.DB.W.QueryRow(`SELECT token_hash, email FROM user_email_tokens WHERE user_id = 2`).Scan(&tokenHash, &email); err != nil {
		t.Fatal(err)
	}
	if tokenHash == "" || tokenHash == "G7!mQv#2xL9rT4pZ" || email != "second@example.test" {
		t.Fatalf("stored signup token/email is invalid: %q %q", tokenHash, email)
	}
}

func TestSignupMailFailureRemovesUnconfirmedAccount(t *testing.T) {
	runtime, handler := emptyAuthServer(t, Options{
		SignupOpen: true, RequireSignupEmailConfirmation: true,
		SendSignupConfirmation: func(context.Context, string, string, string, string) error { return errors.New("mail unavailable") },
	})
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES ('existing-user', 'existing@example.test', 'hash')`); err != nil {
		t.Fatal(err)
	}
	response := serveJSON(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "second-cloud-user", "email": "second@example.test", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("mail failure response = %d: %s", response.Code, response.Body.String())
	}
	var count int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'second-cloud-user'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unconfirmed account retained after send failure: count %d, error %v", count, err)
	}
}

func TestConfirmationGetDoesNotConsumeAndPostVerifiesAddress(t *testing.T) {
	runtime, _ := emptyAuthServer(t, Options{})
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES ('existing-user', 'existing@example.test', 'hash')`); err != nil {
		t.Fatal(err)
	}
	var token string
	identity, err := New(Options{
		DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret,
		SignupOpen: true, RequireSignupEmailConfirmation: true,
		SendSignupConfirmation: func(_ context.Context, _, _, sentToken, _ string) error { token = sentToken; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	identity.Routes(mux)
	handler := runtime.SessionMiddleware(mux)
	register := serveJSON(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "second-cloud-user", "email": "second@example.test", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if register.Code != http.StatusAccepted || token == "" {
		t.Fatalf("register = %d token=%q: %s", register.Code, token, register.Body.String())
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/auth/email/confirm?token="+url.QueryEscape(token), nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `name="token"`) {
		t.Fatalf("confirmation GET = %d: %s", get.Code, get.Body.String())
	}
	var consumed sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT consumed_at FROM user_email_tokens WHERE user_id = 2`).Scan(&consumed); err != nil || consumed.Valid {
		t.Fatalf("GET consumed confirmation token: %v, error %v", consumed, err)
	}
	form := url.Values{"token": []string{token}}
	postRequest := httptest.NewRequest(http.MethodPost, "/api/auth/email/confirm", strings.NewReader(form.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, postRequest)
	if post.Code != http.StatusOK || !strings.Contains(post.Body.String(), "Email confirmed") {
		t.Fatalf("confirmation POST = %d: %s", post.Code, post.Body.String())
	}
	var verified, consumedAt sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT email_verified_at FROM users WHERE id = 2`).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.W.QueryRow(`SELECT consumed_at FROM user_email_tokens WHERE user_id = 2`).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if !verified.Valid || !consumedAt.Valid {
		t.Fatalf("confirmation not applied: verified=%v consumed=%v", verified, consumedAt)
	}
}

func TestEmailConfirmationAcceptsJSONBody(t *testing.T) {
	var token string
	runtime, _ := emptyAuthServer(t, Options{})
	if _, err := runtime.DB.W.Exec(`INSERT INTO users(username,email,password) VALUES('existing-user','existing@example.test','hash')`); err != nil {
		t.Fatal(err)
	}
	identity, err := New(Options{
		DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret,
		SignupOpen: true, RequireSignupEmailConfirmation: true,
		SendSignupConfirmation: func(_ context.Context, _, _, value, _ string) error { token = value; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	identity.Routes(mux)
	handler := runtime.SessionMiddleware(mux)
	registered := serveJSON(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "json-confirm-user", "email": "json-confirm@example.test", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if registered.Code != http.StatusAccepted || token == "" {
		t.Fatalf("register=%d token=%q body=%s", registered.Code, token, registered.Body.String())
	}
	confirmed := serveJSON(t, handler, http.MethodPost, "/api/auth/email/confirm", map[string]string{"token": token}, nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), "Email confirmed") {
		t.Fatalf("JSON confirmation=%d %s", confirmed.Code, confirmed.Body.String())
	}
}

func TestPasswordResetAcceptsJSONBody(t *testing.T) {
	runtime, handler := emptyAuthServer(t, Options{})
	hash, err := passhash.Hash("G7!mQv#2xL9rT4pZ", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.DB.W.Exec(`INSERT INTO users(username,email,password,email_verified_at) VALUES('json-reset-user','json-reset@example.test',?,datetime('now'))`, hash); err != nil {
		t.Fatal(err)
	}
	token, err := passhash.RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.DB.W.Exec(`INSERT INTO user_email_tokens(user_id,type,email,token_hash,expires_at) VALUES(1,'password_reset','json-reset@example.test',?,datetime('now','+1 hour'))`, passhash.DigestToken(token)); err != nil {
		t.Fatal(err)
	}
	newPassword := "Granite-Lantern-58!river"
	reset := serveJSON(t, handler, http.MethodPost, "/api/auth/password/reset", map[string]string{
		"token": token, "password": newPassword, "confirmPassword": newPassword,
	}, nil)
	if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), "Password changed") {
		t.Fatalf("JSON reset=%d %s", reset.Code, reset.Body.String())
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "json-reset-user", "password": newPassword}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login with JSON-reset password=%d %s", login.Code, login.Body.String())
	}
}

func TestRegistrationPasswordPolicyMatchesNeoCloudRules(t *testing.T) {
	if err := validateStandalonePassword("neoCloudIsVeryStrong!98765", "someone", "person@example.test"); err == nil {
		t.Fatal("NeoCloud product pattern was accepted")
	}
	if err := validateStandalonePassword("NeOaGeNt!X7$vQ4zK", "someone", "person@example.test"); err != nil {
		t.Fatalf("NeoAgent-specific pattern should not alter NeoCloud's rule set: %v", err)
	}
}

func emptyAuthServer(t *testing.T, options Options) (*runtimeapp.Runtime, http.Handler) {
	t.Helper()
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := runtimeapp.Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	options.DB, options.Keys = runtime.DB.W, runtime.Keys
	options.SessionSecret = runtime.Config.SessionSecret
	options.DefaultQuotaBytes = 10 << 30
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Routes(mux)
	return runtime, runtime.SessionMiddleware(mux)
}

func validateStandalonePassword(password, username, email string) error {
	handler := &Handler{}
	return handler.validatePassword(username, email, password)
}

func TestTwoFactorLoginConsumesCodeAndRotatesSession(t *testing.T) {
	runtime, handler := testAuthServer(t)
	const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	encrypted, err := runtime.Keys.EncryptValue(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_two_factor (user_id, secret, enabled) VALUES (1, ?, 1)`, encrypted); err != nil {
		t.Fatal(err)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"requiresTwoFactor":true`) {
		t.Fatalf("password step %d: %s", login.Code, login.Body.String())
	}
	pendingCookie := login.Result().Cookies()[0]
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	verify := serveJSON(t, handler, http.MethodPost, "/api/auth/login/2fa", map[string]string{"code": code}, pendingCookie)
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"success":true`) {
		t.Fatalf("second factor %d: %s", verify.Code, verify.Body.String())
	}
	finalCookie := verify.Result().Cookies()[0]
	if finalCookie.Value == pendingCookie.Value {
		t.Fatal("authenticated session reused pending challenge cookie")
	}
	me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, finalCookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me with authenticated session %d: %s", me.Code, me.Body.String())
	}
}

func TestTwoFactorChallengeExpiresAfterFiveBadCodes(t *testing.T) {
	runtime, handler := testAuthServer(t)
	encrypted, err := runtime.Keys.EncryptValue("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_two_factor (user_id, secret, enabled) VALUES (1, ?, 1)`, encrypted); err != nil {
		t.Fatal(err)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	cookie := login.Result().Cookies()[0]
	for attempt := 0; attempt < maxTwoFactorCodes; attempt++ {
		response := serveJSON(t, handler, http.MethodPost, "/api/auth/login/2fa", map[string]string{"code": "000000"}, cookie)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"INVALID_CODE"`) {
			t.Fatalf("bad code %d response = %d: %s", attempt+1, response.Code, response.Body.String())
		}
		cookies := response.Result().Cookies()
		if len(cookies) == 1 {
			cookie = cookies[0]
		}
	}
	expired := serveJSON(t, handler, http.MethodPost, "/api/auth/login/2fa", map[string]string{"code": "000000"}, cookie)
	if expired.Code != http.StatusUnauthorized || !strings.Contains(expired.Body.String(), `"code":"TWO_FACTOR_EXPIRED"`) {
		t.Fatalf("request after five failures = %d: %s", expired.Code, expired.Body.String())
	}
}

func TestLogoutDestroysSessionAndRevokesRecord(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	cookie := login.Result().Cookies()[0]
	var sessionHash string
	if err := runtime.DB.W.QueryRow(`SELECT session_hash FROM user_sessions WHERE user_id = 1`).Scan(&sessionHash); err != nil {
		t.Fatal(err)
	}
	logout := serveJSON(t, handler, http.MethodPost, "/api/auth/logout", nil, cookie)
	if logout.Code != http.StatusOK || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout response %d: %s; cookies %#v", logout.Code, logout.Body.String(), logout.Result().Cookies())
	}
	var revoked sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT revoked_at FROM user_sessions WHERE session_hash = ?`, sessionHash).Scan(&revoked); err != nil || !revoked.Valid {
		t.Fatalf("session record not revoked: value %v, error %v", revoked, err)
	}
	me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, cookie)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d: %s", me.Code, me.Body.String())
	}
}

func TestSecureTransportHonorsConfiguredProxyBoundary(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		switch key {
		case "NEOCLOUD_HOME":
			return home
		case "PUBLIC_URL":
			return "https://cloud.example.test"
		default:
			return ""
		}
	}
	runtime, err := runtimeapp.Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	hash, err := passhash.Hash("G7!mQv#2xL9rT4pZ", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES (?, ?, ?)`, "cloud-user", "cloud@example.test", hash); err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{
		DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret,
		SecureCookies: runtime.Config.SecureCookies, PublicURL: runtime.Config.PublicURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Routes(mux)
	app := runtime.SessionMiddleware(mux)

	trusted := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"cloud-user","password":"G7!mQv#2xL9rT4pZ"}`))
	trusted.RemoteAddr = "127.0.0.1:3000"
	trusted.Header.Set("Content-Type", "application/json")
	trusted.Header.Set("X-Forwarded-Proto", "https")
	trustedResponse := httptest.NewRecorder()
	app.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Code != http.StatusOK {
		t.Fatalf("trusted proxy login = %d: %s", trustedResponse.Code, trustedResponse.Body.String())
	}
	if cookies := trustedResponse.Result().Cookies(); len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("secure proxy login cookies = %#v", cookies)
	}

	untrusted := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"cloud-user","password":"G7!mQv#2xL9rT4pZ"}`))
	untrusted.RemoteAddr = "203.0.113.20:3000"
	untrusted.Header.Set("Content-Type", "application/json")
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	untrustedResponse := httptest.NewRecorder()
	app.ServeHTTP(untrustedResponse, untrusted)
	if untrustedResponse.Code != http.StatusBadRequest || !strings.Contains(untrustedResponse.Body.String(), `"code":"INSECURE_TRANSPORT"`) {
		t.Fatalf("untrusted forwarded header response = %d: %s", untrustedResponse.Code, untrustedResponse.Body.String())
	}
}

func TestStatusAndInvalidLogin(t *testing.T) {
	_, handler := testAuthServer(t)
	status := serveJSON(t, handler, http.MethodGet, "/api/auth/status", nil, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"hasUser":true`) || !strings.Contains(status.Body.String(), `"registrationOpen":true`) {
		t.Fatalf("status %d: %s", status.Code, status.Body.String())
	}
	bad := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "incorrect",
	}, nil)
	if bad.Code != http.StatusUnauthorized || !strings.Contains(bad.Body.String(), `"code":"INVALID_CREDENTIALS"`) {
		t.Fatalf("bad login %d: %s", bad.Code, bad.Body.String())
	}
}

func TestCookiePayloadUsesSharedSessionFormat(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ",
	}, nil)
	cookie := login.Result().Cookies()[0]
	sid, ok := sessionx.DecodeCookieValue(cookie.Value, runtime.Config.SessionSecret)
	if !ok || sid == "" {
		t.Fatal("cookie did not verify using the existing installation session secret")
	}
	var stored string
	if err := runtime.DB.W.QueryRow(`SELECT sid FROM sessions WHERE sid = ?`, sid).Scan(&stored); err != nil || stored != sid {
		t.Fatalf("signed cookie session is not in shared store: value %q, error %v", stored, err)
	}
	if cookie.Path != "/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie attributes differ from Node: %#v", cookie)
	}
}

func TestPasswordRecoveryIsUniformAndInvalidatesCredentials(t *testing.T) {
	mail := make(chan struct{ token, recipient, language string }, 2)
	notice := make(chan string, 1)
	runtime, handler := emptyAuthServer(t, Options{
		EmailStatus: EmailStatus{Enabled: true, Configured: true}, SignupTokenTTL: time.Hour,
		SendPasswordReset: func(_ context.Context, username, recipient, token, language string) error {
			if username != "recovery-user" {
				t.Errorf("reset username = %q", username)
			}
			mail <- struct{ token, recipient, language string }{token, recipient, language}
			return nil
		},
		SendPasswordChangedNotice: func(_ context.Context, _ string, recipient, _ string) error {
			notice <- recipient
			return nil
		},
	})
	oldPassword, newPassword := "G7!mQv#2xL9rT4pZ", "Granite-Lantern-58!river"
	hash, err := passhash.Hash(oldPassword, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES (?, ?, ?)`, "recovery-user", "recovery@example.test", hash); err != nil {
		t.Fatal(err)
	}

	for _, account := range []any{"recovery-user", "not-an-account", 42} {
		started := time.Now()
		response := serveJSON(t, handler, http.MethodPost, "/api/auth/password/forgot", map[string]any{"account": account}, nil)
		if response.Code != http.StatusOK || time.Since(started) < 330*time.Millisecond || !strings.Contains(response.Body.String(), `"success":true`) {
			t.Fatalf("forgot response for %v = %d after %v: %s", account, response.Code, time.Since(started), response.Body.String())
		}
		if account == "recovery-user" {
			select {
			case sent := <-mail:
				if sent.recipient != "recovery@example.test" || sent.language != "en" || len(sent.token) < 40 {
					t.Fatalf("reset email callback: %+v", sent)
				}
				var digest string
				if err := runtime.DB.W.QueryRow(`SELECT token_hash FROM user_email_tokens WHERE type = 'password_reset' AND consumed_at IS NULL`).Scan(&digest); err != nil || digest != passhash.DigestToken(sent.token) {
					t.Fatalf("reset token digest = %q, err = %v", digest, err)
				}
				page := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/api/auth/password/reset?token="+sent.token, nil)
				handler.ServeHTTP(page, request)
				if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="confirmPassword"`) || page.Header().Get("Cache-Control") != "no-store, max-age=0" {
					t.Fatalf("reset page = %d headers=%v body=%s", page.Code, page.Header(), page.Body.String())
				}
				loggedIn := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "recovery-user", "password": oldPassword}, nil)
				if loggedIn.Code != http.StatusOK {
					t.Fatalf("pre-reset login = %d: %s", loggedIn.Code, loggedIn.Body.String())
				}
				cookie := loggedIn.Result().Cookies()[0]
				if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens (user_id, name, platform, token_hash) SELECT id, 'Laptop', 'linux', 'reset-revocation-fixture' FROM users WHERE username = 'recovery-user'`); err != nil {
					t.Fatal(err)
				}
				form := url.Values{"token": {sent.token}, "password": {newPassword}, "confirmPassword": {newPassword}}
				reset := httptest.NewRequest(http.MethodPost, "/api/auth/password/reset", strings.NewReader(form.Encode()))
				reset.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				reset.RemoteAddr = "127.0.0.1:12345"
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, reset)
				if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "Password changed") {
					t.Fatalf("reset = %d: %s", result.Code, result.Body.String())
				}
				me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, cookie)
				if me.Code != http.StatusUnauthorized {
					t.Fatalf("old session remains live after reset: %d %s", me.Code, me.Body.String())
				}
				var revoked sql.NullString
				if err := runtime.DB.W.QueryRow(`SELECT revoked_at FROM device_tokens WHERE token_hash = 'reset-revocation-fixture'`).Scan(&revoked); err != nil || !revoked.Valid {
					t.Fatalf("device token was not revoked: %v %v", revoked, err)
				}
				oldLogin := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "recovery-user", "password": oldPassword}, nil)
				newLogin := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "recovery-user", "password": newPassword}, nil)
				if oldLogin.Code != http.StatusUnauthorized || newLogin.Code != http.StatusOK {
					t.Fatalf("post-reset login statuses old=%d new=%d", oldLogin.Code, newLogin.Code)
				}
				select {
				case recipient := <-notice:
					if recipient != "recovery@example.test" {
						t.Fatalf("password-change notice went to %q", recipient)
					}
				case <-time.After(time.Second):
					t.Fatal("password-change notice was not sent")
				}
				return
			case <-time.After(2 * time.Second):
				t.Fatal("reset mail was not sent")
			}
		}
	}
}

func TestPasswordRecoveryIgnoresLegacySignupTokenWhenConfirmationIsDisabled(t *testing.T) {
	mail := make(chan struct{}, 1)
	runtime, handler := emptyAuthServer(t, Options{
		EmailStatus: EmailStatus{Enabled: true, Configured: true, SignupConfirmationRequired: false},
		SendPasswordReset: func(context.Context, string, string, string, string) error {
			mail <- struct{}{}
			return nil
		},
	})
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES ('legacy-signup', 'legacy@example.test', 'unused')`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_email_tokens (user_id, type, email, token_hash, expires_at) VALUES (1, 'signup_confirmation', 'legacy@example.test', ?, datetime('now', '+1 hour'))`, passhash.DigestToken("legacy-confirmation-token")); err != nil {
		t.Fatal(err)
	}
	response := serveJSON(t, handler, http.MethodPost, "/api/auth/password/forgot", map[string]string{"account": "legacy-signup"}, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatalf("forgot response = %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-mail:
	case <-time.After(time.Second):
		t.Fatal("password reset was blocked by a stale signup confirmation token")
	}
}

func TestPasswordResetRejectsLinkSentToPreviousEmailButAcceptsCurrentAddress(t *testing.T) {
	runtime, handler := emptyAuthServer(t, Options{EmailStatus: EmailStatus{Enabled: true, Configured: true}})
	oldPassword := "G7!mQv#2xL9rT4pZ"
	oldHash, err := passhash.Hash(oldPassword, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, email, password) VALUES (?, ?, ?)`, "stale-reset-user", "old-address@example.test", oldHash); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username=?`, "stale-reset-user").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	oldToken, err := passhash.RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_email_tokens (user_id,type,email,token_hash,expires_at) VALUES(?,?,?,?,datetime('now','+1 hour'))`, userID, resetTokenType, "old-address@example.test", passhash.DigestToken(oldToken)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email=? WHERE id=?`, "new-address@example.test", userID); err != nil {
		t.Fatal(err)
	}
	newPassword := "Granite-Lantern-58!river"
	stale := serveJSON(t, handler, http.MethodPost, "/api/auth/password/reset", map[string]string{"token": oldToken, "password": newPassword}, nil)
	if stale.Code != http.StatusBadRequest || !strings.Contains(stale.Body.String(), "This link does not work") {
		t.Fatalf("stale address reset response = %d: %s", stale.Code, stale.Body.String())
	}
	var consumed sql.NullString
	if err := runtime.DB.W.QueryRow(`SELECT consumed_at FROM user_email_tokens WHERE token_hash=?`, passhash.DigestToken(oldToken)).Scan(&consumed); err != nil || consumed.Valid {
		t.Fatalf("stale reset token was consumed: value=%v err=%v", consumed, err)
	}
	oldLogin := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "stale-reset-user", "password": oldPassword}, nil)
	if oldLogin.Code != http.StatusOK {
		t.Fatalf("stale link changed the password: login=%d %s", oldLogin.Code, oldLogin.Body.String())
	}

	newToken, err := passhash.RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO user_email_tokens (user_id,type,email,token_hash,expires_at) VALUES(?,?,?,?,datetime('now','+1 hour'))`, userID, resetTokenType, "new-address@example.test", passhash.DigestToken(newToken)); err != nil {
		t.Fatal(err)
	}
	valid := serveJSON(t, handler, http.MethodPost, "/api/auth/password/reset", map[string]string{"token": newToken, "password": newPassword}, nil)
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), "Password changed") {
		t.Fatalf("current address reset response = %d: %s", valid.Code, valid.Body.String())
	}
}

func TestQRLoginLifecycleAndOneTimeClaim(t *testing.T) {
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login %d: %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "neocloud.sid" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("missing session cookie")
	}
	created := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge", map[string]any{"requestMetadata": map[string]any{"deviceLabel": "Browser"}}, nil)
	if created.Code != 200 {
		t.Fatalf("create %d: %s", created.Code, created.Body.String())
	}
	var ch struct {
		ID     string `json:"challengeId"`
		Poll   string `json:"pollToken"`
		Secret string `json:"approveSecret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	if ch.Secret != "" {
		t.Fatal("approval secret unexpectedly returned separately")
	}
	var payload map[string]any
	_ = json.Unmarshal(created.Body.Bytes(), &payload)
	qr, _ := payload["qrPayload"].(string)
	parsed, err := url.Parse(qr)
	if err != nil {
		t.Fatal(err)
	}
	secret := parsed.Query().Get("secret")
	if secret == "" {
		t.Fatal("missing QR secret")
	}
	resolve := serveJSON(t, handler, http.MethodPost, "/api/account/qr-login/resolve", map[string]string{"challengeId": ch.ID, "secret": secret}, sessionCookie)
	if resolve.Code != 200 {
		t.Fatalf("resolve %d: %s", resolve.Code, resolve.Body.String())
	}
	approve := serveJSON(t, handler, http.MethodPost, "/api/account/qr-login/approve", map[string]string{"challengeId": ch.ID, "secret": secret}, sessionCookie)
	if approve.Code != 200 {
		t.Fatalf("approve %d: %s", approve.Code, approve.Body.String())
	}
	status := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge/"+ch.ID+"/status", map[string]string{"token": ch.Poll}, nil)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"status":"approved"`) {
		t.Fatalf("status %d: %s", status.Code, status.Body.String())
	}
	claim := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge/"+ch.ID+"/claim", map[string]string{"token": ch.Poll}, nil)
	if claim.Code != 200 {
		t.Fatalf("claim %d: %s", claim.Code, claim.Body.String())
	}
	again := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge/"+ch.ID+"/claim", map[string]string{"token": ch.Poll}, nil)
	if again.Code != 409 || !strings.Contains(again.Body.String(), "QR_LOGIN_USED") {
		t.Fatalf("second claim %d: %s", again.Code, again.Body.String())
	}
	var pollHash, secretHash string
	if err := runtime.DB.W.QueryRow(`SELECT poll_token_hash,approve_secret_hash FROM user_qr_login_challenges WHERE id=?`, ch.ID).Scan(&pollHash, &secretHash); err != nil {
		t.Fatal(err)
	}
	if pollHash == ch.Poll || secretHash == secret {
		t.Fatal("plaintext credential persisted")
	}
}

func TestQRLoginLongPollWakesWhenChallengeChangesAndHonorsCancellation(t *testing.T) {
	runtime, handler := testAuthServer(t)
	created := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge", map[string]any{}, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create %d: %s", created.Code, created.Body.String())
	}
	var challenge struct {
		ID   string `json:"challengeId"`
		Poll string `json:"pollToken"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge/"+challenge.ID+"/status", map[string]string{"token": challenge.Poll, "knownStatus": "pending"}, nil)
	}()
	// The endpoint polls the durable row; changing it through a second DB
	// operation proves this works across handler instances, not just a local
	// notification channel.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status string
		err := runtime.DB.W.QueryRow(`SELECT status FROM user_qr_login_challenges WHERE id=?`, challenge.ID).Scan(&status)
		if err != nil {
			t.Fatal(err)
		}
		if status == "pending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("challenge did not remain pending")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := runtime.DB.W.Exec(`UPDATE user_qr_login_challenges SET status='scanned',scanned_at=datetime('now') WHERE id=?`, challenge.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"scanned"`) || !strings.Contains(response.Body.String(), `"longPoll":true`) {
			t.Fatalf("long-poll response %d: %s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("long poll did not wake after persisted status change")
	}

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/api/auth/qr-login/challenge/"+challenge.ID+"/status", strings.NewReader(`{"token":"`+challenge.Poll+`","knownStatus":"scanned"}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
		// Nobody is left to read an answer; the wait just has to end.
	case <-time.After(time.Second):
		t.Fatal("long poll did not return after request cancellation")
	}
}

func TestQRLoginRejectsUnauthorizedExpiredAndCrossAccount(t *testing.T) {
	runtime, handler := testAuthServer(t)
	unauth := serveJSON(t, handler, http.MethodPost, "/api/account/qr-login/resolve", map[string]string{"challengeId": "missing", "secret": "x"}, nil)
	if unauth.Code != 401 {
		t.Fatalf("unauthorized status %d", unauth.Code)
	}
	created := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge", map[string]any{}, nil)
	var ch struct{ ID, Poll string }
	var data map[string]any
	_ = json.Unmarshal(created.Body.Bytes(), &data)
	ch.ID, _ = data["challengeId"].(string)
	ch.Poll, _ = data["pollToken"].(string)
	_, err := runtime.DB.W.Exec(`UPDATE user_qr_login_challenges SET expires_at=datetime('now','-1 second') WHERE id=?`, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge/"+ch.ID+"/status", map[string]string{"token": ch.Poll}, nil)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"status":"expired"`) {
		t.Fatalf("expired status %d: %s", status.Code, status.Body.String())
	}
	login1 := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	var cookie *http.Cookie
	for _, c := range login1.Result().Cookies() {
		if c.Name == "neocloud.sid" {
			cookie = c
		}
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users(username,email,password) VALUES('other-user','other@example.test','unused')`); err != nil {
		t.Fatal(err)
	}
	// Cross-account approval is exercised through a valid session for the second user.
	if cookie == nil {
		t.Fatal("missing first session")
	}
	otherHash, err := passhash.Hash("G7!mQv#2xL9rT4pZ", 4)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.DB.W.Exec(`UPDATE users SET password=? WHERE username='other-user'`, otherHash)
	if err != nil {
		t.Fatal(err)
	}
	login2 := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "other-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	var otherCookie *http.Cookie
	for _, c := range login2.Result().Cookies() {
		if c.Name == "neocloud.sid" {
			otherCookie = c
		}
	}
	active := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge", map[string]any{}, nil)
	var a map[string]any
	_ = json.Unmarshal(active.Body.Bytes(), &a)
	id, _ := a["challengeId"].(string)
	payload, _ := a["qrPayload"].(string)
	u, _ := url.Parse(payload)
	secret := u.Query().Get("secret")
	approved := serveJSON(t, handler, http.MethodPost, "/api/account/qr-login/approve", map[string]string{"challengeId": id, "secret": secret}, cookie)
	if approved.Code != 200 {
		t.Fatalf("first approve %d %s", approved.Code, approved.Body.String())
	}
	wrong := serveJSON(t, handler, http.MethodPost, "/api/account/qr-login/approve", map[string]string{"challengeId": id, "secret": secret}, otherCookie)
	if wrong.Code != 409 || !strings.Contains(wrong.Body.String(), "QR_LOGIN_APPROVED") {
		t.Fatalf("cross-account status %d: %s", wrong.Code, wrong.Body.String())
	}
}

func TestQRLoginApprovalRequiresPhoneDevice(t *testing.T) {
	runtime, handler := testAuthServer(t)
	created := serveJSON(t, handler, http.MethodPost, "/api/auth/qr-login/challenge", map[string]any{}, nil)
	var response map[string]any
	_ = json.Unmarshal(created.Body.Bytes(), &response)
	id, _ := response["challengeId"].(string)
	payload, _ := url.Parse(response["qrPayload"].(string))
	secret := payload.Query().Get("secret")
	bearer := "nc_test_device_token"
	_, err := runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,'Desktop','desktop',?)`, passhash.DigestToken(bearer))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/account/qr-login/resolve", strings.NewReader(`{"challengeId":"`+id+`","secret":"`+secret+`"}`))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusForbidden {
		t.Fatalf("desktop device approval status %d: %s", responseRecorder.Code, responseRecorder.Body.String())
	}
}
