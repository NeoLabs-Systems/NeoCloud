package photos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	changePageSize = 500
	addBatchSize   = 2000
	defaultProbeN  = 10
)

var photoNameDate = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})([-_.]?)(0[1-9]|1[0-2])([-_.]?)(0[1-9]|[12]\d|3[01])(?:[ _T-]?([01]\d|2[0-3])[-_.:]?([0-5]\d)[-_.:]?([0-5]\d))?`)

func (h *Handler) CatchUp(ctx context.Context, userID int64) error {
	if userID < 1 {
		return errors.New("photos: user id is invalid")
	}
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	if err := h.backfillSources(ctx); err != nil {
		return err
	}
	if err := h.followJournal(ctx); err != nil {
		return err
	}
	var seq int64
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, userID).Scan(&seq); err != nil {
		return err
	}
	if last, ok := h.checkedAt[userID]; ok && last == seq {
		return nil
	}
	if err := h.cleanupInvalid(ctx, userID); err != nil {
		return err
	}
	if err := h.addMissing(ctx, userID); err != nil {
		return err
	}
	h.checkedAt[userID] = seq
	return nil
}

func (h *Handler) followJournal(ctx context.Context) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var cursor string
	err = tx.QueryRowContext(ctx, `SELECT value FROM photo_index_state WHERE key='cursor'`).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes`).Scan(&cursor); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO photo_index_state(key,value) VALUES('cursor',?)`, cursor); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	position, err := parseSeq(cursor)
	if err != nil {
		return err
	}
	for {
		rows, queryErr := h.db.QueryContext(ctx, `SELECT seq,user_id,node_id,op FROM changes WHERE seq>? ORDER BY seq LIMIT ?`, position, changePageSize)
		if queryErr != nil {
			return queryErr
		}
		type change struct {
			seq, userID int64
			nodeID, op  string
		}
		changes := make([]change, 0, changePageSize)
		for rows.Next() {
			var item change
			if queryErr = rows.Scan(&item.seq, &item.userID, &item.nodeID, &item.op); queryErr != nil {
				rows.Close()
				return queryErr
			}
			changes = append(changes, item)
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return queryErr
		}
		rows.Close()
		if len(changes) == 0 {
			return nil
		}
		tx, queryErr = h.db.BeginTx(ctx, nil)
		if queryErr != nil {
			return queryErr
		}
		for _, item := range changes {
			if item.op == "trash" || item.op == "rename" || item.op == "move" {
				if queryErr = h.applyChange(ctx, tx, item.userID, item.nodeID, item.op); queryErr != nil {
					_ = tx.Rollback()
					return queryErr
				}
			}
		}
		position = changes[len(changes)-1].seq
		if _, queryErr = tx.ExecContext(ctx, `UPDATE photo_index_state SET value=? WHERE key='cursor'`, fmt.Sprint(position)); queryErr != nil {
			_ = tx.Rollback()
			return queryErr
		}
		if queryErr = tx.Commit(); queryErr != nil {
			return queryErr
		}
		if len(changes) < changePageSize {
			return nil
		}
	}
}

func parseSeq(value string) (int64, error) {
	var seq int64
	if _, err := fmt.Sscan(value, &seq); err != nil || seq < 0 {
		return 0, errors.New("photos: stored change cursor is invalid")
	}
	return seq, nil
}

func (h *Handler) applyChange(ctx context.Context, tx *sql.Tx, userID int64, nodeID, op string) error {
	if op == "trash" {
		_, err := tx.ExecContext(ctx, `WITH RECURSIVE below(id) AS (
			SELECT id FROM nodes WHERE id=? AND user_id=?
			UNION SELECT child.id FROM nodes child JOIN below ON child.parent_id=below.id WHERE child.user_id=?
		) DELETE FROM photo_items WHERE node_id IN (SELECT id FROM below)`, nodeID, userID, userID)
		return err
	}
	var hidden int
	if err := tx.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id) AS (
		SELECT id,parent_id,trashed_at,vault_id FROM nodes WHERE id=? AND user_id=?
		UNION SELECT n.id,n.parent_id,n.trashed_at,n.vault_id FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT COALESCE(MAX(trashed_at IS NOT NULL OR vault_id IS NOT NULL),0) FROM up`, nodeID, userID, userID).Scan(&hidden); err != nil {
		return err
	}
	if hidden != 0 {
		_, err := tx.ExecContext(ctx, `WITH RECURSIVE below(id) AS (
			SELECT id FROM nodes WHERE id=? AND user_id=?
			UNION SELECT child.id FROM nodes child JOIN below ON child.parent_id=below.id WHERE child.user_id=?
		) DELETE FROM photo_items WHERE node_id IN (SELECT id FROM below)`, nodeID, userID, userID)
		return err
	}
	var nodeType, mime string
	err := tx.QueryRowContext(ctx, `SELECT type,COALESCE(mime,'') FROM nodes WHERE id=? AND user_id=?`, nodeID, userID).Scan(&nodeType, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if nodeType == "file" && !isMedia(mime) {
		_, err = tx.ExecContext(ctx, `DELETE FROM photo_items WHERE node_id=?`, nodeID)
	}
	return err
}

func (h *Handler) cleanupInvalid(ctx context.Context, userID int64) error {
	_, err := h.db.ExecContext(ctx, `WITH RECURSIVE hidden(id) AS (
		SELECT id FROM nodes WHERE user_id=? AND (trashed_at IS NOT NULL OR vault_id IS NOT NULL)
		UNION SELECT child.id FROM nodes child JOIN hidden ON child.parent_id=hidden.id WHERE child.user_id=?
	) DELETE FROM photo_items WHERE user_id=? AND node_id IN (SELECT id FROM hidden)`, userID, userID, userID)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `DELETE FROM photo_items WHERE user_id=? AND node_id IN (
		SELECT p.node_id FROM photo_items p JOIN nodes n ON n.id=p.node_id
		WHERE p.user_id=? AND n.user_id=? AND (n.type!='file' OR n.blob_id IS NULL OR NOT (`+mediaSQL+`))
	)`, userID, userID, userID)
	return err
}

const mediaSQL = `COALESCE(((n.mime LIKE 'image/%' AND lower(n.mime) NOT IN ('image/svg+xml','image/x-icon','image/vnd.microsoft.icon')) OR n.mime LIKE 'video/%'),0)`

func isMedia(mime string) bool {
	value := strings.ToLower(mime)
	if strings.HasPrefix(value, "video/") {
		return true
	}
	return strings.HasPrefix(value, "image/") && value != "image/svg+xml" && value != "image/x-icon" && value != "image/vnd.microsoft.icon"
}

func (h *Handler) addMissing(ctx context.Context, userID int64) error {
	for {
		rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE hidden(id) AS (
			SELECT id FROM nodes WHERE user_id=? AND (trashed_at IS NOT NULL OR vault_id IS NOT NULL)
			UNION SELECT child.id FROM nodes child JOIN hidden ON child.parent_id=hidden.id WHERE child.user_id=?
		) SELECT n.id,n.name,n.mime,n.blob_id,n.modified_at,d.created_at,d.modified_at
		FROM nodes n LEFT JOIN file_client_dates d ON d.node_id=n.id AND d.blob_id=n.blob_id
		WHERE n.user_id=? AND n.type='file' AND n.blob_id IS NOT NULL AND `+mediaSQL+`
		AND n.id NOT IN (SELECT id FROM hidden)
		AND NOT EXISTS (SELECT 1 FROM photo_items p WHERE p.node_id=n.id AND p.blob_id=n.blob_id)
		ORDER BY n.id LIMIT ?`, userID, userID, userID, addBatchSize)
		if err != nil {
			return err
		}
		type candidate struct {
			id, name, mime, blobID, modified string
			created, clientModified          sql.NullString
		}
		items := make([]candidate, 0, addBatchSize)
		for rows.Next() {
			var item candidate
			if err = rows.Scan(&item.id, &item.name, &item.mime, &item.blobID, &item.modified, &item.created, &item.clientModified); err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(items) == 0 {
			return nil
		}
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, item := range items {
			takenAt, source := resolveTakenAt(item.name, item.created, item.clientModified, item.modified, "")
			kind := "photo"
			if strings.HasPrefix(strings.ToLower(item.mime), "video/") {
				kind = "video"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO photo_items(node_id,user_id,blob_id,kind,taken_at,taken_at_source,date_state,probed)
				VALUES(?,?,?,?,?,?,2,0)
				ON CONFLICT(node_id) DO UPDATE SET blob_id=excluded.blob_id,kind=excluded.kind,taken_at=excluded.taken_at,
				taken_at_source=excluded.taken_at_source,date_state=2,width=NULL,height=NULL,duration_seconds=NULL,probed=0`,
				item.id, userID, item.blobID, kind, takenAt, source)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if len(items) < addBatchSize {
			return nil
		}
	}
}

func resolveTakenAt(name string, created, clientModified sql.NullString, modified, exif string) (string, string) {
	if value := normalizeDateTime(exif); value != "" {
		return value, "exif"
	}
	if created.Valid {
		if value := normalizeDateTime(created.String); value != "" {
			return value, "client"
		}
	}
	if value := dateFromName(name); value != "" {
		return value, "filename"
	}
	if clientModified.Valid {
		if value := normalizeDateTime(clientModified.String); value != "" {
			return value, "mtime"
		}
	}
	if value := normalizeDateTime(modified); value != "" {
		return value, "upload"
	}
	return time.Now().UTC().Format("2006-01-02T15:04:05"), "upload"
}

func normalizeDateTime(raw string) string {
	value := strings.TrimSpace(strings.Replace(raw, " ", "T", 1))
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:04:05")
		}
	}
	return ""
}

// dateFromName reads when a camera or phone named the file:
// IMG_20260312_101010.jpg, 2026-03-12 10.10.10.jpg and the like.
func dateFromName(name string) string {
	match := photoNameDate.FindStringSubmatch(name)
	// Both date separators have to be the same.
	if len(match) == 0 || match[2] != match[4] {
		return ""
	}
	year, month, day := match[1], match[3], match[5]
	hour, minute, second := match[6], match[7], match[8]
	if hour == "" {
		hour, minute, second = "00", "00", "00"
	}
	value := year + "-" + month + "-" + day + "T" + hour + ":" + minute + ":" + second
	if !plausible(value) {
		return ""
	}
	return value
}

// backfillSources works out, without reading files, where the date of a row
// written before taken_at_source existed came from. A read row whose date is
// still the upload time had no date of its own.
func (h *Handler) backfillSources(ctx context.Context) error {
	if h.backfilled {
		return nil
	}
	for {
		rows, err := h.db.QueryContext(ctx, `SELECT p.node_id,p.taken_at,p.probed,n.name,n.modified_at
			FROM photo_items p JOIN nodes n ON n.id=p.node_id WHERE p.taken_at_source IS NULL LIMIT 1000`)
		if err != nil {
			return err
		}
		type row struct {
			nodeID, takenAt, name, modified string
			probed                          int
		}
		var found []row
		for rows.Next() {
			var item row
			if err = rows.Scan(&item.nodeID, &item.takenAt, &item.probed, &item.name, &item.modified); err != nil {
				rows.Close()
				return err
			}
			found = append(found, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, item := range found {
			source := "upload"
			switch {
			case dateFromName(item.name) == item.takenAt:
				source = "filename"
			case normalizeDateTime(item.modified) != item.takenAt && item.probed == 1:
				source = "exif"
			}
			state := 1
			if item.probed == 0 {
				state = 2
			} else if source == "upload" {
				state = 0
			}
			if _, err = h.db.ExecContext(ctx, `UPDATE photo_items SET taken_at_source=?,date_state=? WHERE node_id=?`, source, state, item.nodeID); err != nil {
				return err
			}
		}
		if len(found) < 1000 {
			h.backfilled = true
			return nil
		}
	}
}

// RunPass reconciles photo rows for all accounts and probes a bounded number
// of unprocessed media files. It is safe to call from a single managed worker.
func (h *Handler) RunPass(ctx context.Context, maxFiles int) (int, error) {
	if maxFiles <= 0 {
		maxFiles = defaultProbeN
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return 0, err
	}
	users := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		users = append(users, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, userID := range users {
		if err = h.CatchUp(ctx, userID); err != nil {
			return 0, err
		}
	}
	return h.probePending(ctx, maxFiles)
}

// StartWorker starts the bounded indexing loop and returns a stop function
// that cancels and joins it. Application composition owns calling this once.
func (h *Handler) StartWorker(parent context.Context) func() {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			case <-h.wake:
			}
			processed, err := h.RunPass(ctx, defaultProbeN)
			if err != nil && ctx.Err() == nil {
				slog.Error("NeoCloud photo index pass failed", "error", err)
			}
			delay := 5 * time.Second
			if err == nil && processed > 0 {
				delay = 50 * time.Millisecond
			}
			if err == nil {
				hashed, hashErr := h.HashPass(ctx, duplicateHashBatch)
				if hashErr != nil && ctx.Err() == nil {
					slog.Error("NeoCloud photo duplicate hash pass failed", "error", hashErr)
				}
				if hashErr == nil && hashed > 0 {
					delay = 500 * time.Millisecond
				}
			}
			timer.Reset(delay)
		}
	}()
	var once sync.Once
	return func() { once.Do(cancel); <-done }
}
