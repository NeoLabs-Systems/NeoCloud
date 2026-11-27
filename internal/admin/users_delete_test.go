package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteUserProtectsAdminAndSelfAndLeavesDataIntact(t *testing.T) {
	db, mux := testHandler(t)
	admin := adminRequest(mux, testPrincipal{ID: 1}, http.MethodDelete, "/api/admin/users/1", "")
	if admin.Code != http.StatusForbidden || !strings.Contains(admin.Body.String(), "ADMIN_ACCOUNT") {
		t.Fatalf("delete admin=%d %s", admin.Code, admin.Body.String())
	}
	self := adminRequest(mux, testPrincipal{ID: 1}, http.MethodDelete, "/api/admin/users/1", "")
	if self.Code != http.StatusForbidden || !strings.Contains(self.Body.String(), "ADMIN_ACCOUNT") {
		t.Fatalf("delete self=%d %s", self.Code, self.Body.String())
	}
	var accounts int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&accounts); err != nil || accounts != 2 {
		t.Fatalf("accounts=%d err=%v", accounts, err)
	}
}

func TestDeleteUserCommitsAuditAndRemovesAccountStorage(t *testing.T) {
	db, mux, blobDir, previewDir, uploadDir := testHandlerWithStorage(t)
	if _, err := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES('abcdef',2,5,'checksum',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,blob_id,etag) VALUES
		('root-two',2,NULL,'folder','',0,NULL,'root-etag'),('file-two',2,'root-two','file','file.txt',5,'abcdef','file-etag')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO previews(blob_id,variant,user_id,size,mime) VALUES('abcdef','small',2,4,'image/jpeg')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO upload_sessions(id,user_id,parent_id,name,conflict,total_size,chunk_size,expires_at,created_by) VALUES('upload-1',2,'root-two','new.txt','rename',10,10,datetime('now','+1 hour'),2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash) VALUES(2,'hash-two')`); err != nil {
		t.Fatal(err)
	}
	blobPath := filepath.Join(blobDir, "2", "ab", "abcdef")
	previewPath := filepath.Join(previewDir, "2", "abcdef", "small")
	partPath := filepath.Join(uploadDir, "parts", "upload-1", "0")
	for _, path := range []string{blobPath, previewPath, partPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("stored"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodDelete, "/api/admin/users/2", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) {
		t.Fatalf("delete=%d %s", response.Code, response.Body.String())
	}
	var accounts, audits, remainingBlobs, remainingSessions int
	for query, destination := range map[string]*int{
		`SELECT COUNT(*) FROM users`: &accounts,
		`SELECT COUNT(*) FROM access_audit_log WHERE action='account.delete' AND actor_user_id=1 AND subject_user_id IS NULL AND detail_json='{"userId":2,"self":false}'`: &audits,
		`SELECT COUNT(*) FROM blobs WHERE user_id=2`:         &remainingBlobs,
		`SELECT COUNT(*) FROM user_sessions WHERE user_id=2`: &remainingSessions,
	} {
		if err := db.W.QueryRow(query).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if accounts != 1 || audits != 1 || remainingBlobs != 0 || remainingSessions != 0 {
		t.Fatalf("accounts=%d audits=%d blobs=%d sessions=%d", accounts, audits, remainingBlobs, remainingSessions)
	}
	for _, path := range []string{filepath.Join(blobDir, "2"), filepath.Join(previewDir, "2"), filepath.Join(uploadDir, "parts", "upload-1")} {
		assertPathAbsent(t, path)
	}
}

func TestDeleteUserRollbackDoesNotRemoveStorage(t *testing.T) {
	db, mux, blobDir, previewDir, uploadDir := testHandlerWithStorage(t)
	blobPath := filepath.Join(blobDir, "2", "ab", "abcdef")
	previewPath := filepath.Join(previewDir, "2", "abcdef", "small")
	partPath := filepath.Join(uploadDir, "parts", "upload-1", "0")
	for _, path := range []string{blobPath, previewPath, partPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("stored"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.W.Exec(`CREATE TRIGGER reject_account_delete_audit BEFORE INSERT ON access_audit_log WHEN NEW.action='account.delete' BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodDelete, "/api/admin/users/2", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("delete=%d %s", response.Code, response.Body.String())
	}
	var accounts int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&accounts); err != nil || accounts != 2 {
		t.Fatalf("accounts=%d err=%v", accounts, err)
	}
	for _, path := range []string{blobPath, previewPath, partPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("failed deletion removed storage %s: %v", path, err)
		}
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected path to be removed: %s (err=%v)", filepath.Clean(path), err)
	}
}
