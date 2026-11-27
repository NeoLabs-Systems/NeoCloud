package ai

import (
	"context"
	"net/http"
	"strings"
)

func notFound() error {
	return fail(http.StatusNotFound, "File or folder not found", "NOT_FOUND")
}

// assertAllowed refuses unless the owner's node may be sent to an AI provider:
// not for another account's or a trashed node, vault content, or anything
// inside a folder excluded from AI.
func assertAllowed(ctx context.Context, q querier, userID int64, nodeID string) error {
	var found, trashed, vault, excluded int
	err := q.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,vault_id,ai_excluded) AS (
		SELECT id,parent_id,trashed_at,vault_id,ai_excluded FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id,n.trashed_at,n.vault_id,n.ai_excluded FROM nodes n JOIN up ON n.id=up.parent_id
	) SELECT COUNT(*),COALESCE(MAX(trashed_at IS NOT NULL),0),COALESCE(MAX(vault_id IS NOT NULL),0),COALESCE(MAX(ai_excluded=1),0) FROM up`, nodeID, userID).Scan(&found, &trashed, &vault, &excluded)
	switch {
	case err != nil:
		return err
	case found == 0 || trashed != 0:
		return notFound()
	case vault != 0:
		return fail(http.StatusForbidden, "Vault files are never processed by AI.", "AI_VAULT")
	case excluded != 0:
		return fail(http.StatusForbidden, "This item is in a folder excluded from AI.", "AI_EXCLUDED")
	}
	return nil
}

// CheckDirectFeature applies the AI policy to an on-demand call about a file,
// such as help in a note.
func (h *Handler) CheckDirectFeature(ctx context.Context, userID int64, nodeID, feature string) error {
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return err
	}
	if err = h.assertUsable(settings); err != nil {
		return err
	}
	if err = settings.assertFeature(feature); err != nil {
		return err
	}
	if err = assertAllowed(ctx, h.db, userID, nodeID); err != nil {
		return err
	}
	return h.assertWithinBudget(ctx, userID)
}

// node is the columns AI works with.
type node struct {
	ID, ParentID, Type, Name, MIME, ETag, CreatedAt, ModifiedAt, BlobID string
	Size                                                                int64
}

// liveNode reads an owner's node that is not in the trash. "root" names the
// account's root folder; kind "file" or "folder" requires that type.
func liveNode(ctx context.Context, q querier, userID int64, id, kind string) (node, error) {
	if id == "root" {
		err := q.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, userID).Scan(&id)
		if err != nil {
			return node{}, notFound()
		}
	}
	nodes, err := queryAll(ctx, q, func(row scanner) (node, error) {
		var n node
		err := row.Scan(&n.ID, &n.ParentID, &n.Type, &n.Name, &n.MIME, &n.ETag, &n.CreatedAt, &n.ModifiedAt, &n.BlobID, &n.Size)
		return n, err
	}, `WITH RECURSIVE up(parent_id,trashed_at) AS (
		SELECT parent_id,trashed_at FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.parent_id,n.trashed_at FROM nodes n JOIN up ON n.id=up.parent_id
	) SELECT id,COALESCE(parent_id,''),type,name,COALESCE(mime,''),etag,created_at,modified_at,COALESCE(blob_id,''),size
	FROM nodes WHERE id=? AND user_id=? AND NOT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL)`, id, userID, id, userID)
	if err != nil {
		return node{}, err
	}
	if len(nodes) == 0 {
		return node{}, notFound()
	}
	if kind != "" && nodes[0].Type != kind {
		return node{}, fail(http.StatusBadRequest, "That is not a "+kind+".", "NOT_A_"+strings.ToUpper(kind))
	}
	return nodes[0], nil
}

// setFolderExcluded marks or unmarks one of the owner's folders as excluded.
func (h *Handler) setFolderExcluded(r *http.Request, userID int64, input body) (any, error) {
	excluded, ok := input["excluded"].(bool)
	if !ok {
		return nil, fail(http.StatusBadRequest, "excluded must be true or false.", "INVALID_VALUE")
	}
	folder, err := liveNode(r.Context(), h.db, userID, r.PathValue("id"), "folder")
	if err != nil {
		return nil, err
	}
	if folder.ParentID == "" {
		return nil, fail(http.StatusBadRequest, "Turn AI off in Settings to exclude everything.", "ROOT_FOLDER")
	}
	if _, err = h.db.ExecContext(r.Context(), `UPDATE nodes SET ai_excluded=? WHERE id=? AND user_id=?`, excluded, folder.ID, userID); err != nil {
		return nil, err
	}
	views, err := h.options.Files.NodeViews(r.Context(), userID, []string{folder.ID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"node": views[folder.ID]}, nil
}
