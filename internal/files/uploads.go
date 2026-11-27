package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const uploadPartSize int64 = 8 * 1024 * 1024
const uploadTTLHours = 24

type uploadSession struct {
	ID, ParentID, Name, Conflict string
	Size, PartSize               int64
	SHA256                       sql.NullString
	ExpiresAt                    string
	CreatedAt, ModifiedAt        sql.NullString
}

func uploadPartsCount(size, partSize int64) int {
	if size == 0 {
		return 1
	}
	return int((size + partSize - 1) / partSize)
}

func (h *Handler) loadUpload(ctx context.Context, uid int64, id string) (uploadSession, error) {
	var s uploadSession
	err := h.db.QueryRowContext(ctx, `SELECT id,parent_id,name,conflict,total_size,chunk_size,sha256,expires_at,client_created_at,client_modified_at FROM upload_sessions WHERE id=? AND user_id=? AND datetime(expires_at)>datetime('now')`, id, uid).Scan(&s.ID, &s.ParentID, &s.Name, &s.Conflict, &s.Size, &s.PartSize, &s.SHA256, &s.ExpiresAt, &s.CreatedAt, &s.ModifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, apiError{404, "Upload not found or expired", "UPLOAD_NOT_FOUND"}
	}
	return s, err
}

func (h *Handler) uploadView(ctx context.Context, uid int64, s uploadSession) (map[string]any, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT part_index FROM upload_parts WHERE upload_id=? ORDER BY part_index`, s.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	received := []int{}
	for rows.Next() {
		var i int
		if err = rows.Scan(&i); err != nil {
			return nil, err
		}
		received = append(received, i)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"id": s.ID, "parentId": s.ParentID, "name": s.Name, "size": s.Size, "partSize": s.PartSize, "partCount": uploadPartsCount(s.Size, s.PartSize), "receivedParts": received, "expiresAt": s.ExpiresAt}, nil
}

func (h *Handler) createUpload(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.auth(w, r)
	if !ok {
		return
	}
	var body struct {
		ParentID   string          `json:"parentId"`
		Name       string          `json:"name"`
		Size       json.Number     `json:"size"`
		Conflict   string          `json:"conflict"`
		SHA256     string          `json:"sha256"`
		CreatedAt  json.RawMessage `json:"createdAt"`
		ModifiedAt json.RawMessage `json:"modifiedAt"`
	}
	if !decode(w, r, &body) {
		return
	}
	size, err := strconv.ParseInt(string(body.Size), 10, 64)
	if err != nil || size < 0 || size > 9007199254740991 {
		writeErr(w, 400, "size must be a byte count.", "INVALID_SIZE")
		return
	}
	parent := body.ParentID
	if parent == "" {
		parent = "root"
	}
	mode := body.Conflict
	if mode == "" {
		mode = "fail"
	}
	if mode != "fail" && mode != "rename" && mode != "replace" {
		writeErr(w, 400, "conflict must be fail, rename or replace.", "CONFLICT_MODE_INVALID")
		return
	}
	name, err := validateName(body.Name)
	if err != nil {
		fail(w, err)
		return
	}
	hash := strings.ToLower(strings.TrimSpace(body.SHA256))
	if hash != "" && !sha256Pattern.MatchString(hash) {
		writeErr(w, 400, "sha256 must be 64 hex characters.", "INVALID_HASH")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	p, shared, err := h.resolveReadNode(tx, actorID, parent)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	uid := actorID
	var createdBy any
	if shared != nil {
		if shared.permission != "write" {
			failTx(w, tx, readOnlyShare())
			return
		}
		uid = shared.ownerID
		createdBy = actorID
	}
	if p.Type != "folder" {
		failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
		return
	}
	existing, found, err := childByName(tx, p.ID, name, "")
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if found && mode != "rename" && (mode == "fail" || existing.kind != "file") {
		failTx(w, tx, nameExists(existing.name))
		return
	}
	if maxUploadBytes := h.storagePolicy().MaxUploadBytes; maxUploadBytes > 0 && size > maxUploadBytes {
		failTx(w, tx, apiError{413, "This file is larger than the server allows.", "FILE_TOO_LARGE"})
		return
	}
	if err = h.checkQuotaTx(tx, uid, size, ""); err != nil {
		failTx(w, tx, err)
		return
	}
	id := randomID()
	// The file's own dates come as headers or as body fields.
	date := func(header string, field json.RawMessage) *string {
		if value := r.Header.Get(header); value != "" {
			return clientDate(value)
		}
		return clientDate(strings.Trim(string(field), `"`))
	}
	created, modified := date("X-File-Created", body.CreatedAt), date("X-File-Modified", body.ModifiedAt)
	_, err = tx.Exec(`INSERT INTO upload_sessions(id,user_id,parent_id,name,conflict,total_size,chunk_size,sha256,expires_at,client_created_at,client_modified_at,created_by) VALUES(?,?,?,?,?,?,?,?,datetime('now',?),?,?,?)`, id, uid, p.ID, name, mode, size, uploadPartSize, nullString(hash), fmt.Sprintf("+%d hours", uploadTTLHours), created, modified, createdBy)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s, err := h.loadUpload(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	out, err := h.uploadView(r.Context(), uid, s)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (h *Handler) getUpload(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.auth(w, r)
	if !ok {
		return
	}
	uid, err := h.uploadOwner(r, viewer, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	s, err := h.loadUpload(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	out, err := h.uploadView(r.Context(), uid, s)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}

func (h *Handler) partPath(id string, index int) string {
	return filepath.Join(h.uploadTmpDir, "parts", id, strconv.Itoa(index))
}
func uploadPartBlobID(id string, index int) string { return fmt.Sprintf("upload-%s-%d", id, index) }

func (h *Handler) putUploadPart(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.auth(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	uid, err := h.uploadOwner(r, viewer, id)
	if err != nil {
		fail(w, err)
		return
	}
	s, err := h.loadUpload(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	count := uploadPartsCount(s.Size, s.PartSize)
	if err != nil || index < 0 || index >= count {
		writeErr(w, 400, "Part index out of range.", "INVALID_PART")
		return
	}
	expected := s.PartSize
	if index == count-1 {
		expected = s.Size - int64(index)*s.PartSize
	}
	if r.ContentLength >= 0 && r.ContentLength != expected {
		writeErr(w, 400, fmt.Sprintf("Part %d must be %d bytes.", index, expected), "SIZE_MISMATCH")
		return
	}
	if err = h.CheckDiskSpace(expected); err != nil {
		fail(w, err)
		return
	}
	if err = os.MkdirAll(filepath.Dir(h.partPath(id, index)), 0700); err != nil {
		fail(w, err)
		return
	}
	partID := uploadPartBlobID(id, index)
	blob, err := h.encryptStream(r.Context(), uid, r.Body, partID, uploadReadLimit(expected), expected, apiError{400, fmt.Sprintf("Part %d must be %d bytes.", index, expected), "SIZE_MISMATCH"})
	if err != nil {
		fail(w, err)
		return
	}
	if blob.size != expected {
		_ = os.Remove(blob.tempPath)
		writeErr(w, 400, fmt.Sprintf("Part %d must be %d bytes.", index, expected), "SIZE_MISMATCH")
		return
	}
	h.uploadsMu.Lock()
	defer h.uploadsMu.Unlock()
	if h.completingUploads[id] {
		_ = os.Remove(blob.tempPath)
		writeErr(w, 409, "This upload is being completed.", "UPLOAD_COMPLETING")
		return
	}
	if _, err = h.loadUpload(r.Context(), uid, id); err != nil {
		_ = os.Remove(blob.tempPath)
		fail(w, err)
		return
	}
	path := h.partPath(id, index)
	if err = os.Rename(blob.tempPath, path); err != nil {
		_ = os.Remove(blob.tempPath)
		fail(w, err)
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO upload_parts(upload_id,part_index,size) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM upload_sessions WHERE id=? AND user_id=? AND datetime(expires_at)>datetime('now')) ON CONFLICT(upload_id,part_index) DO UPDATE SET size=excluded.size`, id, index, expected, id, uid)
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE upload_sessions SET expires_at=datetime('now',?) WHERE id=? AND user_id=?`, fmt.Sprintf("+%d hours", uploadTTLHours), id, uid)
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"index": index, "size": blob.size, "sha256": blob.hash})
}

func (h *Handler) completeUpload(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.auth(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	uid, err := h.uploadOwner(r, actorID, id)
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		BaseETag string `json:"baseEtag"`
	}
	if !decode(w, r, &body) {
		return
	}
	h.uploadsMu.Lock()
	if h.completingUploads[id] {
		h.uploadsMu.Unlock()
		writeErr(w, 409, "This upload is being completed.", "UPLOAD_COMPLETING")
		return
	}
	s, err := h.loadUpload(r.Context(), uid, id)
	if err != nil {
		h.uploadsMu.Unlock()
		fail(w, err)
		return
	}
	var received int
	err = h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM upload_parts WHERE upload_id=?`, id).Scan(&received)
	if err != nil {
		h.uploadsMu.Unlock()
		fail(w, err)
		return
	}
	if received != uploadPartsCount(s.Size, s.PartSize) {
		h.uploadsMu.Unlock()
		writeErr(w, 409, "Some parts are still missing.", "UPLOAD_INCOMPLETE")
		return
	}
	if err = h.CheckDiskSpace(s.Size); err != nil {
		h.uploadsMu.Unlock()
		fail(w, err)
		return
	}
	h.completingUploads[id] = true
	h.uploadsMu.Unlock()
	defer func() { h.uploadsMu.Lock(); delete(h.completingUploads, id); h.uploadsMu.Unlock() }()
	dek, err := h.keys.GetUserDEK(r.Context(), h.db, uid)
	if err != nil {
		fail(w, err)
		return
	}
	reader, writer := io.Pipe()
	go func() {
		var streamErr error
		for i := 0; i < uploadPartsCount(s.Size, s.PartSize) && streamErr == nil; i++ {
			streamErr = h.decryptPart(writer, s, i, dek)
		}
		_ = writer.CloseWithError(streamErr)
	}()
	blob, err := h.encryptStream(r.Context(), uid, reader, randomID(), uploadReadLimit(s.Size), s.Size, apiError{400, "Upload size did not match its reservation.", "SIZE_MISMATCH"})
	_ = reader.Close()
	if err != nil {
		fail(w, err)
		return
	}
	if blob.size != s.Size {
		_ = os.Remove(blob.tempPath)
		writeErr(w, 400, "Upload size did not match its reservation.", "SIZE_MISMATCH")
		return
	}
	if s.SHA256.Valid && s.SHA256.String != blob.hash {
		_ = os.Remove(blob.tempPath)
		writeErr(w, 422, "The file arrived damaged (checksum mismatch).", "CHECKSUM_MISMATCH")
		return
	}
	blob.declared = s.SHA256.Valid
	blob.finalPath, err = h.placeUpload(uid, blob)
	if err != nil {
		_ = os.Remove(blob.tempPath)
		fail(w, err)
		return
	}
	result, orphans, err := h.commitUpload(r.Context(), uid, actorID, s.ParentID, s.Name, s.Conflict, strings.ReplaceAll(body.BaseETag, "\"", ""), id, blob, stringPointer(s.CreatedAt), stringPointer(s.ModifiedAt))
	if err != nil {
		_ = os.Remove(blob.finalPath)
		fail(w, err)
		return
	}
	for i := 0; i < uploadPartsCount(s.Size, s.PartSize); i++ {
		_ = os.Remove(h.partPath(id, i))
	}
	_ = os.Remove(filepath.Dir(h.partPath(id, 0)))
	h.RemoveBlobFiles(uid, orphans)
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h *Handler) decryptPart(destination io.Writer, session uploadSession, index int, dek []byte) error {
	file, err := os.Open(h.partPath(session.ID, index))
	if err != nil {
		return err
	}
	defer file.Close()
	size := partSizeAt(session, index)
	_, err = cryptox.DecryptBlobRange(file, size, 0, max64(0, size-1), dek, uploadPartBlobID(session.ID, index), destination)
	return err
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}
func partSizeAt(s uploadSession, index int) int64 {
	count := uploadPartsCount(s.Size, s.PartSize)
	if index == count-1 {
		return s.Size - int64(index)*s.PartSize
	}
	return s.PartSize
}
func uploadReadLimit(size int64) int64 {
	if size < int64(^uint64(0)>>1) {
		return size + 1
	}
	return size
}

func (h *Handler) cancelUpload(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.auth(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	uid, err := h.uploadOwner(r, viewer, id)
	if err != nil {
		fail(w, err)
		return
	}
	h.uploadsMu.Lock()
	defer h.uploadsMu.Unlock()
	if h.completingUploads[id] {
		writeErr(w, 409, "This upload is being completed.", "UPLOAD_COMPLETING")
		return
	}
	if _, err := h.loadUpload(r.Context(), uid, id); err != nil {
		fail(w, err)
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM upload_sessions WHERE id=? AND user_id=?`, id, uid); err != nil {
		fail(w, err)
		return
	}
	_ = os.RemoveAll(filepath.Join(h.uploadTmpDir, "parts", id))
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}
