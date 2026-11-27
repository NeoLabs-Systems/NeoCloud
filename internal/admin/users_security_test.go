package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

func TestResetUserPasswordRevokesCredentialsAndAuditsAtomically(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash) VALUES(2,'session-old')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(2,'phone','ios','device-hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,name,kind,redirect_uri) VALUES('security-client','Security client','oauth','http://localhost/callback')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('security-client',2,'access-hash','[]',datetime('now','+1 day'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_refresh_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('security-client',2,'refresh-hash','[]',datetime('now','+1 day'))`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/password", `{"password":"Violet-River!Quartz-9284"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("reset=%d %s", response.Code, response.Body.String())
	}
	var hash string
	if err := db.W.QueryRow(`SELECT password FROM users WHERE id=2`).Scan(&hash); err != nil || !passhash.Verify(hash, "Violet-River!Quartz-9284") {
		t.Fatalf("password hash not updated: err=%v", err)
	}
	var revoked, audited int
	for _, query := range []string{
		`SELECT COUNT(*) FROM user_sessions WHERE user_id=2 AND revoked_at IS NOT NULL`,
		`SELECT COUNT(*) FROM device_tokens WHERE user_id=2 AND revoked_at IS NOT NULL`,
		`SELECT COUNT(*) FROM oauth_access_tokens WHERE user_id=2 AND revoked_at IS NOT NULL`,
		`SELECT COUNT(*) FROM oauth_refresh_tokens WHERE user_id=2 AND revoked_at IS NOT NULL`,
	} {
		if err := db.W.QueryRow(query).Scan(&revoked); err != nil || revoked != 1 {
			t.Fatalf("revocation query %q got=%d err=%v", query, revoked, err)
		}
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.password_reset' AND actor_user_id=1 AND subject_user_id=2`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit count=%d err=%v", audited, err)
	}
}

func TestResetUserPasswordValidatesAndRollsBackOnRevocationFailure(t *testing.T) {
	db, mux := testHandler(t)
	tweak := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/password", `{"password":"short"}`)
	if tweak.Code != http.StatusBadRequest || !strings.Contains(tweak.Body.String(), "PASSWORD_TOO_SHORT") {
		t.Fatalf("weak password=%d %s", tweak.Code, tweak.Body.String())
	}
	if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(2,'phone','ios','device-hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,name,kind,redirect_uri) VALUES('security-client','Security client','oauth','http://localhost/callback')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('security-client',2,'access-hash','[]',datetime('now','+1 day'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`CREATE TRIGGER fail_oauth_revoke BEFORE UPDATE ON oauth_access_tokens BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/password", `{"password":"Violet-River!Quartz-9284"}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("reset with failed revocation=%d %s", response.Code, response.Body.String())
	}
	var password string
	if err := db.W.QueryRow(`SELECT password FROM users WHERE id=2`).Scan(&password); err != nil || password != "not-a-real-password" {
		t.Fatalf("password changed despite rollback: %q err=%v", password, err)
	}
	var revoked, audits int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM device_tokens WHERE user_id=2 AND revoked_at IS NOT NULL`).Scan(&revoked); err != nil || revoked != 0 {
		t.Fatalf("device revocations=%d err=%v", revoked, err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.password_reset'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("audit rows=%d err=%v", audits, err)
	}
}

func TestRevokeUserSessionsPreservesCurrentDeviceAndReportsCounts(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES
		(2,'first','ios','token-one'),(2,'current','ios','token-two')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash) VALUES(2,'old-one'),(2,'old-two')`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodDelete, "/api/admin/users/2/sessions", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sessions":2`) || !strings.Contains(response.Body.String(), `"devices":2`) {
		t.Fatalf("sign out=%d %s", response.Code, response.Body.String())
	}
	var auditCount int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.sign_out_everywhere' AND actor_user_id=1 AND subject_user_id=2`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit=%d err=%v", auditCount, err)
	}
	currentToken := "nc_current-device-secret"
	currentHash := sha256.Sum256([]byte(currentToken))
	if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,'current','ios',?),(1,'other','ios','admin-device-other')`, hex.EncodeToString(currentHash[:])); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/admin/users/1/sessions", nil)
	request.Header.Set("Authorization", "Bearer "+currentToken)
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, testPrincipal{ID: 1, Device: true, Platform: "ios"}))
	response = httptest.NewRecorder()
	requestauth.IdentityMiddleware(db.W, mux).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"devices":1`) {
		t.Fatalf("self sign out=%d %s", response.Code, response.Body.String())
	}
	var currentRevoked, otherRevoked int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM device_tokens WHERE token_hash=? AND revoked_at IS NOT NULL`, hex.EncodeToString(currentHash[:])).Scan(&currentRevoked); err != nil || currentRevoked != 0 {
		t.Fatalf("current device revoked=%d err=%v", currentRevoked, err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM device_tokens WHERE user_id=1 AND name='other' AND revoked_at IS NOT NULL`).Scan(&otherRevoked); err != nil || otherRevoked != 1 {
		t.Fatalf("other device revoked=%d err=%v", otherRevoked, err)
	}
}
