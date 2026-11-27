package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// The AI job queue (table ai_jobs) and the jobs the person starts. A file has at
// most one pending job: asking again widens it. Only the account's own files:
// vault, excluded, trashed and shared-with-me files are refused or left out.

const (
	maxRequestNodes  = 1000
	maxBackfillFiles = 5000
)

// enqueue queues (or widens the pending job of) each of the owner's file ids.
func enqueue(ctx context.Context, tx *sql.Tx, userID int64, nodeIDs, kinds []string, source string, explicit bool, batchID string) error {
	for _, nodeID := range nodeIDs {
		var id int64
		var raw string
		var wasExplicit bool
		err := tx.QueryRowContext(ctx, `SELECT id,kinds,explicit FROM ai_jobs WHERE user_id=? AND node_id=? AND status='pending'`, userID, nodeID).Scan(&id, &raw, &wasExplicit)
		switch {
		case err == nil:
			var merged []string
			_ = json.Unmarshal([]byte(raw), &merged)
			for _, kind := range kinds {
				if !slices.Contains(merged, kind) {
					merged = append(merged, kind)
				}
			}
			_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET kinds=?,explicit=?,batch_id=COALESCE(?,batch_id),updated_at=datetime('now') WHERE id=?`, jsonText(merged), wasExplicit || explicit, nullableString(batchID), id)
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.ExecContext(ctx, `INSERT INTO ai_jobs(user_id,node_id,kinds,source,explicit,batch_id) VALUES(?,?,?,?,?,?)`, userID, nodeID, jsonText(kinds), source, explicit, nullableString(batchID))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func requestedKinds(enabled features, value any) ([]string, error) {
	if value == nil {
		var kinds []string
		for _, kind := range jobKinds {
			if enabled[kind] {
				kinds = append(kinds, kind)
			}
		}
		if len(kinds) == 0 {
			return nil, fail(http.StatusForbidden, "Every AI suggestion feature is turned off.", "AI_FEATURE_OFF")
		}
		return kinds, nil
	}
	invalid := fail(http.StatusBadRequest, "kinds must be a list of "+strings.Join(jobKinds, ", ")+".", "INVALID_KINDS")
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return nil, invalid
	}
	var kinds []string
	for _, item := range list {
		kind, ok := item.(string)
		if !ok || !slices.Contains(jobKinds, kind) {
			return nil, invalid
		}
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range kinds {
		if !enabled[kind] {
			return nil, fail(http.StatusForbidden, "This AI feature is turned off in Settings › AI.", "AI_FEATURE_OFF")
		}
	}
	return kinds, nil
}

// backfillFiles are the files below a folder AI may read.
func (h *Handler) backfillFiles(ctx context.Context, settings account, folderID string) ([]string, error) {
	type file struct{ id, name string }
	rows, err := queryAll(ctx, h.db, func(s scanner) (file, error) {
		var f file
		err := s.Scan(&f.id, &f.name)
		return f, err
	}, `WITH RECURSIVE down(id) AS (
		SELECT id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id FROM nodes n JOIN down ON n.parent_id=down.id WHERE n.trashed_at IS NULL AND n.vault_id IS NULL AND n.ai_excluded=0
	) SELECT n.id,n.name FROM down JOIN nodes n ON n.id=down.id WHERE n.type='file' AND n.blob_id IS NOT NULL`, folderID, settings.id)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, row := range rows {
		if !settings.skipsType(row.name) {
			ids = append(ids, row.id)
		}
	}
	if len(ids) > maxBackfillFiles {
		return nil, fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("A backfill covers at most %d files; pick a smaller folder.", maxBackfillFiles), "TOO_MANY_FILES")
	}
	return ids, nil
}

// requestJobs starts suggestions: nodeIds (their names are suggested even when
// they already read well) or folderId (a backfill over everything below it).
func (h *Handler) requestJobs(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	if err = h.assertUsable(settings); err != nil {
		return nil, err
	}
	kinds, err := requestedKinds(settings.features(), input["kinds"])
	if err != nil {
		return nil, err
	}
	byIDs, byFolder := input["nodeIds"] != nil, input["folderId"] != nil
	if byIDs == byFolder {
		return nil, fail(http.StatusBadRequest, "Send either nodeIds or folderId.", "INVALID_REQUEST")
	}
	var ids []string
	source := "backfill"
	if byIDs {
		source = "request"
		list, ok := input["nodeIds"].([]any)
		if !ok || len(list) == 0 || len(list) > maxRequestNodes {
			return nil, fail(http.StatusBadRequest, fmt.Sprintf("nodeIds must list 1 to %d files.", maxRequestNodes), "INVALID_REQUEST")
		}
		for _, item := range list {
			if id := fmt.Sprint(item); !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			if _, err = liveNode(ctx, h.db, userID, id, "file"); err != nil {
				return nil, err
			}
			if err = assertAllowed(ctx, h.db, userID, id); err != nil {
				return nil, err
			}
		}
	} else {
		folderID, _ := input["folderId"].(string)
		found, err := liveNode(ctx, h.db, userID, folderID, "folder")
		if err != nil {
			return nil, err
		}
		if err = assertAllowed(ctx, h.db, userID, found.ID); err != nil {
			return nil, err
		}
		if ids, err = h.backfillFiles(ctx, settings, found.ID); err != nil {
			return nil, err
		}
	}
	batchID := randomUUID()
	if err = h.inTx(ctx, func(tx *sql.Tx) error { return enqueue(ctx, tx, userID, ids, kinds, source, byIDs, batchID) }); err != nil {
		return nil, err
	}
	h.Wake()
	return map[string]any{"batchId": batchID, "queued": len(ids)}, nil
}

type batchProgress struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Total     int    `json:"total"`
	Pending   int    `json:"pending"`
	Done      int    `json:"done"`
	Skipped   int    `json:"skipped"`
	Failed    int    `json:"failed"`
}

// jobStatus is the account's queue: counts by status, why it is paused, and the
// progress of its recent requests and backfills (one, with batchId).
func (h *Handler) jobStatus(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	type count struct {
		status string
		total  int
	}
	rows, err := queryAll(ctx, h.db, func(s scanner) (count, error) {
		var c count
		err := s.Scan(&c.status, &c.total)
		return c, err
	}, `SELECT status,COUNT(*) FROM ai_jobs WHERE user_id=? GROUP BY status`, userID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"pending": 0, "running": 0, "done": 0, "skipped": 0, "failed": 0, "paused": nil}
	for _, row := range rows {
		result[row.status] = row.total
	}
	batchID := r.URL.Query().Get("batchId")
	var filter any
	if r.URL.Query().Has("batchId") {
		filter = batchID
	}
	batches, err := queryAll(ctx, h.db, func(s scanner) (batchProgress, error) {
		var b batchProgress
		err := s.Scan(&b.ID, &b.CreatedAt, &b.Total, &b.Pending, &b.Done, &b.Skipped, &b.Failed)
		return b, err
	}, `SELECT batch_id,MIN(created_at),COUNT(*),SUM(status IN ('pending','running')),SUM(status='done'),SUM(status='skipped'),SUM(status='failed')
		FROM ai_jobs WHERE user_id=? AND batch_id IS NOT NULL AND (? IS NULL OR batch_id=?) GROUP BY batch_id ORDER BY MIN(id) DESC LIMIT 10`, userID, filter, filter)
	if err != nil {
		return nil, err
	}
	if batchID != "" && len(batches) == 0 {
		return nil, fail(http.StatusNotFound, "Batch not found", "NOT_FOUND")
	}
	if result["pending"] != 0 {
		reason, err := h.pauseReason(ctx, userID)
		if err != nil {
			return nil, err
		}
		result["paused"] = nullableString(reason)
	}
	result["batches"] = batches
	return result, nil
}
