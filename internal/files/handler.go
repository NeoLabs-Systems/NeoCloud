// Package files ports the account-owned file tree as one coherent API domain.
package files

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"golang.org/x/text/unicode/norm"
)

type Handler struct {
	db                 *sql.DB
	policyMu           sync.RWMutex
	blobDir            string
	uploadTmpDir       string
	previewDir         string
	trashRetentionDays int
	maxUploadBytes     int64
	defaultQuotaBytes  int64
	versionRetention   int
	maxNodes           int
	maxCopyNodes       int
	minFreeDiskBytes   int64
	uploadsMu          sync.Mutex
	completingUploads  map[string]bool
	thumbnailSlots     chan struct{}
	rendersMu          sync.Mutex
	renders            map[string]*renderJob
	keys               *cryptox.Keyring
	davLocks           *webdav.LockManager
	searchMu           sync.RWMutex
	searchIndexer      *searchindex.Indexer
}

// SetSearchIndexer enables content search and background indexing through the
// shared encrypted search index. The indexer is optional until runtime startup
// wires the extractor and blob reader.
func (h *Handler) SetSearchIndexer(indexer *searchindex.Indexer) {
	h.searchMu.Lock()
	h.searchIndexer = indexer
	h.searchMu.Unlock()
}

func (h *Handler) currentSearchIndexer() *searchindex.Indexer {
	h.searchMu.RLock()
	defer h.searchMu.RUnlock()
	return h.searchIndexer
}

// StoragePolicy describes the limits and retention rules that can be changed
// by an administrator while the server is running.
type StoragePolicy struct {
	DefaultQuotaBytes  int64
	MaxUploadBytes     int64
	TrashRetentionDays int
	VersionRetention   int
}

// SetStoragePolicy applies the current server storage policy to subsequent
// requests. Existing account-specific quotas continue to take precedence.
func (h *Handler) SetStoragePolicy(policy StoragePolicy) {
	h.policyMu.Lock()
	h.defaultQuotaBytes = policy.DefaultQuotaBytes
	h.maxUploadBytes = policy.MaxUploadBytes
	h.trashRetentionDays = policy.TrashRetentionDays
	h.versionRetention = policy.VersionRetention
	h.policyMu.Unlock()
}

func (h *Handler) storagePolicy() StoragePolicy {
	h.policyMu.RLock()
	defer h.policyMu.RUnlock()
	return StoragePolicy{h.defaultQuotaBytes, h.maxUploadBytes, h.trashRetentionDays, h.versionRetention}
}

type Options struct {
	DB                                   *sql.DB
	Keys                                 *cryptox.Keyring
	BlobDir, UploadTmpDir, PreviewDir    string
	TrashRetentionDays, VersionRetention int
	MaxNodes                             int
	MaxCopyNodes                         int
	MaxUploadBytes, DefaultQuotaBytes    int64
	MinFreeDiskBytes                     int64
}

func New(options Options) *Handler {
	return &Handler{db: options.DB, keys: options.Keys, blobDir: options.BlobDir, uploadTmpDir: options.UploadTmpDir, previewDir: options.PreviewDir, trashRetentionDays: options.TrashRetentionDays, maxUploadBytes: options.MaxUploadBytes, defaultQuotaBytes: options.DefaultQuotaBytes, versionRetention: options.VersionRetention, maxNodes: options.MaxNodes, maxCopyNodes: options.MaxCopyNodes, minFreeDiskBytes: options.MinFreeDiskBytes, completingUploads: make(map[string]bool), thumbnailSlots: make(chan struct{}, 2), renders: make(map[string]*renderJob), davLocks: webdav.NewLockManager(options.DB)}
}

func (h *Handler) checkNodeRoom(tx *sql.Tx, uid int64, adding int) error {
	var count int
	if err := tx.QueryRow(`SELECT node_count FROM users WHERE id=?`, uid).Scan(&count); err != nil {
		return err
	}
	if count+adding > h.maxNodes {
		return apiError{507, fmt.Sprintf("This account has reached its limit of %d files and folders (trash included).", h.maxNodes), "NODE_LIMIT"}
	}
	return nil
}
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/capabilities", h.capabilities)
	mux.HandleFunc("GET /api/folders/{id}/children", h.list)
	changes := h.accountLimiter(time.Minute, 600)
	copies := h.accountLimiter(15*time.Minute, 100)
	mux.Handle("POST /api/folders", changes(h.create))
	mux.HandleFunc("GET /api/nodes/{id}", h.get)
	mux.Handle("PATCH /api/nodes/{id}", changes(h.update))
	mux.Handle("POST /api/nodes/{id}/copy", changes(copies(h.copyNode).ServeHTTP))
	mux.HandleFunc("DELETE /api/nodes/{id}", h.trash)
	mux.HandleFunc("POST /api/trash/{id}/restore", h.restore)
	mux.HandleFunc("GET /api/trash", h.listTrash)
	mux.HandleFunc("DELETE /api/trash/{id}", h.purge)
	mux.HandleFunc("DELETE /api/trash", h.emptyTrash)
	mux.HandleFunc("PUT /api/nodes/{id}/favorite", h.setFavorite)
	mux.HandleFunc("DELETE /api/nodes/{id}/favorite", h.clearFavorite)
	mux.HandleFunc("GET /api/favorites", h.listFavorites)
	mux.HandleFunc("POST /api/nodes/{id}/opened", h.recordOpen)
	mux.HandleFunc("GET /api/recent", h.recent)
	mux.HandleFunc("GET /api/search", h.search)
	mux.HandleFunc("GET /api/storage/breakdown", h.storageBreakdown)
	mux.HandleFunc("GET /api/storage/duplicates", h.storageDuplicates)
	mux.HandleFunc("PUT /api/upload", h.upload)
	mux.HandleFunc("GET /api/files/{id}/content", h.download)
	mux.HandleFunc("GET /api/files/{id}/thumbnail", h.thumbnail)
	mux.HandleFunc("GET /api/files/{id}/preview", h.officePreview)
	mux.HandleFunc("GET /api/download/zip", h.downloadZIP)
	mux.HandleFunc("GET /api/files/{id}/versions", h.listVersions)
	mux.HandleFunc("POST /api/files/{id}/versions/{version}/restore", h.restoreVersion)
	mux.HandleFunc("POST /api/uploads", h.createUpload)
	mux.HandleFunc("POST /api/uploads/check", h.contentExists)
	mux.HandleFunc("GET /api/uploads/{id}", h.getUpload)
	mux.HandleFunc("PUT /api/uploads/{id}/parts/{index}", h.putUploadPart)
	mux.HandleFunc("POST /api/uploads/{id}/complete", h.completeUpload)
	mux.HandleFunc("DELETE /api/uploads/{id}", h.cancelUpload)
}

// accountLimiter bounds tree changes per account, not per address: one account
// behind many addresses is still one account.
func (h *Handler) accountLimiter(window time.Duration, limit int) func(http.HandlerFunc) http.Handler {
	limiter := ratelimit.New(ratelimit.Config{
		Window: window, Max: limit,
		Message: map[string]string{"error": "Too many changes, try again later", "code": "RATE_LIMITED"},
		Key: func(r *http.Request) string {
			id, _ := userID(r)
			return "user:" + strconv.FormatInt(id, 10)
		},
	})
	return func(next http.HandlerFunc) http.Handler {
		limited := limiter.Middleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := h.auth(w, r); ok {
				limited.ServeHTTP(w, r)
			}
		})
	}
}

// capabilities reports optional file features that clients use to decide
// which viewers and search controls to offer.
func (h *Handler) capabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth(w, r); !ok {
		return
	}
	payload := capabilityPayload()
	if indexer := h.currentSearchIndexer(); indexer != nil {
		payload["fullTextSearch"] = indexer.IsEnabled()
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func capabilityPayload() map[string]bool {
	return map[string]bool{
		"imageThumbnails": true,
		"officePreview":   documentconvert.Available(),
		"fullTextSearch":  false,
		"ocr":             ocr.Available(),
	}
}

type apiError struct {
	Status        int
	Message, Code string
}

func (e apiError) Error() string     { return e.Message }
func (e apiError) HTTPStatus() int   { return e.Status }
func (e apiError) ErrorCode() string { return e.Code }
func failTx(w http.ResponseWriter, tx *sql.Tx, err error) {
	_ = tx.Rollback()
	fail(w, err)
}
func writeErr(w http.ResponseWriter, status int, message, code string) {
	// A refused upload may still be sending its body; closing the connection
	// stops it.
	if status == http.StatusRequestEntityTooLarge || status == http.StatusInsufficientStorage {
		w.Header().Set("Connection", "close")
	}
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}
func fail(w http.ResponseWriter, err error) {
	var e apiError
	if errors.As(err, &e) {
		writeErr(w, e.Status, e.Message, e.Code)
		return
	}
	slog.Default().Error("file operation failed", "error", err)
	writeErr(w, 500, "The file operation could not be completed.", "INTERNAL_ERROR")
}
func userID(r *http.Request) (int64, bool) {
	if identity, ok := requestauth.CurrentIdentity(r); ok {
		return identity.UserID, true
	}
	s := sessionx.FromContext(r.Context())
	if s == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(s.String("userId"), 10, 64)
	return id, err == nil && id > 0
}
func (h *Handler) auth(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := userID(r)
	if !ok {
		writeErr(w, 401, "Unauthorized", "UNAUTHORIZED")
	}
	return id, ok
}

// decode reads a JSON body into v; a request without one leaves v as it is.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	err := decoder.Decode(v)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err == nil && decoder.Decode(new(any)) != io.EOF {
		err = errors.New("data after the JSON value")
	}
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "The request body is too large.", "BODY_TOO_LARGE")
	case err != nil:
		writeErr(w, http.StatusBadRequest, "The request body is not valid JSON.", "INVALID_JSON")
	default:
		return true
	}
	return false
}

type node struct {
	ID               string  `json:"id"`
	ParentID         *string `json:"parentId"`
	Type             string  `json:"type"`
	Name             string  `json:"name"`
	Size             int64   `json:"size"`
	Mime             *string `json:"mime"`
	Etag             string  `json:"etag"`
	Version          int64   `json:"version"`
	CreatedAt        string  `json:"createdAt"`
	ModifiedAt       string  `json:"modifiedAt"`
	VaultID          *string `json:"vaultId"`
	AIExcluded       bool    `json:"aiExcluded"`
	Favorite         bool    `json:"favorite"`
	Shared           bool    `json:"shared"`
	Signed           bool    `json:"signed,omitempty"`
	SignedDocumentID *string `json:"signedDocumentId,omitempty"`
	AITags           []any   `json:"aiTags"`
	AIMeta           any     `json:"aiMeta"`
	TrashedAt        *string `json:"trashedAt,omitempty"`
}

const nodeSelect = `SELECT n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.etag,n.version,n.created_at,n.modified_at,n.vault_id,n.ai_excluded,
 EXISTS(SELECT 1 FROM favorites f WHERE f.user_id=n.user_id AND f.node_id=n.id),
 EXISTS(SELECT 1 FROM user_shares s WHERE s.node_id=n.id),
 (SELECT d.id FROM sign_documents d WHERE d.final_node_id=n.id LIMIT 1),
 (SELECT m.tags_json FROM ai_node_meta m WHERE m.node_id=n.id),
 (SELECT m.fields_json || char(31) || COALESCE(m.model,'') FROM ai_node_meta m WHERE m.node_id=n.id AND m.etag=n.etag AND m.fields_json<>'{}'), n.trashed_at FROM nodes n`

// scanNode reads one nodeSelect row; extra receives the columns a query adds
// after them.
func scanNode(row interface{ Scan(...any) error }, extra ...any) (node, error) {
	var n node
	var p, v, sd, tags, meta, trash sql.NullString
	var ai, fav, shared int
	err := row.Scan(append([]any{&n.ID, &p, &n.Type, &n.Name, &n.Size, &n.Mime, &n.Etag, &n.Version, &n.CreatedAt, &n.ModifiedAt, &v, &ai, &fav, &shared, &sd, &tags, &meta, &trash}, extra...)...)
	if err != nil {
		return n, err
	}
	if p.Valid {
		n.ParentID = &p.String
	}
	if v.Valid {
		n.VaultID = &v.String
	}
	if trash.Valid {
		n.TrashedAt = &trash.String
	}
	n.AIExcluded = ai == 1
	n.Favorite = fav == 1
	n.Shared = shared == 1
	if sd.Valid {
		n.Signed = true
		n.SignedDocumentID = &sd.String
	}
	n.AITags = []any{}
	if tags.Valid {
		var x []any
		if json.Unmarshal([]byte(tags.String), &x) == nil && x != nil {
			n.AITags = x
		}
	}
	if meta.Valid {
		parts := strings.SplitN(meta.String, "\x1f", 2)
		var x map[string]any
		if json.Unmarshal([]byte(parts[0]), &x) == nil {
			delete(x, "camera")
			x["model"] = nil
			if len(parts) > 1 && parts[1] != "" {
				x["model"] = parts[1]
			}
			n.AIMeta = x
		}
	}
	return n, nil
}
func (h *Handler) root(tx *sql.Tx, uid int64) (node, error) {
	r, e := scanNode(tx.QueryRow(nodeSelect+` WHERE n.user_id=? AND n.parent_id IS NULL`, uid))
	if e == nil {
		return r, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return node{}, e
	}
	id := randomID()
	_, e = tx.Exec(`INSERT OR IGNORE INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','',?)`, id, uid, randomID())
	if e != nil {
		return node{}, e
	}
	return scanNode(tx.QueryRow(nodeSelect+` WHERE n.user_id=? AND n.parent_id IS NULL`, uid))
}
func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func (h *Handler) live(tx *sql.Tx, uid int64, id string) (node, error) {
	if id == "root" {
		return h.root(tx, uid)
	}
	chain, e := tx.Query(`WITH RECURSIVE up(id,parent_id,trashed_at,depth) AS(SELECT id,parent_id,trashed_at,0 FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id,n.parent_id,n.trashed_at,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id) SELECT trashed_at FROM up`, id, uid)
	if e != nil {
		return node{}, e
	}
	defer chain.Close()
	found := false
	for chain.Next() {
		found = true
		var t sql.NullString
		if e = chain.Scan(&t); e != nil {
			return node{}, e
		}
		if t.Valid {
			return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
		}
	}
	if e = chain.Err(); e != nil {
		return node{}, e
	}
	if !found {
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	return scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, uid))
}
func (h *Handler) path(tx *sql.Tx, uid int64, id string) ([]map[string]string, error) {
	rows, e := tx.Query(`WITH RECURSIVE up(id,parent_id,name,depth) AS(SELECT id,parent_id,name,0 FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id,n.parent_id,n.name,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id) SELECT id,name,parent_id FROM up ORDER BY depth DESC`, id, uid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name string
		var parent sql.NullString
		if e = rows.Scan(&id, &name, &parent); e != nil {
			return nil, e
		}
		out = append(out, map[string]string{"id": id, "name": name})
	}
	return out, rows.Err()
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, e := h.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback()
	n, shared, e := h.resolveReadNode(tx, uid, r.PathValue("id"))
	if e != nil {
		failTx(w, tx, e)
		return
	}
	var p []map[string]string
	var responseNode any = n
	if shared != nil {
		p = memberPath(shared)
		responseNode = memberNodeView(n, shared.rootID)
	} else {
		p, e = h.path(tx, uid, n.ID)
		if e != nil {
			failTx(w, tx, e)
			return
		}
	}
	if e = tx.Commit(); e != nil {
		failTx(w, tx, e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"node": responseNode, "path": p})
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, e := h.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback()
	folder, shared, e := h.resolveReadNode(tx, uid, r.PathValue("id"))
	if e != nil {
		failTx(w, tx, e)
		return
	}
	if folder.Type != "folder" {
		failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
		return
	}
	sortKey := r.URL.Query().Get("sort")
	cols := map[string]string{"name": "lower(n.name)", "size": "n.size", "modified": "n.modified_at", "type": "COALESCE(n.mime,'')"}
	if _, ok := cols[sortKey]; !ok {
		sortKey = "name"
	}
	order := r.URL.Query().Get("order")
	if order != "desc" {
		order = "asc"
	}
	direction := strings.ToUpper(order)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	ownerID := uid
	if shared != nil {
		ownerID = shared.ownerID
	}
	args := []any{ownerID, folder.ID}
	where := ""
	if token := r.URL.Query().Get("cursor"); token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		var parts []json.RawMessage
		if err != nil || json.Unmarshal(decoded, &parts) != nil || len(parts) != 6 {
			failTx(w, tx, apiError{400, "cursor is not valid for this listing.", "INVALID_CURSOR"})
			return
		}
		var cSort, cOrder, sortName, id string
		var isFolder int
		if json.Unmarshal(parts[0], &cSort) != nil || json.Unmarshal(parts[1], &cOrder) != nil || json.Unmarshal(parts[2], &isFolder) != nil || json.Unmarshal(parts[4], &sortName) != nil || json.Unmarshal(parts[5], &id) != nil || cSort != sortKey || cOrder != order || (isFolder != 0 && isFolder != 1) {
			failTx(w, tx, apiError{400, "cursor is not valid for this listing.", "INVALID_CURSOR"})
			return
		}
		var sortValue any
		if sortKey == "size" {
			var n json.Number
			if json.Unmarshal(parts[3], &n) != nil {
				failTx(w, tx, apiError{400, "cursor is not valid for this listing.", "INVALID_CURSOR"})
				return
			}
			sortValue, _ = n.Int64()
		} else if json.Unmarshal(parts[3], &sortValue) != nil {
			failTx(w, tx, apiError{400, "cursor is not valid for this listing.", "INVALID_CURSOR"})
			return
		}
		comparison := ">"
		if order == "desc" {
			comparison = "<"
		}
		where = " AND ((n.type='folder') < ? OR ((n.type='folder') = ? AND (" + cols[sortKey] + " " + comparison + " ? OR (" + cols[sortKey] + " = ? AND (lower(n.name) > ? OR (lower(n.name) = ? AND n.id > ?))))))"
		args = append(args, isFolder, isFolder, sortValue, sortValue, sortName, sortName, id)
		offset = 0
	}
	vaultFilter := ""
	if shared != nil {
		vaultFilter = " AND n.vault_id IS NULL"
	}
	query := nodeSelect + ` WHERE n.user_id=? AND n.parent_id=? AND n.trashed_at IS NULL` + vaultFilter + where + ` ORDER BY (n.type='folder') DESC, ` + cols[sortKey] + ` ` + direction + `,lower(n.name),n.id LIMIT ? OFFSET ?`
	args = append(args, limit+1, offset)
	rows, e := tx.Query(query, args...)
	if e != nil {
		failTx(w, tx, e)
		return
	}
	items := []node{}
	for rows.Next() {
		n, er := scanNode(rows)
		if er != nil {
			rows.Close()
			failTx(w, tx, er)
			return
		}
		items = append(items, n)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		failTx(w, tx, e)
		return
	}
	rows.Close()
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var total int
	if e = tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE user_id=? AND parent_id=? AND trashed_at IS NULL`, ownerID, folder.ID).Scan(&total); e != nil {
		failTx(w, tx, e)
		return
	}
	var p []map[string]string
	var responseFolder any = folder
	var responseItems any = items
	if shared != nil {
		p = memberPath(shared)
		responseFolder = memberNodeView(folder, shared.rootID)
		responseItems = shared.visibleChildren(items)
		vaultCount, err := shared.hideVaultChildCount(tx)
		if err != nil {
			failTx(w, tx, err)
			return
		}
		total -= vaultCount
	} else {
		p, e = h.path(tx, uid, folder.ID)
		if e != nil {
			failTx(w, tx, e)
			return
		}
	}
	var cursor any
	if more && len(items) > 0 {
		last := items[len(items)-1]
		folderFlag := 0
		if last.Type == "folder" {
			folderFlag = 1
		}
		var sortValue any
		switch sortKey {
		case "name":
			sortValue = sqlLower(last.Name)
		case "size":
			sortValue = last.Size
		case "modified":
			sortValue = last.ModifiedAt
		case "type":
			if last.Mime != nil {
				sortValue = *last.Mime
			} else {
				sortValue = ""
			}
		}
		b, _ := json.Marshal([]any{sortKey, order, folderFlag, sortValue, sqlLower(last.Name), last.ID})
		cursor = base64.RawURLEncoding.EncodeToString(b)
	}
	if e = tx.Commit(); e != nil {
		failTx(w, tx, e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"folder": responseFolder, "path": p, "items": responseItems, "total": total, "nextCursor": cursor})
}

// sqlLower matches SQLite's lower(), which only folds ASCII; a page cursor has
// to compare the way the listing was ordered.
func sqlLower(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, value)
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(norm.NFC.String(raw))
	if name == "" {
		return "", apiError{400, "Name can’t be empty.", "NAME_EMPTY"}
	}
	if name == "." || name == ".." {
		return "", apiError{400, "That name is reserved.", "NAME_RESERVED"}
	}
	for _, r := range name {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return "", apiError{400, "Names can’t contain / or \\ or control characters.", "NAME_INVALID"}
		}
	}
	if !utf8.ValidString(name) || len([]byte(name)) > 255 {
		return "", apiError{400, "Name is too long.", "NAME_TOO_LONG"}
	}
	return name, nil
}
