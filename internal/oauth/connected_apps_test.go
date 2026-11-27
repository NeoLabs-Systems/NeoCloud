package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type testPrincipalKey struct{}

func testHandler(t *testing.T) (*Handler, *dbx.DB, time.Time) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neo.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	_, err = db.W.Exec(`INSERT INTO users(username,password,email_verified_at) VALUES('oauth-owner','fixture-hash',datetime('now')),('other-owner','fixture-hash',datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	h, err := New(Options{DB: db.W, Now: func() time.Time { return now }, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(testPrincipalKey{}).(Principal)
		return p, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	return h, db, now
}

func TestOAuthProtocolExchangesPKCECodeRotatesRefreshAndRevokesTokens(t *testing.T) {
	_, db, now := testHandler(t)
	redirect := `http://localhost:3333/api/integrations/oauth/callback`
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('protocol-client','companion:neoagent','NeoAgent',?)`, redirect); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	authorizeRequest := AuthorizationRequest{
		ResponseType: "code", ClientID: "protocol-client", RedirectURI: redirect,
		State: "state_12345678", CodeChallenge: testPKCEChallenge("test-verifier-with-at-least-43-characters-long"), CodeChallengeMethod: "S256",
		Scope: "files:read files:write signing:read signing:write",
	}
	authorization, err := protocol.ValidateAuthorizationRequest(context.Background(), authorizeRequest)
	if err != nil {
		t.Fatal(err)
	}
	scopes := GrantedOAuthScopes(authorization.Scopes, AuthorizationGrant{Files: "write", Signing: true})
	if _, err = protocol.IssueAuthorizationCode(httptest.NewRequest(http.MethodPost, "/oauth/authorize", nil), authorization, scopes); err == nil {
		t.Fatal("anonymous request issued authorization code")
	}
	deviceRequest := authorizedRequest(http.MethodPost, "/oauth/authorize", &Principal{UserID: 1, Device: true, Platform: "ios"})
	if _, err = protocol.IssueAuthorizationCode(deviceRequest, authorization, scopes); err == nil {
		t.Fatal("device token issued browser OAuth consent code")
	}
	code, err := protocol.IssueAuthorizationCode(authorizedRequest(http.MethodPost, "/oauth/authorize", &Principal{UserID: 1}), authorization, scopes)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	protocol.Routes(mux)
	bad := oauthFormRequest(mux, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"protocol-client"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {"wrong-verifier-with-at-least-43-characters-long"},
	})
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"error":"invalid_grant"`) {
		t.Fatalf("invalid PKCE response=%d %s", bad.Code, bad.Body.String())
	}
	first := oauthFormRequest(mux, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"protocol-client"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {"test-verifier-with-at-least-43-characters-long"},
	})
	if first.Code != http.StatusOK || first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("code exchange=%d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Type    string `json:"token_type"`
		Expires int    `json:"expires_in"`
		Scope   string `json:"scope"`
	}
	if err = json.Unmarshal(first.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.Access == "" || tokens.Refresh == "" || tokens.Type != "Bearer" || tokens.Expires != 3600 || tokens.Scope != "files:read files:write signing:read signing:write" {
		t.Fatalf("token response=%#v", tokens)
	}
	var storedHash string
	if err = db.W.QueryRow(`SELECT token_hash FROM oauth_access_tokens WHERE client_id='protocol-client'`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != oauthDigest(tokens.Access) || storedHash == tokens.Access {
		t.Fatalf("access token stored unhashed: %q", storedHash)
	}
	info := httptest.NewRecorder()
	userinfo := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	userinfo.Header.Set("Authorization", "Bearer "+tokens.Access)
	mux.ServeHTTP(info, userinfo)
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), `"preferred_username":"oauth-owner"`) || !strings.Contains(info.Body.String(), `"scope":"files:read files:write signing:read signing:write"`) {
		t.Fatalf("userinfo=%d %s", info.Code, info.Body.String())
	}
	rotated := oauthFormRequest(mux, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {"protocol-client"}, "refresh_token": {tokens.Refresh}})
	if rotated.Code != http.StatusOK {
		t.Fatalf("refresh=%d %s", rotated.Code, rotated.Body.String())
	}
	var nextTokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err = json.Unmarshal(rotated.Body.Bytes(), &nextTokens); err != nil || nextTokens.Access == "" || nextTokens.Refresh == "" || nextTokens.Refresh == tokens.Refresh {
		t.Fatalf("rotated tokens=%#v err=%v", nextTokens, err)
	}
	replay := oauthFormRequest(mux, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {"protocol-client"}, "refresh_token": {tokens.Refresh}})
	if replay.Code != http.StatusBadRequest || !strings.Contains(replay.Body.String(), `"error":"invalid_grant"`) {
		t.Fatalf("refresh replay=%d %s", replay.Code, replay.Body.String())
	}
	revoke := oauthFormRequest(mux, "/oauth/revoke", url.Values{"client_id": {"protocol-client"}, "token": {nextTokens.Access}})
	if revoke.Code != http.StatusOK || revoke.Body.String() != "{}" {
		t.Fatalf("revoke=%d %s", revoke.Code, revoke.Body.String())
	}
	denied := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	request.Header.Set("Authorization", "Bearer "+nextTokens.Access)
	mux.ServeHTTP(denied, request)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("userinfo after revoke=%d %s", denied.Code, denied.Body.String())
	}
}

func TestOAuthAuthorizationValidationAndRevocationAreBoundToClient(t *testing.T) {
	_, db, now := testHandler(t)
	redirect := `https://agent.example/api/integrations/oauth/callback`
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('valid-client','companion:neoagent','NeoAgent',?)`, redirect); err != nil {
		t.Fatal(err)
	}
	protocol, err := NewProtocol(ProtocolOptions{DB: db.W, Now: func() time.Time { return now }, ResolvePrincipal: resolveTestPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	valid := AuthorizationRequest{ResponseType: "code", ClientID: "valid-client", RedirectURI: redirect, State: "state-12345678", CodeChallenge: strings.Repeat("a", 43), CodeChallengeMethod: "S256"}
	if _, err = protocol.ValidateAuthorizationRequest(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	valid.RedirectURI = "https://attacker.example/api/integrations/oauth/callback"
	if _, err = protocol.ValidateAuthorizationRequest(context.Background(), valid); err == nil {
		t.Fatal("authorization request accepted an unregistered redirect")
	}
	mux := http.NewServeMux()
	protocol.Routes(mux)
	unknown := oauthFormRequest(mux, "/oauth/revoke", url.Values{"client_id": {"valid-client"}, "token": {"unknown"}})
	if unknown.Code != http.StatusOK || unknown.Body.String() != "{}" {
		t.Fatalf("RFC7009 unknown token revoke=%d %s", unknown.Code, unknown.Body.String())
	}
	badClient := oauthFormRequest(mux, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {"absent"}})
	if badClient.Code != http.StatusUnauthorized || !strings.Contains(badClient.Body.String(), `"error":"invalid_client"`) {
		t.Fatalf("unknown client response=%d %s", badClient.Code, badClient.Body.String())
	}
}

func testPKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func oauthFormRequest(mux *http.ServeMux, path string, form url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, request)
	return rec
}

func resolveTestPrincipal(r *http.Request) (Principal, bool) {
	p, ok := r.Context().Value(testPrincipalKey{}).(Principal)
	return p, ok
}

func authorizedRequest(method, path string, p *Principal) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if p != nil {
		r = r.WithContext(context.WithValue(r.Context(), testPrincipalKey{}, *p))
	}
	return r
}

func addGrantFixture(t *testing.T, db *dbx.DB, now time.Time, id string, userID int64, expires time.Time, scopes string) {
	t.Helper()
	_, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES(?,'companion:neoagent',?,?)`, id, "NeoAgent", `http://localhost:3333/api/integrations/oauth/callback`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.W.Exec(`INSERT INTO oauth_refresh_tokens(client_id,user_id,token_hash,scopes_json,created_at,expires_at) VALUES(?,?,?,?,?,?)`, id, userID, "refresh-"+id, scopes, now.Add(-time.Hour).Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,created_at,expires_at,last_used_at) VALUES(?,?,?,?,?,?,?)`, id, userID, "access-"+id, scopes, now.Add(-time.Hour).Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano), now.Add(-time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func TestListConnectedAppsFiltersExpiredGrantsAndPreservesScopeOrder(t *testing.T) {
	h, db, now := testHandler(t)
	addGrantFixture(t, db, now, "client-live", 1, now.Add(time.Hour), `["files:read","signing:read","invalid"]`)
	addGrantFixture(t, db, now, "client-expired", 1, now.Add(-time.Minute), `["files:write"]`)
	addGrantFixture(t, db, now, "client-other", 2, now.Add(time.Hour), `["files:write"]`)
	mux := http.NewServeMux()
	h.Routes(mux)
	p := Principal{UserID: 1}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/account/connected-apps", &p))
	if rec.Code != 200 {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var answer struct {
		Apps []connectedApp `json:"apps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Apps) != 1 {
		t.Fatalf("apps=%#v", answer.Apps)
	}
	app := answer.Apps[0]
	if app.ID != "client-live" || app.Host != "localhost:3333" || len(app.Scopes) != 2 || app.Scopes[0] != "files:read" || app.Scopes[1] != "signing:read" {
		t.Fatalf("app=%#v", app)
	}
	if app.LastUsedAt == nil {
		t.Fatal("lastUsedAt missing")
	}
}

func TestDisconnectRevokesOnlyTheSelectedUsersGrantAndConsumesCodes(t *testing.T) {
	h, db, now := testHandler(t)
	addGrantFixture(t, db, now, "client-one", 1, now.Add(time.Hour), `["files:read"]`)
	_, err := db.W.Exec(`INSERT INTO oauth_codes(client_id,user_id,code_hash,redirect_uri,scopes_json,code_challenge,expires_at) VALUES('client-one',1,'one-time-code',?,'["files:read"]','challenge',?)`, `http://localhost:3333/api/integrations/oauth/callback`, now.Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	owner := Principal{UserID: 1}
	other := Principal{UserID: 2}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authorizedRequest(http.MethodDelete, "/api/account/connected-apps/client-one", &other))
	if rec.Code != 404 {
		t.Fatalf("cross-account disconnect=%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, authorizedRequest(http.MethodDelete, "/api/account/connected-apps/client-one", &owner))
	if rec.Code != 200 {
		t.Fatalf("disconnect=%d %s", rec.Code, rec.Body.String())
	}
	var answer map[string]bool
	if err = json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || len(answer) != 1 || !answer["success"] {
		t.Fatalf("disconnect response must contain only success: %s err=%v", rec.Body.String(), err)
	}
	var accessRevoked, refreshRevoked, codeConsumed sql.NullString
	if err = db.W.QueryRow(`SELECT revoked_at FROM oauth_access_tokens WHERE client_id='client-one' AND user_id=1`).Scan(&accessRevoked); err != nil {
		t.Fatal(err)
	}
	if err = db.W.QueryRow(`SELECT revoked_at FROM oauth_refresh_tokens WHERE client_id='client-one' AND user_id=1`).Scan(&refreshRevoked); err != nil {
		t.Fatal(err)
	}
	if err = db.W.QueryRow(`SELECT consumed_at FROM oauth_codes WHERE code_hash='one-time-code'`).Scan(&codeConsumed); err != nil {
		t.Fatal(err)
	}
	if !accessRevoked.Valid || !refreshRevoked.Valid || !codeConsumed.Valid {
		t.Fatalf("grants still active: access=%v refresh=%v code=%v", accessRevoked, refreshRevoked, codeConsumed)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, authorizedRequest(http.MethodDelete, "/api/account/connected-apps/client-one", &owner))
	if rec.Code != 404 {
		t.Fatalf("repeat disconnect=%d", rec.Code)
	}
}

func TestConnectedAppsRequiresSessionOrPhone(t *testing.T) {
	h, _, _ := testHandler(t)
	mux := http.NewServeMux()
	h.Routes(mux)
	for _, tc := range []struct {
		name   string
		p      *Principal
		status int
	}{{"anonymous", nil, 401}, {"desktop token", &Principal{UserID: 1, Device: true, Platform: "macos"}, 403}, {"phone token", &Principal{UserID: 1, Device: true, Platform: "ios"}, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authorizedRequest(http.MethodGet, "/api/account/connected-apps", tc.p))
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
