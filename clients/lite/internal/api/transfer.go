package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SingleUploadLimit is where uploads switch to the resumable flow
// (docs/clients.md §2.3).
const SingleUploadLimit = 8 << 20

// PartSuffix marks a download in progress next to its final name.
const PartSuffix = ".ncpart"

// PartPath is where a download of the content etag to dest collects
// until it is complete. The etag in the name ties a partial download to
// one version, so a later attempt only resumes the same content.
func PartPath(dest, etag string) string {
	if len(etag) >= 16 {
		return dest + "." + etag[:16] + PartSuffix
	}
	return dest + PartSuffix
}

// DownloadFile writes a file to dest via PartPath and renames it into
// place only after the content hash matches wantEtag, so a half-written or
// damaged file never shows up under the real name. A part left by an
// interrupted attempt is resumed with an HTTP Range request. The part is
// fsynced before the rename and the folder after it, so the caller may
// record the file as synced once this returns. beforeCommit, when set,
// runs right before the rename and can veto it (e.g. the local file
// changed meanwhile); the finished part is then kept for the next try.
func (c *Client) DownloadFile(ctx context.Context, id, dest, wantEtag string, beforeCommit func() error) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	partPath := PartPath(dest, wantEtag)
	part, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	keep, closed := true, false
	defer func() {
		if !closed {
			part.Close()
		}
		if !keep {
			os.Remove(partPath)
		}
	}()
	hash := sha256.New()
	offset, err := io.Copy(hash, part) // hashes the prefix and seeks to its end
	if err != nil {
		return err
	}
	if wantEtag == "" && offset > 0 {
		if offset, err = restartPart(part, hash); err != nil {
			return err
		}
	}
	if wantEtag == "" || hex.EncodeToString(hash.Sum(nil)) != wantEtag {
		if err := c.fetchInto(ctx, id, wantEtag, part, hash, offset); err != nil {
			// A transient failure keeps the part for a resume; an answer
			// from the server (gone, forbidden, …) means starting over.
			var apiErr *Error
			keep = !errors.As(err, &apiErr)
			return err
		}
	}
	if got := hex.EncodeToString(hash.Sum(nil)); wantEtag != "" && got != wantEtag {
		keep = false
		return fmt.Errorf("download of %s: content hash %s does not match etag %s", filepath.Base(dest), got, wantEtag)
	}
	if err := part.Sync(); err != nil {
		return err
	}
	closed = true
	if err := part.Close(); err != nil {
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return err
		}
	}
	if err := os.Rename(partPath, dest); err != nil {
		return err
	}
	syncDir(filepath.Dir(dest))
	removeStaleParts(dest)
	return nil
}

// fetchInto appends the content from offset on to part (a Range request)
// and feeds hash, which already holds the prefix. When the server sends
// the whole file instead, or the range does not line up with the part,
// the part starts over.
func (c *Client) fetchInto(ctx context.Context, id, etag string, part *os.File, sum hash.Hash, offset int64) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/files/"+url.PathEscape(id)+"/content", nil, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	res, err := c.do(req)
	if offset > 0 && HasStatus(err, http.StatusRequestedRangeNotSatisfiable) {
		// The part is longer than the file: it belongs to something else.
		if _, err := restartPart(part, sum); err != nil {
			return err
		}
		return c.fetchInto(ctx, id, etag, part, sum, 0)
	}
	if err != nil {
		return err
	}
	defer res.Body.Close()
	served := strings.Trim(res.Header.Get("ETag"), `"`)
	resumed := res.StatusCode == http.StatusPartialContent &&
		strings.HasPrefix(res.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-") &&
		(served == "" || etag == "" || served == etag)
	if offset > 0 && !resumed {
		if _, err := restartPart(part, sum); err != nil {
			return err
		}
		if res.StatusCode == http.StatusPartialContent {
			res.Body.Close()
			return c.fetchInto(ctx, id, etag, part, sum, 0)
		}
	}
	_, err = io.Copy(io.MultiWriter(part, sum), c.Down.Reader(ctx, res.Body))
	return err
}

// ReadSmallFile fetches a file's content into memory, at most limit
// bytes, and checks it against wantEtag. It is for small files the
// client needs before deciding anything else (a .cloudignore).
func (c *Client) ReadSmallFile(ctx context.Context, id, wantEtag string, limit int64) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/files/"+url.PathEscape(id)+"/content", nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(c.Down.Reader(ctx, res.Body), limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file %s is larger than %d bytes", id, limit)
	}
	if sum := sha256.Sum256(data); wantEtag != "" && hex.EncodeToString(sum[:]) != wantEtag {
		return nil, fmt.Errorf("content of %s does not match etag %s", id, wantEtag)
	}
	return data, nil
}

// restartPart empties a part file and its running hash.
func restartPart(part *os.File, sum hash.Hash) (int64, error) {
	sum.Reset()
	if err := part.Truncate(0); err != nil {
		return 0, err
	}
	_, err := part.Seek(0, io.SeekStart)
	return 0, err
}

// isPartTag matches the ".<16 hex>" PartPath puts between name and suffix.
func isPartTag(tag string) bool {
	if len(tag) != 17 || tag[0] != '.' {
		return false
	}
	_, err := hex.DecodeString(tag[1:])
	return err == nil
}

// syncDir makes a rename inside dir durable. Not every system can sync a
// folder (Windows cannot open one), so failures are ignored.
func syncDir(dir string) {
	if folder, err := os.Open(dir); err == nil {
		folder.Sync()
		folder.Close()
	}
}

// removeStaleParts drops parts of older versions of dest.
func removeStaleParts(dest string) {
	dir, base := filepath.Dir(dest), filepath.Base(dest)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		middle, ok := strings.CutPrefix(name, base)
		if !ok || entry.IsDir() {
			continue
		}
		middle, ok = strings.CutSuffix(middle, PartSuffix)
		if ok && (middle == "" || isPartTag(middle)) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// UploadOptions describe where a file goes and how name clashes resolve.
type UploadOptions struct {
	ParentID string
	Name     string
	// Conflict is fail, rename or replace (server semantics).
	Conflict string
	// BaseEtag, with replace, makes the server refuse (412 ETAG_MISMATCH)
	// when the remote file changed since the client last saw it.
	BaseEtag string
	// SHA256 of the content, when the caller already knows it.
	SHA256 string
	// ResumeID continues an earlier resumable upload of the same content.
	ResumeID string
	// OnSession is told the id of a new resumable upload so the caller can
	// persist it and resume after a restart.
	OnSession func(id string)
}

// UploadFile sends a local file: one request below SingleUploadLimit, the
// resumable part-by-part flow above.
func (c *Client) UploadFile(ctx context.Context, localPath string, opts UploadOptions) (*Node, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, err
	}
	if info.Size() < SingleUploadLimit {
		return c.uploadSingle(ctx, localPath, opts)
	}
	return c.uploadResumable(ctx, localPath, info.Size(), opts)
}

func (c *Client) uploadSingle(ctx context.Context, localPath string, opts UploadOptions) (*Node, error) {
	// Small enough to hold in memory, which guarantees the hash we send
	// describes exactly the bytes we send even if the file changes meanwhile.
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	query := url.Values{"parentId": {opts.ParentID}, "name": {opts.Name}}
	if opts.Conflict != "" {
		query.Set("conflict", opts.Conflict)
	}
	req, err := c.newRequest(ctx, http.MethodPut, "/api/upload", query, c.Up.Reader(ctx, bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Content-SHA256", hex.EncodeToString(sum[:]))
	if opts.BaseEtag != "" {
		req.Header.Set("If-Match", `"`+opts.BaseEtag+`"`)
	}
	return decodeNode(c.do(req))
}

type uploadSession struct {
	ID            string `json:"id"`
	Size          int64  `json:"size"`
	PartSize      int64  `json:"partSize"`
	PartCount     int    `json:"partCount"`
	ReceivedParts []int  `json:"receivedParts"`
}

func (c *Client) uploadResumable(ctx context.Context, localPath string, size int64, opts UploadOptions) (*Node, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	sum := opts.SHA256
	if sum == "" {
		if sum, err = HashReader(file); err != nil {
			return nil, err
		}
	}

	session := c.resumeSession(ctx, opts.ResumeID, size)
	if session == nil {
		session = &uploadSession{}
		body := map[string]any{"parentId": opts.ParentID, "name": opts.Name, "size": size, "sha256": sum}
		if opts.Conflict != "" {
			body["conflict"] = opts.Conflict
		}
		if err := c.doJSON(ctx, http.MethodPost, "/api/uploads", nil, body, session); err != nil {
			return nil, err
		}
		if opts.OnSession != nil {
			opts.OnSession(session.ID)
		}
	}

	received := make(map[int]bool, len(session.ReceivedParts))
	for _, index := range session.ReceivedParts {
		received[index] = true
	}
	for index := 0; index < session.PartCount; index++ {
		if received[index] {
			continue
		}
		offset := int64(index) * session.PartSize
		length := min(session.PartSize, size-offset)
		if err := c.putPart(ctx, session.ID, index, io.NewSectionReader(file, offset, length), length); err != nil {
			return nil, err
		}
	}
	// The server re-hashes the joined parts against sha256, so a file that
	// changed between parts is refused (422) instead of stored half-old.
	completion := map[string]string{}
	if opts.BaseEtag != "" {
		completion["baseEtag"] = opts.BaseEtag
	}
	var node Node
	if err := c.doJSON(ctx, http.MethodPost, "/api/uploads/"+url.PathEscape(session.ID)+"/complete", nil, completion, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// resumeSession returns the server's view of an earlier upload, or nil when
// it is gone, expired or for a different size.
func (c *Client) resumeSession(ctx context.Context, id string, size int64) *uploadSession {
	if id == "" {
		return nil
	}
	var session uploadSession
	if err := c.doJSON(ctx, http.MethodGet, "/api/uploads/"+url.PathEscape(id), nil, nil, &session); err != nil {
		return nil
	}
	if session.Size != size {
		return nil
	}
	return &session
}

func (c *Client) putPart(ctx context.Context, uploadID string, index int, body io.Reader, length int64) error {
	path := "/api/uploads/" + url.PathEscape(uploadID) + "/parts/" + strconv.Itoa(index)
	req, err := c.newRequest(ctx, http.MethodPut, path, nil, c.Up.Reader(ctx, body))
	if err != nil {
		return err
	}
	req.ContentLength = length
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := c.do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// CancelUpload drops an unfinished resumable upload.
func (c *Client) CancelUpload(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/uploads/"+url.PathEscape(id), nil, nil, nil)
}

func decodeNode(res *http.Response, err error) (*Node, error) {
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var node Node
	if err := json.NewDecoder(res.Body).Decode(&node); err != nil {
		return nil, err
	}
	return &node, nil
}

// HashReader returns the SHA-256 hex of everything r yields.
func HashReader(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// HashFile returns the SHA-256 hex of a file's content (= its server etag).
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return HashReader(file)
}
