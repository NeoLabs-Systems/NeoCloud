package files

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
)

// OfficeFile is the live, access-checked file metadata needed by a WOPI host.
type OfficeFile struct {
	OwnerID    int64
	ID         string
	ParentID   string
	Name       string
	MIME       string
	Size       int64
	ETag       string
	Version    int64
	ModifiedAt string
	UserName   string
	CanWrite   bool
}

// OpenOfficeFile resolves owner or current shared access on every WOPI call.
func (h *Handler) OpenOfficeFile(ctx context.Context, viewerID int64, nodeID string, wantWrite bool) (OfficeFile, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return OfficeFile{}, err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, viewerID).Scan(&active); err != nil {
		return OfficeFile{}, err
	}
	if active == 0 {
		return OfficeFile{}, apiError{401, "Account unavailable", "UNAUTHORIZED"}
	}
	n, shared, err := h.resolveReadNode(tx, viewerID, nodeID)
	if err != nil {
		return OfficeFile{}, err
	}
	if n.Type != "file" || n.VaultID != nil || n.TrashedAt != nil || n.ParentID == nil {
		return OfficeFile{}, apiError{404, "File not found", "NOT_FOUND"}
	}
	ownerID := viewerID
	canWrite := wantWrite
	if shared != nil {
		ownerID = shared.ownerID
		canWrite = wantWrite && shared.permission == "write"
	}
	var username string
	if err = tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id=? AND disabled_at IS NULL`, viewerID).Scan(&username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OfficeFile{}, apiError{401, "Account unavailable", "UNAUTHORIZED"}
		}
		return OfficeFile{}, err
	}
	if err = tx.Commit(); err != nil {
		return OfficeFile{}, err
	}
	return OfficeFile{OwnerID: ownerID, ID: n.ID, ParentID: *n.ParentID, Name: n.Name, MIME: mimeOrDefault(n.Mime), Size: n.Size, ETag: n.Etag, Version: n.Version, ModifiedAt: n.ModifiedAt, UserName: username, CanWrite: canWrite}, nil
}

// ServeOfficeFile sends the current content like a download, after the same
// access check.
func (h *Handler) ServeOfficeFile(w http.ResponseWriter, r *http.Request, viewerID int64, nodeID string) {
	request := r.Clone(requestauth.WithIdentity(r.Context(), requestauth.RequestIdentity{UserID: viewerID}))
	request.URL.RawQuery = ""
	request.SetPathValue("id", nodeID)
	h.download(w, request)
}

// SaveOfficeFile writes what the document editor sends back through the normal
// encrypted blob, quota, version, activity and sync-journal path. The commit
// only happens while the file still has expectedETag and lock is the file's
// current WOPI lock ("" for none), and records the actor as the author.
// size is -1 when the editor does not announce it. Saving the current content
// again changes nothing.
func (h *Handler) SaveOfficeFile(ctx context.Context, actorID int64, nodeID, expectedETag string, body io.Reader, size int64, lock string) (OfficeFile, error) {
	meta, err := h.OpenOfficeFile(ctx, actorID, nodeID, true)
	if err != nil {
		return OfficeFile{}, err
	}
	if !meta.CanWrite {
		return OfficeFile{}, apiError{403, "You may only view this file.", "READ_ONLY"}
	}
	if size >= 0 {
		if err = h.CheckUploadFits(ctx, meta.OwnerID, size); err != nil {
			return OfficeFile{}, err
		}
	}
	if err = h.CheckDiskSpace(size); err != nil {
		return OfficeFile{}, err
	}
	maxBytes, limitCode, err := h.uploadAllowance(ctx, meta.OwnerID)
	if err != nil {
		return OfficeFile{}, err
	}
	blob, err := h.encryptStream(ctx, meta.OwnerID, body, randomID(), uploadReadLimit(maxBytes), maxBytes, apiError{413, uploadLimitMessage(limitCode), limitCode})
	if err != nil {
		return OfficeFile{}, err
	}
	if size >= 0 && blob.size != size {
		_ = os.Remove(blob.tempPath)
		return OfficeFile{}, apiError{400, "The uploaded document size does not match Content-Length.", "SIZE_MISMATCH"}
	}
	if blob.hash == meta.ETag && blob.size == meta.Size {
		_ = os.Remove(blob.tempPath)
		return meta, nil
	}
	blob.finalPath, err = h.placeUpload(meta.OwnerID, blob)
	if err != nil {
		_ = os.Remove(blob.tempPath)
		return OfficeFile{}, err
	}
	updated, orphans, err := h.commitUploadWithWOPILock(ctx, meta.OwnerID, actorID, meta.ParentID, meta.Name, "replace", expectedETag, "", blob, nil, nil, &lock)
	if err != nil {
		_ = os.Remove(blob.finalPath)
		return OfficeFile{}, err
	}
	h.RemoveBlobFiles(meta.OwnerID, orphans)
	meta.Size, meta.ETag, meta.Version, meta.ModifiedAt = updated.Size, updated.Etag, updated.Version, updated.ModifiedAt
	return meta, nil
}

func mimeOrDefault(value *string) string {
	if value == nil || *value == "" {
		return "application/octet-stream"
	}
	return *value
}
