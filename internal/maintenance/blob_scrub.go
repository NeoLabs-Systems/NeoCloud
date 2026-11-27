package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

const (
	blobScrubCyclePasses = 7 * 24
	blobScrubMinSlice    = 100
	blobSegmentSize      = 64 * 1024
)

var blobIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

type blobScrubber struct {
	db      *sql.DB
	keys    *cryptox.Keyring
	blobDir string
	now     func() time.Time
}

func (r *Runner) SetBlobScrubber(keys *cryptox.Keyring, blobDir string) {
	if r == nil || keys == nil || blobDir == "" {
		return
	}
	r.mu.Lock()
	r.blobScrubber = &blobScrubber{db: r.db, keys: keys, blobDir: blobDir, now: r.Now}
	r.mu.Unlock()
}

func (s *blobScrubber) scrubSlice(ctx context.Context) error {
	cursor, err := readScrubState(ctx, s.db, "maintenance.blob_scrub.cursor")
	if err != nil {
		return err
	}
	cycleStarted := ""
	if cursor == "" {
		cycleStarted = s.now().UTC().Format(time.RFC3339Nano)
	} else {
		cycleStarted, err = readScrubState(ctx, s.db, "maintenance.blob_scrub.cycle_started_at")
		if err != nil {
			return err
		}
	}
	var total int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blobs`).Scan(&total); err != nil {
		return err
	}
	limit := (total + blobScrubCyclePasses - 1) / blobScrubCyclePasses
	if limit < blobScrubMinSlice {
		limit = blobScrubMinSlice
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,size FROM blobs WHERE id>? ORDER BY id LIMIT ?`, cursor, limit)
	if err != nil {
		return err
	}
	type blob struct {
		id   string
		uid  int64
		size int64
	}
	items := make([]blob, 0, limit)
	for rows.Next() {
		var item blob
		if err = rows.Scan(&item.id, &item.uid, &item.size); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	problems := make([]string, len(items))
	for i, item := range items {
		if err = ctx.Err(); err != nil {
			return err
		}
		problems[i] = s.checkBlob(ctx, item.uid, item.id, item.size)
	}
	finished := len(items) < limit
	nextCursor := cursor
	if finished {
		nextCursor = ""
	} else {
		nextCursor = items[len(items)-1].id
	}
	if finished && cycleStarted == "" {
		cycleStarted = s.now().UTC().Format(time.RFC3339Nano)
	}
	lastCycle := ""
	if finished {
		lastCycle = s.now().UTC().Format(time.RFC3339Nano)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, item := range items {
		if problems[i] != "" {
			var already int
			_ = tx.QueryRowContext(ctx, `SELECT 1 FROM blob_scrub_damaged WHERE blob_id=?`, item.id).Scan(&already)
			result, writeErr := tx.ExecContext(ctx, `INSERT INTO blob_scrub_damaged(blob_id,user_id,problem,checked_at)
				SELECT id,user_id,?,datetime('now') FROM blobs WHERE id=?
				ON CONFLICT(blob_id) DO UPDATE SET user_id=excluded.user_id,problem=excluded.problem,checked_at=excluded.checked_at`, problems[i], item.id)
			if writeErr != nil {
				return writeErr
			}
			if count, _ := result.RowsAffected(); count > 0 && already == 0 {
				slog.Warn("Encrypted blob failed periodic scrub", "blobId", item.id, "userId", item.uid, "problem", problems[i])
			}
		} else if _, writeErr := tx.ExecContext(ctx, `DELETE FROM blob_scrub_damaged WHERE blob_id=?`, item.id); writeErr != nil {
			return writeErr
		}
	}
	if err = writeScrubState(ctx, tx, "maintenance.blob_scrub.cursor", nextCursor); err != nil {
		return err
	}
	if err = writeScrubState(ctx, tx, "maintenance.blob_scrub.cycle_started_at", cycleStarted); err != nil {
		return err
	}
	if finished {
		if err = writeScrubState(ctx, tx, "maintenance.blob_scrub.last_cycle_at", lastCycle); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *blobScrubber) checkBlob(ctx context.Context, userID int64, blobID string, size int64) string {
	if !blobIDPattern.MatchString(blobID) || userID < 1 || size < 0 {
		return "invalid metadata"
	}
	path := filepath.Join(s.blobDir, fmt.Sprint(userID), blobID[:2], blobID)
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	wantSize, err := cryptox.EncryptedSize(size)
	if err != nil || info.Size() != wantSize {
		return "wrong size"
	}
	dek, err := s.keys.GetUserDEK(ctx, s.db, userID)
	if err != nil {
		return "damaged"
	}
	file, err := os.Open(path)
	if err != nil {
		return "missing"
	}
	defer file.Close()
	last := size - 1
	ranges := [][2]int64{{0, last}}
	if size >= blobSegmentSize {
		ranges = [][2]int64{{0, 0}, {last, last}}
	}
	for _, bounds := range ranges {
		if _, err = cryptox.DecryptBlobRange(file, size, bounds[0], bounds[1], dek, blobID, io.Discard); err != nil {
			return "damaged"
		}
	}
	return ""
}

func readScrubState(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM maintenance_state WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func writeScrubState(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO maintenance_state(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (r *Runner) ScrubStatus() (int64, *string, error) {
	if r == nil || r.db == nil {
		return 0, nil, nil
	}
	var damaged int64
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM blob_scrub_damaged`).Scan(&damaged); err != nil {
		return 0, nil, err
	}
	last, err := readScrubState(context.Background(), r.db, "maintenance.blob_scrub.last_cycle_at")
	if err != nil {
		return 0, nil, err
	}
	if last == "" {
		return damaged, nil, nil
	}
	return damaged, &last, nil
}
