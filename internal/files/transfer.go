package files

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const transferSandboxCSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'"

type uploadedBlob struct {
	id, tempPath, finalPath, hash string
	size                          int64
	// declared: the client sent the content's SHA-256 with it.
	declared bool
	// within, when set, runs in the committing transaction.
	within func(*sql.Tx, node) error
}

// finish runs the blob's own checks on the stored node and commits.
func (b uploadedBlob) finish(tx *sql.Tx, stored node) error {
	if b.within != nil {
		if err := b.within(tx, stored); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func blobPath(root string, uid int64, id string) string {
	if len(id) != 36 || strings.Trim(id, "0123456789abcdef-") != "" {
		return ""
	}
	return filepath.Join(root, strconv.FormatInt(uid, 10), id[:2], id)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.auth(w, r)
	if !ok {
		return
	}
	mode := r.URL.Query().Get("conflict")
	if mode == "" {
		mode = "fail"
	}
	if mode != "fail" && mode != "rename" && mode != "replace" {
		writeErr(w, 400, "conflict must be fail, rename or replace.", "CONFLICT_MODE_INVALID")
		return
	}
	name, e := validateName(r.URL.Query().Get("name"))
	if e != nil {
		fail(w, e)
		return
	}
	parentID := r.URL.Query().Get("parentId")
	if parentID == "" {
		parentID = "root"
	}
	expectedHash := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Content-SHA256")))
	if expectedHash != "" && !sha256Pattern.MatchString(expectedHash) {
		writeErr(w, 400, "sha256 must be 64 hex characters.", "INVALID_HASH")
		return
	}
	baseETag := strings.ReplaceAll(r.Header.Get("If-Match"), `"`, "")
	// Validate account and conflict before reading a potentially large body.
	tx, e := h.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return
	}
	parent, shared, e := h.resolveReadNode(tx, actorID, parentID)
	if e != nil {
		failTx(w, tx, e)
		return
	}
	uid := actorID
	if shared != nil {
		if shared.permission != "write" {
			failTx(w, tx, readOnlyShare())
			return
		}
		uid = shared.ownerID
	}
	if parent.Type != "folder" {
		failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
		return
	}
	existing, found, e := childByName(tx, parent.ID, name, "")
	if e != nil {
		failTx(w, tx, e)
		return
	}
	if found && mode != "rename" && (mode == "fail" || existing.kind != "file") {
		failTx(w, tx, nameExists(existing.name))
		return
	}
	if e = tx.Commit(); e != nil {
		failTx(w, tx, e)
		return
	}
	maxBytes, limitCode, e := h.uploadAllowance(r.Context(), uid)
	if e != nil {
		fail(w, e)
		return
	}
	if r.ContentLength >= 0 && r.ContentLength > maxBytes {
		writeErr(w, 413, uploadLimitMessage(limitCode), limitCode)
		return
	}
	if e = h.CheckDiskSpace(r.ContentLength); e != nil {
		fail(w, e)
		return
	}
	blob, e := h.encryptUpload(r, uid, maxBytes, limitCode)
	if e != nil {
		fail(w, e)
		return
	}
	if r.ContentLength >= 0 && blob.size != r.ContentLength {
		_ = os.Remove(blob.tempPath)
		fail(w, apiError{400, fmt.Sprintf("Expected %d bytes but received %d.", r.ContentLength, blob.size), "SIZE_MISMATCH"})
		return
	}
	if expectedHash != "" && expectedHash != blob.hash {
		_ = os.Remove(blob.tempPath)
		fail(w, apiError{422, "The file arrived damaged (checksum mismatch).", "CHECKSUM_MISMATCH"})
		return
	}
	blob.declared = expectedHash != ""
	blob.finalPath, e = h.placeUpload(uid, blob)
	if e != nil {
		_ = os.Remove(blob.tempPath)
		fail(w, e)
		return
	}
	result, orphans, e := h.commitUpload(r.Context(), uid, actorID, parent.ID, name, mode, baseETag, "", blob, clientDate(r.Header.Get("X-File-Created")), clientDate(r.Header.Get("X-File-Modified")))
	if e != nil {
		_ = os.Remove(blob.finalPath)
		fail(w, e)
		return
	}
	h.RemoveBlobFiles(uid, orphans)
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func uploadLimitMessage(code string) string {
	if code == "FILE_TOO_LARGE" {
		return "This file is larger than the server allows."
	}
	return "Not enough storage left for this file."
}

func (h *Handler) uploadAllowance(ctx context.Context, uid int64) (int64, string, error) {
	var quota sql.NullInt64
	var used int64
	if err := h.db.QueryRowContext(ctx, `SELECT quota_bytes,used_bytes FROM users WHERE id=?`, uid).Scan(&quota, &used); errors.Is(err, sql.ErrNoRows) {
		return 0, "", apiError{404, "User not found", "NOT_FOUND"}
	} else if err != nil {
		return 0, "", err
	}
	policy := h.storagePolicy()
	quotaBytes := policy.DefaultQuotaBytes
	if quota.Valid {
		quotaBytes = quota.Int64
	}
	remaining := int64(math.MaxInt64 - 1)
	if quotaBytes >= 0 {
		var reserved int64
		if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND datetime(expires_at)>datetime('now')`, uid).Scan(&reserved); err != nil {
			return 0, "", err
		}
		remaining = quotaBytes - used - reserved
		if remaining < 0 {
			remaining = 0
		}
	}
	max := remaining
	code := "QUOTA_EXCEEDED"
	if policy.MaxUploadBytes > 0 && policy.MaxUploadBytes <= remaining {
		max = policy.MaxUploadBytes
		code = "FILE_TOO_LARGE"
	}
	if max < 0 {
		max = 0
	}
	return max, code, nil
}

func (h *Handler) encryptUpload(r *http.Request, uid, maxBytes int64, limitCode string) (uploadedBlob, error) {
	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	return h.encryptStream(r.Context(), uid, r.Body, randomID(), readLimit, maxBytes, apiError{413, uploadLimitMessage(limitCode), limitCode})
}

func (h *Handler) encryptStream(ctx context.Context, uid int64, source io.Reader, id string, readLimit, maxBytes int64, overflow error) (uploadedBlob, error) {
	if err := os.MkdirAll(h.uploadTmpDir, 0700); err != nil {
		return uploadedBlob{}, err
	}
	file, err := os.CreateTemp(h.uploadTmpDir, "upload-*.part")
	if err != nil {
		return uploadedBlob{}, err
	}
	temp := file.Name()
	_ = file.Chmod(0600)
	dek, err := h.keys.GetUserDEK(ctx, h.db, uid)
	if err != nil {
		file.Close()
		os.Remove(temp)
		return uploadedBlob{}, err
	}
	sum := sha256.New()
	meter := &countingReader{source: io.TeeReader(io.LimitReader(source, readLimit), sum)}
	_, err = cryptox.EncryptBlob(file, meter, dek, id)
	if errors.Is(err, io.ErrUnexpectedEOF) || err != nil && ctx.Err() != nil {
		err = apiError{400, "Upload was interrupted.", "UPLOAD_ABORTED"}
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(temp)
		return uploadedBlob{}, err
	}
	if meter.count > maxBytes {
		os.Remove(temp)
		return uploadedBlob{}, overflow
	}
	return uploadedBlob{id: id, tempPath: temp, size: meter.count, hash: hex.EncodeToString(sum.Sum(nil))}, nil
}

type countingReader struct {
	source io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.count += int64(n)
	return n, err
}

func (h *Handler) placeUpload(uid int64, blob uploadedBlob) (string, error) {
	final := filepath.Join(h.blobDir, strconv.FormatInt(uid, 10), blob.id[:2], blob.id)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Rename(blob.tempPath, final); err != nil {
		return "", err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return final, nil
}

func (h *Handler) commitUpload(ctx context.Context, uid, actorID int64, parentID, name, mode, baseETag, uploadID string, blob uploadedBlob, createdAt, clientModifiedAt *string) (node, []string, error) {
	return h.commitUploadWithWOPILock(ctx, uid, actorID, parentID, name, mode, baseETag, uploadID, blob, createdAt, clientModifiedAt, nil)
}

func (h *Handler) commitUploadWithWOPILock(ctx context.Context, uid, actorID int64, parentID, name, mode, baseETag, uploadID string, blob uploadedBlob, createdAt, clientModifiedAt *string, wantedLock *string) (node, []string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return node{}, nil, err
	}
	defer tx.Rollback()
	parent, err := h.live(tx, uid, parentID)
	if err != nil {
		return node{}, nil, err
	}
	if actorID != uid {
		_, access, accessErr := h.resolveReadNode(tx, actorID, parentID)
		if accessErr != nil || access == nil || access.ownerID != uid || access.permission != "write" {
			return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
		}
	}
	if parent.Type != "folder" {
		return node{}, nil, apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
	}
	existing, found, err := childByName(tx, parent.ID, name, "")
	if err != nil {
		return node{}, nil, err
	}
	// A retry of an upload whose answer was lost: the file of that name already
	// holds exactly the declared content, so it is the upload itself.
	if found && blob.declared && mode == "rename" && existing.kind == "file" {
		same, e := nodeByID(tx, uid, existing.id)
		if e != nil {
			return node{}, nil, e
		}
		if same.Etag == blob.hash && same.Size == blob.size {
			if e = endUploadTx(tx, uid, uploadID); e != nil {
				return node{}, nil, e
			}
			// Nothing references the blob just written.
			return same, []string{blob.id}, blob.finish(tx, same)
		}
	}
	if found && mode == "fail" {
		return node{}, nil, nameExists(existing.name)
	}
	if found && mode == "replace" {
		old, er := nodeByID(tx, uid, existing.id)
		if er != nil {
			return node{}, nil, er
		}
		if old.Type != "file" {
			return node{}, nil, nameExists(old.Name)
		}
		if wantedLock != nil {
			var held string
			lockErr := tx.QueryRowContext(ctx, `SELECT lock_value FROM wopi_locks WHERE node_id=? AND expires_at>?`, old.ID, time.Now().UnixMilli()).Scan(&held)
			if lockErr != nil && !errors.Is(lockErr, sql.ErrNoRows) {
				return node{}, nil, lockErr
			}
			if held != *wantedLock {
				return node{}, nil, apiError{409, "The file lock changed before the document was saved.", "WOPI_LOCK_MISMATCH"}
			}
		}
		if baseETag != "" && baseETag != old.Etag {
			return node{}, nil, apiError{412, "The file changed since you last saw it.", "ETAG_MISMATCH"}
		}
		if er = h.recordBlobTx(tx, uid, blob, uploadID); er != nil {
			return node{}, nil, er
		}
		if er = endUploadTx(tx, uid, uploadID); er != nil {
			return node{}, nil, er
		}
		var author any
		if wantedLock != nil {
			author = actorID
		}
		orphans, er := h.replaceContentTx(tx, uid, actorID, old, blob.id, blob.size, blob.hash, author)
		if er != nil {
			return node{}, nil, er
		}
		if createdAt == nil && clientModifiedAt == nil {
			_, er = tx.Exec(`DELETE FROM file_client_dates WHERE node_id=?`, old.ID)
		} else {
			_, er = tx.Exec(`INSERT INTO file_client_dates(node_id,blob_id,created_at,modified_at) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET blob_id=excluded.blob_id,created_at=excluded.created_at,modified_at=excluded.modified_at`, old.ID, blob.id, createdAt, clientModifiedAt)
		}
		if er != nil {
			return node{}, nil, er
		}
		if _, er = tx.Exec(`UPDATE nodes SET modified_at=datetime('now') WHERE id=?`, parent.ID); er != nil {
			return node{}, nil, er
		}
		updated, er := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, old.ID, uid))
		if er != nil {
			return node{}, nil, er
		}
		if er = blob.finish(tx, updated); er != nil {
			return node{}, nil, er
		}
		return updated, orphans, nil
	}
	if er := h.checkQuotaTx(tx, uid, blob.size, uploadID); er != nil {
		return node{}, nil, er
	}
	targetName := name
	if found {
		targetName, err = h.freeName(tx, parent.ID, name, "")
		if err != nil {
			return node{}, nil, err
		}
	}
	if err = h.checkNodeRoom(tx, uid, 1); err != nil {
		return node{}, nil, err
	}
	if err = h.recordBlobTx(tx, uid, blob, uploadID); err != nil {
		return node{}, nil, err
	}
	if err = endUploadTx(tx, uid, uploadID); err != nil {
		return node{}, nil, err
	}
	id := randomID()
	if _, err = tx.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag,vault_id) VALUES(?,?,?,'file',?,?,?,?,?,(SELECT vault_id FROM nodes WHERE id=?))`, id, uid, parent.ID, targetName, blob.size, mimeForName(targetName), blob.id, blob.hash, parent.ID); err != nil {
		return node{}, nil, err
	}
	if err = recordChange(tx, uid, id, "create", parent.ID); err != nil {
		return node{}, nil, err
	}
	if err = touchFolder(tx, parent.ID); err != nil {
		return node{}, nil, err
	}
	created, err := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, uid))
	if err != nil {
		return node{}, nil, err
	}
	if err = recordActivity(tx, uid, actorID, "upload", id, nil); err != nil {
		return node{}, nil, err
	}
	if createdAt == nil && clientModifiedAt == nil {
		_, err = tx.Exec(`DELETE FROM file_client_dates WHERE node_id=?`, id)
	} else {
		_, err = tx.Exec(`INSERT INTO file_client_dates(node_id,blob_id,created_at,modified_at) VALUES(?,?,?,?)`, id, blob.id, createdAt, clientModifiedAt)
	}
	if err != nil {
		return node{}, nil, err
	}
	if err = blob.finish(tx, created); err != nil {
		return node{}, nil, err
	}
	return created, nil, nil
}

// endUploadTx closes a resumable upload with its file: one cancelled or
// completed meanwhile must not give a second file.
func endUploadTx(tx *sql.Tx, ownerID int64, uploadID string) error {
	if uploadID == "" {
		return nil
	}
	result, err := tx.Exec(`DELETE FROM upload_sessions WHERE id=? AND user_id=? AND datetime(expires_at)>datetime('now')`, uploadID, ownerID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return apiError{404, "Upload not found or expired", "UPLOAD_NOT_FOUND"}
	}
	return nil
}

// replaceContentTx points old at new content and keeps what was current as a
// version. The caller holds the reference to blobID. author is the account
// recorded as having written the new content (the document editor), or nil.
// It returns the blobs that became unreferenced.
func (h *Handler) replaceContentTx(tx *sql.Tx, uid, actorID int64, old node, blobID string, size int64, etag string, author any) ([]string, error) {
	if _, err := tx.Exec(`INSERT INTO file_versions(node_id,blob_id,version,size,etag,created_at,modified_by) SELECT id,blob_id,version,size,etag,modified_at,modified_by FROM nodes WHERE id=? AND user_id=?`, old.ID, uid); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE nodes SET blob_id=?,size=?,etag=?,version=version+1,mime=?,modified_at=datetime('now'),modified_by=? WHERE id=? AND user_id=?`, blobID, size, etag, mimeForName(old.Name), author, old.ID, uid); err != nil {
		return nil, err
	}
	if err := recordChange(tx, uid, old.ID, "update"); err != nil {
		return nil, err
	}
	if err := recordActivity(tx, uid, actorID, "version", old.ID, nil); err != nil {
		return nil, err
	}
	return h.pruneVersionsTx(tx, old.ID)
}

func (h *Handler) recordBlobTx(tx *sql.Tx, uid int64, blob uploadedBlob, excludeUploadID string) error {
	if err := h.checkQuotaTx(tx, uid, blob.size, excludeUploadID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,?, ?,1,1)`, blob.id, uid, blob.size, blob.hash); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE users SET used_bytes=used_bytes+? WHERE id=?`, blob.size, uid)
	return err
}
func (h *Handler) checkQuotaTx(tx *sql.Tx, uid, size int64, excludeUploadID string) error {
	var quota sql.NullInt64
	var used, reserved int64
	if err := tx.QueryRow(`SELECT quota_bytes,used_bytes FROM users WHERE id=?`, uid).Scan(&quota, &used); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND id<>? AND datetime(expires_at)>datetime('now')`, uid, excludeUploadID).Scan(&reserved); err != nil {
		return err
	}
	policy := h.storagePolicy()
	q := policy.DefaultQuotaBytes
	if quota.Valid {
		q = quota.Int64
	}
	if q >= 0 && size > q-used-reserved {
		return apiError{413, "Not enough storage left for this file.", "QUOTA_EXCEEDED"}
	}
	return nil
}
func (h *Handler) pruneVersionsTx(tx *sql.Tx, nodeID string) ([]string, error) {
	rows, err := tx.Query(`SELECT id,blob_id FROM file_versions WHERE node_id=? ORDER BY version DESC LIMIT -1 OFFSET ?`, nodeID, h.storagePolicy().VersionRetention)
	if err != nil {
		return nil, err
	}
	type version struct {
		id   int64
		blob string
	}
	stale := []version{}
	for rows.Next() {
		var v version
		if err = rows.Scan(&v.id, &v.blob); err != nil {
			rows.Close()
			return nil, err
		}
		stale = append(stale, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	orphans := []string{}
	for _, v := range stale {
		if _, err = tx.Exec(`DELETE FROM file_versions WHERE id=?`, v.id); err != nil {
			return nil, err
		}
		ids, e := releaseBlobTx(tx, v.blob)
		if e != nil {
			return nil, e
		}
		if ids {
			orphans = append(orphans, v.blob)
		}
	}
	return orphans, nil
}
func releaseBlobTx(tx *sql.Tx, id string) (bool, error) {
	var uid, size int64
	var refs int
	if err := tx.QueryRow(`SELECT user_id,size,ref_count FROM blobs WHERE id=?`, id).Scan(&uid, &size, &refs); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if refs > 1 {
		_, err := tx.Exec(`UPDATE blobs SET ref_count=ref_count-1 WHERE id=?`, id)
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM blobs WHERE id=?`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE users SET used_bytes=MAX(0,used_bytes-?) WHERE id=?`, size, uid); err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	n, shared, err := h.resolveReadNode(tx, uid, r.PathValue("id"))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	ownerID := uid
	if shared != nil {
		ownerID = shared.ownerID
		if r.URL.Query().Get("version") != "" {
			failTx(w, tx, apiError{404, "Version not found", "NOT_FOUND"})
			return
		}
	}
	if n.Type != "file" {
		failTx(w, tx, notAFile())
		return
	}
	blobID := ""
	size := n.Size
	etag := n.Etag
	modified := n.ModifiedAt
	if version := r.URL.Query().Get("version"); version != "" {
		v, e := strconv.ParseInt(version, 10, 64)
		if e != nil {
			failTx(w, tx, apiError{404, "Version not found", "NOT_FOUND"})
			return
		}
		var previousModified string
		if e = tx.QueryRow(`SELECT blob_id,size,etag,created_at FROM file_versions WHERE node_id=? AND version=?`, n.ID, v).Scan(&blobID, &size, &etag, &previousModified); errors.Is(e, sql.ErrNoRows) {
			failTx(w, tx, apiError{404, "Version not found", "NOT_FOUND"})
			return
		} else if e != nil {
			failTx(w, tx, e)
			return
		}
		modified = previousModified
	} else if err = tx.QueryRow(`SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, n.ID, ownerID).Scan(&blobID); err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	mimeType := ""
	if n.Mime != nil {
		mimeType = *n.Mime
	}
	download := r.URL.Query().Get("download") == "1" || r.URL.Query().Get("download") == "true"
	if err = h.SendContent(w, r, ownerID, Content{BlobID: blobID, Name: n.Name, MIME: mimeType, ETag: etag, ModifiedAt: modified, Size: size}, download); err != nil {
		fail(w, err)
	}
}

// abortResponse cuts the connection once a body is under way, so a client never
// takes a partial file for a whole one.
func abortResponse(message string, err error) {
	slog.Warn(message, "error", err)
	panic(http.ErrAbortHandler)
}

func (h *Handler) HeaderForFile(w http.ResponseWriter, name, mimeType, disposition, etag, modified string) {
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", contentDisposition(disposition, name))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", etag))
	if t, err := time.Parse("2006-01-02 15:04:05", modified); err == nil {
		w.Header().Set("Last-Modified", t.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", transferSandboxCSP)
}
func contentDisposition(kind, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	encoded := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, encoded)
}
func activeContent(value string) bool {
	switch strings.ToLower(value) {
	case "text/html", "image/svg+xml", "application/xml", "text/javascript":
		return true
	}
	return false
}
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == `"`+etag+`"` {
			return true
		}
	}
	return false
}
func parseByteRange(header string, size int64) (start, end int64, requested, unsatisfiable bool) {
	v := strings.TrimSpace(header)
	if !strings.HasPrefix(v, "bytes=") || strings.Contains(v[6:], ",") {
		return 0, max64(size-1, 0), false, false
	}
	spec := strings.SplitN(v[6:], "-", 2)
	if len(spec) != 2 || (spec[0] == "" && spec[1] == "") {
		return 0, max64(size-1, 0), false, false
	}
	if spec[0] == "" {
		suffix, e := strconv.ParseInt(spec[1], 10, 64)
		if e != nil || suffix <= 0 {
			return 0, 0, true, true
		}
		start = max64(0, size-suffix)
		end = size - 1
	} else {
		first, e := strconv.ParseInt(spec[0], 10, 64)
		if e != nil || first < 0 {
			return 0, 0, false, false
		}
		start = first
		end = size - 1
		if spec[1] != "" {
			last, er := strconv.ParseInt(spec[1], 10, 64)
			if er != nil || last < 0 {
				return 0, 0, false, false
			}
			end = min64(last, size-1)
		}
	}
	if size == 0 || start >= size || start > end {
		return 0, 0, true, true
	}
	return start, end, true, false
}

// ParseByteRange parses one HTTP byte range using the same policy as file downloads.
func ParseByteRange(header string, size int64) (start, end int64, requested, unsatisfiable bool) {
	return parseByteRange(header, size)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
