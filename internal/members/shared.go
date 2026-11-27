// Package members shares nodes with other accounts on this server.
package members

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

type Principal struct{ UserID int64 }
type ResolvePrincipal func(*http.Request) (Principal, bool)
type Options struct {
	DB               *sql.DB
	ResolvePrincipal ResolvePrincipal
	// NodeViews are the API representations of the owner's nodes, by id.
	NodeViews func(ctx context.Context, ownerID int64, ids []string) (map[string]any, error)
}
type Handler struct {
	db         *sql.DB
	resolve    ResolvePrincipal
	views      func(context.Context, int64, []string) (map[string]any, error)
	addLimiter *ratelimit.Limiter
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.ResolvePrincipal == nil || o.NodeViews == nil {
		return nil, errors.New("members: database, principal resolver and node views are required")
	}
	h := &Handler{db: o.DB, resolve: o.ResolvePrincipal, views: o.NodeViews}
	h.addLimiter = ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute, Max: 30,
		Message: map[string]string{"error": "Too many attempts, try again later", "code": "RATE_LIMITED"},
		Key: func(r *http.Request) string {
			p, ok := o.ResolvePrincipal(r)
			if !ok || p.UserID < 1 {
				return "anonymous"
			}
			return "user:" + strconv.FormatInt(p.UserID, 10)
		},
	})
	return h, nil
}
func (h *Handler) Routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/shared-with-me", h.sharedWithMe)
	m.Handle("POST /api/nodes/{id}/members", h.addLimiter.Middleware(http.HandlerFunc(h.addMember)))
	m.HandleFunc("GET /api/nodes/{id}/members", h.listMembers)
	m.HandleFunc("PATCH /api/nodes/{id}/members/{shareId}", h.updateMember)
	m.HandleFunc("DELETE /api/nodes/{id}/members/{shareId}", h.removeMember)
	m.HandleFunc("DELETE /api/shared-with-me/{shareId}", h.leaveShare)
}

type apiError struct {
	status        int
	message, code string
}

func (e apiError) Error() string { return e.message }
func writeError(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}
func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return 0, false
	}
	return p.UserID, true
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var api apiError
	if errors.As(err, &api) {
		writeError(w, api.status, api.message, api.code)
		return
	}
	writeError(w, 500, "Internal server error", "INTERNAL_ERROR")
}

type grantBody struct {
	Recipient  any             `json:"recipient"`
	Permission json.RawMessage `json:"permission"`
	ExpiresAt  json.RawMessage `json:"expiresAt"`
}

func grantPermission(raw json.RawMessage, fallback string) (string, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	if string(raw) == "null" {
		return "read", nil
	}
	var permission string
	if err := json.Unmarshal(raw, &permission); err != nil {
		return "", apiError{400, "permission must be read or write.", "INVALID_PERMISSION"}
	}
	if permission == "" {
		permission = "read"
	}
	if permission != "read" && permission != "write" {
		return "", apiError{400, "permission must be read or write.", "INVALID_PERMISSION"}
	}
	return permission, nil
}

func decodeGrant(w http.ResponseWriter, r *http.Request) (grantBody, bool) {
	var body grantBody
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if d.Decode(&body) != nil {
		writeError(w, 400, "Invalid JSON request", "INVALID_JSON")
		return body, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		writeError(w, 400, "Invalid JSON request", "INVALID_JSON")
		return body, false
	}
	return body, true
}

// shareID is the numeric id of a share route.
func (h *Handler) shareID(w http.ResponseWriter, r *http.Request, label string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("shareId"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "Invalid "+label, "INVALID_ID")
		return 0, false
	}
	return id, true
}

func (h *Handler) liveNode(ctx context.Context, owner int64, id string) (node, error) {
	var n node
	var parent, mime, vault, trashed sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT id,parent_id,type,name,size,mime,etag,version,created_at,modified_at,vault_id,trashed_at FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL`, id, owner).Scan(&n.ID, &parent, &n.Type, &n.Name, &n.Size, &mime, &n.ETag, &n.Version, &n.CreatedAt, &n.ModifiedAt, &vault, &trashed)
	if errors.Is(err, sql.ErrNoRows) {
		return n, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	if err != nil {
		return n, err
	}
	n.ParentID = ptr(parent)
	n.MIME = ptr(mime)
	n.VaultID = ptr(vault)
	n.TrashedAt = ptr(trashed)
	return n, nil
}
func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.principal(w, r)
	if !ok {
		return
	}
	body, ok := decodeGrant(w, r)
	if !ok {
		return
	}
	n, err := h.liveNode(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.ParentID == nil {
		h.fail(w, apiError{400, "The root folder can’t be shared.", "ROOT_IMMUTABLE"})
		return
	}
	if n.VaultID != nil {
		h.fail(w, apiError{409, "Vault folders can’t be shared.", "VAULT_NOT_SHAREABLE"})
		return
	}
	recipient, ok := body.Recipient.(string)
	recipient = strings.TrimSpace(recipient)
	if !ok || recipient == "" || len([]rune(recipient)) > 254 {
		h.fail(w, apiError{400, "Enter the exact email address or username.", "INVALID_RECIPIENT"})
		return
	}
	permission, err := grantPermission(body.Permission, "read")
	if err != nil {
		h.fail(w, err)
		return
	}
	if permission == "write" && n.Type != "folder" {
		h.fail(w, apiError{400, "Only folders can be shared with write access.", "WRITE_NEEDS_FOLDER"})
		return
	}
	var expiryValue any
	if len(body.ExpiresAt) > 0 {
		if err = json.Unmarshal(body.ExpiresAt, &expiryValue); err != nil {
			h.fail(w, apiError{400, "expiresAt must be a date.", "INVALID_EXPIRY"})
			return
		}
	}
	expiry, err := parseGrantExpiry(expiryValue)
	if err != nil {
		h.fail(w, err)
		return
	}
	column := "username"
	if strings.Contains(recipient, "@") {
		column = "email"
	}
	var grantee int64
	err = h.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE lower(`+column+`)=lower(?)`, recipient).Scan(&grantee)
	if errors.Is(err, sql.ErrNoRows) || grantee == uid {
		h.fail(w, apiError{404, "No account can receive this share.", "RECIPIENT_NOT_FOUND"})
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient,expires_at) VALUES(?,?,?,?,?,?)`, uid, n.ID, grantee, permission, recipient, expiry)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			h.fail(w, apiError{409, "Already shared with this account.", "ALREADY_MEMBER"})
		} else {
			h.fail(w, err)
		}
		return
	}
	id, _ := res.LastInsertId()
	detail, _ := json.Marshal(map[string]int64{"granteeId": grantee})
	if n.VaultID == nil {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO activity(owner_id,actor_id,kind,node_id,parent_id,node_type,name,detail) VALUES(?,?,'share',?,?,?,?,?)`, uid, uid, n.ID, n.ParentID, n.Type, n.Name, string(detail)); err != nil {
			h.fail(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		h.fail(w, err)
		return
	}
	item, err := h.memberRow(r.Context(), uid, n.ID, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpStatus(w, 201, item)
}
func parseGrantExpiry(v any) (any, error) {
	if v == nil || v == "" {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, apiError{400, "expiresAt must be a date.", "INVALID_EXPIRY"}
	}
	t, err := parseNodeExpiryDate(s)
	if err != nil {
		return nil, apiError{400, "expiresAt must be a date.", "INVALID_EXPIRY"}
	}
	if !t.After(time.Now()) {
		return nil, apiError{400, "expiresAt must be in the future.", "INVALID_EXPIRY"}
	}
	return t.UTC().Format("2006-01-02 15:04:05"), nil
}

// parseNodeExpiryDate accepts the ISO date and date-time forms Node's
// Date.parse accepts for member grants. Date-only values are UTC; date-times
// without a zone use the server's local zone, matching JavaScript Date.parse.
func parseNodeExpiryDate(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), nil
	}
	localLayouts := []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range localLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid date")
}
func httpStatus(w http.ResponseWriter, status int, v any) { httpx.WriteJSON(w, status, v) }

type member struct {
	ID          int64   `json:"id"`
	NodeID      string  `json:"nodeId"`
	Recipient   string  `json:"recipient"`
	DisplayName *string `json:"displayName"`
	Permission  string  `json:"permission"`
	CreatedAt   string  `json:"createdAt"`
	ExpiresAt   *string `json:"expiresAt"`
	Expired     bool    `json:"expired"`
}

func (h *Handler) memberRow(ctx context.Context, owner int64, nodeID string, id int64) (member, error) {
	var m member
	var display sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT s.id,s.node_id,s.recipient,u.display_name,s.permission,s.created_at,s.expires_at,(s.expires_at IS NOT NULL AND datetime(s.expires_at)<=datetime('now')) FROM user_shares s JOIN users u ON u.id=s.grantee_id WHERE s.id=? AND s.owner_id=? AND s.node_id=?`, id, owner, nodeID).Scan(&m.ID, &m.NodeID, &m.Recipient, &display, &m.Permission, &m.CreatedAt, &m.ExpiresAt, &m.Expired)
	if display.Valid && display.String != "" {
		m.DisplayName = &display.String
	}
	return m, err
}
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.principal(w, r)
	if !ok {
		return
	}
	n, err := h.liveNode(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT s.id FROM user_shares s WHERE s.owner_id=? AND s.node_id=? ORDER BY s.id`, uid, n.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			h.fail(w, err)
			return
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		h.fail(w, err)
		return
	}
	if err = rows.Close(); err != nil {
		h.fail(w, err)
		return
	}
	out := make([]member, 0, len(ids))
	for _, id := range ids {
		m, e := h.memberRow(r.Context(), uid, n.ID, id)
		if e != nil {
			h.fail(w, e)
			return
		}
		out = append(out, m)
	}
	httpStatus(w, 200, map[string]any{"members": out})
}
func (h *Handler) updateMember(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.principal(w, r)
	if !ok {
		return
	}
	body, ok := decodeGrant(w, r)
	if !ok {
		return
	}
	n, err := h.liveNode(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	id, ok := h.shareID(w, r, "member id")
	if !ok {
		return
	}
	var old member
	var grantee int64
	err = h.db.QueryRowContext(r.Context(), `SELECT s.permission,s.expires_at,s.grantee_id FROM user_shares s WHERE s.id=? AND s.owner_id=? AND s.node_id=?`, id, uid, n.ID).Scan(&old.Permission, &old.ExpiresAt, &grantee)
	if errors.Is(err, sql.ErrNoRows) {
		h.fail(w, apiError{404, "Member not found", "MEMBER_NOT_FOUND"})
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	permission, err := grantPermission(body.Permission, old.Permission)
	if err != nil {
		h.fail(w, err)
		return
	}
	if permission == "write" && n.Type != "folder" {
		h.fail(w, apiError{400, "Only folders can be shared with write access.", "WRITE_NEEDS_FOLDER"})
		return
	}
	expiry := any(nil)
	if old.ExpiresAt != nil {
		expiry = *old.ExpiresAt
	}
	if len(body.ExpiresAt) > 0 {
		var expiryValue any
		if err = json.Unmarshal(body.ExpiresAt, &expiryValue); err != nil {
			h.fail(w, apiError{400, "expiresAt must be a date.", "INVALID_EXPIRY"})
			return
		}
		expiry, err = parseGrantExpiry(expiryValue)
		if err != nil {
			h.fail(w, err)
			return
		}
	}
	_, err = h.db.ExecContext(r.Context(), `UPDATE user_shares SET permission=?,expires_at=? WHERE id=?`, permission, expiry, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	m, err := h.memberRow(r.Context(), uid, n.ID, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpStatus(w, 200, m)
}
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.principal(w, r)
	if !ok {
		return
	}
	n, err := h.liveNode(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	id, ok := h.shareID(w, r, "member id")
	if !ok {
		return
	}
	var deleted int64
	err = h.db.QueryRowContext(r.Context(), `DELETE FROM user_shares WHERE id=? AND owner_id=? AND node_id=? RETURNING id`, id, uid, n.ID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		h.fail(w, apiError{404, "Member not found", "MEMBER_NOT_FOUND"})
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpStatus(w, 200, map[string]bool{"success": true})
}
func (h *Handler) leaveShare(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, ok := h.shareID(w, r, "share id")
	if !ok {
		return
	}
	var deleted int64
	err := h.db.QueryRowContext(r.Context(), `DELETE FROM user_shares WHERE id=? AND grantee_id=? RETURNING id`, id, uid).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		h.fail(w, apiError{404, "Member not found", "MEMBER_NOT_FOUND"})
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpStatus(w, 200, map[string]bool{"success": true})
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
type share struct {
	ID         int64          `json:"id"`
	Permission string         `json:"permission"`
	OwnerName  string         `json:"ownerName"`
	SharedAt   string         `json:"sharedAt"`
	ExpiresAt  *string        `json:"expiresAt"`
	Node       map[string]any `json:"node"`
}

func (h *Handler) sharedWithMe(w http.ResponseWriter, r *http.Request) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		httpx.WriteJSON(w, 401, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return
	}
	items, err := h.list(r.Context(), p.UserID)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Internal server error", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"shares": items})
}

func (h *Handler) list(ctx context.Context, userID int64) ([]share, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT s.id,s.owner_id,s.node_id,s.permission,s.created_at,s.expires_at,
	 COALESCE(NULLIF(u.display_name,''),u.username)
	 FROM user_shares s JOIN users u ON u.id=s.owner_id
	 WHERE s.grantee_id=? AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now'))
	 ORDER BY s.created_at DESC,s.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		id, owner                   int64
		nodeID, permission, created string
		expiry                      sql.NullString
		ownerName                   string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.owner, &c.nodeID, &c.permission, &c.created, &c.expiry, &c.ownerName); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	out := make([]share, 0, len(candidates))
	for _, c := range candidates {
		var active int
		err = h.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id,depth) AS (
		 SELECT id,parent_id,trashed_at,vault_id,0 FROM nodes WHERE id=? AND user_id=?
		 UNION ALL SELECT n.id,n.parent_id,n.trashed_at,n.vault_id,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
		) SELECT EXISTS(SELECT 1 FROM users o WHERE o.id=? AND o.disabled_at IS NULL
		 AND NOT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL)
		 AND EXISTS(SELECT 1 FROM up JOIN user_shares g ON g.node_id=up.id WHERE g.owner_id=? AND g.grantee_id=? AND (g.expires_at IS NULL OR datetime(g.expires_at)>datetime('now'))))`, c.nodeID, c.owner, c.owner, c.owner, c.owner, userID).Scan(&active)
		if err != nil {
			return nil, err
		}
		if active == 0 {
			continue
		}
		view, err := h.memberView(ctx, c.owner, c.nodeID)
		if err != nil {
			return nil, err
		}
		if view == nil {
			continue
		}
		item := share{ID: c.id, Permission: c.permission, OwnerName: c.ownerName, SharedAt: c.created, Node: view}
		if c.expiry.Valid {
			v := c.expiry.String
			item.ExpiresAt = &v
		}
		out = append(out, item)
	}
	return out, nil
}

// memberView is the shared root as a member sees it: the owner's favorites,
// sharing and AI tags stay the owner's, and so does the folder above it.
func (h *Handler) memberView(ctx context.Context, ownerID int64, nodeID string) (map[string]any, error) {
	views, err := h.views(ctx, ownerID, []string{nodeID})
	if err != nil || views[nodeID] == nil {
		return nil, err
	}
	encoded, err := json.Marshal(views[nodeID])
	if err != nil {
		return nil, err
	}
	var view map[string]any
	if err := json.Unmarshal(encoded, &view); err != nil {
		return nil, err
	}
	delete(view, "shared")
	delete(view, "aiTags")
	delete(view, "aiMeta")
	view["favorite"] = false
	view["parentId"] = nil
	return view, nil
}

func ptr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
