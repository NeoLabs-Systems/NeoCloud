// Package agentapi exposes the scoped file surface used by connected apps.
package agentapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/oauth"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/google/uuid"
)

const maxContentBytes = 5 << 20
const maxTextBytes = 1 << 20

var pathLimitPattern = regexp.MustCompile(`^\s*([+-]?\d+)`)
var textMIMEPattern = regexp.MustCompile(`(?i)^(text/|application/(json|xml|javascript|x-yaml|yaml|x-sh|sql|toml|x-ndjson)\b)|\+(json|xml)$`)

type Authenticate func(context.Context, string) (oauth.AccessPrincipal, error)

type Options struct {
	DB                *sql.DB
	Keys              *cryptox.Keyring
	BlobDir           string
	Files             *files.Handler
	Authenticate      Authenticate
	DefaultQuotaBytes int64
	MaxUploadBytes    int64
	MaxNodes          int
	VersionRetention  int
}

type Handler struct {
	policyMu          sync.RWMutex
	db                *sql.DB
	keys              *cryptox.Keyring
	blobDir           string
	files             *files.Handler
	auth              Authenticate
	limiter           *ratelimit.Limiter
	heavyLimiter      *ratelimit.Limiter
	defaultQuotaBytes int64
	maxUploadBytes    int64
	maxNodes          int
	versionRetention  int
}

// SetStoragePolicy updates account default quota, upload size, and version
// retention for subsequent agent file writes.
func (h *Handler) SetStoragePolicy(defaultQuotaBytes, maxUploadBytes int64, versionRetention int) {
	h.policyMu.Lock()
	h.defaultQuotaBytes = defaultQuotaBytes
	h.maxUploadBytes = maxUploadBytes
	h.versionRetention = versionRetention
	h.policyMu.Unlock()
}

type principalKey struct{}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.Keys == nil || options.BlobDir == "" || options.Files == nil || options.Authenticate == nil {
		return nil, errors.New("agentapi: database, keyring, blob directory, files handler, and authenticator are required")
	}
	if options.MaxNodes < 1 {
		options.MaxNodes = 1_000_000
	}
	if options.VersionRetention < 0 {
		options.VersionRetention = 0
	}
	if options.VersionRetention == 0 {
		options.VersionRetention = 10
	}
	h := &Handler{db: options.DB, keys: options.Keys, blobDir: options.BlobDir, files: options.Files, auth: options.Authenticate,
		defaultQuotaBytes: options.DefaultQuotaBytes, maxUploadBytes: options.MaxUploadBytes, maxNodes: options.MaxNodes, versionRetention: options.VersionRetention}
	perUser := func(r *http.Request) string {
		return "user:" + strconv.FormatInt(principal(r).UserID, 10)
	}
	h.heavyLimiter = ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 200, Key: perUser,
		Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	h.limiter = ratelimit.New(ratelimit.Config{
		Window:  15 * time.Minute,
		Max:     3000,
		Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"},
		Key:     perUser,
	})
	return h, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/files", h.protected(http.HandlerFunc(h.list)))
	mux.Handle("GET /api/v1/files/search", h.protected(http.HandlerFunc(h.search)))
	mux.Handle("GET /api/v1/files/{id}/content", h.protected(http.HandlerFunc(h.content)))
	mux.Handle("GET /api/v1/files/{id}", h.protected(http.HandlerFunc(h.get)))
	mux.Handle("POST /api/v1/files", h.protectedScope(http.HandlerFunc(h.createFile), "files:write"))
	mux.Handle("PUT /api/v1/files/{id}/content", h.protectedScope(http.HandlerFunc(h.updateContent), "files:write"))
	mux.Handle("PATCH /api/v1/files/{id}", h.protectedScope(http.HandlerFunc(h.moveOrRename), "files:write"))
	mux.Handle("DELETE /api/v1/files/{id}", h.protectedScope(http.HandlerFunc(h.trashItem), "files:write"))
	mux.Handle("POST /api/v1/folders", h.protectedScope(http.HandlerFunc(h.createFolder), "files:write"))
	mux.Handle("POST /api/v1/documents/pdf", h.Guard("files:write", true, h.createPDF))
}

func (h *Handler) protected(next http.Handler) http.Handler {
	return h.protectedScope(next, "files:read")
}

// Guard admits a connected app's access token that was granted scope. Every
// /api/v1 route shares one per-account limit; heavy routes (rendering,
// signing requests) have a tighter one of their own.
func (h *Handler) Guard(scope string, heavy bool, next func(w http.ResponseWriter, r *http.Request, userID int64)) http.Handler {
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next(w, r, principal(r).UserID) })
	if heavy {
		handler = h.heavyLimiter.Middleware(handler)
	}
	return h.protectedScope(handler, scope)
}

// HasScope reports whether the guarded request's token was granted a scope.
func (h *Handler) HasScope(r *http.Request, scope string) bool {
	return hasScope(principal(r).Scopes, scope)
}

func (h *Handler) protectedScope(next http.Handler, scope string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			errorJSON(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		accessPrincipal, err := h.auth(r.Context(), token)
		if errors.Is(err, oauth.ErrInvalidAccessToken) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			errorJSON(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, "Could not validate access token", "INTERNAL_ERROR")
			return
		}
		if accessPrincipal.UserID < 1 {
			errorJSON(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, accessPrincipal))
		scoped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasScope(principal(r).Scopes, scope) {
				w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+scope+`"`)
				errorJSON(w, http.StatusForbidden, "This connection was not allowed to do that ("+scope+").", "INSUFFICIENT_SCOPE")
				return
			}
			next.ServeHTTP(w, r)
		})
		h.limiter.Middleware(scoped).ServeHTTP(w, r)
	})
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func hasScope(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}

func principal(r *http.Request) oauth.AccessPrincipal {
	p, _ := r.Context().Value(principalKey{}).(oauth.AccessPrincipal)
	return p
}

func errorJSON(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

type node struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Type       string  `json:"type"`
	Size       int64   `json:"size"`
	MIMEType   *string `json:"mimeType"`
	ModifiedAt string  `json:"modifiedAt"`
	Revision   string  `json:"revision,omitempty"`
}

func sqlMIME(value sql.NullString) *string {
	if value.Valid && value.String != "" {
		return &value.String
	}
	return nil
}

// ensureRootSQL creates the account's root folder on first use: a connected app
// may be the first thing that ever touches a new account.
const ensureRootSQL = `INSERT OR IGNORE INTO nodes(id,user_id,parent_id,type,name,etag)
	SELECT ?,?,NULL,'folder','',? WHERE NOT EXISTS(SELECT 1 FROM nodes WHERE user_id=? AND parent_id IS NULL)`

func (h *Handler) root(ctx context.Context, userID int64) (string, error) {
	if _, err := h.db.ExecContext(ctx, ensureRootSQL, uuid.NewString(), userID, uuid.NewString(), userID); err != nil {
		return "", err
	}
	var id string
	err := h.db.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL AND trashed_at IS NULL`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
	}
	return id, err
}

type fileError struct {
	status  int
	message string
	code    string
}

func (e fileError) Error() string     { return e.message }
func (e fileError) StatusCode() int   { return e.status }
func (e fileError) ErrorCode() string { return e.code }

type httpStatusError interface {
	error
	HTTPStatus() int
}

type codedError interface {
	error
	ErrorCode() string
}

func mapFileError(err error) error {
	if err == nil {
		return nil
	}
	var fe fileError
	if errors.As(err, &fe) {
		return err
	}
	var se httpStatusError
	if errors.As(err, &se) {
		code := "INTERNAL_ERROR"
		var ce codedError
		if errors.As(err, &ce) {
			code = ce.ErrorCode()
		}
		status, msg := se.HTTPStatus(), se.Error()
		switch code {
		case "NAME_EXISTS":
			code = "ALREADY_EXISTS"
		case "ETAG_MISMATCH":
			status = http.StatusConflict
			code = "CONFLICT"
			msg = "The file changed since that revision."
		}
		return fileError{status: status, message: msg, code: code}
	}
	return err
}

func writeFileError(w http.ResponseWriter, err error) {
	err = mapFileError(err)
	var e fileError
	if errors.As(err, &e) {
		errorJSON(w, e.status, e.message, e.code)
		return
	}
	errorJSON(w, http.StatusInternalServerError, "The file operation could not be completed.", "INTERNAL_ERROR")
}

func (h *Handler) pathParts(raw string) ([]string, error) {
	if raw == "" {
		raw = "/"
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(raw, "/") {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return nil, fileError{http.StatusBadRequest, "path may not contain . or .. segments.", "INVALID_PATH"}
		}
		parts = append(parts, part)
	}
	if len(parts) > 64 {
		return nil, fileError{http.StatusBadRequest, "path is too deep.", "PATH_TOO_DEEP"}
	}
	return parts, nil
}

func (h *Handler) findPathNode(ctx context.Context, userID int64, raw string) (string, error) {
	parts, err := h.pathParts(raw)
	if err != nil {
		return "", err
	}
	id, err := h.root(ctx, userID)
	if err != nil {
		return "", err
	}
	for _, part := range parts {
		var child, vault sql.NullString
		err = h.db.QueryRowContext(ctx, `SELECT id,vault_id FROM nodes WHERE parent_id=? AND user_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, id, userID, part).Scan(&child, &vault)
		if errors.Is(err, sql.ErrNoRows) || vault.Valid {
			return "", fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
		}
		if err != nil {
			return "", err
		}
		id = child.String
	}
	return id, nil
}

func (h *Handler) itemAt(ctx context.Context, userID int64, id string) (node, error) {
	var n node
	var parent, mime, vault, trash sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.modified_at,n.etag,n.vault_id,n.trashed_at
		FROM nodes n WHERE n.id=? AND n.user_id=?`, id, userID).Scan(&n.ID, &parent, &n.Type, &n.Name, &n.Size, &mime, &n.ModifiedAt, &n.Revision, &vault, &trash)
	if errors.Is(err, sql.ErrNoRows) || vault.Valid || trash.Valid {
		return node{}, fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
	}
	if err != nil {
		return node{}, err
	}
	var hidden int
	err = h.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id) AS (
		SELECT id,parent_id,trashed_at,vault_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id,p.trashed_at,p.vault_id FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL)`, id, userID, userID).Scan(&hidden)
	if err != nil {
		return node{}, err
	}
	if hidden != 0 {
		return node{}, fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
	}
	n.MIMEType = sqlMIME(mime)
	if n.Type == "file" && n.MIMEType == nil {
		value := "application/octet-stream"
		n.MIMEType = &value
	} else if n.Type != "file" {
		n.Size = 0
		n.MIMEType = nil
	}
	n.Path, err = h.nodePath(ctx, userID, n.ID)
	return n, err
}

func (h *Handler) nodePath(ctx context.Context, userID int64, id string) (string, error) {
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE up(id,parent_id,name,depth) AS (
		SELECT id,parent_id,name,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id,p.name,up.depth+1 FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT name,parent_id FROM up ORDER BY depth DESC`, id, userID, userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	parts := []string{}
	for rows.Next() {
		var name string
		var parent sql.NullString
		if err = rows.Scan(&name, &parent); err != nil {
			return "", err
		}
		if parent.Valid {
			parts = append(parts, name)
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return "/" + strings.Join(parts, "/"), nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := principal(r).UserID
	folderID, err := h.findPathNode(r.Context(), userID, r.URL.Query().Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	var kind string
	err = h.db.QueryRowContext(r.Context(), `SELECT type FROM nodes WHERE id=? AND user_id=?`, folderID, userID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		writeFileError(w, fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"})
		return
	}
	if err != nil {
		writeFileError(w, err)
		return
	}
	if kind != "folder" {
		writeFileError(w, fileError{http.StatusBadRequest, "That is not a folder.", "NOT_A_FOLDER"})
		return
	}
	folderPath, err := h.nodePath(r.Context(), userID, folderID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,type,name,size,mime,modified_at FROM nodes WHERE parent_id=? AND user_id=? AND trashed_at IS NULL AND vault_id IS NULL ORDER BY type='folder' DESC,lower(name)`, folderID, userID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer rows.Close()
	items := []node{}
	for rows.Next() {
		var n node
		var mime sql.NullString
		if err = rows.Scan(&n.ID, &n.Type, &n.Name, &n.Size, &mime, &n.ModifiedAt); err != nil {
			writeFileError(w, err)
			return
		}
		if n.Type != "file" {
			n.Size = 0
		}
		n.MIMEType = nil
		if n.Type == "file" {
			value := "application/octet-stream"
			if mime.Valid && mime.String != "" {
				value = mime.String
			}
			n.MIMEType = &value
		}
		if folderPath == "/" {
			n.Path = "/" + n.Name
		} else {
			n.Path = folderPath + "/" + n.Name
		}
		items = append(items, n)
	}
	if err = rows.Err(); err != nil {
		writeFileError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"path": folderPath, "items": items})
}

func parseLimit(raw string) int {
	if raw == "" {
		return 100
	}
	match := pathLimitPattern.FindStringSubmatch(raw)
	if match == nil {
		return 100
	}
	limit, err := strconv.Atoi(match[1])
	if err != nil || limit < 1 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

type searchItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Type       string  `json:"type"`
	Size       int64   `json:"size"`
	MIMEType   *string `json:"mimeType"`
	ModifiedAt string  `json:"modifiedAt"`
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	userID := principal(r).UserID
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	if text == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": []searchItem{}})
		return
	}
	text = strings.ToLower(text)
	terms := strings.Fields(text)
	if len(terms) > 8 {
		terms = terms[:8]
	}
	where := []string{"n.user_id=?", "n.parent_id IS NOT NULL", "n.vault_id IS NULL", "n.id NOT IN (SELECT id FROM hidden)"}
	args := []any{userID, userID, userID}
	for _, term := range terms {
		pattern := "%" + escapeLike(term) + "%"
		where = append(where, `(lower(n.name) LIKE ? ESCAPE '\' OR EXISTS(SELECT 1 FROM ai_node_meta m WHERE m.user_id=n.user_id AND m.node_id=n.id AND lower(m.search_text) LIKE ? ESCAPE '\'))`)
		args = append(args, pattern, pattern)
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	prefix := escapeLike(text) + "%"
	rank := `CASE WHEN lower(n.name)=? THEN 0 WHEN lower(n.name) LIKE ? ESCAPE '\' THEN 1 WHEN lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' THEN 2 ELSE 3 END,`
	args = append(args, text, prefix, "% "+prefix, "%-"+prefix, "%\\_"+prefix, "%."+prefix, "%("+prefix, limit)
	query := `WITH RECURSIVE hidden(id) AS (
		SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL
		UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?
	) SELECT n.id,n.parent_id,n.name,n.type,n.size,n.mime,n.modified_at FROM nodes n WHERE ` + strings.Join(where, " AND ") + ` ORDER BY ` + rank + ` n.modified_at DESC,lower(n.name),n.id LIMIT ?`
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeFileError(w, err)
		return
	}
	type searchRow struct {
		id, parent, name, kind, modified string
		size                             int64
		mime                             sql.NullString
	}
	found := []searchRow{}
	for rows.Next() {
		var item searchRow
		if err = rows.Scan(&item.id, &item.parent, &item.name, &item.kind, &item.size, &item.mime, &item.modified); err != nil {
			rows.Close()
			writeFileError(w, err)
			return
		}
		found = append(found, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeFileError(w, err)
		return
	}
	if err = rows.Close(); err != nil {
		writeFileError(w, err)
		return
	}
	items := []searchItem{}
	for _, row := range found {
		var item searchItem
		item.ID, item.Name, item.Type, item.ModifiedAt = row.id, row.name, row.kind, row.modified
		parentPath, pathErr := h.nodePath(r.Context(), userID, row.parent)
		if pathErr != nil {
			writeFileError(w, pathErr)
			return
		}
		if parentPath == "/" {
			item.Path = "/" + item.Name
		} else {
			item.Path = parentPath + "/" + item.Name
		}
		if item.Type == "file" {
			item.Size = row.size
			value := "application/octet-stream"
			if row.mime.Valid && row.mime.String != "" {
				value = row.mime.String
			}
			item.MIMEType = &value
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	n, err := h.itemAt(r.Context(), principal(r).UserID, r.PathValue("id"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, n)
}

func (h *Handler) content(w http.ResponseWriter, r *http.Request) {
	userID := principal(r).UserID
	id := r.PathValue("id")
	n, err := h.itemAt(r.Context(), userID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if n.Type != "file" {
		writeFileError(w, fileError{http.StatusBadRequest, "That is not a file.", "NOT_A_FILE"})
		return
	}
	if n.Size > maxContentBytes {
		writeFileError(w, fileError{http.StatusRequestEntityTooLarge, "This file is too large to read here (5 MB at most).", "FILE_TOO_LARGE"})
		return
	}
	data, err := h.readContent(r.Context(), userID, n)
	if err != nil {
		writeFileError(w, err)
		return
	}
	mimeType := "application/octet-stream"
	if n.MIMEType != nil {
		mimeType = *n.MIMEType
	}
	asText := textMIMEPattern.MatchString(mimeType) || mimeType == "application/octet-stream"
	asText = asText && len(data) <= maxTextBytes && !bytes.Contains(data, []byte{0}) && utf8.Valid(data)
	encoding, content := "base64", base64.StdEncoding.EncodeToString(data)
	if asText {
		encoding, content = "utf8", string(data)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "mimeType": mimeType, "encoding": encoding, "content": content, "revision": n.Revision})
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (h *Handler) blobFile(userID int64, blobID string) string {
	return filepath.Join(h.blobDir, strconv.FormatInt(userID, 10), blobID[:2], blobID)
}
