package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

const restoreTestBlobID = "00112233-4455-6677-8899-aabbccddeeff"

func restoreFixture(t *testing.T, includeBackupBlob bool) (RestoreOptions, string, []byte) {
	t.Helper()
	root := t.TempDir()
	backupRoot := filepath.Join(root, "target")
	snapshotName := "2026-11-27T06-00-00Z"
	snapshotDir := filepath.Join(backupRoot, "snapshots", snapshotName)
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		t.Fatal(err)
	}
	databaseFile := filepath.Join(root, "install", "neocloud.db")
	if err := os.MkdirAll(filepath.Dir(databaseFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := createRestoreFixtureDB(databaseFile, "before"); err != nil {
		t.Fatal(err)
	}
	blobRoot := filepath.Join(root, "install", "blobs")
	pidFile := filepath.Join(root, "install", "neocloud.pid")
	master := strings.Repeat("17", 32)
	keys, err := cryptox.NewKeyring(master)
	if err != nil {
		t.Fatal(err)
	}
	backupDB := filepath.Join(root, "snapshot-source.db")
	if err = createRestoreSnapshotDB(backupDB); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backupDB)
	if err != nil {
		t.Fatal(err)
	}
	backupKey, err := keys.BackupKey()
	if err != nil {
		t.Fatal(err)
	}
	if err = encryptFile(context.Background(), filepath.Join(snapshotDir, "neocloud.db.enc"), backupDB, backupKey, "snapshot:"+snapshotName); err != nil {
		t.Fatal(err)
	}
	blobEntries := []string{"1/" + restoreTestBlobID}
	var blobBytes []byte
	if includeBackupBlob {
		blobSize, sizeErr := cryptox.EncryptedSize(5)
		if sizeErr != nil {
			t.Fatal(sizeErr)
		}
		blobBytes = make([]byte, blobSize)
		for i := range blobBytes {
			blobBytes[i] = byte(i % 251)
		}
		blobPath := filepath.Join(backupRoot, "blobs", "1", restoreTestBlobID)
		if err = os.MkdirAll(filepath.Dir(blobPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(blobPath, blobBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{Format: 2, App: "NeoCloud", Version: "test", Schema: 1, CreatedAt: "2026-11-27T06:00:00Z", DatabaseSize: info.Size(), Blobs: blobEntries}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(snapshotDir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return RestoreOptions{DatabaseFile: databaseFile, BlobDir: blobRoot, PIDFile: pidFile, BackupRoot: backupRoot, Snapshot: snapshotName, Port: freeRestorePort(t), Keys: keys, Now: func() time.Time { return time.Date(2026, 11, 27, 7, 8, 9, 0, time.UTC) }}, master, blobBytes
}

func createRestoreFixtureDB(path, username string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT NOT NULL); INSERT INTO users(id,username) VALUES(1,?); PRAGMA user_version=1;`, username)
	return err
}

func createRestoreSnapshotDB(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT NOT NULL);
INSERT INTO users(id,username) VALUES(1,'restored');
CREATE TABLE blobs(id TEXT NOT NULL,user_id INTEGER NOT NULL,size INTEGER NOT NULL);
INSERT INTO blobs(id,user_id,size) VALUES(?,1,5);
PRAGMA user_version=1;`, restoreTestBlobID)
	return err
}

func freeRestorePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(strings.Split(listener.Addr().String(), ":")[1])
	listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestRestoreSnapshotRestoresDBAndMissingBlobAndKeepsPreviousDatabase(t *testing.T) {
	options, _, expectedBlob := restoreFixture(t, true)
	result, err := RestoreSnapshot(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot != options.Snapshot || result.Previous == "" || result.SchemaVersion != 1 || result.BlobsRestored != 1 {
		t.Fatalf("restore result=%+v", result)
	}
	current, err := sql.Open("sqlite", options.DatabaseFile)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	var currentName string
	if err = current.QueryRow("SELECT username FROM users WHERE id=1").Scan(&currentName); err != nil || currentName != "restored" {
		t.Fatalf("restored database user=%q err=%v", currentName, err)
	}
	previous, err := sql.Open("sqlite", result.Previous)
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Close()
	var oldName string
	if err = previous.QueryRow("SELECT username FROM users WHERE id=1").Scan(&oldName); err != nil || oldName != "before" {
		t.Fatalf("previous database user=%q err=%v", oldName, err)
	}
	blobPath := localBlobPath(options.BlobDir, Blob{ID: restoreTestBlobID, UserID: 1})
	gotBlob, err := os.ReadFile(blobPath)
	if err != nil || string(gotBlob) != string(expectedBlob) {
		t.Fatalf("restored blob bytes match=%v err=%v", string(gotBlob) == string(expectedBlob), err)
	}
}

func TestRestoreSnapshotHydratesRemoteS3SnapshotBeforeLocalMutation(t *testing.T) {
	options, _, blobBytes := restoreFixture(t, true)
	fixture := newS3Fixture()
	for _, rel := range []string{
		"snapshots/" + options.Snapshot + "/manifest.json",
		"snapshots/" + options.Snapshot + "/neocloud.db.enc",
		"blobs/1/" + restoreTestBlobID,
	} {
		data, err := os.ReadFile(filepath.Join(options.BackupRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		fixture.objects["tenant/backups/"+rel] = data
		fixture.storageClass["tenant/backups/"+rel] = "STANDARD"
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewS3TargetWithHTTPClient(S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret", Prefix: "tenant/backups", ForcePathStyle: true}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	options.Target = target
	options.BackupRoot = ""
	result, err := RestoreSnapshot(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Waiting || result.Snapshot != options.Snapshot || result.BlobsRestored != 1 {
		t.Fatalf("restore result %+v", result)
	}
	db, err := sql.Open("sqlite", options.DatabaseFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var username string
	if err = db.QueryRow("SELECT username FROM users WHERE id=1").Scan(&username); err != nil || username != "restored" {
		t.Fatalf("username %q err=%v", username, err)
	}
	gotBlob, err := os.ReadFile(localBlobPath(options.BlobDir, Blob{ID: restoreTestBlobID, UserID: 1}))
	if err != nil || string(gotBlob) != string(blobBytes) {
		t.Fatalf("restored blob mismatch err=%v", err)
	}
}

func TestRestoreSnapshotRequestsGlacierAndDeepArchiveWithoutMutatingInstall(t *testing.T) {
	options, _, _ := restoreFixture(t, true)
	fixture := newS3Fixture()
	items := []struct{ rel, class string }{
		{"snapshots/" + options.Snapshot + "/manifest.json", "STANDARD"},
		{"snapshots/" + options.Snapshot + "/neocloud.db.enc", "DEEP_ARCHIVE"},
		{"blobs/1/" + restoreTestBlobID, "GLACIER"},
	}
	for _, item := range items {
		data, err := os.ReadFile(filepath.Join(options.BackupRoot, filepath.FromSlash(item.rel)))
		if err != nil {
			t.Fatal(err)
		}
		key := "tenant/backups/" + item.rel
		fixture.objects[key] = data
		fixture.storageClass[key] = item.class
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewS3TargetWithHTTPClient(S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret", Prefix: "tenant/backups", ForcePathStyle: true}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	options.Target = target
	options.BackupRoot = ""
	result, err := RestoreSnapshot(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Waiting || result.Requested != 2 || result.Pending != 0 || result.Archived != 2 {
		t.Fatalf("waiting result %+v", result)
	}
	db, err := sql.Open("sqlite", options.DatabaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var username string
	err = db.QueryRow("SELECT username FROM users WHERE id=1").Scan(&username)
	db.Close()
	if err != nil || username != "before" {
		t.Fatalf("installation changed before hydration: username=%q err=%v", username, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.restoreCalls) != 2 {
		t.Fatalf("restore requests %v", fixture.restoreCalls)
	}
}

func TestPreserveDatabaseCopiesPendingWALBeforeCheckpoint(t *testing.T) {
	options, _, _ := restoreFixture(t, true)
	live, err := sql.Open("sqlite", options.DatabaseFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = live.Exec("PRAGMA journal_mode=WAL"); err != nil {
		live.Close()
		t.Fatal(err)
	}
	if _, err = live.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		live.Close()
		t.Fatal(err)
	}
	if _, err = live.Exec("INSERT INTO users(id,username) VALUES(2,'pending-wal')"); err != nil {
		live.Close()
		t.Fatal(err)
	}
	walInfo, err := os.Stat(options.DatabaseFile + "-wal")
	if err != nil || walInfo.Size() == 0 {
		live.Close()
		t.Fatalf("pending WAL not created: info=%v err=%v", walInfo, err)
	}
	previous, err := preserveDatabase(options.DatabaseFile, "wal-test")
	if err != nil {
		live.Close()
		t.Fatal(err)
	}
	if _, err = os.Stat(previous + "-wal"); err != nil {
		live.Close()
		t.Fatalf("previous WAL sidecar not preserved: %v", err)
	}
	previousDB, err := sql.Open("sqlite", previous)
	if err != nil {
		live.Close()
		t.Fatal(err)
	}
	var username string
	err = previousDB.QueryRow("SELECT username FROM users WHERE id=2").Scan(&username)
	previousDB.Close()
	live.Close()
	if err != nil || username != "pending-wal" {
		t.Fatalf("previous database WAL row=%q err=%v", username, err)
	}
}

func TestRestoreSnapshotRejectsWrongKeyAndMissingBlobsBeforeDatabaseChange(t *testing.T) {
	t.Run("wrong key", func(t *testing.T) {
		options, _, _ := restoreFixture(t, true)
		wrongKey, err := cryptox.NewKeyring(strings.Repeat("29", 32))
		if err != nil {
			t.Fatal(err)
		}
		options.Keys = wrongKey
		if _, err = RestoreSnapshot(context.Background(), options); err == nil || !strings.Contains(err.Error(), "could not be decrypted") {
			t.Fatalf("wrong key error=%v", err)
		}
		assertRestoreDBValue(t, options.DatabaseFile, "before")
	})
	t.Run("missing blob", func(t *testing.T) {
		options, _, _ := restoreFixture(t, false)
		if _, err := RestoreSnapshot(context.Background(), options); err == nil || !strings.Contains(err.Error(), "required blob") {
			t.Fatalf("missing blob error=%v", err)
		}
		assertRestoreDBValue(t, options.DatabaseFile, "before")
	})
}

func TestRestoreSnapshotRefusesLiveServer(t *testing.T) {
	t.Run("shared runtime lock", func(t *testing.T) {
		options, _, _ := restoreFixture(t, true)
		lock, err := os.OpenFile(options.PIDFile+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			lock.Close()
			t.Fatal(err)
		}
		defer lock.Close()
		if _, err = RestoreSnapshot(context.Background(), options); err == nil || !strings.Contains(err.Error(), "runtime lock") {
			t.Fatalf("held runtime lock error=%v", err)
		}
		assertRestoreDBValue(t, options.DatabaseFile, "before")
	})
	t.Run("PID", func(t *testing.T) {
		options, _, _ := restoreFixture(t, true)
		if err := os.WriteFile(options.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := RestoreSnapshot(context.Background(), options); err == nil || !strings.Contains(err.Error(), "PID file") {
			t.Fatalf("live PID error=%v", err)
		}
		assertRestoreDBValue(t, options.DatabaseFile, "before")
	})
	t.Run("health endpoint", func(t *testing.T) {
		options, _, _ := restoreFixture(t, true)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
		defer server.Close()
		_, portRaw, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		options.Port, err = strconv.Atoi(portRaw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = RestoreSnapshot(context.Background(), options); err == nil || !(strings.Contains(err.Error(), "health endpoint") || strings.Contains(err.Error(), "listening")) {
			t.Fatalf("health response error=%v", err)
		}
		assertRestoreDBValue(t, options.DatabaseFile, "before")
	})
}

func TestRestoreSnapshotRejectsManifestMismatchAndUnsafeSnapshotName(t *testing.T) {
	options, _, _ := restoreFixture(t, true)
	manifestPath := filepath.Join(options.BackupRoot, "snapshots", options.Snapshot, "manifest.json")
	manifest := Manifest{Format: 2, App: "NeoCloud", Schema: 1, DatabaseSize: 1, Blobs: []string{"1/../../escape"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = RestoreSnapshot(context.Background(), options); err == nil {
		t.Fatal("mismatched manifest unexpectedly restored")
	}
	assertRestoreDBValue(t, options.DatabaseFile, "before")
	options.Snapshot = "../outside"
	if _, err = RestoreSnapshot(context.Background(), options); err == nil {
		t.Fatal("unsafe snapshot path unexpectedly accepted")
	}
}

func assertRestoreDBValue(t *testing.T, path, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err = db.QueryRow("SELECT username FROM users WHERE id=1").Scan(&got); err != nil || got != want {
		t.Fatalf("database value=%q want=%q err=%v", got, want, err)
	}
}

func TestRestoreSnapshotBlobDestinationRejectsSymlink(t *testing.T) {
	options, _, _ := restoreFixture(t, true)
	destinationDir := filepath.Join(options.BlobDir, "1")
	if err := os.MkdirAll(filepath.Dir(destinationDir), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, destinationDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := RestoreSnapshot(context.Background(), options); err == nil || !strings.Contains(err.Error(), "symlink or non-directory") {
		t.Fatalf("symlink destination error=%v", err)
	}
}
