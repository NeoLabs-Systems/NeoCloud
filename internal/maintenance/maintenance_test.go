package maintenance

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	_ "modernc.org/sqlite"
)

func TestRunOnceAppliesNodeRetentionAndRecordsDatabaseCheck(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "neocloud.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE user_sessions (id INTEGER PRIMARY KEY, revoked_at TEXT, expires_at TEXT);
		CREATE TABLE activity (id INTEGER PRIMARY KEY, at TEXT NOT NULL);
		INSERT INTO user_sessions (id,revoked_at,expires_at) VALUES
			(1,datetime('now','-31 days'),datetime('now','+1 day')),
			(2,NULL,datetime('now','-31 days')),
			(3,NULL,datetime('now','+1 day'));
		INSERT INTO activity (id,at) VALUES
			(1,datetime('now','-91 days')),
			(2,datetime('now','-89 days'));`); err != nil {
		t.Fatal(err)
	}
	runner := New(db, path)
	runner.Now = func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) }
	runner.RunOnce(ctx)
	var sessions, activity int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity`).Scan(&activity); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || activity != 1 {
		t.Fatalf("retained sessions=%d activity=%d; want 1 each", sessions, activity)
	}
	check := runner.Status()
	if check.Status != "ok" || check.CheckedAt == "" || check.DurationMS < 0 {
		t.Fatalf("database check status = %#v", check)
	}
}

func TestCanceledRunDoesNotStartMaintenanceWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := New(nil, "")
	runner.RunOnce(ctx)
	if got := runner.Status(); got.Status != "pending" {
		t.Fatalf("status after canceled run = %#v", got)
	}
}

func TestBlobScrubPersistsCursorAcrossRunnerRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "neocloud.db")
	db, err := dbx.Open(ctx, databasePath, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(root, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("4c", 32))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := keys.WrapDEK(1, bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.ExecContext(ctx, `INSERT INTO users(id,username,password,wrapped_dek) VALUES(1,'scrub-owner','x',?)`, wrapped); err != nil {
		t.Fatal(err)
	}
	blobDir := filepath.Join(root, "blobs")
	if err = os.MkdirAll(blobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	insert := func(id string) {
		t.Helper()
		if _, e := db.W.ExecContext(ctx, `INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,0,?,1,1)`, id, strings.Repeat("0", 64)); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < blobScrubMinSlice+1; i++ {
		insert(fmt.Sprintf("%036x", i+1))
	}
	first := New(db.W, databasePath)
	first.SetBlobScrubber(keys, blobDir)
	if err = first.blobScrubber.scrubSlice(ctx); err != nil {
		t.Fatal(err)
	}
	var cursor string
	if err = db.W.QueryRowContext(ctx, `SELECT value FROM maintenance_state WHERE key='maintenance.blob_scrub.cursor'`).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	var damaged int
	if err = db.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM blob_scrub_damaged`).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	if cursor == "" || damaged != blobScrubMinSlice {
		t.Fatalf("first pass cursor=%q damaged=%d", cursor, damaged)
	}
	// A fresh runner models an app restart: persisted state resumes after the
	// first 100 IDs and closes the cycle without rechecking the earlier slice.
	second := New(db.W, databasePath)
	second.SetBlobScrubber(keys, blobDir)
	if err = second.blobScrubber.scrubSlice(ctx); err != nil {
		t.Fatal(err)
	}
	status, lastCycle, err := second.ScrubStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status != blobScrubMinSlice+1 || lastCycle == nil {
		t.Fatalf("scrub status damaged=%d lastCycle=%v", status, lastCycle)
	}
	if err = db.W.QueryRowContext(ctx, `SELECT value FROM maintenance_state WHERE key='maintenance.blob_scrub.cursor'`).Scan(&cursor); err != nil || cursor != "" {
		t.Fatalf("completed cursor=%q error=%v", cursor, err)
	}
}

func TestBlobScrubAuthenticatesSegmentsAndClearsRepairedDamage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(root, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("7d", 32))
	if err != nil {
		t.Fatal(err)
	}
	dek := bytes.Repeat([]byte{0x72}, 32)
	wrapped, err := keys.WrapDEK(1, dek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.ExecContext(ctx, `INSERT INTO users(id,username,password,wrapped_dek) VALUES(1,'scrub-valid','x',?)`, wrapped); err != nil {
		t.Fatal(err)
	}
	blobID := "70000000-0000-4000-8000-000000000001"
	plain := []byte("authenticated blob")
	filePath := filepath.Join(root, "blobs", "1", blobID[:2], blobID)
	if err = os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeBlob := func() {
		t.Helper()
		file, openErr := os.Create(filePath)
		if openErr != nil {
			t.Fatal(openErr)
		}
		_, encryptErr := cryptox.EncryptBlob(file, bytes.NewReader(plain), dek, blobID)
		closeErr := file.Close()
		if encryptErr != nil {
			t.Fatal(encryptErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	writeBlob()
	if _, err = db.W.ExecContext(ctx, `INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,?, ?,1,1)`, blobID, len(plain), strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	runner := New(db.W, filepath.Join(root, "neocloud.db"))
	runner.SetBlobScrubber(keys, filepath.Join(root, "blobs"))
	if err = runner.blobScrubber.scrubSlice(ctx); err != nil {
		t.Fatal(err)
	}
	if count, _, statusErr := runner.ScrubStatus(); statusErr != nil || count != 0 {
		t.Fatalf("healthy blob scrub status=%d error=%v", count, statusErr)
	}
	file, err := os.OpenFile(filePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err == nil {
		var changed [1]byte
		_, err = file.ReadAt(changed[:], info.Size()-1)
		changed[0] ^= 0xff
		if err == nil {
			_, err = file.WriteAt(changed[:], info.Size()-1)
		}
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.blobScrubber.scrubSlice(ctx); err != nil {
		t.Fatal(err)
	}
	if count, _, statusErr := runner.ScrubStatus(); statusErr != nil || count != 1 {
		t.Fatalf("tampered blob scrub status=%d error=%v", count, statusErr)
	}
	writeBlob()
	if err = runner.blobScrubber.scrubSlice(ctx); err != nil {
		t.Fatal(err)
	}
	if count, _, statusErr := runner.ScrubStatus(); statusErr != nil || count != 0 {
		t.Fatalf("repaired blob scrub status=%d error=%v", count, statusErr)
	}
}
