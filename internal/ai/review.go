package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Deciding on suggestions. AI never renames, moves or tags on its own: the owner
// accepts (optionally edited) or rejects, and an accepted one can be undone
// while the file is still as it left it. With auto-apply on, high-confidence
// suggestions are applied at once and stay listed as `applied`. A move into a
// new folder creates that folder only on acceptance, with the move.

const (
	maxAcceptedTags = 20
	maxBulk         = 1000
)

func staleError() error {
	return fail(http.StatusConflict, "The file changed since this was suggested.", "AI_SUGGESTION_STALE")
}

func decidedError(status string) error {
	return fail(http.StatusConflict, "This suggestion is "+status+".", "AI_SUGGESTION_DECIDED")
}

func markStale(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `UPDATE ai_suggestions SET status='stale',decided_at=datetime('now') WHERE id=?`, id)
	return err
}

// appliedState is what undo compares the file with.
type appliedState struct {
	Name     string   `json:"name,omitempty"`
	ParentID string   `json:"parentId,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type decision struct {
	to      any
	applied any
	// folders whose listing changed.
	folders []string
}

// current is the file, live and unchanged since the suggestion was made.
func current(ctx context.Context, tx *sql.Tx, userID int64, s suggestion) (node, error) {
	n, err := liveNode(ctx, tx, userID, s.nodeID, "")
	var refusal *Error
	if err != nil && !errors.As(err, &refusal) {
		return node{}, err
	}
	if err != nil || n.ETag != s.basisETag || s.kind == "rename" && n.Name != s.basisName || s.kind == "move" && n.ParentID != s.basisParent {
		return node{}, errStale
	}
	return n, nil
}

var (
	errStale         = errors.New("stale suggestion")
	errNewFoldersOff = errors.New("new folders are off")
)

func (h *Handler) applyRename(tx *sql.Tx, userID int64, s suggestion, n node, value any, edited bool) (decision, error) {
	if !edited {
		value = decoded(s.to)
	}
	name := ""
	if value != nil {
		name = fmt.Sprint(value)
	}
	name, parentID, err := h.options.Files.UpdateTx(tx, userID, n.ID, &name, nil, true)
	if err != nil {
		return decision{}, err
	}
	return decision{to: name, applied: appliedState{Name: name, ParentID: parentID}, folders: []string{parentID}}, nil
}

func (h *Handler) applyMove(ctx context.Context, tx *sql.Tx, userID int64, settings account, s suggestion, n node, value any, edited bool) (decision, error) {
	var to struct {
		ID       string `json:"id"`
		ParentID string `json:"parentId"`
		Name     string `json:"name"`
	}
	_ = json.Unmarshal([]byte(s.to), &to)
	var folders []string
	targetID := to.ID
	switch {
	case edited:
		chosen, ok := value.(string)
		if !ok || chosen == "" {
			return decision{}, fail(http.StatusBadRequest, "value must be a folder id.", "INVALID_VALUE")
		}
		target, err := assertMoveTarget(ctx, tx, userID, n.ParentID, chosen)
		if err != nil {
			return decision{}, err
		}
		targetID = target.ID
	case to.ID == "":
		if !settings.features()["newFolders"] {
			return decision{}, errNewFoldersOff
		}
		if _, err := assertMoveTarget(ctx, tx, userID, n.ParentID, to.ParentID); err != nil {
			return decision{}, err
		}
		id, created, err := h.options.Files.EnsureFolderTx(tx, userID, to.ParentID, to.Name)
		if err != nil {
			return decision{}, err
		}
		if created {
			if _, err = tx.ExecContext(ctx, `UPDATE nodes SET created_by_ai=1 WHERE id=?`, id); err != nil {
				return decision{}, err
			}
		}
		targetID, folders = id, []string{to.ParentID}
	default:
		if _, err := assertMoveTarget(ctx, tx, userID, n.ParentID, to.ID); err != nil {
			return decision{}, err
		}
	}
	name, parentID, err := h.options.Files.UpdateTx(tx, userID, n.ID, nil, &targetID, true)
	if err != nil {
		return decision{}, err
	}
	path, err := folderPath(ctx, tx, userID, targetID)
	if err != nil {
		return decision{}, err
	}
	return decision{
		to:      folderRef{targetID, path},
		applied: appliedState{Name: name, ParentID: parentID},
		folders: append(folders, parentID, n.ParentID),
	}, nil
}

func parseTagValue(value any) ([]string, error) {
	if _, ok := value.([]any); !ok {
		return nil, fail(http.StatusBadRequest, "value must be a list of tags.", "INVALID_VALUE")
	}
	return normalizeTags(value, maxAcceptedTags), nil
}

func applyTags(ctx context.Context, tx *sql.Tx, userID int64, s suggestion, n node, value any, edited bool) (decision, error) {
	tags := []string{}
	if edited {
		parsed, err := parseTagValue(value)
		if err != nil {
			return decision{}, err
		}
		tags = parsed
	} else if err := json.Unmarshal([]byte(s.to), &tags); err != nil {
		return decision{}, err
	}
	if err := saveTags(ctx, tx, userID, n, tags); err != nil {
		return decision{}, err
	}
	return decision{to: tags, applied: map[string]any{"tags": tags}, folders: []string{n.ParentID}}, nil
}

// recordAcceptedMove counts an accepted move of a (type, issuer) document into a
// folder (-1 when undone), for rule proposals.
func recordAcceptedMove(ctx context.Context, tx *sql.Tx, userID int64, raw, folderID string, delta int) error {
	var from signal
	_ = json.Unmarshal([]byte(raw), &from)
	if from.Type == nil || from.Issuer == nil || *from.Type == "" || *from.Issuer == "" || folderID == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_move_stats(user_id,doc_type,issuer,target_folder_id,accepts) VALUES(?,?,?,?,MAX(?,0))
		ON CONFLICT(user_id,doc_type,issuer,target_folder_id) DO UPDATE SET accepts=MAX(accepts+?,0)`,
		userID, strings.ToLower(*from.Type), *from.Issuer, folderID, delta, delta)
	return err
}

// apply applies a pending suggestion, as `accepted` by the owner or `applied`
// automatically. value replaces its proposal when edited is set.
func (h *Handler) apply(ctx context.Context, userID int64, id string, value any, edited bool, status string) error {
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return err
	}
	var folders []string
	var refusal error
	err = h.inTx(ctx, func(tx *sql.Tx) error {
		s, err := suggestionByID(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if s.status != "pending" {
			return decidedError(s.status)
		}
		n, err := current(ctx, tx, userID, s)
		var result decision
		if err == nil {
			switch s.kind {
			case "rename":
				result, err = h.applyRename(tx, userID, s, n, value, edited)
			case "move":
				result, err = h.applyMove(ctx, tx, userID, settings, s, n, value, edited)
			default:
				result, err = applyTags(ctx, tx, userID, s, n, value, edited)
			}
		}
		// A suggestion that can no longer apply is marked stale, and stays so.
		switch {
		case errors.Is(err, errStale):
			refusal = staleError()
			return markStale(ctx, tx, s.id)
		case errors.Is(err, errNewFoldersOff):
			refusal = fail(http.StatusConflict, "Suggesting new folders is turned off in Settings › AI.", "AI_NEW_FOLDERS_OFF")
			return markStale(ctx, tx, s.id)
		case err != nil:
			return err
		}
		// Only the owner's own accepts teach rule proposals.
		if target, ok := result.to.(folderRef); ok && status == "accepted" {
			if err = recordAcceptedMove(ctx, tx, userID, s.signal, target.ID, 1); err != nil {
				return err
			}
		}
		folders = result.folders
		_, err = tx.ExecContext(ctx, `UPDATE ai_suggestions SET status=?,to_json=?,applied_json=?,decided_at=datetime('now') WHERE id=?`, status, jsonText(result.to), jsonText(result.applied), s.id)
		return err
	})
	if err != nil {
		return err
	}
	h.announceChanges(ctx, userID, folders...)
	return refusal
}

// autoApply applies new high-tier suggestions; one that can't stays pending.
func (h *Handler) autoApply(ctx context.Context, userID int64, ids []string) {
	for _, id := range ids {
		_ = h.apply(ctx, userID, id, nil, false, "applied")
	}
}

func (h *Handler) accept(r *http.Request, userID int64, input body) (any, error) {
	ctx, id := r.Context(), r.PathValue("id")
	if _, err := suggestionByID(ctx, h.db, userID, id); err != nil {
		return nil, err
	}
	value, edited := input["value"]
	err := h.apply(ctx, userID, id, value, edited, "accepted")
	h.AnnounceSuggestions(ctx, userID)
	if err != nil {
		return nil, err
	}
	return h.suggestionView(ctx, userID, id)
}

func (h *Handler) rejectOne(ctx context.Context, userID int64, id string) error {
	s, err := suggestionByID(ctx, h.db, userID, id)
	if err != nil {
		return err
	}
	if s.status != "pending" && s.status != "stale" {
		return decidedError(s.status)
	}
	_, err = h.db.ExecContext(ctx, `UPDATE ai_suggestions SET status='rejected',decided_at=datetime('now') WHERE id=?`, s.id)
	return err
}

func (h *Handler) reject(r *http.Request, userID int64, _ body) (any, error) {
	ctx, id := r.Context(), r.PathValue("id")
	if err := h.rejectOne(ctx, userID, id); err != nil {
		return nil, err
	}
	h.AnnounceSuggestions(ctx, userID)
	return h.suggestionView(ctx, userID, id)
}

type failure struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

func (h *Handler) acceptAllHigh(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	kind := ""
	if raw := input["kind"]; raw != nil {
		kind = fmt.Sprint(raw)
	}
	kinds, err := listFilter(kind, suggestionKinds, "kind")
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		kinds = suggestionKinds
	}
	if err = h.refreshStale(ctx, userID); err != nil {
		return nil, err
	}
	ids, err := queryStrings(ctx, h.db, `SELECT id FROM ai_suggestions WHERE user_id=? AND status='pending' AND tier='high' AND kind IN (SELECT value FROM json_each(?))
		ORDER BY created_at,CASE kind WHEN 'rename' THEN 0 WHEN 'tags' THEN 1 ELSE 2 END`, userID, jsonText(kinds))
	if err != nil {
		return nil, err
	}
	accepted, failed := 0, []failure{}
	for _, id := range ids {
		if err = h.apply(ctx, userID, id, nil, false, "accepted"); err != nil {
			failed = append(failed, failure{id, codeOf(err)})
			continue
		}
		accepted++
	}
	h.AnnounceSuggestions(ctx, userID)
	return map[string]any{"accepted": accepted, "failed": failed}, nil
}

// idList reads 1..limit non-empty string ids, without repeats.
func idList(value any, limit int) ([]string, bool) {
	list, ok := value.([]any)
	if !ok || len(list) == 0 || len(list) > limit {
		return nil, false
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		id, ok := item.(string)
		if !ok || id == "" {
			return nil, false
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, true
}

func (h *Handler) bulkDecide(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	action := input["action"]
	if action != "accept" && action != "reject" {
		return nil, fail(http.StatusBadRequest, "action must be accept or reject.", "INVALID_ACTION")
	}
	ids, ok := idList(input["ids"], maxBulk)
	if !ok {
		return nil, fail(http.StatusBadRequest, "ids must be a list of 1–1000 suggestion ids.", "INVALID_REQUEST")
	}
	done, failed := 0, []failure{}
	for _, id := range ids {
		var err error
		if action == "accept" {
			err = h.apply(ctx, userID, id, nil, false, "accepted")
		} else {
			err = h.rejectOne(ctx, userID, id)
		}
		if err != nil {
			failed = append(failed, failure{id, codeOf(err)})
			continue
		}
		done++
	}
	h.AnnounceSuggestions(ctx, userID)
	return map[string]any{"done": done, "failed": failed}, nil
}

func undoConflict() error {
	return fail(http.StatusConflict, "The file changed since; it can’t be put back automatically.", "AI_UNDO_CONFLICT")
}

// undo puts the old name, folder or tags back while the file is still as the
// suggestion left it.
func (h *Handler) undo(r *http.Request, userID int64, _ body) (any, error) {
	ctx, id := r.Context(), r.PathValue("id")
	var folders []string
	err := h.inTx(ctx, func(tx *sql.Tx) error {
		s, err := suggestionByID(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if s.status != "accepted" && s.status != "applied" {
			return fail(http.StatusConflict, "This suggestion is "+s.status+".", "AI_NOT_UNDOABLE")
		}
		var applied appliedState
		_ = json.Unmarshal([]byte(s.applied.String), &applied)
		n, err := liveNode(ctx, tx, userID, s.nodeID, "")
		if err != nil {
			if statusOf(err) == http.StatusNotFound {
				return undoConflict()
			}
			return err
		}
		if s.kind == "tags" {
			tags, err := acceptedTags(ctx, tx, n.ID)
			if err != nil {
				return err
			}
			if !slices.Equal(tags, applied.Tags) {
				return undoConflict()
			}
			previous := []string{}
			_ = json.Unmarshal([]byte(s.from), &previous)
			folders = []string{n.ParentID}
			if err = saveTags(ctx, tx, userID, n, previous); err != nil {
				return err
			}
		} else {
			if n.Name != applied.Name || n.ParentID != applied.ParentID {
				return undoConflict()
			}
			if s.kind == "rename" {
				previous, ok := decoded(s.from).(string)
				if !ok {
					return undoConflict()
				}
				if _, _, err = h.options.Files.UpdateTx(tx, userID, n.ID, &previous, nil, false); err != nil {
					return err
				}
				folders = []string{n.ParentID}
			} else {
				var previous folderRef
				_ = json.Unmarshal([]byte(s.from), &previous)
				if _, _, err = h.options.Files.UpdateTx(tx, userID, n.ID, &s.basisName, &previous.ID, false); err != nil {
					return err
				}
				folders = []string{previous.ID, applied.ParentID}
				if s.status == "accepted" {
					if err = recordAcceptedMove(ctx, tx, userID, s.signal, applied.ParentID, -1); err != nil {
						return err
					}
				}
				emptied, err := h.removeIfEmptyAIFolder(ctx, tx, userID, applied.ParentID)
				if err != nil {
					return err
				}
				folders = append(folders, emptied)
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE ai_suggestions SET status='undone',decided_at=datetime('now') WHERE id=?`, s.id)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.announceChanges(ctx, userID, folders...)
	h.AnnounceSuggestions(ctx, userID)
	return h.suggestionView(ctx, userID, id)
}

// A folder AI made for a move that was just undone goes to the trash once
// nothing is left in it. Returns the folder it was in.
func (h *Handler) removeIfEmptyAIFolder(ctx context.Context, tx *sql.Tx, userID int64, folderID string) (string, error) {
	var empty int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes f WHERE f.id=? AND f.user_id=? AND f.created_by_ai=1 AND f.trashed_at IS NULL
		AND NOT EXISTS(SELECT 1 FROM nodes c WHERE c.parent_id=f.id AND c.trashed_at IS NULL)`, folderID, userID).Scan(&empty); err != nil || empty == 0 {
		return "", err
	}
	return h.options.Files.TrashTx(tx, userID, folderID)
}
