// Package comments serves comments on files and folders. Everyone who can reach
// a node reads and writes them; share links never do. Bodies are stored sealed.
package comments

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neocloud/internal/activity"
	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const failureMessage = "Something went wrong. Please try again."

const (
	maxLength    = 2000
	defaultLimit = 50
	maxLimit     = 100
)

type Principal struct{ UserID int64 }
type ResolvePrincipal func(*http.Request) (Principal, bool)
type Options struct {
	DB               *sql.DB
	Keys             *cryptox.Keyring
	ResolvePrincipal ResolvePrincipal
	// Notify receives comments:changed for each account that can see the node.
	Notify func(userID int64, event string, payload map[string]any)
}
type Handler struct {
	db      *sql.DB
	keys    *cryptox.Keyring
	resolve ResolvePrincipal
	notify  func(int64, string, map[string]any)
	limiter *ratelimit.Limiter
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Keys == nil || o.ResolvePrincipal == nil {
		return nil, errors.New("comments: database, keyring, and principal resolver are required")
	}
	h := &Handler{db: o.DB, keys: o.Keys, resolve: o.ResolvePrincipal, notify: o.Notify}
	h.limiter = ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute, Max: 120,
		Message: map[string]string{"error": "Too many comments, try again later", "code": "RATE_LIMITED"},
		Key: func(r *http.Request) string {
			p, _ := o.ResolvePrincipal(r)
			return "user:" + strconv.FormatInt(p.UserID, 10)
		},
	})
	return h, nil
}

func (h *Handler) Routes(m *http.ServeMux) {
	m.Handle("GET /api/files/{id}/comments", h.authenticated(h.list, false))
	m.Handle("POST /api/files/{id}/comments", h.authenticated(h.add, true))
	m.Handle("PATCH /api/files/{id}/comments/{commentId}", h.authenticated(h.edit, true))
	m.Handle("DELETE /api/files/{id}/comments/{commentId}", h.authenticated(h.remove, true))
}

type action func(http.ResponseWriter, *http.Request, int64) error

// authenticated refuses anonymous callers before they reach the per-account
// write limit.
func (h *Handler) authenticated(run action, write bool) http.Handler {
	var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := h.resolve(r)
		if err := run(w, r, p.UserID); err != nil {
			apijson.Fail(w, err, failureMessage)
		}
	})
	if write {
		next = h.limiter.Middleware(next)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := h.resolve(r); !ok || p.UserID < 1 {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type node struct {
	id    string
	owner int64
}

func nodeNotFound() error {
	return apperr.New(http.StatusNotFound, "File or folder not found", "NOT_FOUND")
}

// target is a node the caller may comment on: their own or one shared with
// them, live and outside any vault, never a root. Everything else looks missing.
func (h *Handler) target(ctx context.Context, viewer int64, id string) (node, error) {
	n := node{id: id}
	var parent sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT user_id,parent_id FROM nodes WHERE id=?`, id).Scan(&n.owner, &parent)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !parent.Valid {
		return n, nodeNotFound()
	}
	if err != nil {
		return n, err
	}
	var hidden, granted bool
	err = h.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id) AS (
		SELECT id,parent_id,trashed_at,vault_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id,p.trashed_at,p.vault_id FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL),
		EXISTS(SELECT 1 FROM user_shares s JOIN up ON up.id=s.node_id JOIN users o ON o.id=s.owner_id
			WHERE s.owner_id=? AND s.grantee_id=? AND o.disabled_at IS NULL AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now')))`,
		id, n.owner, n.owner, n.owner, viewer).Scan(&hidden, &granted)
	if err != nil {
		return n, err
	}
	if hidden || n.owner != viewer && !granted {
		return n, nodeNotFound()
	}
	return n, nil
}

// tell notifies the owner and every account with a live grant on the node or a
// folder above it.
func (h *Handler) tell(ctx context.Context, n node) {
	if h.notify == nil {
		return
	}
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE up(id,parent_id) AS (
		SELECT id,parent_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT DISTINCT grantee_id FROM user_shares WHERE owner_id=? AND node_id IN (SELECT id FROM up) AND (expires_at IS NULL OR datetime(expires_at)>datetime('now'))`,
		n.id, n.owner, n.owner, n.owner)
	if err != nil {
		return
	}
	accounts := []int64{n.owner}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			accounts = append(accounts, id)
		}
	}
	rows.Close()
	for _, id := range accounts {
		h.notify(id, "comments:changed", map[string]any{"nodeId": n.id})
	}
}

type comment struct {
	ID        int64   `json:"id"`
	Author    string  `json:"author"`
	Mine      bool    `json:"mine"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"createdAt"`
	EditedAt  *string `json:"editedAt"`
	CanDelete bool    `json:"canDelete"`
}

const selectComment = `SELECT c.id,c.user_id,COALESCE(NULLIF(u.display_name,''),u.username),c.body,c.created_at,c.edited_at FROM node_comments c JOIN users u ON u.id=c.user_id`

type scanner interface{ Scan(...any) error }

func (h *Handler) scan(row scanner, viewer, owner int64) (comment, error) {
	var c comment
	var author int64
	var sealed, created string
	var edited sql.NullString
	if err := row.Scan(&c.ID, &author, &c.Author, &sealed, &created, &edited); err != nil {
		return c, err
	}
	body, err := h.keys.DecryptValue(sealed)
	if err != nil {
		return c, err
	}
	c.Body = body
	c.Mine = author == viewer
	c.CanDelete = c.Mine || owner == viewer
	c.CreatedAt = isoTime(created)
	if edited.Valid {
		value := isoTime(edited.String)
		c.EditedAt = &value
	}
	return c, nil
}

func isoTime(value string) string { return strings.Replace(value, " ", "T", 1) + "Z" }

func (h *Handler) fetch(ctx context.Context, id int64, n node, viewer int64) (comment, error) {
	return h.scan(h.db.QueryRowContext(ctx, selectComment+` WHERE c.id=? AND c.node_id=?`, id, n.id), viewer, n.owner)
}

func positive(raw string) int64 {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || value < 1 {
		return 0
	}
	return int64(math.Min(value, 1<<53-1))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, viewer int64) error {
	n, err := h.target(r.Context(), viewer, r.PathValue("id"))
	if err != nil {
		return err
	}
	query := r.URL.Query()
	limit := defaultLimit
	if value := positive(query.Get("limit")); value > 0 {
		limit = int(min(value, maxLimit))
	}
	before := int64(1<<53 - 1)
	if value := positive(query.Get("cursor")); value > 0 {
		before = value
	}
	rows, err := h.db.QueryContext(r.Context(), selectComment+` WHERE c.node_id=? AND c.id<? ORDER BY c.id DESC LIMIT ?`, n.id, before, limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []comment{}
	for rows.Next() {
		item, err := h.scan(rows, viewer, n.owner)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	var next any
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"comments": items, "nextCursor": next})
	return nil
}

func requestBody(w http.ResponseWriter, r *http.Request) (string, error) {
	object, err := apijson.Object(w, r)
	if err != nil {
		return "", err
	}
	text, ok := object["body"].(string)
	if !ok {
		return "", apperr.New(http.StatusBadRequest, "Write a comment first.", "COMMENT_EMPTY")
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", apperr.New(http.StatusBadRequest, "Write a comment first.", "COMMENT_EMPTY")
	}
	if len(utf16.Encode([]rune(text))) > maxLength {
		return "", apperr.New(http.StatusBadRequest, "A comment can be up to 2000 characters.", "COMMENT_TOO_LONG")
	}
	for _, c := range text {
		if c < 32 && c != '\t' && c != '\n' && c != '\r' || c == 127 {
			return "", apperr.New(http.StatusBadRequest, "That comment has characters that can’t be saved.", "COMMENT_INVALID")
		}
	}
	return text, nil
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request, viewer int64) error {
	body, err := requestBody(w, r)
	if err != nil {
		return err
	}
	n, err := h.target(r.Context(), viewer, r.PathValue("id"))
	if err != nil {
		return err
	}
	sealed, err := h.keys.EncryptValue(body)
	if err != nil {
		return err
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO node_comments(node_id,user_id,body) VALUES(?,?,?)`, n.id, viewer, sealed)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if err = activity.Record(r.Context(), tx, activity.Event{OwnerID: n.owner, ActorID: viewer, Kind: "comment", NodeID: n.id}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	created, err := h.fetch(r.Context(), id, n, viewer)
	if err != nil {
		return err
	}
	h.tell(r.Context(), n)
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

func commentID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("commentId"), 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(http.StatusBadRequest, "Invalid comment id", "INVALID_ID")
	}
	return id, nil
}

func (h *Handler) author(ctx context.Context, id int64, n node) (int64, error) {
	var author int64
	err := h.db.QueryRowContext(ctx, `SELECT user_id FROM node_comments WHERE id=? AND node_id=?`, id, n.id).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.New(http.StatusNotFound, "Comment not found", "COMMENT_NOT_FOUND")
	}
	return author, err
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request, viewer int64) error {
	id, err := commentID(r)
	if err != nil {
		return err
	}
	body, err := requestBody(w, r)
	if err != nil {
		return err
	}
	n, err := h.target(r.Context(), viewer, r.PathValue("id"))
	if err != nil {
		return err
	}
	author, err := h.author(r.Context(), id, n)
	if err != nil {
		return err
	}
	if author != viewer {
		return apperr.New(http.StatusForbidden, "You can only edit your own comments.", "COMMENT_NOT_YOURS")
	}
	sealed, err := h.keys.EncryptValue(body)
	if err != nil {
		return err
	}
	if _, err = h.db.ExecContext(r.Context(), `UPDATE node_comments SET body=?,edited_at=datetime('now') WHERE id=?`, sealed, id); err != nil {
		return err
	}
	updated, err := h.fetch(r.Context(), id, n, viewer)
	if err != nil {
		return err
	}
	h.tell(r.Context(), n)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, viewer int64) error {
	id, err := commentID(r)
	if err != nil {
		return err
	}
	n, err := h.target(r.Context(), viewer, r.PathValue("id"))
	if err != nil {
		return err
	}
	author, err := h.author(r.Context(), id, n)
	if err != nil {
		return err
	}
	if author != viewer && n.owner != viewer {
		return apperr.New(http.StatusForbidden, "You can only delete your own comments.", "COMMENT_NOT_YOURS")
	}
	if _, err = h.db.ExecContext(r.Context(), `DELETE FROM node_comments WHERE id=?`, id); err != nil {
		return err
	}
	h.tell(r.Context(), n)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	return nil
}
