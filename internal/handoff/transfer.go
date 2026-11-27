package handoff

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Staged files are ordinary blobs under the account's key (encrypted at rest,
// counted in used_bytes), referenced by handoff_files until the send ends.

const sandboxCSP = "sandbox; default-src 'none'"

var errFileNotFound = failure(http.StatusNotFound, "File not found.", "NOT_FOUND")

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

type stagedBlob struct {
	id, sha256, path string
	size             int64
}

// writeBlob stores exactly size bytes of body encrypted for the account.
func (h *Handler) writeBlob(ctx context.Context, userID, size int64, body io.Reader) (stagedBlob, error) {
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return stagedBlob{}, err
	}
	id, err := newID()
	if err != nil {
		return stagedBlob{}, err
	}
	path, err := h.blobPath(userID, id)
	if err != nil {
		return stagedBlob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return stagedBlob{}, err
	}
	temporary := path + ".tmp"
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return stagedBlob{}, err
	}
	defer os.Remove(temporary)
	defer out.Close()
	counted := &countingReader{r: io.LimitReader(body, size+1)}
	hash := sha256.New()
	_, err = cryptox.EncryptBlob(out, io.TeeReader(counted, hash), dek, id)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return stagedBlob{}, failure(http.StatusBadRequest, "Upload was interrupted.", "UPLOAD_ABORTED")
	}
	if err != nil {
		return stagedBlob{}, err
	}
	if counted.n != size {
		return stagedBlob{}, failure(http.StatusBadRequest, fmt.Sprintf("Expected %d bytes but received %d.", size, counted.n), "SIZE_MISMATCH")
	}
	if err := errors.Join(out.Sync(), out.Close(), os.Rename(temporary, path)); err != nil {
		return stagedBlob{}, err
	}
	return stagedBlob{id: id, sha256: hex.EncodeToString(hash.Sum(nil)), path: path, size: size}, nil
}

// upload stores the request body as one file of a send still collecting them.
// Sending a file again replaces what was staged.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request, c caller) error {
	ctx := r.Context()
	row, err := h.senderRow(ctx, c.UserID, c.key, r.PathValue("id"))
	if err != nil {
		return err
	}
	fileID, _ := strconv.ParseInt(r.PathValue("fileId"), 10, 64)
	var size int64
	var source sql.NullString
	err = h.db.QueryRowContext(ctx, `SELECT size,source_node_id FROM handoff_files WHERE id=? AND offer_id=?`, fileID, row.id).Scan(&size, &source)
	if errors.Is(err, sql.ErrNoRows) || err == nil && source.Valid {
		return errFileNotFound
	}
	if err != nil {
		return err
	}
	closed := failure(http.StatusConflict, "This send is no longer waiting for files.", "OFFER_CLOSED")
	if row.status != "uploading" {
		return closed
	}
	if err := h.files.CheckDiskSpace(size); err != nil {
		return err
	}
	if err := h.files.CheckUploadFits(ctx, c.UserID, size); err != nil {
		return err
	}
	blob, err := h.writeBlob(ctx, c.UserID, size, r.Body)
	if err != nil {
		return err
	}
	replaced, err := h.attach(ctx, c.UserID, fileID, blob, closed)
	if err != nil {
		_ = os.Remove(blob.path)
		return err
	}
	h.files.RemoveBlobFiles(c.UserID, replaced)
	if err := h.markReadyWhenComplete(ctx, c.UserID, row.id); err != nil {
		return err
	}
	o, err := h.offerFor(ctx, c.UserID, c.key, row.id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, o)
	return nil
}

func (h *Handler) attach(ctx context.Context, userID, fileID int64, blob stagedBlob, closed error) ([]string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var previous sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT f.blob_id FROM handoff_files f JOIN handoff_offers o ON o.id=f.offer_id
		WHERE f.id=? AND o.user_id=? AND o.status='uploading'`, fileID, userID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, closed
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,?,?,1,1)`, blob.id, userID, blob.size, blob.sha256); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET used_bytes=used_bytes+? WHERE id=?`, blob.size, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE handoff_files SET blob_id=? WHERE id=?`, blob.id, fileID); err != nil {
		return nil, err
	}
	var replaced []string
	if previous.Valid {
		removed, err := releaseBlob(ctx, tx, userID, previous.String)
		if err != nil {
			return nil, err
		}
		if removed {
			replaced = append(replaced, previous.String)
		}
	}
	return replaced, tx.Commit()
}

// acceptedRow is the send the caller may download: addressed to it and
// accepted. The offer row is the whole authorisation.
func (h *Handler) acceptedRow(ctx context.Context, c caller, id string) (offerRow, error) {
	row, err := h.receiverRow(ctx, c.UserID, c.key, id)
	if err != nil {
		return offerRow{}, err
	}
	if row.status != "accepted" {
		return offerRow{}, failure(http.StatusConflict, "Accept the send first.", "NOT_ACCEPTED")
	}
	if row.expired() {
		return offerRow{}, errExpired
	}
	return row, nil
}

type staged struct {
	id, size   int64
	name, blob string
}

func (h *Handler) openStaged(userID int64, f staged) (*os.File, error) {
	unreadable := failure(http.StatusInternalServerError, "This file can’t be read: its stored data is missing or damaged.", "FILE_UNREADABLE")
	path, err := h.blobPath(userID, f.blob)
	if err != nil {
		return nil, err
	}
	source, err := os.Open(path)
	if err != nil {
		slog.Error("open a staged file", "error", err)
		return nil, unreadable
	}
	info, err := source.Stat()
	expected, sizeErr := cryptox.EncryptedSize(f.size)
	if err != nil || sizeErr != nil || info.Size() != expected {
		_ = source.Close()
		return nil, unreadable
	}
	return source, nil
}

// download serves one file with HTTP Range, so a long download over a flaky
// link resumes. It is always an attachment: a sent file is never rendered.
func (h *Handler) download(w http.ResponseWriter, r *http.Request, c caller) error {
	ctx := r.Context()
	row, err := h.acceptedRow(ctx, c, r.PathValue("id"))
	if err != nil {
		return err
	}
	var f staged
	fileID, _ := strconv.ParseInt(r.PathValue("fileId"), 10, 64)
	var blob sql.NullString
	err = h.db.QueryRowContext(ctx, `SELECT id,name,size,blob_id FROM handoff_files WHERE id=? AND offer_id=?`, fileID, row.id).Scan(&f.id, &f.name, &f.size, &blob)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !blob.Valid {
		return errFileNotFound
	}
	if err != nil {
		return err
	}
	f.blob = blob.String
	start, end, partial, unsatisfiable := files.ParseByteRange(r.Header.Get("Range"), f.size)
	if !partial {
		start, end = 0, f.size-1
	}
	var source *os.File
	var dek []byte
	if !unsatisfiable && r.Method != http.MethodHead && f.size > 0 {
		if dek, err = h.keys.GetUserDEK(ctx, h.db, c.UserID); err != nil {
			return err
		}
		if source, err = h.openStaged(c.UserID, f); err != nil {
			return err
		}
		defer source.Close()
	}
	header := w.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", files.ContentDisposition("attachment", f.name))
	header.Set("Accept-Ranges", "bytes")
	header.Set("ETag", `"`+f.blob+`"`)
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", sandboxCSP)
	if unsatisfiable {
		header.Set("Content-Range", fmt.Sprintf("bytes */%d", f.size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}
	header.Set("Content-Length", strconv.FormatInt(max(0, end-start+1), 10))
	if partial {
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, f.size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if source == nil {
		if f.size == 0 && r.Method != http.MethodHead {
			h.fetched(ctx, c.UserID, row.id, f.id)
		}
		return nil
	}
	if _, err := cryptox.DecryptBlobRange(source, f.size, start, end, dek, f.blob, w); err != nil {
		slog.Warn("send a staged file", "error", err)
		panic(http.ErrAbortHandler)
	}
	if end == f.size-1 {
		h.fetched(ctx, c.UserID, row.id, f.id)
	}
	return nil
}

// zip serves every file of the send as one stored ZIP, for browsers that block
// several downloads.
func (h *Handler) zip(w http.ResponseWriter, r *http.Request, c caller) error {
	ctx := r.Context()
	row, err := h.acceptedRow(ctx, c, r.PathValue("id"))
	if err != nil {
		return err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,size,blob_id FROM handoff_files WHERE offer_id=? AND blob_id IS NOT NULL ORDER BY position`, row.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []staged
	for rows.Next() {
		var f staged
		if err := rows.Scan(&f.id, &f.name, &f.size, &f.blob); err != nil {
			return err
		}
		list = append(list, f)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, c.UserID)
	if err != nil {
		return err
	}
	header := w.Header()
	header.Set("Content-Type", "application/zip")
	header.Set("Content-Disposition", files.ContentDisposition("attachment", "NeoCloud files.zip"))
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return nil
	}
	archive := zip.NewWriter(w)
	sent := make([]int64, 0, len(list))
	for _, f := range list {
		if err := h.addToZip(archive, c.UserID, f, dek); err != nil {
			// The status line has gone out: cutting the connection is all that is left.
			slog.Warn("send staged files", "error", err)
			panic(http.ErrAbortHandler)
		}
		sent = append(sent, f.id)
	}
	if err := archive.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
	h.fetched(ctx, c.UserID, row.id, sent...)
	return nil
}

func (h *Handler) addToZip(archive *zip.Writer, userID int64, f staged, dek []byte) error {
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store})
	if err != nil || f.size == 0 {
		return err
	}
	source, err := h.openStaged(userID, f)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = cryptox.DecryptBlobRange(source, f.size, 0, f.size-1, dek, f.blob, entry)
	return err
}
