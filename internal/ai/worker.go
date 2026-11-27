package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"
)

// The AI job worker: one loop, at most two files at a time. It runs the jobs
// people start and queues its own from the change journal, followed from a
// saved cursor: a file created for an account with "on upload" switched on gets
// a job, whichever way it arrived. A job runs only while AI can run for its
// account; a failed provider call is retried with backoff (1, 4, 16, 64
// minutes), then the job fails.

const (
	workerConcurrency = 2
	jobsPerPass       = 10
	journalPage       = 500
	maxAttempts       = 5
	retryBase         = time.Minute
	indexRetry        = 15 * time.Second
	keepFinished      = "-7 days"
	idleDelay         = 5 * time.Second
	firstPassDelay    = 20 * time.Second
	busyDelay         = 50 * time.Millisecond
	cursorKey         = "cursor"
)

var (
	skipCodes  = []string{"NOT_FOUND", "AI_VAULT", "AI_EXCLUDED", "NOT_A_FILE", "AI_FEATURE_OFF", "AI_TYPE_SKIPPED"}
	pauseCodes = []string{"AI_DISABLED", "AI_NOT_OPTED_IN", "AI_BUDGET_EXCEEDED"}
)

type job struct {
	id, userID                     int64
	nodeID, kinds, source, batchID string
	explicit                       bool
	attempts                       int
}

// uploadKinds are the kinds an upload gets for this account, or none.
func (h *Handler) uploadKinds(ctx context.Context, userID int64) (account, []string, error) {
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return settings, nil, err
	}
	reason, err := h.pauseReason(ctx, userID)
	if err != nil || reason != "" && reason != "AI_BUDGET_EXCEEDED" || !settings.features()["onUpload"] {
		return settings, nil, err
	}
	var kinds []string
	for _, kind := range jobKinds {
		if settings.features()[kind] {
			kinds = append(kinds, kind)
		}
	}
	return settings, kinds, nil
}

type created struct {
	seq, userID  int64
	nodeID, name string
}

func (h *Handler) followJournal(ctx context.Context) error {
	var latest int64
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes`).Scan(&latest); err != nil {
		return err
	}
	var saved string
	err := h.db.QueryRowContext(ctx, `SELECT value FROM ai_state WHERE key=?`, cursorKey).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		// No backfill of what was there before.
		_, err = h.db.ExecContext(ctx, `INSERT OR IGNORE INTO ai_state(key,value) VALUES(?,?)`, cursorKey, strconv.FormatInt(latest, 10))
		return err
	}
	if err != nil {
		return err
	}
	cursor, err := strconv.ParseInt(saved, 10, 64)
	if err != nil {
		cursor = latest
	}
	type uploader struct {
		settings account
		kinds    []string
	}
	cache := map[int64]uploader{}
	for cursor < latest {
		rows, err := queryAll(ctx, h.db, func(s scanner) (created, error) {
			var c created
			err := s.Scan(&c.seq, &c.userID, &c.nodeID, &c.name)
			return c, err
		}, `SELECT c.seq,c.user_id,c.node_id,n.name FROM changes c JOIN nodes n ON n.id=c.node_id
			WHERE c.seq>? AND c.seq<=? AND c.op='create' AND n.type='file' ORDER BY c.seq LIMIT ?`, cursor, latest, journalPage)
		if err != nil {
			return err
		}
		head := latest
		if len(rows) == journalPage {
			head = rows[len(rows)-1].seq
		}
		var queue []created
		for _, row := range rows {
			who, ok := cache[row.userID]
			if !ok {
				if who.settings, who.kinds, err = h.uploadKinds(ctx, row.userID); err != nil {
					return err
				}
				cache[row.userID] = who
			}
			if len(who.kinds) > 0 && !who.settings.skipsType(row.name) && assertAllowed(ctx, h.db, row.userID, row.nodeID) == nil {
				queue = append(queue, row)
			}
		}
		if err = h.inTx(ctx, func(tx *sql.Tx) error {
			for _, row := range queue {
				if err := enqueue(ctx, tx, row.userID, []string{row.nodeID}, cache[row.userID].kinds, "upload", false, ""); err != nil {
					return err
				}
			}
			_, err := tx.ExecContext(ctx, `UPDATE ai_state SET value=? WHERE key=?`, strconv.FormatInt(head, 10), cursorKey)
			return err
		}); err != nil {
			return err
		}
		cursor = head
	}
	return nil
}

// claim marks the due jobs of accounts that aren't paused as running, oldest
// first.
func (h *Handler) claim(ctx context.Context, limit int) ([]job, error) {
	due, err := queryAll(ctx, h.db, func(s scanner) (job, error) {
		var j job
		err := s.Scan(&j.id, &j.userID, &j.nodeID, &j.kinds, &j.source, &j.explicit, &j.batchID, &j.attempts)
		return j, err
	}, `SELECT id,user_id,node_id,kinds,source,explicit,COALESCE(batch_id,''),attempts FROM ai_jobs
		WHERE status='pending' AND run_after<=datetime('now') ORDER BY id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	paused := map[int64]bool{}
	var claimed []job
	for _, candidate := range due {
		if _, ok := paused[candidate.userID]; !ok {
			reason, err := h.pauseReason(ctx, candidate.userID)
			if err != nil {
				return nil, err
			}
			paused[candidate.userID] = reason != ""
		}
		if paused[candidate.userID] {
			continue
		}
		result, err := h.db.ExecContext(ctx, `UPDATE ai_jobs SET status='running',updated_at=datetime('now') WHERE id=? AND status='pending'`, candidate.id)
		if err != nil {
			return nil, err
		}
		if changed, err := result.RowsAffected(); err == nil && changed == 1 {
			claimed = append(claimed, candidate)
		}
		if len(claimed) >= limit {
			break
		}
	}
	return claimed, nil
}

func (h *Handler) finish(ctx context.Context, j job, status, code string) {
	if _, err := h.db.ExecContext(ctx, `UPDATE ai_jobs SET status=?,error_code=?,updated_at=datetime('now') WHERE id=?`, status, nullableString(code), j.id); err != nil {
		slog.Error("finish AI job", "jobId", j.id, "error", err)
	}
}

func (h *Handler) later(ctx context.Context, j job, delay time.Duration, countAttempt bool) {
	attempts := 0
	if countAttempt {
		attempts = 1
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE ai_jobs SET status='pending',attempts=attempts+?,run_after=datetime('now',?),updated_at=datetime('now') WHERE id=?`,
		attempts, fmt.Sprintf("+%d seconds", int(delay.Seconds())), j.id); err != nil {
		slog.Error("delay AI job", "jobId", j.id, "error", err)
	}
}

func (h *Handler) jobFailed(ctx context.Context, j job, err error) {
	code := codeOf(err)
	switch {
	case slices.Contains(skipCodes, code):
		h.finish(ctx, j, "skipped", code)
	case slices.Contains(pauseCodes, code):
		// Paused: it runs once AI can run for the account again.
		h.later(ctx, j, 0, false)
	case j.attempts+1 >= maxAttempts:
		slog.Warn("an AI job failed for good", "code", code)
		h.finish(ctx, j, "failed", code)
	default:
		if _, err := h.db.ExecContext(ctx, `UPDATE ai_jobs SET error_code=? WHERE id=?`, code, j.id); err != nil {
			slog.Error("record AI job failure", "jobId", j.id, "error", err)
		}
		h.later(ctx, j, retryBase<<(2*j.attempts), true)
	}
}

func (h *Handler) process(ctx context.Context, j job) (result outcome, err error) {
	// A hostile file must not take the server down.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("AI job panicked: %v", recovered)
		}
	}()
	var kinds []string
	if err = json.Unmarshal([]byte(j.kinds), &kinds); err != nil {
		return outcome{}, err
	}
	return h.suggestForFile(ctx, j.userID, j.nodeID, kinds, j.explicit, j.source == "upload", j.batchID)
}

func (h *Handler) runJob(ctx context.Context, j job) {
	result, err := h.process(ctx, j)
	record := context.WithoutCancel(ctx)
	switch {
	case ctx.Err() != nil:
		h.later(record, j, 0, false)
	case err != nil:
		h.jobFailed(record, j, err)
	case result.wait:
		h.later(record, j, indexRetry, false)
	case result.skipped != "":
		h.finish(record, j, "skipped", result.skipped)
	default:
		h.finish(record, j, "done", "")
		if result.created > 0 {
			h.AnnounceSuggestions(record, j.userID)
		}
	}
}

// RunPass queues what was uploaded, tidies the queue and runs the due jobs,
// returning how many ran. One pass at a time.
func (h *Handler) RunPass(ctx context.Context) (int, error) {
	h.passMu.Lock()
	defer h.passMu.Unlock()
	if err := h.followJournal(ctx); err != nil {
		return 0, err
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM ai_jobs WHERE status IN ('done','skipped','failed') AND updated_at<datetime('now',?)`, keepFinished); err != nil {
		return 0, err
	}
	if err := dropOldVotes(ctx, h.db); err != nil {
		return 0, err
	}
	jobs, err := h.claim(ctx, jobsPerPass)
	if err != nil {
		return 0, err
	}
	slots := make(chan struct{}, workerConcurrency)
	var running sync.WaitGroup
	for _, next := range jobs {
		slots <- struct{}{}
		running.Go(func() {
			defer func() { <-slots }()
			h.runJob(ctx, next)
		})
	}
	running.Wait()
	return len(jobs), nil
}

// Wake runs a pass now instead of after the idle wait.
func (h *Handler) Wake() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Run works the queue until ctx ends. Jobs a stop left running wait again.
func (h *Handler) Run(ctx context.Context) {
	if _, err := h.db.ExecContext(ctx, `UPDATE ai_jobs SET status='pending' WHERE status='running'`); err != nil && ctx.Err() == nil {
		slog.Error("recover AI jobs", "error", err)
	}
	timer := time.NewTimer(firstPassDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-h.wake:
		}
		done, err := h.RunPass(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("AI job pass failed", "error", err)
		}
		delay := idleDelay
		if done > 0 && err == nil {
			delay = busyDelay
		}
		timer.Reset(delay)
	}
}
