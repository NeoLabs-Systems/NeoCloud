package imports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	sqlite3 "modernc.org/sqlite/lib"
)

// Jobs run one at a time per account, two downloads at a time per job. A job
// lists the remote tree folder by folder into import_items, creates the folders,
// then streams each file into storage. Progress lives in import_items, so a job
// stopped anywhere (pause, restart, crash) carries on where it was.

const (
	parallelDownloads = 2
	// The remote server decides how big a tree it reports; a scan stops (and the
	// job fails) past these, before import_items grows without bound.
	maxScanItems  = 250000
	maxScanDepth  = 64
	maxPathLength = 4096
)

type run struct {
	userID   int64
	cancel   context.CancelFunc
	finished chan struct{}
}

// StartWorker puts jobs that were running when the server stopped back in the
// queue and starts running them.
func (h *Handler) StartWorker(parent context.Context) {
	h.mu.Lock()
	started := h.stop != nil
	if !started {
		h.runContext, h.stop = context.WithCancel(parent)
	}
	h.mu.Unlock()
	if started {
		return
	}
	result, err := h.db.ExecContext(parent, `UPDATE import_jobs SET status='queued' WHERE status IN ('scanning','running')`)
	if err != nil {
		slog.Error("Could not recover interrupted imports", "error", err)
	} else if count, _ := result.RowsAffected(); count > 0 {
		slog.Info("Resuming interrupted imports", "count", count)
	}
	h.wake()
}

// Close stops all downloads, leaving the jobs to resume on the next start.
func (h *Handler) Close() {
	h.mu.Lock()
	if h.stop != nil {
		h.stop()
		h.stop, h.runContext = nil, nil
	}
	runs := make([]*run, 0, len(h.active))
	for _, current := range h.active {
		runs = append(runs, current)
	}
	h.mu.Unlock()
	for _, current := range runs {
		<-current.finished
	}
}

// wake starts queued jobs of accounts that have none running.
func (h *Handler) wake() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runContext == nil || h.runContext.Err() != nil {
		return
	}
	rows, err := h.db.QueryContext(h.runContext, `SELECT id,user_id FROM import_jobs WHERE status='queued' ORDER BY created_at,rowid`)
	if err != nil {
		slog.Error("Could not read the import queue", "error", err)
		return
	}
	defer rows.Close()
	busy := map[int64]bool{}
	for _, current := range h.active {
		busy[current.userID] = true
	}
	for rows.Next() {
		var id string
		var userID int64
		if err = rows.Scan(&id, &userID); err != nil {
			slog.Error("Could not read the import queue", "error", err)
			return
		}
		if busy[userID] {
			continue
		}
		busy[userID] = true
		ctx, cancel := context.WithCancel(h.runContext)
		current := &run{userID: userID, cancel: cancel, finished: make(chan struct{})}
		h.active[id] = current
		go func() {
			h.runJob(ctx, id)
			cancel()
			h.mu.Lock()
			delete(h.active, id)
			h.mu.Unlock()
			close(current.finished)
			h.wake()
		}()
	}
}

// interrupt stops a running job's downloads; its new status is already set.
func (h *Handler) interrupt(jobID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current := h.active[jobID]; current != nil {
		current.cancel()
	}
}

func asDiskFull(err error) bool {
	if known, ok := apperr.As(err); ok {
		return known.Code == "DISK_FULL"
	}
	var coded interface {
		HTTPStatus() int
		ErrorCode() string
	}
	if errors.As(err, &coded) {
		return coded.ErrorCode() == "DISK_FULL"
	}
	var database interface{ Code() int }
	return errors.Is(err, syscall.ENOSPC) || errors.As(err, &database) && database.Code() == sqlite3.SQLITE_FULL
}

// runJob records how a job ended. A cancelled context means someone else
// already decided its status.
func (h *Handler) runJob(ctx context.Context, id string) {
	err := h.process(ctx, id)
	if err == nil || ctx.Err() != nil {
		return
	}
	status := failed
	// A full disk is the server's problem, not the job's: wait for space.
	if asDiskFull(err) {
		status = paused
		err = apperr.New(http.StatusInsufficientStorage, "NeoCloud is out of space right now. Try again later or contact the person who runs it.", "DISK_FULL")
	}
	h.addError(id, "/", err)
	if _, recordErr := h.setStatus(context.WithoutCancel(ctx), id, status, queued, scanning, running); recordErr != nil {
		slog.Error("Could not record the end of an import", "job", id, "error", recordErr)
	}
}

func (h *Handler) process(ctx context.Context, id string) error {
	j, err := h.jobByID(ctx, id)
	if err != nil || j.status != queued {
		return err
	}
	if j.password.String == "" {
		return passwordRequired()
	}
	next := scanning
	if j.scanned {
		next = running
	}
	if changed, err := h.setStatus(ctx, id, next, queued); err != nil || !changed {
		return err
	}
	if _, err = h.targetFolder(ctx, j); err != nil {
		return err
	}
	password, err := h.keys.DecryptValue(j.password.String)
	if err != nil {
		return err
	}
	remote, err := h.clientFor(ctx, j.userID, connection{source: j.source, url: j.url, username: j.username, password: password, sourcePath: j.sourcePath}, defaultRetries)
	if err != nil {
		return err
	}
	defer remote.close()
	if err = remote.connect(ctx); err != nil {
		return err
	}
	if !j.scanned {
		if err = h.scan(ctx, j, remote); err != nil {
			return err
		}
		if _, err = h.setStatus(ctx, id, running, scanning); err != nil {
			return err
		}
	}
	if err = h.assertRoomFor(ctx, j); err != nil {
		return err
	}
	if err = h.createFolders(ctx, j); err != nil {
		return err
	}
	if err = h.transferFiles(ctx, j, remote); err != nil {
		return err
	}
	if err = h.finishFolders(ctx, j); err != nil {
		return err
	}
	_, err = h.setStatus(ctx, id, done, running)
	return err
}

func (h *Handler) scan(ctx context.Context, j job, remote *client) error {
	folders := []string{""}
	items := 0
	for len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]
		_, children, err := remote.list(ctx, folder)
		if err != nil {
			if folder == "" || ctx.Err() != nil {
				return err
			}
			h.addError(j.id, "/"+folder, err)
			continue
		}
		if items += len(children); items > maxScanItems {
			return invalid(fmt.Sprintf("The source has more than %d files and folders; import a smaller folder.", maxScanItems), "IMPORT_TOO_LARGE")
		}
		found, err := h.record(ctx, j.id, folder, children)
		if err != nil {
			return err
		}
		folders = append(folders, found...)
		h.Announce(j.userID, j.id, false)
	}
	_, err := h.db.ExecContext(ctx, `UPDATE import_jobs SET scanned=1 WHERE id=?`, j.id)
	return err
}

// record stores one folder listing and returns the folders in it. A changed
// item is fetched again.
func (h *Handler) record(ctx context.Context, jobID, folder string, children []remoteItem) ([]string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	upsert, err := tx.PrepareContext(ctx, `INSERT INTO import_items(job_id,path,type,size,mtime,favorite) VALUES(?,?,?,?,?,?)
		ON CONFLICT(job_id,path) DO UPDATE SET
		state=CASE WHEN import_items.type!=excluded.type OR import_items.size!=excluded.size OR import_items.mtime IS NOT excluded.mtime THEN 'pending' ELSE import_items.state END,
		type=excluded.type,size=excluded.size,mtime=excluded.mtime,favorite=excluded.favorite`)
	if err != nil {
		return nil, err
	}
	defer upsert.Close()
	var folders []string
	for _, child := range children {
		itemPath := child.name
		if folder != "" {
			itemPath = folder + "/" + child.name
		}
		if utf8.RuneCountInString(itemPath) > maxPathLength || strings.Count(itemPath, "/")+1 > maxScanDepth {
			return nil, invalid(fmt.Sprintf("The source has paths longer than %d characters or deeper than %d folders.", maxPathLength, maxScanDepth), "IMPORT_TOO_DEEP")
		}
		itemType, size := "file", child.size
		if child.folder {
			itemType, size = "folder", 0
			folders = append(folders, itemPath)
		}
		var mtime any
		if child.mtime != "" {
			mtime = child.mtime
		}
		if _, err = upsert.ExecContext(ctx, jobID, itemPath, itemType, size, mtime, child.favorite); err != nil {
			return nil, err
		}
	}
	return folders, tx.Commit()
}

func (h *Handler) createFolders(ctx context.Context, j job) error {
	pending, err := h.items(ctx, j.id, "folder", "pending", "path")
	if err != nil {
		return err
	}
	for _, item := range pending {
		if _, err = h.folderFor(ctx, j, item.path); err == nil {
			err = h.setItemState(ctx, j.id, item.path, done)
		} else if ctx.Err() == nil && !asDiskFull(err) {
			err = h.failItem(ctx, j.id, item.path, err)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) transferFiles(ctx context.Context, j job, remote *client) error {
	pending, err := h.items(ctx, j.id, "file", "pending", "path")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	queue := make(chan importItem)
	var workers sync.WaitGroup
	for range parallelDownloads {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				if ctx.Err() != nil {
					continue
				}
				state, err := h.storeFile(ctx, j, remote, item)
				switch {
				case err == nil:
					err = h.setItemState(ctx, j.id, item.path, state)
				case ctx.Err() != nil:
					continue
				case !asDiskFull(err):
					// Anything else is this file's problem alone.
					err = h.failItem(ctx, j.id, item.path, err)
				}
				if err != nil {
					cancel(err)
					continue
				}
				h.Announce(j.userID, j.id, false)
			}
		}()
	}
	for _, item := range pending {
		queue <- item
	}
	close(queue)
	workers.Wait()
	return context.Cause(ctx)
}
