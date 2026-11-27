package admin

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func TestRecalculateUserUsageMatchesRecordedBlobs(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`UPDATE users SET used_bytes=999 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES
		('blob-one',2,123,'sha-one',1,1),('blob-two',2,456,'sha-two',1,1),
		('blob-admin',1,500,'sha-admin',1,1)`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/2/usage/recalculate", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"usedBytes":579`) {
		t.Fatalf("recalculate response=%d %s", response.Code, response.Body.String())
	}
	var got int64
	if err := db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=2`).Scan(&got); err != nil || got != 579 {
		t.Fatalf("user usage=%d err=%v", got, err)
	}
	if denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPost, "/api/admin/users/2/usage/recalculate", ""); denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin recalculate=%d %s", denied.Code, denied.Body.String())
	}
	if missing := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/users/999/usage/recalculate", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("missing user recalculate=%d %s", missing.Code, missing.Body.String())
	}
}

func TestRecalculateAllUsageMatchesRecordedBlobs(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`UPDATE users SET used_bytes=999`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES
		('blob-one',2,123,'sha-one',1,1),('blob-two',2,456,'sha-two',1,1),
		('blob-admin',1,500,'sha-admin',1,1)`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/storage/recalculate", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) || !strings.Contains(response.Body.String(), `"users":2`) {
		t.Fatalf("recalculate all response=%d %s", response.Code, response.Body.String())
	}
	var accountUsage, adminUsage int64
	if err := db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=2`).Scan(&accountUsage); err != nil || accountUsage != 579 {
		t.Fatalf("account usage=%d err=%v", accountUsage, err)
	}
	if err := db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=1`).Scan(&adminUsage); err != nil || adminUsage != 500 {
		t.Fatalf("admin usage=%d err=%v", adminUsage, err)
	}
	if denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPost, "/api/admin/storage/recalculate", ""); denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin recalculate=%d %s", denied.Code, denied.Body.String())
	}
}

func TestAdminRoleChangesAreValidatedAndAudited(t *testing.T) {
	db, mux := testHandler(t)
	admin := testPrincipal{ID: 1}
	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPut, "/api/admin/users/2/admin", `{"isAdmin":true}`)
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("non-admin mutation = %d %s", denied.Code, denied.Body.String())
	}
	if got := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/1/admin", `{"isAdmin":false}`); got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "SELF_DEMOTE") {
		t.Fatalf("self demotion = %d %s", got.Code, got.Body.String())
	}
	for _, body := range []string{`{}`, `{"isAdmin":"true"}`, `{"isAdmin":1}`} {
		if got := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/admin", body); got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "INVALID_VALUE") {
			t.Fatalf("invalid admin value %s = %d %s", body, got.Code, got.Body.String())
		}
	}
	granted := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/admin", `{"isAdmin":true}`)
	if granted.Code != http.StatusOK || !strings.Contains(granted.Body.String(), `"isAdmin":true`) {
		t.Fatalf("grant admin = %d %s", granted.Code, granted.Body.String())
	}
	revoked := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/admin", `{"isAdmin":false}`)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), `"isAdmin":false`) {
		t.Fatalf("revoke admin = %d %s", revoked.Code, revoked.Body.String())
	}
	rows, err := db.W.Query(`SELECT action,actor_user_id,subject_user_id,detail_json FROM access_audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, detail string
		var actorID, subjectID int64
		if err = rows.Scan(&action, &actorID, &subjectID, &detail); err != nil {
			t.Fatal(err)
		}
		if actorID != 1 || subjectID != 2 || !strings.Contains(detail, `"source":"app"`) {
			t.Fatalf("unexpected admin audit actor=%d subject=%d detail=%s", actorID, subjectID, detail)
		}
		actions = append(actions, action)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0] != "admin.grant" || actions[1] != "admin.revoke" {
		t.Fatalf("actions=%v", actions)
	}
}

func TestDisableRevokesSessionsDevicesAndOAuthAtomically(t *testing.T) {
	db, mux := testHandler(t)
	_, err := db.W.Exec(`INSERT INTO sessions(sid,sess,expire) VALUES('session-two','{"userId":2}',datetime('now','+1 day'))`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash) VALUES(2,'session-hash-two')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(2,'Desktop','linux',?)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO device_authorizations(device_code_hash,user_code,name,platform,status,user_id,expires_at) VALUES(?,?,?,?, 'approved',2,datetime('now','+10 minutes'))`, strings.Repeat("b", 64), "ABCD-EFGH", "Desktop", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO device_pairings(user_id,code_hash,platform,expires_at) VALUES(2,?,'linux',datetime('now','+10 minutes'))`, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO user_qr_login_challenges(id,poll_token_hash,approve_secret_hash,status,approved_by_user_id,expires_at) VALUES('challenge-two',?,?, 'approved',2,datetime('now','+10 minutes'))`, strings.Repeat("d", 64), strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('client-two','companion:neoagent','NeoAgent','http://localhost:3333/callback')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('client-two',2,?,?,datetime('now','+1 hour'))`, strings.Repeat("f", 64), `[]`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO oauth_refresh_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('client-two',2,?,?,datetime('now','+1 day'))`, strings.Repeat("g", 64), `[]`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO oauth_codes(client_id,user_id,code_hash,redirect_uri,scopes_json,code_challenge,expires_at) VALUES('client-two',2,?,'http://localhost:3333/callback','[]',?,datetime('now','+10 minutes'))`, strings.Repeat("h", 64), strings.Repeat("i", 43)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO external_mounts(id,user_id,name,provider,config_enc) VALUES('mount-two',2,'Remote','webdav','encrypted')`); err != nil {
		t.Fatal(err)
	}
	self := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/1/disabled", `{"disabled":true}`)
	if self.Code != http.StatusBadRequest || !strings.Contains(self.Body.String(), "CANNOT_DISABLE_SELF") {
		t.Fatalf("self disable = %d %s", self.Code, self.Body.String())
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/disabled", `{"disabled":true}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"disabled":true`) {
		t.Fatalf("disable user = %d %s", response.Code, response.Body.String())
	}
	var sessions, activeSessions, activeDevices, approvedCodes, pendingPairings, activeAccess, activeRefresh, activeCodes, mounts int
	queries := []struct {
		query string
		out   *int
	}{
		{`SELECT COUNT(*) FROM sessions WHERE sid='session-two'`, &sessions},
		{`SELECT COUNT(*) FROM user_sessions WHERE user_id=2 AND revoked_at IS NULL`, &activeSessions},
		{`SELECT COUNT(*) FROM device_tokens WHERE user_id=2 AND revoked_at IS NULL`, &activeDevices},
		{`SELECT COUNT(*) FROM device_authorizations WHERE user_id=2 AND status='approved'`, &approvedCodes},
		{`SELECT COUNT(*) FROM device_pairings WHERE user_id=2 AND status='pending' AND datetime(expires_at)>datetime('now')`, &pendingPairings},
		{`SELECT COUNT(*) FROM oauth_access_tokens WHERE user_id=2 AND revoked_at IS NULL`, &activeAccess},
		{`SELECT COUNT(*) FROM oauth_refresh_tokens WHERE user_id=2 AND revoked_at IS NULL`, &activeRefresh},
		{`SELECT COUNT(*) FROM oauth_codes WHERE user_id=2 AND consumed_at IS NULL`, &activeCodes},
		{`SELECT COUNT(*) FROM external_mounts WHERE user_id=2`, &mounts},
	}
	for _, query := range queries {
		if err = db.W.QueryRow(query.query).Scan(query.out); err != nil {
			t.Fatal(err)
		}
	}
	if sessions != 0 || activeSessions != 0 || activeDevices != 0 || approvedCodes != 0 || pendingPairings != 0 || activeAccess != 0 || activeRefresh != 0 || activeCodes != 0 || mounts != 0 {
		t.Fatalf("disable left active access: sessions=%d userSessions=%d devices=%d auth=%d pairings=%d oauth=%d/%d/%d mounts=%d", sessions, activeSessions, activeDevices, approvedCodes, pendingPairings, activeAccess, activeRefresh, activeCodes, mounts)
	}
	var qrStatus string
	if err = db.W.QueryRow(`SELECT status FROM user_qr_login_challenges WHERE id='challenge-two'`).Scan(&qrStatus); err != nil || qrStatus != "declined" {
		t.Fatalf("QR challenge status=%q err=%v", qrStatus, err)
	}
	var action string
	if err = db.W.QueryRow(`SELECT action FROM access_audit_log ORDER BY id DESC LIMIT 1`).Scan(&action); err != nil || action != "account.disable" {
		t.Fatalf("disable audit action=%q err=%v", action, err)
	}
	enabled := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/disabled", `{"disabled":false}`)
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"disabled":false`) {
		t.Fatalf("enable user = %d %s", enabled.Code, enabled.Body.String())
	}
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM device_tokens WHERE user_id=2 AND revoked_at IS NULL`).Scan(&activeDevices); err != nil || activeDevices != 0 {
		t.Fatalf("enabling restored device tokens=%d err=%v", activeDevices, err)
	}
	if err = db.W.QueryRow(`SELECT action FROM access_audit_log ORDER BY id DESC LIMIT 1`).Scan(&action); err != nil || action != "account.enable" {
		t.Fatalf("enable audit action=%q err=%v", action, err)
	}
}

func TestDisableRollsBackAccountAndAuditWhenRevocationFails(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO sessions(sid,sess,expire) VALUES('session-two','{"userId":2}',datetime('now','+1 day'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash) VALUES(2,'session-hash-two')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES('client-two','companion:neoagent','NeoAgent','http://localhost:3333/callback')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO oauth_access_tokens(client_id,user_id,token_hash,scopes_json,expires_at) VALUES('client-two',2,?,?,datetime('now','+1 hour'))`, strings.Repeat("j", 64), `[]`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`CREATE TRIGGER fail_oauth_revocation BEFORE UPDATE ON oauth_access_tokens BEGIN SELECT RAISE(ABORT,'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/disabled", `{"disabled":true}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("disable with revocation failure = %d %s", response.Code, response.Body.String())
	}
	var disabled sql.NullString
	if err := db.W.QueryRow(`SELECT disabled_at FROM users WHERE id=2`).Scan(&disabled); err != nil || disabled.Valid {
		t.Fatalf("account disabled after rollback: %#v err=%v", disabled, err)
	}
	var sessions, audits, activeSession, activeToken int
	for _, query := range []struct {
		query string
		out   *int
	}{
		{`SELECT COUNT(*) FROM sessions WHERE sid='session-two'`, &sessions},
		{`SELECT COUNT(*) FROM access_audit_log WHERE action='account.disable'`, &audits},
		{`SELECT COUNT(*) FROM user_sessions WHERE user_id=2 AND revoked_at IS NULL`, &activeSession},
		{`SELECT COUNT(*) FROM oauth_access_tokens WHERE user_id=2 AND revoked_at IS NULL`, &activeToken},
	} {
		if err := db.W.QueryRow(query.query).Scan(query.out); err != nil {
			t.Fatal(err)
		}
	}
	if sessions != 1 || audits != 0 || activeSession != 1 || activeToken != 1 {
		t.Fatalf("partial disable persisted: sessions=%d audits=%d activeSessions=%d activeTokens=%d", sessions, audits, activeSession, activeToken)
	}
}
