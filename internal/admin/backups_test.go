package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

func TestBackupTargetConfigurationEncryptsSecretsAndPreservesBlankUpdates(t *testing.T) {
	db, mux := testHandler(t)
	created := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"archive","type":"sftp","config":{"host":"backup.example.test","port":22,"username":"cloud","password":"hidden-password","path":"/backups"},"schedule":"weekly","weekday":2,"hour":4,"retention":12}`)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "hidden-password") || !strings.Contains(created.Body.String(), `"passwordSet":true`) {
		t.Fatalf("secret leaked or missing presence flag: %s", created.Body.String())
	}
	var id int64
	var encrypted string
	if err := db.W.QueryRow(`SELECT id,config_enc FROM backup_targets WHERE name='archive'`).Scan(&id, &encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "hidden-password") || !strings.HasPrefix(encrypted, "enc:v1:") {
		t.Fatalf("backup config is not encrypted: %q", encrypted)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := keys.DecryptValue(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "hidden-password") {
		t.Fatalf("stored config lost secret: %s", plain)
	}
	update := adminRequest(mux, testPrincipal{ID: 1}, "PUT", "/api/admin/backups/targets/"+itoa(id), `{"config":{"host":"backup2.example.test","port":22,"username":"cloud","password":"","path":"/backups"},"enabled":false}`)
	if update.Code != 200 {
		t.Fatalf("update=%d %s", update.Code, update.Body.String())
	}
	if strings.Contains(update.Body.String(), "hidden-password") || !strings.Contains(update.Body.String(), `"passwordSet":true`) || !strings.Contains(update.Body.String(), `"enabled":false`) {
		t.Fatalf("update response=%s", update.Body.String())
	}
	if err := db.W.QueryRow(`SELECT config_enc FROM backup_targets WHERE id=?`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plain, err = keys.DecryptValue(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "hidden-password") || !strings.Contains(plain, "backup2.example.test") {
		t.Fatalf("blank update did not retain secret: %s", plain)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_runs(target_id,status,snapshot,bytes_uploaded,blobs_uploaded,error) VALUES(?,'failed','snapshot-1',4,1,'offline')`, id); err != nil {
		t.Fatal(err)
	}
	listed := adminRequest(mux, testPrincipal{ID: 1}, "GET", "/api/admin/backups", "")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"status":"failed"`) || !strings.Contains(listed.Body.String(), `"bytesUploaded":4`) {
		t.Fatalf("list=%d %s", listed.Code, listed.Body.String())
	}
	var auditCount int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action LIKE 'backup.target.%'`).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
}

func TestBackupTargetTestSaysWhetherConnectingOrWritingFailed(t *testing.T) {
	db, _ := testHandler(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	verifications := map[int64]any{}
	h, err := New(Options{
		DB: db.W, Keys: keys,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
		OnBackupTargetTest: func(_ context.Context, _ string, config map[string]any) (map[string]any, error) {
			return config, failure
		},
		OnBackupTargetVerify: func(context.Context, string, map[string]any) any { return map[string]any{"ok": true} },
		OnBackupVerified:     func(id int64, result any) error { verifications[id] = result; return nil },
		OnBackupVerification: func(id int64) (any, error) { return verifications[id], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	created := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"archive","type":"local","config":{"path":`+mustJSON(t, t.TempDir())+`}}`)
	if created.Code != 201 || !strings.Contains(created.Body.String(), `"lastVerification":null`) {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	base := "/api/admin/backups/targets/1"
	for code, cause := range map[string]error{
		"BACKUP_CONNECT_FAILED": fmt.Errorf("%w: %w", ErrBackupConnect, errors.New("connection refused")),
		"BACKUP_WRITE_FAILED":   fmt.Errorf("%w: %w", ErrBackupWrite, errors.New("read-only")),
	} {
		failure = cause
		if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/test", "{}"); response.Code != 400 || !strings.Contains(response.Body.String(), code) {
			t.Fatalf("test=%d %s, want %s", response.Code, response.Body.String(), code)
		}
	}
	failure = nil
	if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/test", "{}"); response.Code != 200 {
		t.Fatalf("test=%d %s", response.Code, response.Body.String())
	}
	adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/verify", "{}")
	if listed := adminRequest(mux, testPrincipal{ID: 1}, "GET", "/api/admin/backups", ""); !strings.Contains(listed.Body.String(), `"lastVerification":{"ok":true}`) {
		t.Fatalf("verification not kept: %s", listed.Body.String())
	}
	if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets/999/test", "{}"); response.Code != 404 {
		t.Fatalf("missing target=%d %s", response.Code, response.Body.String())
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBackupTargetAuthorizationValidationAndAuditRollback(t *testing.T) {
	db, mux := testHandler(t)
	request := `{"name":"archive","type":"local","config":{"path":"/tmp/archive"}}`
	if denied := adminRequest(mux, testPrincipal{ID: 2}, "POST", "/api/admin/backups/targets", request); denied.Code != 403 {
		t.Fatalf("non-admin create=%d %s", denied.Code, denied.Body.String())
	}
	if invalid := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"remote","type":"webdav","config":{"url":"file:///etc/passwd"}}`); invalid.Code != 400 {
		t.Fatalf("invalid URL=%d %s", invalid.Code, invalid.Body.String())
	}
	if invalid := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"archive","type":"local","config":{"path":"/tmp/archive"},"retention":0}`); invalid.Code != 400 {
		t.Fatalf("invalid plan=%d %s", invalid.Code, invalid.Body.String())
	}
	if _, err := db.W.Exec(`CREATE TRIGGER fail_backup_audit BEFORE INSERT ON access_audit_log WHEN NEW.action LIKE 'backup.target.%' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	failed := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", request)
	if failed.Code != 500 {
		t.Fatalf("audit failure=%d %s", failed.Code, failed.Body.String())
	}
	var count int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM backup_targets`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("target persisted despite audit failure: count=%d err=%v", count, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(failed.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestStorageClassMigrationRouteValidatesAndExposesTargetProgress(t *testing.T) {
	db, _ := testHandler(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	principal := func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: p.ID, Device: p.Device}, ok
	}
	starts := 0
	h, err := New(Options{
		DB: db.W, Keys: keys, ResolvePrincipal: principal,
		OnStorageClassMigrationStart: func(id int64, config map[string]any) error {
			starts++
			if id != 1 || config["storageClass"] != "STANDARD_IA" || config["secretAccessKey"] != "secret" {
				t.Fatalf("migration target=%d config=%v", id, config)
			}
			return nil
		},
		OnStorageClassMigrationStatus: func(int64) any {
			return map[string]any{"status": "running", "objectsDone": 1, "objectsTotal": 2}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	created := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/backups/targets", `{"name":"archive","type":"s3","config":{"region":"us-east-1","bucket":"archive","accessKeyId":"access","secretAccessKey":"secret","endpoint":"https://s3.example.test","storageClass":"STANDARD_IA"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create target=%d %s", created.Code, created.Body.String())
	}
	path := "/api/admin/backups/targets/1/storage-class/migrate"
	if denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPost, path, "{}"); denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin migration=%d %s", denied.Code, denied.Body.String())
	}
	started := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, path, "{}")
	if started.Code != http.StatusAccepted || !strings.Contains(started.Body.String(), `"started":true`) || starts != 1 {
		t.Fatalf("migration start=%d body=%s starts=%d", started.Code, started.Body.String(), starts)
	}
	listed := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/backups", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"storageMigration":{"objectsDone":1,"objectsTotal":2,"status":"running"}`) {
		t.Fatalf("migration status absent from target list: %d %s", listed.Code, listed.Body.String())
	}
	if _, err := db.W.Exec(`UPDATE backup_targets SET type='local' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	notS3 := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, path, "{}")
	if notS3.Code != http.StatusBadRequest || !strings.Contains(notS3.Body.String(), `"code":"BACKUP_NOT_S3"`) {
		t.Fatalf("non-S3 migration=%d %s", notS3.Code, notS3.Body.String())
	}
}

func TestRemoteBackupTargetOperationsUseDecryptedTargetCallbacks(t *testing.T) {
	db, _ := testHandler(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	principal := func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
	}
	testCalls, runCalls, listCalls, verifyCalls := 0, 0, 0, 0
	h, err := New(Options{
		DB: db.W, Keys: keys, ResolvePrincipal: principal,
		OnBackupTargetTest: func(_ context.Context, typ string, config map[string]any) (map[string]any, error) {
			testCalls++
			if typ != "webdav" || config["url"] != "https://backup.example.test/dav" || config["password"] != "secret" {
				t.Fatalf("test callback target=%q config=%v", typ, config)
			}
			return config, nil
		},
		OnBackupTargetRun: func(id int64, typ string, config map[string]any, retention int) (int64, error) {
			runCalls++
			if id < 1 || typ != "webdav" || config["url"] != "https://backup.example.test/dav" || retention != 7 {
				t.Fatalf("run callback id=%d type=%q config=%v retention=%d", id, typ, config, retention)
			}
			return 91, nil
		},
		OnBackupTargetSnapshots: func(_ context.Context, typ string, config map[string]any) ([]string, error) {
			listCalls++
			if typ != "webdav" {
				t.Fatalf("snapshot callback type=%q", typ)
			}
			return []string{"snapshot-1"}, nil
		},
		OnBackupTargetVerify: func(_ context.Context, typ string, config map[string]any) any {
			verifyCalls++
			return map[string]any{"ok": typ == "webdav", "snapshot": "snapshot-1"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	created := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"remote","type":"webdav","config":{"url":"https://backup.example.test/dav","username":"cloud","password":"secret"},"retention":7}`)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var id int64
	if err = db.W.QueryRow(`SELECT id FROM backup_targets WHERE name='remote'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	base := "/api/admin/backups/targets/" + itoa(id)
	if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/test", "{}"); response.Code != 200 || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("test target=%d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/run", "{}"); response.Code != 202 || !strings.Contains(response.Body.String(), `"runId":91`) {
		t.Fatalf("run target=%d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(mux, testPrincipal{ID: 1}, "GET", base+"/snapshots", ""); response.Code != 200 || !strings.Contains(response.Body.String(), "snapshot-1") {
		t.Fatalf("list target=%d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/verify", "{}"); response.Code != 200 || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("verify target=%d %s", response.Code, response.Body.String())
	}
	if testCalls != 1 || runCalls != 1 || listCalls != 1 || verifyCalls != 1 {
		t.Fatalf("callback counts test=%d run=%d list=%d verify=%d", testCalls, runCalls, listCalls, verifyCalls)
	}
}

func TestSFTPBackupTestPinsHostKeyAndHostChangeClearsPin(t *testing.T) {
	db, _ := testHandler(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	principal := func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
	}
	fingerprint := "SHA256:KJx80a7qz6zO6d9pd8nWjz7m9e9S5fUmh4rG5wZsQ2A"
	h, err := New(Options{
		DB: db.W, Keys: keys, ResolvePrincipal: principal,
		OnBackupTargetTest: func(_ context.Context, typ string, config map[string]any) (map[string]any, error) {
			if typ != "sftp" {
				t.Fatalf("target type=%q", typ)
			}
			verified := cloneBackupConfig(config)
			verified["hostKey"] = fingerprint
			return verified, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	created := adminRequest(mux, testPrincipal{ID: 1}, "POST", "/api/admin/backups/targets", `{"name":"sftp","type":"sftp","config":{"host":"backup.example.test","port":22,"username":"backup","password":"password","path":"/archives"}}`)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var id int64
	var encrypted string
	if err = db.W.QueryRow(`SELECT id,config_enc FROM backup_targets WHERE name='sftp'`).Scan(&id, &encrypted); err != nil {
		t.Fatal(err)
	}
	base := "/api/admin/backups/targets/" + itoa(id)
	verified := adminRequest(mux, testPrincipal{ID: 1}, "POST", base+"/test", "{}")
	if verified.Code != 200 || !strings.Contains(verified.Body.String(), fingerprint) {
		t.Fatalf("verify=%d %s", verified.Code, verified.Body.String())
	}
	if err = db.W.QueryRow(`SELECT config_enc FROM backup_targets WHERE id=?`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plain, err := keys.DecryptValue(encrypted)
	if err != nil || !strings.Contains(plain, fingerprint) {
		t.Fatalf("stored fingerprint=%s err=%v", plain, err)
	}
	listed := adminRequest(mux, testPrincipal{ID: 1}, "GET", "/api/admin/backups", "")
	if listed.Code != 200 || strings.Contains(listed.Body.String(), fingerprint) || strings.Contains(listed.Body.String(), `"hostKey"`) {
		t.Fatalf("backup list leaked pinned host key: %d %s", listed.Code, listed.Body.String())
	}
	updated := adminRequest(mux, testPrincipal{ID: 1}, "PUT", base, `{"config":{"host":"backup2.example.test","port":22,"username":"backup","password":"","path":"/archives"}}`)
	if updated.Code != 200 {
		t.Fatalf("host update=%d %s", updated.Code, updated.Body.String())
	}
	if err = db.W.QueryRow(`SELECT config_enc FROM backup_targets WHERE id=?`, id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plain, err = keys.DecryptValue(encrypted)
	if err != nil || strings.Contains(plain, fingerprint) {
		t.Fatalf("host change retained old fingerprint: %s err=%v", plain, err)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
