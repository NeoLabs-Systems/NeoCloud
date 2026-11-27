package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	firstPassDelay = 30 * time.Second
	interval       = time.Hour
	dailyInterval  = 24 * time.Hour
	walLimit       = 32 * 1024 * 1024
)

type Check struct {
	Status     string `json:"database"`
	CheckedAt  string `json:"databaseCheckedAt"`
	DurationMS int64  `json:"databaseCheckDurationMs"`
}

type Runner struct {
	db            *sql.DB
	path          string
	mu            sync.RWMutex
	check         Check
	lastDaily     time.Time
	trashCleanup  func(context.Context) error
	uploadCleanup func(context.Context) error
	blobScrubber  *blobScrubber
	Now           func() time.Time
}

func New(db *sql.DB, databasePath string) *Runner {
	return &Runner{db: db, path: databasePath, check: Check{Status: "pending"}, Now: time.Now}
}

// SetFileCleanup attaches the file-service cleanup operations after the
// application composes that service and before workers are started.
func (r *Runner) SetFileCleanup(trash, uploads func(context.Context) error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.trashCleanup = trash
	r.uploadCleanup = uploads
	r.mu.Unlock()
}

func (r *Runner) Status() Check {
	if r == nil {
		return Check{Status: "pending"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.check
}

// Run starts the process-owned maintenance schedule and joins it on shutdown.
func (r *Runner) Run(ctx context.Context) {
	if r == nil || r.db == nil {
		return
	}
	first := time.NewTimer(firstPassDelay)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			r.RunOnce(ctx)
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce is idempotent and can be called by an operator or a test as well as
// the managed schedule. Each maintenance step is independent, matching Node.
func (r *Runner) RunOnce(ctx context.Context) {
	if r == nil || r.db == nil || ctx.Err() != nil {
		return
	}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"ended session cleanup", r.purgeEndedSessions},
		{"activity retention", r.pruneActivity},
		{"WAL checkpoint", r.checkpointLargeWAL},
	}
	r.mu.RLock()
	trashCleanup, uploadCleanup := r.trashCleanup, r.uploadCleanup
	r.mu.RUnlock()
	if trashCleanup != nil {
		steps = append(steps, struct {
			name string
			fn   func(context.Context) error
		}{"expired trash cleanup", trashCleanup})
	}
	if uploadCleanup != nil {
		steps = append(steps, struct {
			name string
			fn   func(context.Context) error
		}{"expired upload cleanup", uploadCleanup})
	}
	r.mu.RLock()
	blobScrubber := r.blobScrubber
	r.mu.RUnlock()
	if blobScrubber != nil {
		steps = append(steps, struct {
			name string
			fn   func(context.Context) error
		}{"blob scrub", blobScrubber.scrubSlice})
	}
	for _, step := range steps {
		if err := step.fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("NeoCloud maintenance step failed", "step", step.name, "error", err)
		}
	}
	r.mu.Lock()
	dailyDue := r.lastDaily.IsZero() || r.Now().Sub(r.lastDaily) >= dailyInterval
	if dailyDue {
		r.lastDaily = r.Now()
	}
	r.mu.Unlock()
	if dailyDue {
		if _, err := r.db.ExecContext(ctx, `PRAGMA optimize`); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("NeoCloud maintenance step failed", "step", "PRAGMA optimize", "error", err)
		}
		r.checkDatabase(ctx)
	}
}

func (r *Runner) purgeEndedSessions(ctx context.Context) error {
	// The cookie store's own rows: nothing else removes an expired one.
	store, err := sessionx.NewStore(ctx, r.db)
	if err != nil {
		return err
	}
	if err := store.ClearExpired(ctx); err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM user_sessions
		WHERE datetime(revoked_at) <= datetime('now', '-30 days')
		   OR datetime(expires_at) <= datetime('now', '-30 days')`)
	return err
}

func (r *Runner) pruneActivity(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM activity WHERE at < datetime('now', '-90 days')`)
	return err
}

func (r *Runner) checkpointLargeWAL(ctx context.Context) error {
	info, err := os.Stat(r.path + "-wal")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Size() < walLimit {
		return nil
	}
	var busy, logFrames, checkpointed int
	if err := r.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return err
	}
	slog.Info("NeoCloud maintenance checkpointed large WAL", "walBytes", info.Size(), "busy", busy, "frames", logFrames, "checkpointed", checkpointed)
	return nil
}

func (r *Runner) checkDatabase(ctx context.Context) {
	started := r.Now()
	status := "ok"
	if err := r.quickCheck(ctx); err != nil {
		status = "damaged"
		slog.Error("NeoCloud database quick_check failed", "error", err)
	}
	checked := r.Now()
	r.mu.Lock()
	r.check = Check{Status: status, CheckedAt: checked.UTC().Format(time.RFC3339Nano), DurationMS: checked.Sub(started).Milliseconds()}
	r.mu.Unlock()
}

func (r *Runner) quickCheck(ctx context.Context) error {
	absolute, err := filepath.Abs(r.path)
	if err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}).String()
	checkDB, err := sql.Open("sqlite", uri)
	if err != nil {
		return err
	}
	defer checkDB.Close()
	checkDB.SetMaxOpenConns(1)
	rows, err := checkDB.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		if result != "ok" {
			problems = append(problems, result)
			if len(problems) == 5 {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("quick_check reported: %s", strings.Join(problems, "; "))
	}
	return nil
}
