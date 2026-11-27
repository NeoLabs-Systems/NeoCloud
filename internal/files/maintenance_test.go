package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPurgeExpiredTrashUsesNormalReferenceAndJournalPath(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.trashRetentionDays = 30
	root := "60000000-0000-4000-8000-000000000001"
	fileID := "60000000-0000-4000-8000-000000000002"
	f.addRoot(t, root, 1)
	f.addFile(t, fileID, root, 1, "expired.txt", []byte("expire me"))
	if _, err := f.db.W.Exec(`UPDATE nodes SET trashed_at=datetime('now','-31 days') WHERE id=?`, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.W.Exec(`UPDATE users SET used_bytes=9 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := f.handler.PurgeExpiredTrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	var nodes, blobs, journal int
	var used int64
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id=?`, fileID).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM blobs WHERE id=?`, fileID).Scan(&blobs); err != nil {
		t.Fatal(err)
	}
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id=? AND op='delete'`, fileID).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if err := f.db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=1`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if nodes != 0 || blobs != 0 || journal != 1 || used != 0 {
		t.Fatalf("expired trash state nodes=%d blobs=%d journal=%d used=%d", nodes, blobs, journal, used)
	}
	if _, err := os.Stat(blobPath(f.blobDir, 1, fileID)); !os.IsNotExist(err) {
		t.Fatalf("expired blob remains or stat failed: %v", err)
	}
}

func TestPurgeExpiredUploadsPreservesCompletingAndLiveUploads(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.uploadTmpDir = t.TempDir()
	if _, err := f.db.W.Exec(`INSERT INTO upload_sessions(id,user_id,parent_id,name,conflict,total_size,chunk_size,expires_at) VALUES
		('expired',1,'root','old.bin','fail',4,4,datetime('now','-25 hours')),
		('completing',1,'root','busy.bin','fail',4,4,datetime('now','-25 hours')),
		('live',1,'root','live.bin','fail',4,4,datetime('now','+1 hour'))`); err != nil {
		t.Fatal(err)
	}
	parts := filepath.Join(f.handler.uploadTmpDir, "parts")
	for _, id := range []string{"expired", "completing", "live", "orphan"} {
		dir := filepath.Join(parts, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "0"), []byte("encrypted part"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale := filepath.Join(f.handler.uploadTmpDir, "upload-abandoned.part")
	if err := os.WriteFile(stale, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	f.handler.uploadsMu.Lock()
	f.handler.completingUploads["completing"] = true
	f.handler.uploadsMu.Unlock()
	if err := f.handler.PurgeExpiredUploads(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"expired", "orphan"} {
		if _, err := os.Stat(filepath.Join(parts, id)); !os.IsNotExist(err) {
			t.Errorf("parts for %s remain or stat failed: %v", id, err)
		}
	}
	for _, id := range []string{"completing", "live"} {
		if _, err := os.Stat(filepath.Join(parts, id, "0")); err != nil {
			t.Errorf("parts for %s were removed: %v", id, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temporary upload remains or stat failed: %v", err)
	}
	var expiredRows, liveRows int
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM upload_sessions WHERE id IN ('expired','completing')`).Scan(&expiredRows); err != nil {
		t.Fatal(err)
	}
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM upload_sessions WHERE id='live'`).Scan(&liveRows); err != nil {
		t.Fatal(err)
	}
	if expiredRows != 1 || liveRows != 1 {
		t.Fatalf("expired/completing rows=%d live rows=%d", expiredRows, liveRows)
	}
	f.handler.uploadsMu.Lock()
	delete(f.handler.completingUploads, "completing")
	f.handler.uploadsMu.Unlock()
	if err := f.handler.PurgeExpiredUploads(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parts, "completing")); !os.IsNotExist(err) {
		t.Fatalf("completed expired upload parts remain: %v", err)
	}
}
