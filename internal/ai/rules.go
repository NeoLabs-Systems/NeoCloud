package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The owner's sorting rules (deterministic, checked before any model) and rule
// proposals: after three accepted moves of the same kind of document from the
// same issuer into one folder, a rule is offered.

const (
	maxRules          = 200
	proposalThreshold = 3
)

type rule struct {
	id                                   int64
	name, target, createdAt              string
	kind, issuer, extension, namePattern sql.NullString
	enabled                              bool
}

const ruleColumns = `id,name,match_type,match_issuer,match_extension,match_name_pattern,target_folder_id,enabled,created_at`

func scanRule(row scanner) (rule, error) {
	var r rule
	err := row.Scan(&r.id, &r.name, &r.kind, &r.issuer, &r.extension, &r.namePattern, &r.target, &r.enabled, &r.createdAt)
	return r, err
}

func (r rule) view(path string) map[string]any {
	return map[string]any{
		"id": r.id, "name": r.name,
		"match":          map[string]any{"type": nullable(r.kind), "issuer": nullable(r.issuer), "extension": nullable(r.extension), "namePattern": nullable(r.namePattern)},
		"targetFolderId": r.target, "targetPath": path, "enabled": r.enabled, "createdAt": r.createdAt,
	}
}

func invalidRule(message string) error { return fail(http.StatusBadRequest, message, "INVALID_RULE") }

func optionalText(value any, field string, limit int) (sql.NullString, error) {
	if value == nil || value == "" {
		return sql.NullString{}, nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > limit {
		return sql.NullString{}, invalidRule(fmt.Sprintf("%s must be text of at most %d characters.", field, limit))
	}
	return sql.NullString{String: strings.TrimSpace(text), Valid: true}, nil
}

func (r *rule) setName(value any) error {
	name, err := optionalText(value, "name", 80)
	if err != nil {
		return err
	}
	if !name.Valid {
		return invalidRule("A rule needs a name.")
	}
	r.name = name.String
	return nil
}

// setMatch reads the conditions of a rule; at least one must be set.
func (r *rule) setMatch(value any) error {
	match, _ := value.(map[string]any)
	var err error
	for _, field := range []struct {
		target *sql.NullString
		key    string
		limit  int
	}{{&r.kind, "type", 40}, {&r.issuer, "issuer", 80}, {&r.extension, "extension", 16}, {&r.namePattern, "namePattern", 120}} {
		if *field.target, err = optionalText(match[field.key], "match."+field.key, field.limit); err != nil {
			return err
		}
	}
	r.kind.String = strings.ToLower(r.kind.String)
	r.extension.String = strings.ToLower(strings.TrimPrefix(r.extension.String, "."))
	if !r.kind.Valid && !r.issuer.Valid && !r.extension.Valid && !r.namePattern.Valid {
		return invalidRule("A rule needs at least one condition.")
	}
	return nil
}

// A rule's target must be a folder AI may file into.
func (r *rule) setTarget(ctx context.Context, q querier, userID int64, value any) error {
	id, _ := value.(string)
	target, err := filingFolder(ctx, q, userID, id)
	r.target = target.ID
	return err
}

func (h *Handler) ruleView(ctx context.Context, userID, id int64) (any, error) {
	r, err := scanRule(h.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM ai_rules WHERE id=? AND user_id=?`, id, userID))
	if err != nil {
		return nil, err
	}
	path, err := folderPath(ctx, h.db, userID, r.target)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rule": r.view(path)}, nil
}

func (h *Handler) ownRule(ctx context.Context, userID int64, raw string) (rule, error) {
	id, _ := strconv.ParseInt(raw, 10, 64)
	r, err := scanRule(h.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM ai_rules WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fail(http.StatusNotFound, "Rule not found", "NOT_FOUND")
	}
	return r, err
}

func (h *Handler) listRules(r *http.Request, userID int64, _ body) (any, error) {
	rules, err := queryAll(r.Context(), h.db, scanRule, `SELECT `+ruleColumns+` FROM ai_rules WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	targets := make([]string, len(rules))
	for index, item := range rules {
		targets[index] = item.target
	}
	paths, err := folderPaths(r.Context(), h.db, userID, targets)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, len(rules))
	for index, item := range rules {
		views[index] = item.view(paths[item.target])
	}
	return map[string]any{"rules": views}, nil
}

func (h *Handler) insertRule(ctx context.Context, userID int64, input body) (any, error) {
	var next rule
	if err := next.setName(input["name"]); err != nil {
		return nil, err
	}
	if err := next.setMatch(input["match"]); err != nil {
		return nil, err
	}
	if err := next.setTarget(ctx, h.db, userID, input["targetFolderId"]); err != nil {
		return nil, err
	}
	var count int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_rules WHERE user_id=?`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= maxRules {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("At most %d rules.", maxRules), "TOO_MANY_RULES")
	}
	var id int64
	if err := h.db.QueryRowContext(ctx, `INSERT INTO ai_rules(user_id,name,match_type,match_issuer,match_extension,match_name_pattern,target_folder_id,enabled)
		VALUES(?,?,?,?,?,?,?,?) RETURNING id`, userID, next.name, next.kind, next.issuer, next.extension, next.namePattern, next.target, input["enabled"] != false).Scan(&id); err != nil {
		return nil, err
	}
	return h.ruleView(ctx, userID, id)
}

func (h *Handler) createRule(r *http.Request, userID int64, input body) (any, error) {
	return h.insertRule(r.Context(), userID, input)
}

func (h *Handler) updateRule(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	next, err := h.ownRule(ctx, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if value, ok := input["name"]; ok {
		if err = next.setName(value); err != nil {
			return nil, err
		}
	}
	if value, ok := input["match"]; ok {
		if err = next.setMatch(value); err != nil {
			return nil, err
		}
	}
	if value, ok := input["targetFolderId"]; ok {
		if err = next.setTarget(ctx, h.db, userID, value); err != nil {
			return nil, err
		}
	}
	if value, ok := input["enabled"]; ok {
		if next.enabled, ok = value.(bool); !ok {
			return nil, invalidRule("enabled must be true or false.")
		}
	}
	if _, err = h.db.ExecContext(ctx, `UPDATE ai_rules SET name=?,match_type=?,match_issuer=?,match_extension=?,match_name_pattern=?,target_folder_id=?,enabled=? WHERE id=?`,
		next.name, next.kind, next.issuer, next.extension, next.namePattern, next.target, next.enabled, next.id); err != nil {
		return nil, err
	}
	return h.ruleView(ctx, userID, next.id)
}

func (h *Handler) deleteRule(r *http.Request, userID int64, _ body) (any, error) {
	found, err := h.ownRule(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if _, err = h.db.ExecContext(r.Context(), `DELETE FROM ai_rules WHERE id=?`, found.id); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}

// globMatches matches `*` and `?` case-insensitively in linear steps per star:
// a pattern must never backtrack exponentially.
func globMatches(pattern, name string) bool {
	p, n := []rune(strings.ToLower(pattern)), []rune(strings.ToLower(name))
	pi, ni, star, resume := 0, 0, -1, 0
	for ni < len(n) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] != '*' && p[pi] == n[ni]):
			pi++
			ni++
		case pi < len(p) && p[pi] == '*':
			star, resume = pi, ni
			pi++
		case star >= 0:
			pi = star + 1
			resume++
			ni = resume
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

func (r rule) matches(name string, f fields) bool {
	_, extension := splitExtension(name)
	return (!r.kind.Valid || strings.EqualFold(r.kind.String, f.Type)) &&
		(!r.issuer.Valid || strings.EqualFold(r.issuer.String, f.Issuer)) &&
		(!r.extension.Valid || r.extension.String == strings.ToLower(strings.TrimPrefix(extension, "."))) &&
		(!r.namePattern.Valid || globMatches(r.namePattern.String, name))
}

// matchingRule is the first enabled rule (oldest first) matching a file.
func matchingRule(ctx context.Context, q querier, userID int64, name string, f fields) (*rule, error) {
	rules, err := queryAll(ctx, q, scanRule, `SELECT `+ruleColumns+` FROM ai_rules WHERE user_id=? AND enabled=1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	if index := slices.IndexFunc(rules, func(r rule) bool { return r.matches(name, f) }); index >= 0 {
		return &rules[index], nil
	}
	return nil, nil
}

var openProposals = fmt.Sprintf(`FROM ai_move_stats p WHERE p.user_id=? AND p.accepts>=%d AND p.dismissed=0 AND NOT EXISTS (
	SELECT 1 FROM ai_rules r WHERE r.user_id=p.user_id AND r.target_folder_id=p.target_folder_id
	AND lower(COALESCE(r.match_type,''))=p.doc_type AND lower(COALESCE(r.match_issuer,''))=lower(p.issuer))`, proposalThreshold)

type proposal struct {
	ID             int64  `json:"id"`
	Type           string `json:"type"`
	Issuer         string `json:"issuer"`
	TargetFolderID string `json:"targetFolderId"`
	TargetPath     string `json:"targetPath"`
	Accepts        int    `json:"accepts"`
	Name           string `json:"name"`
}

func scanProposal(row scanner) (proposal, error) {
	var p proposal
	err := row.Scan(&p.ID, &p.Type, &p.Issuer, &p.TargetFolderID, &p.Accepts)
	p.Name = p.Issuer + " " + p.Type
	return p, err
}

const proposalColumns = `SELECT p.id,p.doc_type,p.issuer,p.target_folder_id,p.accepts `

func (h *Handler) listProposals(r *http.Request, userID int64, _ body) (any, error) {
	proposals, err := queryAll(r.Context(), h.db, scanProposal, proposalColumns+openProposals+` ORDER BY p.accepts DESC,p.id`, userID)
	if err != nil {
		return nil, err
	}
	targets := make([]string, len(proposals))
	for index, item := range proposals {
		targets[index] = item.TargetFolderID
	}
	paths, err := folderPaths(r.Context(), h.db, userID, targets)
	if err != nil {
		return nil, err
	}
	for index := range proposals {
		proposals[index].TargetPath = paths[proposals[index].TargetFolderID]
	}
	return map[string]any{"proposals": proposals}, nil
}

func (h *Handler) ownProposal(ctx context.Context, userID int64, raw string) (proposal, error) {
	id, _ := strconv.ParseInt(raw, 10, 64)
	p, err := scanProposal(h.db.QueryRowContext(ctx, proposalColumns+openProposals+` AND p.id=?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fail(http.StatusNotFound, "Proposal not found", "NOT_FOUND")
	}
	return p, err
}

func (h *Handler) acceptProposal(r *http.Request, userID int64, input body) (any, error) {
	found, err := h.ownProposal(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	name := input["name"]
	if name == nil {
		name = found.Name
	}
	return h.insertRule(r.Context(), userID, body{"name": name, "match": map[string]any{"type": found.Type, "issuer": found.Issuer}, "targetFolderId": found.TargetFolderID})
}

func (h *Handler) dismissProposal(r *http.Request, userID int64, _ body) (any, error) {
	found, err := h.ownProposal(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if _, err = h.db.ExecContext(r.Context(), `UPDATE ai_move_stats SET dismissed=1 WHERE id=?`, found.ID); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}
