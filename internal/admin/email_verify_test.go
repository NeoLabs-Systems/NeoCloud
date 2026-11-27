package admin

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminVerifyUserEmailConfirmsAndConsumesSignupToken(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO user_email_tokens(user_id,type,email,token_hash,expires_at) VALUES(2,'signup_confirmation','account@example.test','digest','2099-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/email/verify", "")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"success":true}` {
		t.Fatalf("verify response=%d %s", response.Code, response.Body.String())
	}
	var verified, consumed, audits int
	if err := db.W.QueryRow(`SELECT email_verified_at IS NOT NULL FROM users WHERE id=2`).Scan(&verified); err != nil || verified != 1 {
		t.Fatalf("verified=%d err=%v", verified, err)
	}
	if err := db.W.QueryRow(`SELECT consumed_at IS NOT NULL FROM user_email_tokens WHERE user_id=2 AND type='signup_confirmation'`).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("consumed=%d err=%v", consumed, err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.email_verify' AND actor_user_id=1 AND subject_user_id=2`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}

func TestAdminVerifyUserEmailRejectsMissingEmailAndUnknownUser(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`UPDATE users SET email=NULL WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	noEmail := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/email/verify", "")
	if noEmail.Code != http.StatusConflict {
		t.Fatalf("no email response=%d %s", noEmail.Code, noEmail.Body.String())
	}
	missing := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/999/email/verify", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing response=%d %s", missing.Code, missing.Body.String())
	}
	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPost, "/api/admin/users/1/email/verify", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin response=%d %s", denied.Code, denied.Body.String())
	}
}
