// Package activity implements the account activity feed.
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const (
	defaultLimit = 30
	maxLimit     = 100
	scanBatch    = 200
	maxScanned   = 1000
)

type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

type Options struct {
	DB               *sql.DB
	ResolvePrincipal ResolvePrincipal
}

type Handler struct {
	db      *sql.DB
	resolve ResolvePrincipal
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("activity: database and principal resolver are required")
	}
	return &Handler{db: options.DB, resolve: options.ResolvePrincipal}, nil
}

func (h *Handler) Routes(mux *http.ServeMux) { mux.HandleFunc("GET /api/activity", h.list) }

type activityRow struct {
	ID       int64
	OwnerID  int64
	ActorID  int64
	Kind     string
	NodeID   string
	ParentID sql.NullString
	NodeType string
	Name     string
	Detail   sql.NullString
	Count    int64
	At       string
}

type detail struct {
	Names     []string `json:"names"`
	From      string   `json:"from"`
	GranteeID int64    `json:"granteeId"`
}

type person struct {
	Name string `json:"name"`
	Self bool   `json:"self"`
}

type nodeSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type memberSummary struct {
	Name string `json:"name"`
	Self bool   `json:"self"`
}

type item struct {
	ID     int64          `json:"id"`
	Kind   string         `json:"kind"`
	At     string         `json:"at"`
	Count  int64          `json:"count"`
	Actor  person         `json:"actor"`
	Owner  person         `json:"owner"`
	Node   nodeSummary    `json:"node"`
	Folder string         `json:"folder,omitempty"`
	Names  []string       `json:"names,omitempty"`
	From   string         `json:"from,omitempty"`
	Member *memberSummary `json:"member,omitempty"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return
	}
	if p.Device && p.Platform != "android" && p.Platform != "ios" {
		httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "Use a browser or the NeoCloud phone app for this.", "code": "PHONE_REQUIRED"})
		return
	}
	q := r.URL.Query()
	scope := queryValue(q, "scope", "all")
	if scope != "all" && scope != "mine" && scope != "shared" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown scope.", "code": "INVALID_SCOPE"})
		return
	}
	limit := parseLimit(queryValue(q, "limit", "30"))
	var focus *focusNode
	if raw := queryValue(q, "nodeId", ""); raw != "" {
		var found focusNode
		if err := h.resolveFocus(r.Context(), p.UserID, raw, &found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "File or folder not found", "code": "NOT_FOUND"})
			} else {
				httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error", "code": "INTERNAL_ERROR"})
			}
			return
		}
		focus = &found
	}
	result, err := h.listActivity(r.Context(), p.UserID, scope, focus, queryValue(q, "cursor", ""), limit)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type focusNode struct {
	ownerID int64
	nodeID  string
}

func queryValue(q url.Values, key, fallback string) string {
	values, ok := q[key]
	if !ok || len(values) == 0 {
		return fallback
	}
	return values[0]
}

func parseLimit(value string) int {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return defaultLimit
	}
	n = math.Trunc(n)
	if n == 0 {
		return defaultLimit
	}
	if n < 1 {
		n = 1
	}
	if n > maxLimit {
		n = maxLimit
	}
	return int(n)
}

func (h *Handler) resolveFocus(ctx context.Context, userID int64, nodeID string, focus *focusNode) error {
	// Own nodes and nodes granted through any live ancestor are the only visible
	// focus roots. Other accounts' identifiers are deliberately hidden as 404.
	err := h.db.QueryRowContext(ctx, `SELECT n.user_id FROM nodes n WHERE n.id=? AND n.vault_id IS NULL`, nodeID).Scan(&focus.ownerID)
	if err != nil {
		return err
	}
	focus.nodeID = nodeID
	if focus.ownerID == userID {
		return nil
	}
	var granted int
	return h.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id) AS (
		SELECT id,parent_id,trashed_at,vault_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id,n.trashed_at,n.vault_id FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT 1 FROM users u WHERE u.id=? AND u.disabled_at IS NULL
	AND NOT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL)
	AND EXISTS(SELECT 1 FROM user_shares s JOIN up ON up.id=s.node_id WHERE s.owner_id=? AND s.grantee_id=? AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now'))) LIMIT 1`, nodeID, focus.ownerID, focus.ownerID, focus.ownerID, focus.ownerID, userID).Scan(&granted)
}

func (h *Handler) listActivity(ctx context.Context, viewer int64, scope string, focus *focusNode, cursor string, limit int) (map[string]any, error) {
	owners := []int64{}
	if focus != nil {
		owners = append(owners, focus.ownerID)
	} else {
		if scope != "shared" {
			owners = append(owners, viewer)
		}
		if scope != "mine" {
			rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT owner_id FROM user_shares WHERE grantee_id=? AND (expires_at IS NULL OR datetime(expires_at)>datetime('now'))`, viewer)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var id int64
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				owners = append(owners, id)
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
		}
	}
	ids, err := json.Marshal(owners)
	if err != nil {
		return nil, err
	}
	before := int64(9007199254740991)
	if n, err := strconv.ParseFloat(cursor, 64); err == nil && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
		before = int64(math.Min(math.Trunc(n), 9007199254740991))
	}
	var focusID any
	if focus != nil {
		focusID = focus.nodeID
	}
	page := []activityRow{}
	scanned := 0
	exhausted := false
	for len(page) <= limit && scanned < maxScanned {
		rows, err := h.db.QueryContext(ctx, `SELECT a.id,a.owner_id,a.actor_id,a.kind,a.node_id,a.parent_id,a.node_type,a.name,a.detail,a.count,a.at FROM activity a
			WHERE a.owner_id IN (SELECT value FROM json_each(?)) AND a.id<?
			AND (? IS NULL OR a.node_id=? OR a.parent_id=?)
			AND NOT EXISTS(SELECT 1 FROM nodes n WHERE n.id=a.node_id AND n.vault_id IS NOT NULL)
			ORDER BY a.id DESC LIMIT ?`, string(ids), before, focusID, focusID, focusID, scanBatch)
		if err != nil {
			return nil, err
		}
		batch := []activityRow{}
		for rows.Next() {
			var row activityRow
			if err = rows.Scan(&row.ID, &row.OwnerID, &row.ActorID, &row.Kind, &row.NodeID, &row.ParentID, &row.NodeType, &row.Name, &row.Detail, &row.Count, &row.At); err != nil {
				rows.Close()
				return nil, err
			}
			batch = append(batch, row)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		exhausted = len(batch) < scanBatch
		for _, row := range batch {
			scanned++
			before = row.ID
			visible, err := h.visibleToMember(ctx, viewer, row)
			if err != nil {
				return nil, err
			}
			if visible {
				page = append(page, row)
			}
			if len(page) > limit {
				break
			}
		}
		if exhausted || len(page) > limit || len(batch) == 0 {
			break
		}
	}
	more := len(page) > limit
	shown := page
	if more {
		shown = page[:limit]
	}
	nextCursor := any(nil)
	resumeAfter := before
	if more && len(shown) > 0 {
		resumeAfter = shown[len(shown)-1].ID
	}
	if more || !exhausted {
		nextCursor = resumeAfter
	}
	items, err := h.serialize(ctx, viewer, shown)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "nextCursor": nextCursor}, nil
}

func parseDetail(raw sql.NullString) detail {
	var d detail
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &d)
	}
	return d
}

func (h *Handler) visibleToMember(ctx context.Context, viewer int64, row activityRow) (bool, error) {
	if row.OwnerID == viewer {
		return true, nil
	}
	d := parseDetail(row.Detail)
	if row.Kind == "share" && d.GranteeID != viewer {
		return false, nil
	}
	checkID := row.NodeID
	var trashed sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT trashed_at FROM nodes WHERE id=?`, row.NodeID).Scan(&trashed)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == sql.ErrNoRows || trashed.Valid {
		if !row.ParentID.Valid {
			return false, nil
		}
		checkID = row.ParentID.String
	}
	return h.sharedAccess(ctx, viewer, row.OwnerID, checkID)
}

func (h *Handler) sharedAccess(ctx context.Context, viewer, owner int64, nodeID string) (bool, error) {
	var exists int
	err := h.db.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id) AS (
		SELECT id,parent_id,trashed_at,vault_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id,n.trashed_at,n.vault_id FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.disabled_at IS NULL
	AND NOT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL)
	AND EXISTS(SELECT 1 FROM user_shares s JOIN up ON up.id=s.node_id WHERE s.owner_id=? AND s.grantee_id=? AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now'))))`, nodeID, owner, owner, owner, owner, viewer).Scan(&exists)
	return exists == 1, err
}

func (h *Handler) serialize(ctx context.Context, viewer int64, rows []activityRow) ([]item, error) {
	peopleIDs := map[int64]struct{}{}
	for _, row := range rows {
		d := parseDetail(row.Detail)
		peopleIDs[row.ActorID] = struct{}{}
		peopleIDs[row.OwnerID] = struct{}{}
		if d.GranteeID > 0 {
			peopleIDs[d.GranteeID] = struct{}{}
		}
	}
	people := map[int64]string{}
	if len(peopleIDs) > 0 {
		ids := make([]int64, 0, len(peopleIDs))
		for id := range peopleIDs {
			ids = append(ids, id)
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		pRows, err := h.db.QueryContext(ctx, `SELECT id,COALESCE(NULLIF(display_name,''),username) FROM users WHERE id IN(SELECT value FROM json_each(?))`, string(encoded))
		if err != nil {
			return nil, err
		}
		for pRows.Next() {
			var id int64
			var name string
			if err = pRows.Scan(&id, &name); err != nil {
				pRows.Close()
				return nil, err
			}
			people[id] = name
		}
		if err = pRows.Err(); err != nil {
			pRows.Close()
			return nil, err
		}
		pRows.Close()
	}
	out := make([]item, 0, len(rows))
	for _, row := range rows {
		d := parseDetail(row.Detail)
		at := strings.Replace(row.At, " ", "T", 1) + "Z"
		entry := item{ID: row.ID, Kind: row.Kind, At: at, Count: row.Count, Actor: person{Name: people[row.ActorID], Self: row.ActorID == viewer}, Owner: person{Name: people[row.OwnerID], Self: row.OwnerID == viewer}, Node: nodeSummary{ID: row.NodeID, Name: row.Name, Type: row.NodeType}}
		if row.ParentID.Valid && row.Kind != "share" {
			allowed := row.OwnerID == viewer
			if !allowed {
				var err error
				allowed, err = h.sharedAccess(ctx, viewer, row.OwnerID, row.ParentID.String)
				if err != nil {
					return nil, err
				}
			}
			if allowed {
				// The top of an account has no name.
				var folder string
				err := h.db.QueryRowContext(ctx, `SELECT name FROM nodes WHERE id=?`, row.ParentID.String).Scan(&folder)
				if err != nil && err != sql.ErrNoRows {
					return nil, err
				}
				entry.Folder = folder
			}
		}
		if len(d.Names) > 0 {
			entry.Names = d.Names
		}
		entry.From = d.From
		if d.GranteeID > 0 {
			entry.Member = &memberSummary{Name: people[d.GranteeID], Self: d.GranteeID == viewer}
		}
		out = append(out, entry)
	}
	return out, nil
}
