// Package sync implements the account's initial tree snapshot and change feed.
package sync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type Principal struct {
	UserID int64
}
type ResolvePrincipal func(*http.Request) (Principal, bool)
type Options struct {
	DB                 *sql.DB
	ResolvePrincipal   ResolvePrincipal
	WatchPollInterval  time.Duration
	WatchShareInterval time.Duration
	WatchMaxDuration   time.Duration
}
type Handler struct {
	db                 *sql.DB
	resolve            ResolvePrincipal
	watchPollInterval  time.Duration
	watchShareInterval time.Duration
	watchMaxDuration   time.Duration
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("sync: database and principal resolver are required")
	}
	pollInterval := options.WatchPollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	shareInterval := options.WatchShareInterval
	if shareInterval <= 0 {
		shareInterval = 5 * time.Second
	}
	maxDuration := options.WatchMaxDuration
	if maxDuration <= 0 {
		maxDuration = 5 * time.Minute
	}
	return &Handler{db: options.DB, resolve: options.ResolvePrincipal, watchPollInterval: pollInterval, watchShareInterval: shareInterval, watchMaxDuration: maxDuration}, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sync/tree", h.tree)
	mux.HandleFunc("GET /api/sync/changes", h.changes)
	mux.HandleFunc("GET /api/sync/watch", h.watch)
}

// watch streams durable change-journal cursor hints. The journal remains the
// source of truth; clients fetch changes through /api/sync/changes. Polling the
// shared database makes this work across server instances without local pub/sub.
func (h *Handler) watch(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is unavailable", http.StatusInternalServerError)
		return
	}
	cursorText := r.URL.Query().Get("cursor")
	if cursorText == "" {
		cursorText = r.Header.Get("Last-Event-ID")
	}
	if cursorText == "" {
		cursorText = "latest"
	}
	cursor := cursorValue(cursorText)
	if cursorText == "latest" {
		var err error
		cursor, err = h.currentCursor(r, userID)
		if err != nil {
			writeInternal(w)
			return
		}
	}
	feed, err := follow(r.Context(), h.db, userID)
	if err != nil {
		writeInternal(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	deadline := time.NewTimer(h.watchMaxDuration)
	defer deadline.Stop()
	poll := time.NewTicker(h.watchPollInterval)
	defer poll.Stop()
	sharePoll := time.NewTicker(h.watchShareInterval)
	defer sharePoll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	if err := writeWatchEvent(w, "ready", cursor); err != nil {
		return
	}
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			_, _ = fmt.Fprint(w, "event: reconnect\ndata: {}\n\n")
			flusher.Flush()
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
			latest, err := h.currentCursor(r, userID)
			if err != nil {
				return
			}
			if latest > cursor {
				cursor = latest
				if err := writeWatchEvent(w, "sync:changed", cursor); err != nil {
					return
				}
				flusher.Flush()
			}
		case <-sharePoll.C:
			changed, err := feed.SharesChanged(r.Context())
			if err != nil {
				return
			}
			if changed {
				if err := writeWatchInvalidation(w, "shares:changed", cursor); err != nil {
					return
				}
				flusher.Flush()
			}
			ids, err := feed.sharedFolders(r.Context())
			if err != nil {
				return
			}
			if len(ids) > 0 {
				if err := writeWatchFoldersEvent(w, ids); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func (h *Handler) currentCursor(r *http.Request, userID int64) (int64, error) {
	var cursor int64
	err := h.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, userID).Scan(&cursor)
	return cursor, err
}

func writeWatchEvent(w http.ResponseWriter, name string, cursor int64) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: {\"cursor\":%d}\n\n", cursor, name, cursor)
	return err
}

func writeWatchInvalidation(w http.ResponseWriter, name string, cursor int64) error {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: {}\n\n", cursor, name)
	return err
}

func writeWatchFoldersEvent(w http.ResponseWriter, folderIDs []string) error {
	encoded, err := json.Marshal(map[string][]string{"folderIds": folderIDs})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: files:changed\ndata: %s\n\n", encoded)
	return err
}

type node struct {
	ID         string  `json:"id"`
	ParentID   *string `json:"parentId"`
	Type       string  `json:"type"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	MIME       *string `json:"mime"`
	ETag       string  `json:"etag"`
	Version    int64   `json:"version"`
	CreatedAt  string  `json:"createdAt"`
	ModifiedAt string  `json:"modifiedAt"`
	VaultID    *string `json:"vaultId"`
	TrashedAt  *string `json:"trashedAt,omitempty"`
}
type treeNode struct {
	node
	Path string `json:"path"`
}
type change struct {
	Seq    int64  `json:"seq"`
	Op     string `json:"op"`
	NodeID string `json:"nodeId"`
	At     string `json:"at"`
	Node   *node  `json:"node"`
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return 0, false
	}
	return p.UserID, true
}

func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: false})
	if err != nil {
		writeInternal(w)
		return
	}
	defer tx.Rollback()
	root, err := ensureRoot(r.Context(), tx, userID)
	if err != nil {
		writeInternal(w)
		return
	}
	var cursor int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, userID).Scan(&cursor); err != nil {
		writeInternal(w)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `WITH RECURSIVE down(id,path,depth) AS (
		SELECT id,'',0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL
		SELECT n.id,CASE WHEN down.depth=0 THEN n.name ELSE down.path||'/'||n.name END,down.depth+1
		FROM nodes n JOIN down ON n.parent_id=down.id
		WHERE n.user_id=? AND n.trashed_at IS NULL
	) SELECT n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.etag,n.version,n.created_at,n.modified_at,n.vault_id,n.trashed_at,down.path
	FROM down JOIN nodes n ON n.id=down.id AND n.user_id=? ORDER BY down.depth,lower(n.name)`, root, userID, userID, userID)
	if err != nil {
		writeInternal(w)
		return
	}
	nodes, err := scanTreeRows(rows)
	rows.Close()
	if err != nil {
		writeInternal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		writeInternal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"cursor": cursor, "rootId": root, "nodes": nodes})
}

func ensureRoot(ctx context.Context, tx *sql.Tx, userID int64) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, userID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var raw [16]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	id = encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
	if _, err = tx.ExecContext(ctx, `INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','',?)`, id, userID, id); err != nil {
		return "", err
	}
	return id, nil
}

func (h *Handler) changes(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if q.Get("cursor") == "latest" {
		var cursor int64
		if err := h.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, userID).Scan(&cursor); err != nil {
			writeInternal(w)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"changes": []change{}, "cursor": cursor, "hasMore": false})
		return
	}
	after := cursorValue(q.Get("cursor"))
	limit := pageLimit(q.Get("limit"))
	rows, err := h.db.QueryContext(r.Context(), `SELECT c.seq,c.op,c.node_id,c.at,n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.etag,n.version,n.created_at,n.modified_at,n.vault_id,n.trashed_at
		FROM changes c LEFT JOIN nodes n ON n.id=c.node_id AND n.user_id=c.user_id
		WHERE c.user_id=? AND c.seq>? ORDER BY c.seq LIMIT ?`, userID, after, limit+1)
	if err != nil {
		writeInternal(w)
		return
	}
	page := []change{}
	for rows.Next() {
		var entry change
		var present sql.NullString
		var parent, typ, name, mime, etag, created, modified, vault, trashed sql.NullString
		var size, version sql.NullInt64
		if err = rows.Scan(&entry.Seq, &entry.Op, &entry.NodeID, &entry.At, &present, &parent, &typ, &name, &size, &mime, &etag, &version, &created, &modified, &vault, &trashed); err != nil {
			rows.Close()
			writeInternal(w)
			return
		}
		if present.Valid {
			current := node{Type: typ.String, Name: name.String, Size: size.Int64, ETag: etag.String, Version: version.Int64, CreatedAt: created.String, ModifiedAt: modified.String}
			current.ID = present.String
			current.ParentID = nullableString(parent)
			current.MIME = nullableString(mime)
			current.VaultID = nullableString(vault)
			current.TrashedAt = nullableString(trashed)
			entry.Node = &current
		}
		page = append(page, entry)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeInternal(w)
		return
	}
	rows.Close()
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	cursor := after
	if len(page) > 0 {
		cursor = page[len(page)-1].Seq
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"changes": page, "cursor": cursor, "hasMore": hasMore})
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}
func cursorValue(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0
	}
	if f >= 9007199254740991 {
		return 9007199254740991
	}
	return int64(math.Trunc(f))
}
func pageLimit(value string) int {
	limit := 1000
	if value != "" {
		// Match Node's Number(limit) semantics before truncation. In
		// particular, scientific notation is a valid numeric query value.
		if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && !math.IsNaN(n) {
			if math.IsInf(n, 1) {
				limit = 5000
			} else if math.IsInf(n, -1) {
				limit = 1
			} else if n != 0 {
				if n >= 5000 {
					limit = 5000
				} else if n <= 1 {
					limit = 1
				} else {
					limit = int(math.Trunc(n))
				}
			}
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 5000 {
		limit = 5000
	}
	return limit
}
func writeInternal(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error", "code": "INTERNAL_ERROR"})
}

func scanTreeRows(rows *sql.Rows) ([]treeNode, error) {
	out := []treeNode{}
	for rows.Next() {
		var n treeNode
		var parent, mime, vault, trashed sql.NullString
		if err := rows.Scan(&n.ID, &parent, &n.Type, &n.Name, &n.Size, &mime, &n.ETag, &n.Version, &n.CreatedAt, &n.ModifiedAt, &vault, &trashed, &n.Path); err != nil {
			return nil, err
		}
		n.ParentID, n.MIME, n.VaultID, n.TrashedAt = nullableString(parent), nullableString(mime), nullableString(vault), nullableString(trashed)
		out = append(out, n)
	}
	return out, rows.Err()
}
