package backups

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestLocalDailySnapshotUsesNodeCipherIdentityAndRetention(t *testing.T) {
	db, keys, runner, root := schedulerFixture(t)
	ctx := context.Background()
	now := time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','x')`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runner.backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	for day := 1; day <= 15; day++ {
		name := "daily-" + time.Date(2026, time.January, day, 3, 0, 0, 0, time.UTC).Format("2006-01-02T15-04-05Z") + ".db.enc"
		if err := os.WriteFile(filepath.Join(runner.backupDir, name), []byte("old snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.createLocalDailySnapshot(ctx, now); err != nil {
		t.Fatal(err)
	}
	items, err := listLocalDailySnapshots(runner.backupDir)
	if err != nil || len(items) != snapshotRetention[SnapshotDaily] {
		t.Fatalf("retained snapshots=%d err=%v", len(items), err)
	}
	wantName := "daily-2026-02-01T12-00-00Z.db.enc"
	if items[0].name != wantName {
		t.Fatalf("newest snapshot=%q want %q", items[0].name, wantName)
	}
	file, err := os.Open(items[0].path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err = file.ReadAt(header, 0); err != nil || string(header) != "NCB1" {
		t.Fatalf("snapshot header=%q err=%v", header, err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	plainSize, err := nodeEncryptedPlaintextSize(info.Size())
	if err != nil {
		t.Fatal(err)
	}
	backupKey, err := keys.BackupKey()
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	base := strings.TrimSuffix(wantName, ".db.enc")
	if _, err = cryptox.DecryptBlobRange(file, plainSize, 0, plainSize-1, backupKey, "local:"+base, &plain); err != nil {
		t.Fatalf("decrypt Node-compatible snapshot: %v", err)
	}
	copyPath := filepath.Join(root, "daily-copy.db")
	if err = os.WriteFile(copyPath, plain.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	copyDB, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var check, username string
	if err = copyDB.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("snapshot integrity=%q err=%v", check, err)
	}
	if err = copyDB.QueryRow(`SELECT username FROM users WHERE id=1`).Scan(&username); err != nil || username != "owner" {
		t.Fatalf("snapshot user=%q err=%v", username, err)
	}
}

func nodeEncryptedPlaintextSize(encrypted int64) (int64, error) {
	const header, tag, segment = int64(20), int64(16), int64(64 * 1024)
	if encrypted < header+tag {
		return 0, os.ErrInvalid
	}
	segments := (encrypted - header + segment + tag - 1) / (segment + tag)
	plain := encrypted - header - segments*tag
	expected, err := cryptox.EncryptedSize(plain)
	if err != nil || plain < 1 || expected != encrypted {
		return 0, os.ErrInvalid
	}
	return plain, nil
}

func TestSchedulerDailyFallbackOnlyRunsWithoutScheduledTargets(t *testing.T) {
	db, _, runner, _ := schedulerFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	runner.checkSchedules(ctx, now)
	items, err := listLocalDailySnapshots(runner.backupDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("initial local snapshot count=%d err=%v", len(items), err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(1,'manual','local','sealed','manual',3,0,7,1)`); err != nil {
		t.Fatal(err)
	}
	runner.checkSchedules(ctx, now.Add(24*time.Hour))
	items, err = listLocalDailySnapshots(runner.backupDir)
	if err != nil || len(items) != 2 {
		t.Fatalf("manual target should not suppress fallback; count=%d err=%v", len(items), err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(2,'remote','s3','sealed','daily',3,0,7,1)`); err != nil {
		t.Fatal(err)
	}
	runner.checkSchedules(ctx, now.Add(48*time.Hour))
	items, err = listLocalDailySnapshots(runner.backupDir)
	if err != nil || len(items) != 2 {
		t.Fatalf("scheduled remote target should suppress fallback; count=%d err=%v", len(items), err)
	}
}

func TestPreUpdateSnapshotRestoresTheDatabaseAsItWas(t *testing.T) {
	db, keys, runner, dir := schedulerFixture(t)
	snapshot, err := runner.CreateLocalSnapshot(context.Background(), SnapshotPreUpdate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO users(username,password) VALUES('later','x')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "neocloud.db")
	previous, err := RestoreLocalSnapshot(context.Background(), RestoreOptions{
		DatabaseFile: database, PIDFile: filepath.Join(dir, "neocloud.pid"), Port: 1, Keys: keys,
	}, snapshot)
	if err != nil || previous == "" || snapshot.SchemaVersion != schema.Version {
		t.Fatalf("restore = %q, %+v, %v", previous, snapshot, err)
	}
	restored, err := dbx.Open(context.Background(), database, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var users int
	if err := restored.R.QueryRow(`SELECT COUNT(*) FROM users WHERE username='later'`).Scan(&users); err != nil || users != 0 {
		t.Fatalf("users added after the snapshot = %d, %v", users, err)
	}
}
