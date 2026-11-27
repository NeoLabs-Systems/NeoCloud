package files

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOfficeFileStoreRechecksMemberGrantAndSavesThroughVersionedUpload(t *testing.T) {
	f := newZIPFixture(t)
	f.handler = New(Options{DB: f.db.W, Keys: f.keys, BlobDir: f.blobDir, UploadTmpDir: t.TempDir(), DefaultQuotaBytes: 1 << 20, MaxUploadBytes: 1 << 20, VersionRetention: 5})
	f.addRoot(t, "office-root", 1)
	f.addFolder(t, "office-folder", stringPtr("office-root"), 1, "Documents", nil)
	const officeDocID = "00000000-0000-4000-8000-000000000099"
	f.addFile(t, officeDocID, "office-folder", 1, "agenda.docx", []byte("old"))
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,grantee_id,node_id,permission,recipient) VALUES(1,2,'office-folder','read','member@example.test')`); err != nil {
		t.Fatal(err)
	}
	member, err := f.handler.OpenOfficeFile(context.Background(), 2, officeDocID, true)
	if err != nil {
		t.Fatal(err)
	}
	if member.CanWrite || member.OwnerID != 1 || member.UserName != "zip-member" || member.Name != "agenda.docx" {
		t.Fatalf("read share metadata=%+v", member)
	}
	if _, err = f.handler.SaveOfficeFile(context.Background(), 2, officeDocID, member.ETag, bytes.NewReader([]byte("edited")), 6, ""); err == nil {
		t.Fatal("read-only share was allowed to save")
	}
	if _, err = f.db.W.Exec(`UPDATE user_shares SET permission='write' WHERE owner_id=1 AND grantee_id=2 AND node_id='office-folder'`); err != nil {
		t.Fatal(err)
	}
	member, err = f.handler.OpenOfficeFile(context.Background(), 2, officeDocID, true)
	if err != nil || !member.CanWrite {
		t.Fatalf("write share metadata=%+v err=%v", member, err)
	}
	saved, err := f.handler.SaveOfficeFile(context.Background(), 2, officeDocID, member.ETag, bytes.NewReader([]byte("edited")), 6, "")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 2 || saved.ETag == member.ETag || saved.Size != 6 {
		t.Fatalf("saved metadata=%+v", saved)
	}
	content := httptest.NewRecorder()
	f.handler.ServeOfficeFile(content, httptest.NewRequest(http.MethodGet, "/wopi/files/"+officeDocID+"/contents", nil), 2, officeDocID)
	read, err := f.handler.OpenOfficeFile(context.Background(), 2, officeDocID, true)
	if err != nil || read.Version != 2 || content.Body.String() != "edited" {
		t.Fatalf("read after save meta=%+v content=%q err=%v", read, content.Body.String(), err)
	}
	var author int64
	if err = f.db.W.QueryRow(`SELECT modified_by FROM nodes WHERE id=?`, officeDocID).Scan(&author); err != nil || author != 2 {
		t.Fatalf("author of the saved content=%d err=%v", author, err)
	}
	if _, err = f.handler.SaveOfficeFile(context.Background(), 2, officeDocID, read.ETag, bytes.NewReader([]byte("blocked")), 7, "stale-lock"); codeOf(err) != "WOPI_LOCK_MISMATCH" {
		t.Fatalf("save without current WOPI lock error=%v", err)
	}
	var versionAfterRejectedSave int64
	if err = f.db.W.QueryRow(`SELECT version FROM nodes WHERE id=?`, officeDocID).Scan(&versionAfterRejectedSave); err != nil || versionAfterRejectedSave != 2 {
		t.Fatalf("rejected WOPI save changed version=%d err=%v", versionAfterRejectedSave, err)
	}
	if _, err = f.db.W.Exec(`INSERT INTO wopi_locks(node_id,lock_value,expires_at) VALUES(?,?,?)`, officeDocID, "live-lock", time.Now().Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	saved, err = f.handler.SaveOfficeFile(context.Background(), 2, officeDocID, read.ETag, bytes.NewReader([]byte("locked edit")), 11, "live-lock")
	if err != nil || saved.Version != 3 {
		t.Fatalf("save with current WOPI lock metadata=%+v err=%v", saved, err)
	}
	var previous int
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM file_versions WHERE node_id=?`, officeDocID).Scan(&previous); err != nil || previous != 2 {
		t.Fatalf("version history count=%d err=%v", previous, err)
	}
	var journal int
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE user_id=1 AND node_id=? AND op='update'`, officeDocID).Scan(&journal); err != nil || journal != 2 {
		t.Fatalf("sync journal update count=%d err=%v", journal, err)
	}
}

func codeOf(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	return ""
}

func stringPtr(value string) *string { return &value }
