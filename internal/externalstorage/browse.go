package externalstorage

import (
	"cmp"
	"context"
	"io"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// What a person can do inside a mount. Every path is normalised first and a
// read-only mount refuses all changes.

type item struct {
	Name  string  `json:"name"`
	Path  string  `json:"path"`
	Type  string  `json:"type"`
	Size  int64   `json:"size"`
	Mtime *string `json:"mtime"`
	Mime  string  `json:"mime,omitempty"`
}

func describe(folder string, e entry) (item, error) {
	path, err := joinPath(folder, e.name)
	described := item{Name: e.name, Path: path, Type: "folder", Size: e.size}
	if !e.folder {
		described.Type, described.Mime = "file", files.MimeForName(e.name)
	}
	if !e.modified.IsZero() {
		stamp := e.modified.UTC().Format("2006-01-02T15:04:05.000Z")
		described.Mtime = &stamp
	}
	return described, err
}

func sortItems(items []item) {
	names := collate.New(language.Und, collate.Loose, collate.Numeric)
	slices.SortStableFunc(items, func(a, b item) int {
		if a.Type != b.Type {
			return cmp.Compare(b.Type, a.Type) // "folder" before "file"
		}
		return names.CompareString(a.Name, b.Name)
	})
}

// session is an open connection for one request.
type session struct {
	mount   mount
	adapter adapter
	release func()
}

func (h *Handler) session(ctx context.Context, userID int64, mountID string, write bool) (session, error) {
	m, err := h.mount(ctx, userID, mountID)
	if err != nil {
		return session{}, err
	}
	if write && m.ReadOnly {
		return session{}, apperr.New(http.StatusForbidden, "This connection is read-only.", "EXTERNAL_READ_ONLY")
	}
	connected, release, err := h.dial(ctx, userID, m)
	return session{m, connected, release}, err
}

// queryPath reads a path parameter; a repeated one is not a path.
func queryPath(r *http.Request) (string, error) {
	values := r.URL.Query()["path"]
	if len(values) > 1 {
		return "", invalidPath()
	}
	if len(values) == 0 {
		return "", nil
	}
	return normalizePath(values[0])
}

func (h *Handler) listFolder(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	path, err := queryPath(r)
	if err != nil {
		return 0, nil, err
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), false)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	entries, truncated, err := s.adapter.list(r.Context(), path, listLimit)
	if err != nil {
		return 0, nil, err
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		// A name NeoCloud can't hold could not be addressed afterwards.
		if described, err := describe(path, e); err == nil {
			items = append(items, described)
		}
	}
	sortItems(items)
	return http.StatusOK, map[string]any{
		"mount": map[string]any{"id": s.mount.ID, "name": s.mount.Name, "readOnly": s.mount.ReadOnly},
		"path":  path, "items": items, "truncated": truncated,
	}, nil
}

// openFile connects and looks up the file at path. The caller releases.
func (h *Handler) openFile(ctx context.Context, userID int64, mountID, path string) (session, entry, error) {
	s, err := h.session(ctx, userID, mountID, false)
	if err != nil {
		return session{}, entry{}, err
	}
	info, err := s.adapter.stat(ctx, path)
	switch {
	case err != nil:
	case info == nil:
		err = notFound()
	case info.folder:
		err = apperr.New(http.StatusBadRequest, "That is a folder.", "NOT_A_FILE")
	}
	if err != nil {
		s.release()
		return session{}, entry{}, err
	}
	info.name = baseName(path)
	return s, *info, nil
}

const sandboxPolicy = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'"

// Only these may show inline; everything else (HTML, SVG, PDF, ...) is an
// attachment, so a file at a provider can never run as part of the app.
var inlineTypes = regexp.MustCompile(`^(?:image/(?:png|jpeg|gif|webp|avif|bmp)|audio/[\w.+-]+|video/[\w.+-]+)$`)

func (h *Handler) download(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	path, err := queryPath(r)
	if err != nil {
		return 0, nil, err
	}
	s, info, err := h.openFile(r.Context(), userID, r.PathValue("id"), path)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	start, end, partial, unsatisfiable := files.ParseByteRange(r.Header.Get("Range"), info.size)
	if unsatisfiable {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(info.size, 10))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return 0, nil, nil
	}
	var part *byteRange
	length := info.size
	if partial {
		part = &byteRange{start, end}
		length = end - start + 1
	}
	var body io.ReadCloser
	if info.size > 0 && r.Method != http.MethodHead {
		if body, err = s.adapter.read(r.Context(), path, part); err != nil {
			return 0, nil, err
		}
		defer body.Close()
	}
	contentType := files.MimeForName(info.name)
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = parsed
	}
	disposition := "attachment"
	if r.URL.Query().Get("download") != "1" && inlineTypes.MatchString(contentType) {
		disposition = "inline"
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Disposition", files.ContentDisposition(disposition, info.name))
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	header.Set("Accept-Ranges", "bytes")
	header.Set("Cache-Control", "private, no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", sandboxPolicy)
	if partial {
		header.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(info.size, 10))
		w.WriteHeader(http.StatusPartialContent)
	}
	if body != nil {
		// A broken stream leaves the response short, which cuts the connection.
		_, _ = io.CopyN(w, body, length)
	}
	return 0, nil, nil
}

// uploadBody passes the request body on and remembers why it failed: too many
// bytes, or a sender that went away before the end. The lock covers an HTTP
// transport still reading after the provider has answered.
type uploadBody struct {
	mu     sync.Mutex
	source io.Reader
	left   int64
	err    error
}

func tooLarge() error {
	return apperr.New(http.StatusRequestEntityTooLarge, "This file is larger than the upload limit.", "FILE_TOO_LARGE")
}

func (u *uploadBody) failure() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.err
}

func (u *uploadBody) Read(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return 0, u.err
	}
	n, err := u.source.Read(p)
	u.left -= int64(n)
	switch {
	case u.left < 0:
		u.err = tooLarge()
		return 0, u.err
	case err != nil && err != io.EOF:
		u.err = apperr.New(http.StatusBadRequest, "Upload was interrupted.", "UPLOAD_ABORTED")
		return n, u.err
	}
	return n, err
}

// freeName is name, or the first "name (n)" nothing has yet.
func freeName(ctx context.Context, a adapter, folder, name string) (string, error) {
	for number := range 100 {
		candidate := name
		if number > 0 {
			candidate = numberedName(name, number)
		}
		path, err := joinPath(folder, candidate)
		if err != nil {
			return "", err
		}
		existing, err := a.stat(ctx, path)
		if err != nil || existing == nil {
			return path, err
		}
	}
	return "", nameExists()
}

func (h *Handler) upload(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	path, err := queryPath(r)
	if err != nil {
		return 0, nil, err
	}
	if path == "" {
		return 0, nil, pathError("Give the file a name.")
	}
	size := r.ContentLength
	if size > h.maxUploadBytes {
		return 0, nil, tooLarge()
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), true)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	folder, target := parentOf(path), path
	if conflict := r.URL.Query().Get("conflict"); conflict != "replace" {
		existing, err := s.adapter.stat(r.Context(), path)
		if err != nil {
			return 0, nil, err
		}
		if existing != nil {
			if conflict != "rename" {
				return 0, nil, nameExists()
			}
			if target, err = freeName(r.Context(), s.adapter, folder, baseName(path)); err != nil {
				return 0, nil, err
			}
		}
	}
	body := &uploadBody{source: r.Body, left: h.maxUploadBytes}
	err = s.adapter.write(r.Context(), target, body, size)
	if failure := body.failure(); failure != nil {
		err = failure
	}
	if err != nil {
		return 0, nil, err
	}
	stored, err := describe(folder, entry{name: baseName(target), size: max(size, 0), modified: time.Now()})
	return http.StatusCreated, stored, err
}

func bodyPaths(w http.ResponseWriter, r *http.Request, names ...string) ([]string, error) {
	body, err := apijson.Object(w, r)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(names))
	for i, name := range names {
		if paths[i], err = normalizePath(body[name]); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	paths, err := bodyPaths(w, r, "path")
	if err != nil {
		return 0, nil, err
	}
	path := paths[0]
	if path == "" {
		return 0, nil, pathError("Give the folder a name.")
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), true)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	existing, err := s.adapter.stat(r.Context(), path)
	if err != nil {
		return 0, nil, err
	}
	if existing != nil {
		return 0, nil, nameExists()
	}
	if err = s.adapter.mkdir(r.Context(), path); err != nil {
		return 0, nil, err
	}
	created, err := describe(parentOf(path), entry{name: baseName(path), folder: true})
	return http.StatusCreated, created, err
}

func (h *Handler) move(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	paths, err := bodyPaths(w, r, "from", "to")
	if err != nil {
		return 0, nil, err
	}
	from, to := paths[0], paths[1]
	if from == "" || to == "" {
		return 0, nil, pathError("The connection itself can’t be renamed or moved here.")
	}
	if isInside(from, to) {
		return 0, nil, pathError("An item can’t be moved into itself.")
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), true)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	source, err := s.adapter.stat(r.Context(), from)
	if err != nil {
		return 0, nil, err
	}
	if source == nil {
		return 0, nil, notFound()
	}
	existing, err := s.adapter.stat(r.Context(), to)
	if err != nil {
		return 0, nil, err
	}
	if existing != nil {
		return 0, nil, nameExists()
	}
	if err = s.adapter.move(r.Context(), from, to); err != nil {
		return 0, nil, err
	}
	if err = audit(r.Context(), h.db, "external_storage.move", userID, map[string]string{"mountId": s.mount.ID}); err != nil {
		return 0, nil, err
	}
	source.name = baseName(to)
	moved, err := describe(parentOf(to), *source)
	return http.StatusOK, moved, err
}

func (h *Handler) remove(_ http.ResponseWriter, r *http.Request, userID int64) (int, any, error) {
	path, err := queryPath(r)
	if err != nil {
		return 0, nil, err
	}
	if path == "" {
		return 0, nil, pathError("The connection itself can’t be deleted here.")
	}
	s, err := h.session(r.Context(), userID, r.PathValue("id"), true)
	if err != nil {
		return 0, nil, err
	}
	defer s.release()
	if err = s.adapter.remove(r.Context(), path); err != nil {
		return 0, nil, err
	}
	err = audit(r.Context(), h.db, "external_storage.delete", userID, map[string]string{"mountId": s.mount.ID})
	return http.StatusOK, map[string]bool{"success": true}, err
}
