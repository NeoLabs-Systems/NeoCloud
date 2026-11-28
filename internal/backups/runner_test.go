package backups

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestLocalRunCreatesNodeCompatibleEncryptedSnapshotAndCopiesBlobs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataDir, blobsDir, tmpDir, destination := filepath.Join(root, "data"), filepath.Join(root, "blobs"), filepath.Join(root, "tmp"), filepath.Join(root, "backup")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := dbx.Open(ctx, filepath.Join(dataDir, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = schema.Migrate(ctx, db, filepath.Join(root, "migration-backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','x');
		INSERT INTO backup_targets(id,name,type,config_enc) VALUES(1,'local','local','sealed');
		INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,?,?,1,1)`, "123e4567-e89b-12d3-a456-426614174001", 1, 20, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	blobID := "123e4567-e89b-12d3-a456-426614174001"
	source := filepath.Join(blobsDir, "1", blobID[:2], blobID)
	if err = os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	blobFile, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.EncryptBlob(blobFile, strings.NewReader("actual content in the source blob"), bytes.Repeat([]byte{7}, 32), blobID); err != nil {
		t.Fatal(err)
	}
	if err = blobFile.Close(); err != nil {
		t.Fatal(err)
	}
	runner, err := New(db, keys, blobsDir, tmpDir, filepath.Join(root, "backups"), "test-version")
	if err != nil {
		t.Fatal(err)
	}
	runner.Start(ctx)
	defer runner.Close()
	if err = os.Mkdir(destination, 0750); err != nil {
		t.Fatal(err)
	}
	runID, err := runner.StartRun(1, destination, 4)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, db.W, runID)
	destinationInfo, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if destinationInfo.Mode().Perm() != 0750 {
		t.Fatalf("backup target permissions=%v err=%v", destinationInfo.Mode().Perm(), err)
	}

	var snapshot string
	var status string
	if err = db.W.QueryRow(`SELECT snapshot,status FROM backup_runs WHERE id=?`, runID).Scan(&snapshot, &status); err != nil || status != "ok" {
		t.Fatalf("backup status=%q snapshot=%q err=%v", status, snapshot, err)
	}
	manifestPath := filepath.Join(destination, "snapshots", snapshot, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.Format != 2 || manifest.DatabaseSize < 1 || len(manifest.Blobs) != 1 || manifest.Blobs[0] != "1/"+blobID {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	backupBlob := filepath.Join(destination, "blobs", "1", blobID)
	localBytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	remoteBytes, err := os.ReadFile(backupBlob)
	if err != nil || !bytes.Equal(remoteBytes, localBytes) {
		t.Fatalf("backup blob mismatch err=%v", err)
	}
	listed, err := ListSnapshots(destination)
	if err != nil || len(listed) != 1 || listed[0] != snapshot {
		t.Fatalf("listed snapshots=%v err=%v", listed, err)
	}
	verified := runner.VerifySnapshot(destination)
	if !verified.OK || verified.Database != "ok" || verified.BlobsChecked != 1 || verified.MissingBlobs != 0 {
		t.Fatalf("snapshot verification=%+v", verified)
	}
	manifest.Blobs = []string{}
	invalidManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifestPath, invalidManifest, 0600); err != nil {
		t.Fatal(err)
	}
	verified = runner.VerifySnapshot(destination)
	if verified.OK || verified.Database != "failed" {
		t.Fatalf("manifest/database mismatch was not reported: %+v", verified)
	}
	if err = os.WriteFile(manifestPath, manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(backupBlob); err != nil {
		t.Fatal(err)
	}
	verified = runner.VerifySnapshot(destination)
	if verified.OK || verified.MissingBlobs != 1 {
		t.Fatalf("missing blob was not reported: %+v", verified)
	}
	if err = os.WriteFile(backupBlob, remoteBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	backupKey, err := keys.BackupKey()
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(destination, "snapshots", snapshot, "neocloud.db.enc")
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.DecryptBlobRange(bytes.NewReader(mustRead(t, databasePath)), manifest.DatabaseSize, 0, manifest.DatabaseSize-1, backupKey, "snapshot:"+snapshot, &plain); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(tmpDir, "restored.db")
	if err = os.WriteFile(copyPath, plain.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	copyDB, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var user string
	if err = copyDB.QueryRow(`SELECT username FROM users WHERE id=1`).Scan(&user); err != nil || user != "owner" {
		t.Fatalf("snapshot database user=%q err=%v", user, err)
	}
	if err = copyDB.Close(); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(destination, "blobs", "1", "123e4567-e89b-12d3-a456-426614174099")
	if err = os.WriteFile(orphan, []byte("unreferenced"), 0600); err != nil {
		t.Fatal(err)
	}
	secondID, err := startRunRetry(t, runner, 1, destination, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, db.W, secondID)
	var secondSnapshot string
	if err = db.W.QueryRow(`SELECT snapshot FROM backup_runs WHERE id=?`, secondID).Scan(&secondSnapshot); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(destination, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != secondSnapshot {
		t.Fatalf("retained snapshots=%v want only %q", entries, secondSnapshot)
	}
	if _, err = os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced blob remains, stat err=%v", err)
	}
	if _, err = os.Stat(filepath.Join(destination, "blobs", "1", blobID)); err != nil {
		t.Fatalf("retained snapshot lost referenced blob: %v", err)
	}
}

func TestBackupTargetRunIsExcludedAcrossRunnerInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = schema.Migrate(ctx, db, filepath.Join(root, "migration-backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc) VALUES(1,'local','local','sealed')`); err != nil {
		t.Fatal(err)
	}
	secondDB, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()
	keys, err := cryptox.NewKeyring(strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := filepath.Join(root, "tmp")
	if err = os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := New(db, keys, filepath.Join(root, "blobs"), tmpDir, filepath.Join(root, "backups"), "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(secondDB, keys, filepath.Join(root, "blobs"), tmpDir, filepath.Join(root, "backups"), "test")
	if err != nil {
		t.Fatal(err)
	}
	first.Start(ctx)
	second.Start(ctx)
	defer second.Close()
	started := make(chan struct{}, 1)
	firstTarget := &blockedBackupTarget{started: started}
	if _, err = first.startTargetRun(1, firstTarget, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("first runner never entered the active target")
	}
	if _, err = second.startTargetRun(1, &blockedBackupTarget{started: make(chan struct{}, 1)}, 1); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second runner error=%v, want ErrAlreadyRunning", err)
	}
	first.Close()
	var runs int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM backup_runs WHERE target_id=1`).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("backup run count=%d err=%v, want one persisted run", runs, err)
	}
}

type blockedBackupTarget struct {
	started chan<- struct{}
}

func (t *blockedBackupTarget) Open(ctx context.Context) error {
	select {
	case t.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}
func (*blockedBackupTarget) Close() error                                   { return nil }
func (*blockedBackupTarget) Exists(context.Context, string) (bool, error)   { return false, nil }
func (*blockedBackupTarget) List(context.Context, string) ([]string, error) { return nil, nil }
func (*blockedBackupTarget) Put(context.Context, string, string) error      { return nil }
func (*blockedBackupTarget) Get(context.Context, string, string) error      { return nil }
func (*blockedBackupTarget) Delete(context.Context, string) error           { return nil }
func (*blockedBackupTarget) DeletePrefix(context.Context, string) error     { return nil }

func waitRun(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRow(`SELECT status FROM backup_runs WHERE id=?`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "ok" {
			return
		}
		if status == "failed" {
			var message sql.NullString
			_ = db.QueryRow(`SELECT error FROM backup_runs WHERE id=?`, id).Scan(&message)
			t.Fatalf("backup failed: %s", message.String)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("backup did not finish before timeout")
}

func startRunRetry(t *testing.T, runner *Runner, targetID int64, destination string, retention int) (int64, error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		id, err := runner.StartRun(targetID, destination, retention)
		if !errors.Is(err, ErrAlreadyRunning) {
			return id, err
		}
		if time.Now().After(deadline) {
			return 0, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
