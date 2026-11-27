package files

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// GeneratedFile is a file committed through the same encrypted blob, quota,
// activity, version, and sync-journal path as a client upload.
type GeneratedFile struct {
	ID   string
	Name string
	Etag string
	Size int64
}

// StoreGeneratedFile encrypts and commits a server-produced file. Repeating a
// commit with identical content and name returns the existing node, which
// makes background finalization safe to retry after a crash.
func (h *Handler) StoreGeneratedFile(ctx context.Context, userID int64, parentID, name string, content []byte) (GeneratedFile, error) {
	if ctx == nil || userID < 1 || len(content) == 0 {
		return GeneratedFile{}, errors.New("files: user, context, and non-empty generated content are required")
	}
	cleanName, err := validateName(name)
	if err != nil {
		return GeneratedFile{}, err
	}
	if parentID == "" {
		parentID = "root"
	}
	if err = h.CheckDiskSpace(int64(len(content))); err != nil {
		return GeneratedFile{}, err
	}
	maxBytes, limitCode, err := h.uploadAllowance(ctx, userID)
	if err != nil {
		return GeneratedFile{}, err
	}
	if int64(len(content)) > maxBytes {
		return GeneratedFile{}, apiError{413, uploadLimitMessage(limitCode), limitCode}
	}
	blob, err := h.encryptStream(ctx, userID, bytes.NewReader(content), randomID(), maxBytes+1, maxBytes, apiError{413, uploadLimitMessage(limitCode), limitCode})
	if err != nil {
		return GeneratedFile{}, err
	}
	defer func() { _ = os.Remove(blob.tempPath) }()

	var existing node
	existing, err = scanNode(h.db.QueryRowContext(ctx, nodeSelect+` WHERE n.user_id=? AND n.parent_id=? AND lower(n.name)=lower(?) AND n.trashed_at IS NULL`, userID, parentID, cleanName))
	if err == nil && existing.Type == "file" && existing.Etag == blob.hash && existing.Size == blob.size {
		return GeneratedFile{ID: existing.ID, Name: existing.Name, Etag: existing.Etag, Size: existing.Size}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return GeneratedFile{}, err
	}
	blob.finalPath, err = h.placeUpload(userID, blob)
	if err != nil {
		return GeneratedFile{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(blob.finalPath)
		}
	}()
	created, orphaned, err := h.commitUpload(ctx, userID, userID, parentID, cleanName, "rename", "", "", blob, nil, nil)
	if err != nil {
		return GeneratedFile{}, fmt.Errorf("files: commit generated content: %w", err)
	}
	committed = true
	h.RemoveBlobFiles(userID, orphaned)
	return GeneratedFile{ID: created.ID, Name: created.Name, Etag: created.Etag, Size: created.Size}, nil
}
