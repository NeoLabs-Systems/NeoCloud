package files

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
)

// The latest accepted file time: year 9999.
const davMaxUnix = 253402300799

// davParseTime reads a Unix time in seconds (X-OC-Mtime, rclone) or an HTTP
// or ISO date.
func davParseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds < 0 || seconds > davMaxUnix || strings.ContainsAny(value, "eE+-") {
			return time.Time{}, false
		}
		return time.Unix(int64(seconds), 0).UTC(), true
	}
	for _, layout := range []string{http.TimeFormat, time.RFC1123Z, time.RFC3339Nano, "2006-01-02T15:04:05", time.DateOnly} {
		if parsed, err := time.Parse(layout, value); err == nil && parsed.Unix() >= 0 && parsed.Unix() <= davMaxUnix {
			return parsed.UTC().Truncate(time.Second), true
		}
	}
	return time.Time{}, false
}

// setModifiedAtTx keeps the file time a client carries.
func setModifiedAtTx(tx *sql.Tx, uid int64, nodeID string, at time.Time) error {
	if _, err := tx.Exec(`UPDATE nodes SET modified_at=? WHERE id=? AND user_id=?`, at.UTC().Format(time.DateTime), nodeID, uid); err != nil {
		return err
	}
	return recordChange(tx, uid, nodeID, "update")
}

// davStore writes body as a file of parent through the upload path. size is -1
// when the client does not announce it.
func (h *Handler) davStore(ctx context.Context, uid int64, parentID, rawName, mode, baseETag string, body io.Reader, size int64) (node, error) {
	name, err := validateName(rawName)
	if err != nil {
		return node{}, err
	}
	var existingType string
	err = h.db.QueryRowContext(ctx, `SELECT type FROM nodes WHERE user_id=? AND parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, uid, parentID, name).Scan(&existingType)
	if err == nil && (mode == "fail" || existingType == "folder") {
		return node{}, apiError{http.StatusConflict, "That name already exists here.", "NAME_EXISTS"}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return node{}, err
	}
	maxBytes, code, err := h.uploadAllowance(ctx, uid)
	if err != nil {
		return node{}, err
	}
	tooBig := apiError{http.StatusRequestEntityTooLarge, uploadLimitMessage(code), code}
	if size > maxBytes {
		return node{}, tooBig
	}
	if err = h.CheckDiskSpace(size); err != nil {
		return node{}, err
	}
	blob, err := h.encryptStream(ctx, uid, body, randomID(), uploadReadLimit(maxBytes), maxBytes, tooBig)
	if err != nil {
		return node{}, err
	}
	if size >= 0 && blob.size != size {
		_ = os.Remove(blob.tempPath)
		return node{}, apiError{http.StatusBadRequest, "The upload ended before all of it arrived.", "SIZE_MISMATCH"}
	}
	blob.finalPath, err = h.placeUpload(uid, blob)
	if err != nil {
		_ = os.Remove(blob.tempPath)
		return node{}, err
	}
	stored, orphans, err := h.commitUpload(ctx, uid, uid, parentID, name, mode, baseETag, "", blob, nil, nil)
	if err != nil {
		_ = os.Remove(blob.finalPath)
		return node{}, err
	}
	h.RemoveBlobFiles(uid, orphans)
	return stored, nil
}

func (h *Handler) davPut(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	if len(segments) == 0 {
		return davError(http.StatusMethodNotAllowed, "The root folder is not a file.")
	}
	existing, parent, err := h.davResolve(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	if parent == nil {
		return errDAVParentMissing
	}
	if existing != nil && existing.Type == "folder" {
		return davError(http.StatusMethodNotAllowed, "A folder can’t be overwritten with a file.")
	}
	if err = h.davAssertUnlocked(request, segments, false); err != nil {
		return err
	}
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch != "" && existing == nil || strings.TrimSpace(r.Header.Get("If-None-Match")) == "*" && existing != nil {
		return davError(http.StatusPreconditionFailed, "The precondition failed.")
	}
	baseETag := ""
	if ifMatch != "*" {
		baseETag = strings.Trim(strings.TrimPrefix(ifMatch, "W/"), `"`)
	}
	// Finder streams chunked and announces the size in this header instead.
	if expected, err := strconv.ParseInt(r.Header.Get("X-Expected-Entity-Length"), 10, 64); err == nil && expected > 0 {
		if err = h.CheckUploadFits(r.Context(), uid, expected); err != nil {
			return err
		}
	}
	name := segments[len(segments)-1]
	if existing != nil {
		name = existing.Name
	}
	stored, err := h.davStore(r.Context(), uid, parent.ID, name, "replace", baseETag, r.Body, r.ContentLength)
	if err != nil {
		return err
	}
	header := request.w.Header()
	if mtime, ok := davParseTime(r.Header.Get("X-OC-Mtime")); ok {
		if err = h.davTx(r.Context(), func(tx *sql.Tx) error { return setModifiedAtTx(tx, uid, stored.ID, mtime) }); err != nil {
			return err
		}
		header.Set("X-OC-MTime", "accepted")
	}
	header.Set("ETag", `"`+stored.Etag+`"`)
	header.Set("OC-ETag", `"`+stored.Etag+`"`)
	header.Set("OC-FileId", stored.ID)
	if existing != nil {
		request.w.WriteHeader(http.StatusNoContent)
	} else {
		request.w.WriteHeader(http.StatusCreated)
	}
	return nil
}

// davNewFolderTx creates a folder whose name must still be free.
func (h *Handler) davNewFolderTx(tx *sql.Tx, uid int64, parentID, name string) error {
	_, created, err := h.EnsureFolderTx(tx, uid, parentID, name)
	if err == nil && !created {
		err = apiError{http.StatusConflict, "That name already exists here.", "NAME_EXISTS"}
	}
	return err
}

func (h *Handler) davMkcol(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		return davError(http.StatusUnsupportedMediaType, "MKCOL with a body is not supported.")
	}
	existing, parent, err := h.davResolve(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	if existing != nil {
		return davError(http.StatusMethodNotAllowed, "Something with this name already exists.")
	}
	if parent == nil {
		return errDAVParentMissing
	}
	if err = h.davAssertUnlocked(request, segments, false); err != nil {
		return err
	}
	err = h.davTx(r.Context(), func(tx *sql.Tx) error {
		return h.davNewFolderTx(tx, uid, parent.ID, segments[len(segments)-1])
	})
	if err != nil {
		return err
	}
	request.w.WriteHeader(http.StatusCreated)
	return nil
}

func (h *Handler) davDelete(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	if len(segments) == 0 {
		return davError(http.StatusForbidden, "The root folder can’t be deleted.")
	}
	n, err := h.davExisting(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	if err = h.davAssertUnlocked(request, segments, true); err != nil {
		return err
	}
	err = h.davTx(r.Context(), func(tx *sql.Tx) error {
		_, err := h.TrashTx(tx, uid, n.ID)
		return err
	})
	if err != nil {
		return err
	}
	if err = h.davLocks.ReleaseTree(r.Context(), uid, webdav.LockKey(segments)); err != nil {
		return err
	}
	request.w.WriteHeader(http.StatusNoContent)
	return nil
}

// davReplaceFileTx gives an existing file the source's content as a new
// version, so overwriting (also "save to a temp file, move it over") keeps the
// target's history. A moved source lives on as that content: its own old
// versions stay recoverable in the trash, without any it goes away.
func (h *Handler) davReplaceFileTx(tx *sql.Tx, uid int64, source, target node, move bool) ([]string, error) {
	var blobID string
	if err := tx.QueryRow(`SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, source.ID, uid).Scan(&blobID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE blobs SET ref_count=ref_count+1 WHERE id=? AND user_id=?`, blobID, uid); err != nil {
		return nil, err
	}
	orphans, err := h.replaceContentTx(tx, uid, uid, target, blobID, source.Size, source.Etag, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE nodes SET modified_at=datetime('now') WHERE id=?`, *target.ParentID); err != nil || !move {
		return orphans, err
	}
	modified, ok := davStoredTime(source.ModifiedAt)
	if ok {
		if err = setModifiedAtTx(tx, uid, target.ID, modified); err != nil {
			return nil, err
		}
	}
	var versions int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM file_versions WHERE node_id=?`, source.ID).Scan(&versions); err != nil {
		return nil, err
	}
	if versions > 0 {
		_, err = h.TrashTx(tx, uid, source.ID)
		return orphans, err
	}
	purged, err := purgeSubtreeTx(tx, uid, source.ID)
	return append(orphans, purged...), err
}

type davTransfer struct {
	uid              int64
	sourceID         string
	existing         *node // what the destination replaces
	parentID, name   string
	move, folderOnly bool
}

func (h *Handler) davTransferTx(tx *sql.Tx, t davTransfer) ([]string, error) {
	source, err := h.live(tx, t.uid, t.sourceID)
	if err != nil {
		return nil, err
	}
	if t.existing != nil {
		target, err := h.live(tx, t.uid, t.existing.ID)
		if err != nil {
			return nil, err
		}
		if source.Type == "file" && target.Type == "file" {
			return h.davReplaceFileTx(tx, t.uid, source, target, t.move)
		}
		if _, err = h.TrashTx(tx, t.uid, target.ID); err != nil {
			return nil, err
		}
		// Fails when the replaced folder held the source.
		if source, err = h.live(tx, t.uid, t.sourceID); err != nil {
			return nil, err
		}
	}
	if t.folderOnly {
		return nil, h.davNewFolderTx(tx, t.uid, t.parentID, t.name)
	}
	if t.move {
		_, err = h.UpdateTx(tx, t.uid, source.ID, &t.name, &t.parentID, false)
		return nil, err
	}
	return nil, h.davCopyTx(tx, t, source)
}

func (h *Handler) davCopyTx(tx *sql.Tx, t davTransfer, source node) error {
	parent, err := h.live(tx, t.uid, t.parentID)
	if err != nil {
		return err
	}
	name, err := validateName(t.name)
	if err != nil {
		return err
	}
	if source.Type == "folder" {
		var inside int
		if err = tx.QueryRow(`WITH RECURSIVE up(id,parent_id) AS(SELECT id,parent_id FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id,n.parent_id FROM nodes n JOIN up ON n.id=up.parent_id) SELECT COUNT(*) FROM up WHERE id=?`, parent.ID, t.uid, source.ID).Scan(&inside); err != nil {
			return err
		}
		if inside > 0 {
			return apiError{http.StatusBadRequest, "A folder can’t be copied into itself.", "COPY_INTO_SELF"}
		}
	}
	if _, taken, err := childByName(tx, parent.ID, name, ""); err != nil {
		return err
	} else if taken {
		return apiError{http.StatusConflict, "That name already exists here.", "NAME_EXISTS"}
	}
	_, err = h.copyTreeTx(tx, t.uid, t.uid, source, parent, name)
	return err
}

func (h *Handler) davCopyMove(request davRequest) error {
	r, uid, segments := request.r, request.principal.UserID, request.target.Segments
	move := r.Method == "MOVE"
	if len(segments) == 0 {
		return davError(http.StatusForbidden, "The root folder can’t be moved or copied.")
	}
	source, err := h.davExisting(r.Context(), uid, segments)
	if err != nil {
		return err
	}
	destination, err := webdav.ParseDestination(r.Header.Get("Destination"))
	if err != nil {
		return davError(http.StatusBadRequest, "Destination is missing or outside WebDAV.")
	}
	if destination.Username != "" && !strings.EqualFold(destination.Username, request.principal.Username) {
		return davError(http.StatusForbidden, "The destination belongs to another account.")
	}
	if len(destination.Segments) == 0 {
		return davError(http.StatusForbidden, "The root folder can’t be replaced.")
	}
	existing, parent, err := h.davResolve(r.Context(), uid, destination.Segments)
	if err != nil {
		return err
	}
	if parent == nil {
		return errDAVParentMissing
	}
	name := destination.Segments[len(destination.Segments)-1]
	if existing != nil && existing.ID == source.ID {
		// Same node, same spelling: nothing to do. A MOVE that changes only
		// the case is a rename.
		if !move || existing.Name == name {
			return davError(http.StatusForbidden, "Source and destination are the same.")
		}
		existing = nil
	}
	if existing != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("Overwrite")), "F") {
		return davError(http.StatusPreconditionFailed, "The destination exists and Overwrite is F.")
	}
	if move {
		if err = h.davAssertUnlocked(request, segments, true); err != nil {
			return err
		}
	}
	if err = h.davAssertUnlocked(request, destination.Segments, true); err != nil {
		return err
	}
	var orphans []string
	err = h.davTx(r.Context(), func(tx *sql.Tx) error {
		orphans, err = h.davTransferTx(tx, davTransfer{
			uid: uid, sourceID: source.ID, existing: existing, parentID: parent.ID, name: name, move: move,
			folderOnly: !move && source.Type == "folder" && strings.TrimSpace(r.Header.Get("Depth")) == "0",
		})
		return err
	})
	if err != nil {
		return err
	}
	h.RemoveBlobFiles(uid, orphans)
	if move {
		if err = h.davLocks.ReleaseTree(r.Context(), uid, webdav.LockKey(segments)); err != nil {
			return err
		}
	}
	if existing != nil {
		if err = h.davLocks.ReleaseTree(r.Context(), uid, webdav.LockKey(destination.Segments)); err != nil {
			return err
		}
		request.w.WriteHeader(http.StatusNoContent)
		return nil
	}
	request.w.WriteHeader(http.StatusCreated)
	return nil
}
