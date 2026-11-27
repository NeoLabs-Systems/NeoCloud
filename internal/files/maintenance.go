package files

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// PurgeExpiredTrash removes expired trash roots through the same transaction
// used by explicit permanent deletion, preserving blob references, quota and
// durable change-journal semantics.
func (h *Handler) PurgeExpiredTrash(ctx context.Context) error {
	days := h.storagePolicy().TrashRetentionDays
	if days <= 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,user_id FROM nodes
		WHERE trashed_at IS NOT NULL AND datetime(trashed_at)<=datetime('now',?)`, "-"+strconv.Itoa(days)+" days")
	if err != nil {
		return err
	}
	type expiredNode struct {
		id  string
		uid int64
	}
	var expired []expiredNode
	for rows.Next() {
		var item expiredNode
		if err = rows.Scan(&item.id, &item.uid); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range expired {
		orphans, purgeErr := h.purgeExpiredTrashRoot(ctx, item.uid, item.id, days)
		if purgeErr != nil {
			slog.Error("Could not purge expired trash root", "userId", item.uid, "nodeId", item.id, "error", purgeErr)
			continue
		}
		h.RemoveBlobFiles(item.uid, orphans)
	}
	return nil
}

func (h *Handler) purgeExpiredTrashRoot(ctx context.Context, uid int64, id string, days int) ([]string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NOT NULL
		AND datetime(trashed_at)<=datetime('now',?)`, id, uid, "-"+strconv.Itoa(days)+" days").Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	orphans, err := purgeSubtreeTx(tx, uid, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return orphans, nil
}

// PurgeExpiredUploads deletes expired upload reservations and their encrypted
// parts. It shares uploadsMu with completion and cancellation so a cleanup
// pass cannot unlink parts after a completion has claimed the session.
func (h *Handler) PurgeExpiredUploads(ctx context.Context) error {
	rows, err := h.db.QueryContext(ctx, `SELECT id FROM upload_sessions WHERE datetime(expires_at)<=datetime('now')`)
	if err != nil {
		return err
	}
	var expired []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	partsDir := filepath.Join(h.uploadTmpDir, "parts")
	for _, id := range expired {
		if !safeUploadID(id) {
			slog.Error("Skipping expired upload with unsafe identifier", "uploadId", id)
			continue
		}
		h.uploadsMu.Lock()
		if h.completingUploads[id] {
			h.uploadsMu.Unlock()
			continue
		}
		result, deleteErr := h.db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=? AND datetime(expires_at)<=datetime('now')`, id)
		if deleteErr != nil {
			h.uploadsMu.Unlock()
			return deleteErr
		}
		deleted, rowsErr := result.RowsAffected()
		h.uploadsMu.Unlock()
		if rowsErr != nil {
			return rowsErr
		}
		if deleted > 0 {
			if err = os.RemoveAll(filepath.Join(partsDir, id)); err != nil {
				slog.Error("Could not remove expired upload parts", "uploadId", id, "error", err)
			}
		}
	}
	if err = h.removeOrphanUploadParts(ctx, partsDir); err != nil {
		return err
	}
	if err = h.removeStaleUploadTemps(); err != nil {
		return err
	}
	return nil
}

func (h *Handler) removeOrphanUploadParts(ctx context.Context, partsDir string) error {
	entries, err := os.ReadDir(partsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if !safeUploadID(id) {
			continue
		}
		h.uploadsMu.Lock()
		var exists int
		err = h.db.QueryRowContext(ctx, `SELECT 1 FROM upload_sessions WHERE id=?`, id).Scan(&exists)
		h.uploadsMu.Unlock()
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = os.RemoveAll(filepath.Join(partsDir, id)); err != nil {
			slog.Error("Could not remove orphan upload parts", "uploadId", id, "error", err)
		}
	}
	return nil
}

func safeUploadID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id && id != string(filepath.Separator) && !filepath.IsAbs(id)
}

func (h *Handler) removeStaleUploadTemps() error {
	entries, err := os.ReadDir(h.uploadTmpDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	staleBefore := time.Now().Add(-uploadTTLHours * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".part" {
			continue
		}
		file := filepath.Join(h.uploadTmpDir, entry.Name())
		info, statErr := os.Stat(file)
		if statErr != nil {
			if !errors.Is(statErr, os.ErrNotExist) {
				slog.Error("Could not inspect temporary upload", "path", file, "error", statErr)
			}
			continue
		}
		if info.ModTime().Before(staleBefore) {
			if removeErr := os.Remove(file); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				slog.Error("Could not remove stale temporary upload", "path", file, "error", removeErr)
			}
		}
	}
	return nil
}
