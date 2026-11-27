package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

const (
	firstScheduleCheck = time.Minute
	scheduleCheckEvery = 5 * time.Minute
)

var scheduleRetryDelays = [...]time.Duration{10 * time.Minute, 30 * time.Minute, 90 * time.Minute}

type scheduledTarget struct {
	id        int64
	typeName  string
	config    string
	schedule  string
	hour      int
	weekday   int
	retention int
	enabled   bool
}

type scheduledRun struct {
	status   string
	finished sql.NullString
}

// schedule keeps local backup targets on the same server-local schedule slots
// as the Node scheduler. It runs inside the Runner lifecycle, so shutdown
// waits for its loop and any snapshot jobs to finish.
func (r *Runner) schedule(ctx context.Context) {
	timer := time.NewTimer(firstScheduleCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			r.checkSchedules(ctx, time.Now())
			timer.Reset(scheduleCheckEvery)
		}
	}
}

func (r *Runner) checkSchedules(ctx context.Context, now time.Time) {
	_, err := r.db.ExecContext(ctx, `UPDATE backup_runs SET status='failed',finished_at=datetime('now'),error='interrupted'
		WHERE status='running' AND datetime(COALESCE(heartbeat_at,started_at))<=datetime('now','-5 minutes')`)
	if err != nil {
		slog.Error("mark stale backup runs", "error", err)
		return
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,type,config_enc,schedule,hour,weekday,retention,enabled FROM backup_targets ORDER BY id`)
	if err != nil {
		slog.Error("read backup schedules", "error", err)
		return
	}
	targets := make([]scheduledTarget, 0)
	for rows.Next() {
		var target scheduledTarget
		var enabled int
		if err = rows.Scan(&target.id, &target.typeName, &target.config, &target.schedule, &target.hour, &target.weekday, &target.retention, &enabled); err != nil {
			rows.Close()
			slog.Error("read backup schedule", "error", err)
			return
		}
		target.enabled = enabled == 1
		targets = append(targets, target)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		slog.Error("read backup schedules", "error", err)
		return
	}
	if err = rows.Close(); err != nil {
		slog.Error("close backup schedules", "error", err)
		return
	}
	hasScheduledTarget := false
	for _, target := range targets {
		if ctx.Err() != nil {
			return
		}
		if !target.enabled || target.schedule == "manual" || (target.schedule != "daily" && target.schedule != "weekly") {
			continue
		}
		hasScheduledTarget = true
		if target.typeName != "local" && target.typeName != "webdav" && target.typeName != "s3" && target.typeName != "sftp" {
			r.logUnsupportedSchedule(target)
			continue
		}
		due, dueErr := r.scheduleDue(ctx, target, now)
		if dueErr != nil {
			slog.Error("check backup schedule", "target", target.id, "error", dueErr)
			continue
		}
		if !due {
			continue
		}
		var config map[string]any
		plain, decryptErr := r.keys.DecryptValue(target.config)
		if decryptErr == nil {
			decryptErr = json.Unmarshal([]byte(plain), &config)
		}
		if decryptErr != nil || config == nil {
			slog.Error("scheduled backup target has invalid configuration", "target", target.id)
			continue
		}
		if _, startErr := r.RunTarget(target.id, target.typeName, config, target.retention); startErr != nil && !errors.Is(startErr, ErrAlreadyRunning) && !errors.Is(startErr, ErrNotStarted) {
			slog.Error("start scheduled backup", "target", target.id, "error", startErr)
		}
	}
	if hasScheduledTarget || ctx.Err() != nil {
		return
	}
	due, err := localDailySnapshotDue(r.backupDir, now)
	if err != nil {
		slog.Error("check local daily backup", "error", err)
		return
	}
	if !due {
		return
	}
	if err = r.createLocalDailySnapshot(ctx, now); err != nil {
		slog.Error("create local daily backup", "error", err)
	}
}

func (r *Runner) logUnsupportedSchedule(target scheduledTarget) {
	r.mu.Lock()
	first := !r.unsupportedLogged[target.id]
	if first {
		r.unsupportedLogged[target.id] = true
	}
	r.mu.Unlock()
	if first {
		slog.Warn("scheduled backup target type is not supported by the Go runtime", "target", target.id, "type", target.typeName)
	}
}

func (r *Runner) scheduleDue(ctx context.Context, target scheduledTarget, now time.Time) (bool, error) {
	if target.schedule == "manual" || !target.enabled {
		return false, nil
	}
	slot := scheduleSlotStart(target.schedule, target.hour, target.weekday, now)
	rows, err := r.db.QueryContext(ctx, `SELECT status,finished_at FROM backup_runs WHERE target_id=? AND started_at>=? ORDER BY id`, target.id, slot.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return false, err
	}
	runs := make([]scheduledRun, 0, 4)
	for rows.Next() {
		var run scheduledRun
		if err = rows.Scan(&run.status, &run.finished); err != nil {
			rows.Close()
			return false, err
		}
		runs = append(runs, run)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err = rows.Close(); err != nil {
		return false, err
	}
	if len(runs) == 0 {
		return true, nil
	}
	for _, run := range runs {
		if run.status != "failed" {
			return false, nil
		}
	}
	if len(runs) > len(scheduleRetryDelays) || !runs[len(runs)-1].finished.Valid {
		return false, nil
	}
	finished, err := time.ParseInLocation("2006-01-02 15:04:05", runs[len(runs)-1].finished.String, time.UTC)
	if err != nil {
		return false, err
	}
	return now.Sub(finished) >= scheduleRetryDelays[len(runs)-1], nil
}

// scheduleSlotStart returns the start of the current-or-most-recent scheduled
// day/week in the same local timezone carried by now. Node stores run times in
// UTC, so callers convert this boundary to UTC for the database query.
func scheduleSlotStart(schedule string, hour, weekday int, now time.Time) time.Time {
	local := now.In(now.Location())
	start := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, local.Location())
	if schedule == "weekly" {
		back := (int(start.Weekday()) - weekday + 7) % 7
		start = start.AddDate(0, 0, -back)
	}
	if start.After(local) {
		if schedule == "weekly" {
			start = start.AddDate(0, 0, -7)
		} else {
			start = start.AddDate(0, 0, -1)
		}
	}
	return start
}
