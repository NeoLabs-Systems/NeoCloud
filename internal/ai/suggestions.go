package ai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The review queue. A pending suggestion turns stale once its file changed in a
// way that matters: new content (any kind), another name (renames), another
// folder (moves), or the file went to the trash. A move into a new folder also
// turns stale once "new folders" is switched off, and is never high-tier.

var (
	suggestionKinds    = []string{"rename", "move", "tags"}
	suggestionTiers    = []string{"high", "medium", "low"}
	suggestionStatuses = []string{"pending", "accepted", "rejected", "applied", "undone", "stale"}
)

const (
	highConfidence   = 0.85
	mediumConfidence = 0.6
	defaultPage      = 50
	maxPage          = 200
)

type signal struct {
	Type   *string `json:"type"`
	Issuer *string `json:"issuer"`
}

func signalOf(f fields) signal {
	optional := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	return signal{optional(f.Type), optional(f.Issuer)}
}

type folderRef struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// draft is a suggestion before it is stored.
type draft struct {
	kind       string
	from, to   any
	reason     string
	confidence float64
	model      string
	source     string
	ruleID     int64
	signal     signal
	// move is set for a move into a folder that doesn't exist yet.
	move *moveTarget
}

// heldDraft is a move into a new folder while its vote waits.
type heldDraft struct {
	Kind string    `json:"kind"`
	From folderRef `json:"from"`
	To   struct {
		ParentID string `json:"parentId"`
		Name     string `json:"name"`
		Path     string `json:"path"`
	} `json:"to"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Model      string  `json:"model"`
	Source     string  `json:"source"`
	Signal     signal  `json:"signal"`
}

func (d draft) held() heldDraft {
	held := heldDraft{Kind: d.kind, Reason: d.reason, Confidence: d.confidence, Model: d.model, Source: d.source, Signal: d.signal}
	held.From, _ = d.from.(folderRef)
	held.To.ParentID, held.To.Name, held.To.Path = d.move.ParentID, d.move.Name, d.move.Path
	return held
}

func (h heldDraft) draft() draft {
	return draft{kind: "move", from: h.From, reason: h.Reason, confidence: h.Confidence, model: h.Model, source: h.Source, signal: h.Signal,
		move: &moveTarget{ParentID: h.To.ParentID, Name: h.To.Name, Path: h.To.Path}}
}

type suggestion struct {
	id, nodeID, kind, from, to, reason, tier, status, source, createdAt, signal, basisName, basisParent, basisETag string
	confidence                                                                                                     float64
	ruleID                                                                                                         sql.NullInt64
	model, decidedAt, applied                                                                                      sql.NullString
}

const suggestionColumns = `id,node_id,kind,from_json,to_json,reason,confidence,tier,status,source,rule_id,model,created_at,decided_at,applied_json,signal_json,basis_name,COALESCE(basis_parent_id,''),basis_etag`

func scanSuggestion(row scanner) (suggestion, error) {
	var s suggestion
	err := row.Scan(&s.id, &s.nodeID, &s.kind, &s.from, &s.to, &s.reason, &s.confidence, &s.tier, &s.status, &s.source, &s.ruleID, &s.model, &s.createdAt, &s.decidedAt, &s.applied, &s.signal, &s.basisName, &s.basisParent, &s.basisETag)
	return s, err
}

func suggestionByID(ctx context.Context, q querier, userID int64, id string) (suggestion, error) {
	s, err := scanSuggestion(q.QueryRowContext(ctx, `SELECT `+suggestionColumns+` FROM ai_suggestions WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return s, fail(http.StatusNotFound, "Suggestion not found", "NOT_FOUND")
	}
	return s, err
}

func decoded(raw string) any {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return nil
	}
	return value
}

func (s suggestion) view(node any) map[string]any {
	var ruleID any
	if s.ruleID.Valid {
		ruleID = s.ruleID.Int64
	}
	return map[string]any{
		"id": s.id, "nodeId": s.nodeID, "kind": s.kind, "from": decoded(s.from), "to": decoded(s.to), "reason": s.reason,
		"confidence": math.Round(s.confidence*100) / 100, "tier": s.tier, "status": s.status, "source": s.source,
		"ruleId": ruleID, "model": nullable(s.model), "createdAt": s.createdAt, "decidedAt": nullable(s.decidedAt), "node": node,
	}
}

// nodeViews are nodes as the apps list them, each with the path of its folder.
func (h *Handler) nodeViews(ctx context.Context, userID int64, ids []string) (map[string]any, error) {
	views, err := h.options.Files.NodeViews(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	var parents []string
	objects := make(map[string]map[string]any, len(views))
	for id, view := range views {
		var object map[string]any
		raw, err := json.Marshal(view)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		objects[id] = object
		if parent, ok := object["parentId"].(string); ok {
			parents = append(parents, parent)
		}
	}
	paths, err := folderPaths(ctx, h.db, userID, parents)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(objects))
	for id, object := range objects {
		object["path"] = "/"
		if parent, ok := object["parentId"].(string); ok {
			object["path"] = paths[parent]
		}
		result[id] = object
	}
	return result, nil
}

func (h *Handler) suggestionView(ctx context.Context, userID int64, id string) (any, error) {
	s, err := suggestionByID(ctx, h.db, userID, id)
	if err != nil {
		return nil, err
	}
	views, err := h.nodeViews(ctx, userID, []string{s.nodeID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"suggestion": s.view(views[s.nodeID])}, nil
}

func (h *Handler) refreshStale(ctx context.Context, userID int64) error {
	if _, err := h.db.ExecContext(ctx, `UPDATE ai_suggestions SET status='stale',decided_at=datetime('now')
		WHERE user_id=? AND status='pending' AND NOT EXISTS(
			SELECT 1 FROM nodes n WHERE n.id=ai_suggestions.node_id AND n.trashed_at IS NULL AND n.etag=ai_suggestions.basis_etag
			AND (ai_suggestions.kind<>'rename' OR n.name=ai_suggestions.basis_name)
			AND (ai_suggestions.kind<>'move' OR n.parent_id IS ai_suggestions.basis_parent_id))`, userID); err != nil {
		return err
	}
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil || settings.features()["newFolders"] {
		return err
	}
	_, err = h.db.ExecContext(ctx, `UPDATE ai_suggestions SET status='stale',decided_at=datetime('now')
		WHERE user_id=? AND status='pending' AND kind='move' AND json_extract(to_json,'$.id') IS NULL`, userID)
	return err
}

// suggestionCounts are the pending suggestions per kind (high and medium) and
// the low ones apart, plus open rule proposals.
func (h *Handler) suggestionCounts(ctx context.Context, userID int64) (map[string]any, error) {
	if err := h.refreshStale(ctx, userID); err != nil {
		return nil, err
	}
	type group struct {
		kind, tier string
		count      int
	}
	groups, err := queryAll(ctx, h.db, func(s scanner) (group, error) {
		var g group
		err := s.Scan(&g.kind, &g.tier, &g.count)
		return g, err
	}, `SELECT kind,tier,COUNT(*) FROM ai_suggestions WHERE user_id=? AND status='pending' GROUP BY kind,tier`, userID)
	if err != nil {
		return nil, err
	}
	pending := map[string]int{"rename": 0, "move": 0, "tags": 0, "total": 0}
	highByKind := map[string]int{"rename": 0, "move": 0, "tags": 0}
	high, hiddenLow := 0, 0
	for _, g := range groups {
		if g.tier == "low" {
			hiddenLow += g.count
			continue
		}
		pending[g.kind] += g.count
		pending["total"] += g.count
		if g.tier == "high" {
			high += g.count
			highByKind[g.kind] += g.count
		}
	}
	var proposals int
	if err = h.db.QueryRowContext(ctx, `SELECT COUNT(*) `+openProposals, userID).Scan(&proposals); err != nil {
		return nil, err
	}
	return map[string]any{"pending": pending, "high": high, "highByKind": highByKind, "hiddenLow": hiddenLow, "proposals": proposals}, nil
}

// AnnounceSuggestions tells the account's open apps to refresh their review
// counts.
func (h *Handler) AnnounceSuggestions(ctx context.Context, userID int64) {
	if h.options.NotifyEvent == nil {
		return
	}
	counts, err := h.suggestionCounts(context.WithoutCancel(ctx), userID)
	if err != nil {
		slog.Error("count AI suggestions", "error", err)
		return
	}
	h.options.NotifyEvent(userID, "ai:suggestions", counts)
}

// A rule's suggestion is high. A move into a folder that doesn't exist yet is
// at most medium, so nothing applies it in bulk or by itself.
func tierOf(d draft, confidence float64) string {
	switch {
	case d.source == "rule":
		return "high"
	case confidence >= highConfidence && d.move == nil:
		return "high"
	case confidence >= mediumConfidence:
		return "medium"
	}
	return "low"
}

func randomUUID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := hex.EncodeToString(raw[:])
	return strings.Join([]string{encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]}, "-")
}

// createSuggestion stores a pending suggestion. An identical pending one is
// kept instead, an older pending one of the same kind replaced. It returns the
// new id and its tier, or "" when nothing changed.
func (h *Handler) createSuggestion(ctx context.Context, userID int64, n node, d draft) (id, tier string, err error) {
	to := jsonText(d.to)
	err = h.inTx(ctx, func(tx *sql.Tx) error {
		var same int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_suggestions WHERE user_id=? AND node_id=? AND kind=? AND status='pending' AND to_json=?`, userID, n.ID, d.kind, to).Scan(&same); err != nil || same > 0 {
			return err
		}
		confidence := min(max(d.confidence, 0), 1)
		if _, err := tx.ExecContext(ctx, `UPDATE ai_suggestions SET status='stale',decided_at=datetime('now') WHERE user_id=? AND node_id=? AND kind=? AND status='pending'`, userID, n.ID, d.kind); err != nil {
			return err
		}
		source, ruleID := "ai", any(nil)
		if d.source == "rule" {
			source = "rule"
		}
		if d.ruleID != 0 {
			ruleID = d.ruleID
		}
		id, tier = randomUUID(), tierOf(d, confidence)
		_, err := tx.ExecContext(ctx, `INSERT INTO ai_suggestions(id,user_id,node_id,kind,from_json,to_json,reason,confidence,tier,source,rule_id,model,signal_json,basis_name,basis_parent_id,basis_etag)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, userID, n.ID, d.kind, jsonText(d.from), to, truncate(d.reason, 300), confidence, tier, source, ruleID,
			nullableString(d.model), jsonText(d.signal), n.Name, nullableString(n.ParentID), n.ETag)
		return err
	})
	return id, tier, err
}

// listFilter reads a comma-separated query or body value.
func listFilter(value string, allowed []string, field string) ([]string, error) {
	var list []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item == "" || slices.Contains(list, item) {
			continue
		}
		if !slices.Contains(allowed, item) {
			return nil, fail(http.StatusBadRequest, field+" must be one of "+strings.Join(allowed, ", ")+".", "INVALID_PARAM")
		}
		list = append(list, item)
	}
	if value == "" {
		return nil, nil
	}
	return append([]string{}, list...), nil
}

// leadingInt is a query number the way parseInt reads it; 0 when there is none.
func leadingInt(value string) int {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] >= '0' && value[end] <= '9' || end == 0 && (value[end] == '-' || value[end] == '+')) {
		end++
	}
	parsed, err := strconv.Atoi(value[:end])
	if err != nil {
		return 0
	}
	return parsed
}

func (h *Handler) countSuggestions(r *http.Request, userID int64, _ body) (any, error) {
	return h.suggestionCounts(r.Context(), userID)
}

func (h *Handler) listSuggestions(r *http.Request, userID int64, _ body) (any, error) {
	ctx, query := r.Context(), r.URL.Query()
	if err := h.refreshStale(ctx, userID); err != nil {
		return nil, err
	}
	kinds, err := listFilter(query.Get("kind"), suggestionKinds, "kind")
	if err != nil {
		return nil, err
	}
	if kinds == nil {
		kinds = suggestionKinds
	}
	tiers := suggestionTiers
	if query.Get("tier") != "all" {
		if tiers, err = listFilter(query.Get("tier"), suggestionTiers, "tier"); err != nil {
			return nil, err
		}
		if tiers == nil {
			tiers = []string{"high", "medium"}
		}
	}
	statuses, err := listFilter(query.Get("status"), suggestionStatuses, "status")
	if err != nil {
		return nil, err
	}
	if statuses == nil {
		statuses = []string{"pending"}
	}
	size := leadingInt(query.Get("limit"))
	if size == 0 {
		size = defaultPage
	}
	size = min(max(size, 1), maxPage)
	offset := max(leadingInt(query.Get("offset")), 0)
	nodeID := nullableString(query.Get("nodeId"))
	where := ` FROM ai_suggestions WHERE user_id=? AND kind IN (SELECT value FROM json_each(?)) AND tier IN (SELECT value FROM json_each(?))
		AND status IN (SELECT value FROM json_each(?)) AND (? IS NULL OR node_id=?)`
	args := []any{userID, jsonText(kinds), jsonText(tiers), jsonText(statuses), nodeID, nodeID}
	rows, err := queryAll(ctx, h.db, scanSuggestion, `SELECT `+suggestionColumns+where+`
		ORDER BY created_at DESC,CASE tier WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,id LIMIT ? OFFSET ?`, append(args, size+1, offset)...)
	if err != nil {
		return nil, err
	}
	var total int
	if err = h.db.QueryRowContext(ctx, `SELECT COUNT(*)`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	var next any
	if len(rows) > size {
		rows, next = rows[:size], offset+size
	}
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.nodeID
	}
	views, err := h.nodeViews(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, len(rows))
	for index, row := range rows {
		items[index] = row.view(views[row.nodeID])
	}
	counts, err := h.suggestionCounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "total": total, "nextOffset": next, "counts": counts}, nil
}
