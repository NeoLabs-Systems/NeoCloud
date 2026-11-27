package oauth

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

func TestOAuthBrowserSignInConsentAndSingleUseCSRF(t *testing.T) {
	_, db, now := testHandler(t)
	password := "OAuth-consent-password-7!"
	hash, err := passhash.Hash(password, passhash.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE users SET password=? WHERE id=1`, hash); err != nil {
		t.Fatal(err)
	}
	redirect := "http://client.example/callback"
	if _, err = db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('consent-client','companion:neoagent','NeoAgent',?)`, redirect); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, SignIn: testSignIn(t, db.W, nil, func() time.Time { return now }), Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sessionx.NewStore(context.Background(), db.W)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := sessionx.NewManager(store, sessionx.Options{Name: "neocloud.sid", Secrets: []string{"test-session-secret-with-enough-entropy"}, SameSite: http.SameSiteLaxMode, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	protocol.Routes(mux)
	server := httptest.NewServer(manager.Middleware(mux))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	challenge := testPKCEChallenge("oauth-consent-test-verifier-that-is-at-least-43-characters")
	query := url.Values{"response_type": {"code"}, "client_id": {"consent-client"}, "redirect_uri": {redirect}, "state": {"csrf_test_state_123"}, "scope": {"files:read files:write"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	authorizeURL := server.URL + "/oauth/authorize?" + query.Encode()
	response, err := client.Get(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	location := response.Header.Get("Location")
	response.Body.Close()
	if response.StatusCode != http.StatusFound || !strings.HasPrefix(location, "/oauth/sign-in?continue=") {
		t.Fatalf("anonymous authorize=%d location=%q", response.StatusCode, location)
	}
	response, err = client.Get(server.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	form := url.Values{"account": {"oauth-owner"}, "password": {password}, "continue": {authorizePath(query)}}
	response, err = client.PostForm(server.URL+"/oauth/sign-in", form)
	if err != nil {
		t.Fatal(err)
	}
	location = response.Header.Get("Location")
	response.Body.Close()
	if response.StatusCode != http.StatusFound || location != authorizePath(query) {
		t.Fatalf("sign-in=%d location=%q", response.StatusCode, location)
	}
	response, err = client.Get(server.URL + location)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Connect NeoAgent?") {
		t.Fatalf("consent page=%d err=%v body=%q", response.StatusCode, err, page)
	}
	match := regexp.MustCompile(`name="consent" value="([^"]+)"`).FindSubmatch(page)
	if len(match) != 2 {
		t.Fatalf("consent token missing: %s", page)
	}
	consent := string(match[1])
	approved := url.Values{"consent": {consent}, "decision": {"approve"}, "files": {"write"}}
	response, err = client.PostForm(server.URL+"/oauth/authorize", approved)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(response.Header.Get("Location"))
	response.Body.Close()
	if response.StatusCode != http.StatusFound || err != nil || callback.Host != "client.example" || callback.Query().Get("state") != "csrf_test_state_123" || callback.Query().Get("code") == "" {
		t.Fatalf("approve=%d location=%q err=%v", response.StatusCode, response.Header.Get("Location"), err)
	}
	var userID int64
	var scopes string
	if err = db.W.QueryRow(`SELECT user_id,scopes_json FROM oauth_codes WHERE client_id='consent-client'`).Scan(&userID, &scopes); err != nil {
		t.Fatal(err)
	}
	if userID != 1 || scopes != `["files:read","files:write"]` {
		t.Fatalf("issued code user=%d scopes=%s", userID, scopes)
	}
	response, err = client.PostForm(server.URL+"/oauth/authorize", approved)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("replayed consent token=%d", response.StatusCode)
	}
	var codes int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM oauth_codes WHERE client_id='consent-client'`).Scan(&codes); err != nil || codes != 1 {
		t.Fatalf("code count=%d err=%v", codes, err)
	}
}

func TestOAuthStandaloneSignInCompletesTwoFactorChallenge(t *testing.T) {
	_, db, now := testHandler(t)
	password := "OAuth-consent-password-7!"
	hash, err := passhash.Hash(password, passhash.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keys.EncryptValue(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE users SET password=? WHERE id=1`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO user_two_factor(user_id,secret,enabled) VALUES(1,?,1)`, sealed); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, SignIn: testSignIn(t, db.W, keys, func() time.Time { return now }), Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sessionx.NewStore(context.Background(), db.W)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := sessionx.NewManager(store, sessionx.Options{Name: "neocloud.sid", Secrets: []string{"test-session-secret-with-enough-entropy"}, SameSite: http.SameSiteLaxMode, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	protocol.Routes(mux)
	server := httptest.NewServer(manager.Middleware(mux))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.PostForm(server.URL+"/oauth/sign-in", url.Values{"account": {"oauth-owner"}, "password": {password}, "continue": {"/oauth/authorize"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Enter your code") {
		t.Fatalf("password step=%d err=%v body=%q", response.StatusCode, err, page)
	}
	code, err := totp.Code(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= factorAttempts; attempt++ {
		response, err = client.PostForm(server.URL+"/oauth/sign-in", url.Values{"code": {"not-a-valid-code"}, "continue": {"/oauth/authorize"}})
		if err != nil {
			t.Fatal(err)
		}
		page, err = io.ReadAll(response.Body)
		response.Body.Close()
		wantStatus := http.StatusUnauthorized
		wantMessage := "That code isn’t right."
		if attempt == factorAttempts {
			wantStatus = http.StatusUnauthorized
			wantMessage = "Too many wrong codes. Sign in again."
		}
		if err != nil || response.StatusCode != wantStatus || !strings.Contains(string(page), wantMessage) {
			t.Fatalf("wrong factor attempt %d=%d err=%v body=%q", attempt, response.StatusCode, err, page)
		}
	}
	var challengeFailures int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM login_failures WHERE account_key='user:1' AND kind='second_factor' AND challenge_id IS NOT NULL`).Scan(&challengeFailures); err != nil || challengeFailures != factorAttempts {
		t.Fatalf("challenge failures=%d err=%v", challengeFailures, err)
	}
	// Exhausting a challenge requires the password again; it cannot be reused
	// with a correct code after the fifth failed attempt.
	response, err = client.PostForm(server.URL+"/oauth/sign-in", url.Values{"code": {code}, "continue": {"/oauth/authorize"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusUnauthorized || !strings.Contains(string(page), "That took too long. Sign in again.") {
		t.Fatalf("exhausted challenge reuse=%d err=%v body=%q", response.StatusCode, err, page)
	}
	// The account-wide throttle still applies across challenges; wait out the
	// first short delay caused by the five failed codes.
	now = now.Add(3 * time.Second)
	response, err = client.PostForm(server.URL+"/oauth/sign-in", url.Values{"account": {"oauth-owner"}, "password": {password}, "continue": {"/oauth/authorize"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("password restart=%d", response.StatusCode)
	}
	response, err = client.PostForm(server.URL+"/oauth/sign-in", url.Values{"code": {code}, "continue": {"/oauth/authorize"}})
	if err != nil {
		t.Fatal(err)
	}
	location := response.Header.Get("Location")
	response.Body.Close()
	if response.StatusCode != http.StatusFound || location != "/oauth/authorize" {
		t.Fatalf("2FA step=%d location=%q", response.StatusCode, location)
	}
}

const factorAttempts = 5

func testSignIn(t *testing.T, db *sql.DB, keys *cryptox.Keyring, clock func() time.Time) SignIn {
	t.Helper()
	if keys == nil {
		var err error
		if keys, err = cryptox.NewKeyring(strings.Repeat("ab", 32)); err != nil {
			t.Fatal(err)
		}
	}
	identity, err := auth.New(auth.Options{DB: db, Keys: keys, SessionSecret: strings.Repeat("s", 64), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
