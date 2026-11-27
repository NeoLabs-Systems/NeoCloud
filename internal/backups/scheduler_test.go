package backups

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func schedulerFixture(t *testing.T) (*dbx.DB, *cryptox.Keyring, *Runner, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(root, "migration-backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	blobDir, tmpDir := filepath.Join(root, "blobs"), filepath.Join(root, "tmp")
	if err = os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	runner, err := New(db, keys, blobDir, tmpDir, filepath.Join(root, "backups"), "test")
	if err != nil {
		t.Fatal(err)
	}
	return db, keys, runner, root
}

func TestScheduleSlotStartUsesLocalWallClockAndWeeklyWeekday(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 27, 4, 30, 0, 0, loc) // Friday, before the scheduled hour.
	daily := scheduleSlotStart("daily", 5, 0, now)
	if daily.Day() != 26 || daily.Hour() != 5 || daily.Location() != loc {
		t.Fatalf("daily slot=%s", daily)
	}
	weekly := scheduleSlotStart("weekly", 3, int(time.Monday), now)
	if weekly.Weekday() != time.Monday || weekly.Day() != 23 || weekly.Hour() != 3 {
		t.Fatalf("weekly slot=%s", weekly)
	}
}

func TestScheduleDueRetriesFailedRunsWithNodeDelays(t *testing.T) {
	db, _, runner, _ := schedulerFixture(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 27, 12, 0, 0, 0, time.UTC)
	target := scheduledTarget{id: 8, schedule: "daily", hour: 3, enabled: true}
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || !due {
		t.Fatalf("empty slot due=%v err=%v", due, err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,enabled) VALUES(8,'local','local','sealed','daily',3,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_runs(target_id,status,started_at,finished_at) VALUES(8,'failed','2026-11-27 04:00:00','2026-11-27 04:05:00')`); err != nil {
		t.Fatal(err)
	}
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || !due {
		t.Fatalf("first retry at 12:00 due=%v err=%v", due, err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_runs(target_id,status,started_at,finished_at) VALUES(8,'failed','2026-11-27 04:10:00','2026-11-27 11:50:00')`); err != nil {
		t.Fatal(err)
	}
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || due {
		t.Fatalf("second retry before 30 minutes due=%v err=%v", due, err)
	}
	now = now.Add(31 * time.Minute)
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || !due {
		t.Fatalf("second retry after 30 minutes due=%v err=%v", due, err)
	}
	now = now.Add(time.Minute)
	if _, err := db.W.Exec(`INSERT INTO backup_runs(target_id,status,started_at,finished_at) VALUES(8,'failed','2026-11-27 12:31:00','2026-11-27 12:32:00')`); err != nil {
		t.Fatal(err)
	}
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || due {
		t.Fatalf("third retry before 90 minutes due=%v err=%v", due, err)
	}
	now = now.Add(89 * time.Minute)
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || due {
		t.Fatalf("third retry before 90-minute delay due=%v err=%v", due, err)
	}
	now = now.Add(2 * time.Minute)
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || !due {
		t.Fatalf("third retry after 90 minutes due=%v err=%v", due, err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_runs(target_id,status,started_at) VALUES(8,'running','2026-11-27 14:03:00')`); err != nil {
		t.Fatal(err)
	}
	if due, err := runner.scheduleDue(ctx, target, now); err != nil || due {
		t.Fatalf("in-progress run settled slot due=%v err=%v", due, err)
	}
}

func TestScheduleDueWaitsTenMinutesBeforeFirstRetry(t *testing.T) {
	db, _, runner, _ := schedulerFixture(t)
	if _, err := db.W.Exec(`INSERT INTO users(id,username,password) VALUES(8,'owner','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,enabled) VALUES(8,'local','local','sealed','daily',3,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO backup_runs(target_id,status,started_at,finished_at) VALUES(8,'failed','2026-11-27 04:00:00','2026-11-27 04:05:00')`); err != nil {
		t.Fatal(err)
	}
	target := scheduledTarget{id: 8, schedule: "daily", hour: 3, enabled: true}
	for _, check := range []struct {
		time time.Time
		due  bool
	}{{time.Date(2026, 11, 27, 4, 14, 0, 0, time.UTC), false}, {time.Date(2026, 11, 27, 4, 15, 0, 0, time.UTC), true}} {
		due, err := runner.scheduleDue(context.Background(), target, check.time)
		if err != nil || due != check.due {
			t.Fatalf("schedule due at %s=%v want %v err=%v", check.time, due, check.due, err)
		}
	}
}

func TestSchedulerStartsLocalTargetsButDoesNotRunRemoteTargets(t *testing.T) {
	db, keys, runner, root := schedulerFixture(t)
	ctx := context.Background()
	now := time.Now()
	scheduleHour := (now.Hour() + 23) % 24
	destination := filepath.Join(root, "backup")
	config, err := json.Marshal(map[string]string{"path": destination})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keys.EncryptValue(string(config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(1,'local','local',?,'daily',?,0,2,1)`, sealed, scheduleHour); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(2,'remote','s3','sealed','daily',?,0,2,1)`, scheduleHour); err != nil {
		t.Fatal(err)
	}
	runner.Start(ctx)
	defer runner.Close()
	runner.checkSchedules(ctx, now)
	var runID int64
	if err = db.W.QueryRow(`SELECT id FROM backup_runs WHERE target_id=1 ORDER BY id DESC LIMIT 1`).Scan(&runID); err != nil {
		t.Fatalf("local scheduled run missing: %v", err)
	}
	waitRun(t, db.W, runID)
	var remoteRuns int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM backup_runs WHERE target_id=2`).Scan(&remoteRuns); err != nil || remoteRuns != 0 {
		t.Fatalf("remote target runs=%d err=%v", remoteRuns, err)
	}
}
