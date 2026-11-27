package imports

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

const maxErrors = 50

const (
	queued    = "queued"
	scanning  = "scanning"
	running   = "running"
	paused    = "paused"
	done      = "done"
	failed    = "failed"
	cancelled = "cancelled"
)

type job struct {
	id, source, url, username, sourcePath, targetID, status, createdAt string
	userID                                                             int64
	password                                                           sql.NullString
	startedAt, finishedAt                                              sql.NullString
	includeFavorites, scanned                                          bool
}

const jobColumns = `SELECT id,user_id,source,url,username,password_enc,source_path,target_folder_id,include_favorites,status,scanned,created_at,started_at,finished_at FROM import_jobs`

func scanJob(row interface{ Scan(...any) error }) (job, error) {
	var j job
	err := row.Scan(&j.id, &j.userID, &j.source, &j.url, &j.username, &j.password, &j.sourcePath, &j.targetID, &j.includeFavorites, &j.status, &j.scanned, &j.createdAt, &j.startedAt, &j.finishedAt)
	return j, err
}

func (h *Handler) jobByID(ctx context.Context, id string) (job, error) {
	return scanJob(h.db.QueryRowContext(ctx, jobColumns+` WHERE id=?`, id))
}

// owned is a job of this account; another account's job is missing.
func (h *Handler) owned(ctx context.Context, userID int64, id string) (job, error) {
	j, err := scanJob(h.db.QueryRowContext(ctx, jobColumns+` WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return j, apperr.New(http.StatusNotFound, "Import not found", "NOT_FOUND")
	}
	return j, err
}

type jobError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func optional(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (h *Handler) serialize(ctx context.Context, j job) (map[string]any, error) {
	var total, totalBytes, doneFiles, doneBytes, skipped, failedFiles int64
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(size),0),COALESCE(SUM(state IN ('done','skipped')),0),
		COALESCE(SUM(CASE WHEN state IN ('done','skipped') THEN size ELSE 0 END),0),COALESCE(SUM(state='skipped'),0),COALESCE(SUM(state='failed'),0)
		FROM import_items WHERE job_id=? AND type='file'`, j.id).Scan(&total, &totalBytes, &doneFiles, &doneBytes, &skipped, &failedFiles)
	if err != nil {
		return nil, err
	}
	problems, err := h.jobErrors(ctx, j.id)
	if err != nil {
		return nil, err
	}
	target, err := h.targetPath(ctx, j.userID, j.targetID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": j.id, "source": j.source, "url": j.url, "username": j.username, "sourcePath": j.sourcePath,
		"targetFolderId": j.targetID, "targetPath": target, "includeFavorites": j.includeFavorites, "status": j.status,
		"totalFiles": total, "totalBytes": totalBytes, "doneFiles": doneFiles, "doneBytes": doneBytes,
		"skippedFiles": skipped, "failedFiles": failedFiles, "errors": problems,
		"startedAt": optional(j.startedAt), "finishedAt": optional(j.finishedAt), "createdAt": j.createdAt,
	}, nil
}

// jobErrors are the newest problems of a job, oldest first.
func (h *Handler) jobErrors(ctx context.Context, jobID string) ([]jobError, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT path,message FROM import_errors WHERE job_id=? ORDER BY id DESC LIMIT ?`, jobID, maxErrors)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	problems := []jobError{}
	for rows.Next() {
		var problem jobError
		if err = rows.Scan(&problem.Path, &problem.Message); err != nil {
			return nil, err
		}
		problems = append(problems, problem)
	}
	slices.Reverse(problems)
	return problems, rows.Err()
}

// targetPath names the folder from the account's root, trashed or not.
func (h *Handler) targetPath(ctx context.Context, userID int64, folderID string) (string, error) {
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE up(parent_id,name,depth) AS (
		SELECT parent_id,name,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.parent_id,n.name,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id
	) SELECT name FROM up WHERE parent_id IS NOT NULL ORDER BY depth DESC`, folderID, userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return "", err
		}
		names = append(names, name)
	}
	return "/" + strings.Join(names, "/"), rows.Err()
}

// setStatus moves a job to status if it is currently in one of from. Ended jobs
// lose their stored password. It reports whether the row changed.
func (h *Handler) setStatus(ctx context.Context, jobID, status string, from ...string) (bool, error) {
	changed, err := setStatus(ctx, h.db, jobID, status, from...)
	if err == nil && changed {
		h.announceStatus(ctx, jobID)
	}
	return changed, err
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func setStatus(ctx context.Context, db execer, jobID, status string, from ...string) (bool, error) {
	ended := status == done || status == failed || status == cancelled
	allowed, err := json.Marshal(from)
	if err != nil {
		return false, err
	}
	result, err := db.ExecContext(ctx, `UPDATE import_jobs SET
		status=?1,
		password_enc=CASE WHEN ?2 THEN NULL ELSE password_enc END,
		started_at=CASE WHEN ?1 IN ('scanning','running') THEN COALESCE(started_at,datetime('now')) ELSE started_at END,
		finished_at=CASE WHEN ?2 THEN datetime('now') ELSE NULL END
		WHERE id=?3 AND status IN (SELECT value FROM json_each(?4))`, status, ended, jobID, string(allowed))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (h *Handler) announceStatus(ctx context.Context, jobID string) {
	if j, err := h.jobByID(ctx, jobID); err == nil {
		h.Announce(j.userID, jobID, true)
	}
}

// describe is what a person reads about a failure; technical causes stay in
// the log.
func describe(err error) string {
	if known := apijson.Public(err); known != nil && known.Code != "" {
		return known.Message
	}
	slog.Warn("Import step failed", "error", err)
	return "Something went wrong on this server."
}

// addError records a problem with one path ("/" for the job itself), keeping
// the newest maxErrors.
func (h *Handler) addError(jobID, path string, cause error) {
	_, err := h.db.Exec(`INSERT INTO import_errors(job_id,path,message) VALUES(?,?,?)`, jobID, path, describe(cause))
	if err == nil {
		_, err = h.db.Exec(`DELETE FROM import_errors WHERE job_id=? AND id NOT IN (SELECT id FROM import_errors WHERE job_id=? ORDER BY id DESC LIMIT ?)`, jobID, jobID, maxErrors)
	}
	if err != nil {
		slog.Error("Could not record an import error", "job", jobID, "error", err)
	}
}

func (h *Handler) setItemState(ctx context.Context, jobID, path, state string) error {
	_, err := h.db.ExecContext(ctx, `UPDATE import_items SET state=? WHERE job_id=? AND path=?`, state, jobID, path)
	return err
}

func (h *Handler) failItem(ctx context.Context, jobID, path string, cause error) error {
	err := h.setItemState(ctx, jobID, path, failed)
	h.addError(jobID, "/"+path, cause)
	return err
}

type importItem struct {
	path     string
	size     int64
	mtime    sql.NullString
	favorite bool
}

// items lists a job's items of one type and state.
func (h *Handler) items(ctx context.Context, jobID, itemType, state, order string) ([]importItem, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT path,size,mtime,favorite FROM import_items WHERE job_id=? AND type=? AND state=? ORDER BY `+order, jobID, itemType, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []importItem
	for rows.Next() {
		var item importItem
		if err = rows.Scan(&item.path, &item.size, &item.mtime, &item.favorite); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
