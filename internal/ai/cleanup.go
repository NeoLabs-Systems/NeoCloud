package ai

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
)

// Removing empty folders AI left behind: the ones made by accepting a move into
// a new folder, and other empty top-level folders of the last seven days (older
// AI folders can't be told apart from the person's own). Only empty folders
// qualify, never a vault or a shared folder; they go to the trash.

const maxEmptyFolders = 500

const emptyFolderSQL = `SELECT f.id,f.name,f.created_at FROM nodes f
	WHERE f.user_id=? AND f.type='folder' AND f.trashed_at IS NULL AND f.parent_id IS NOT NULL AND f.vault_id IS NULL
	AND NOT EXISTS(SELECT 1 FROM vaults v WHERE v.folder_id=f.id)
	AND NOT EXISTS(SELECT 1 FROM nodes c WHERE c.parent_id=f.id AND c.trashed_at IS NULL)
	AND NOT EXISTS(SELECT 1 FROM user_shares s WHERE s.node_id=f.id)
	AND NOT EXISTS(SELECT 1 FROM shares l WHERE l.node_id=f.id AND l.revoked_at IS NULL) AND `

type emptyFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	CreatedAt string `json:"createdAt"`
}

func scanEmptyFolder(row scanner) (emptyFolder, error) {
	var f emptyFolder
	err := row.Scan(&f.ID, &f.Name, &f.CreatedAt)
	return f, err
}

func emptyFolders(ctx context.Context, q querier, userID int64) (aiCreated, recent []emptyFolder, err error) {
	if aiCreated, err = queryAll(ctx, q, scanEmptyFolder, emptyFolderSQL+`f.created_by_ai=1 ORDER BY f.created_at DESC LIMIT ?`, userID, maxEmptyFolders); err != nil {
		return nil, nil, err
	}
	recent, err = queryAll(ctx, q, scanEmptyFolder, emptyFolderSQL+`f.created_by_ai=0 AND f.parent_id=(SELECT id FROM nodes WHERE user_id=f.user_id AND parent_id IS NULL)
		AND f.created_at>=datetime('now','-7 days') ORDER BY f.created_at DESC LIMIT ?`, userID, maxEmptyFolders)
	return aiCreated, recent, err
}

func (h *Handler) listEmptyFolders(r *http.Request, userID int64, _ body) (any, error) {
	aiCreated, recent, err := emptyFolders(r.Context(), h.db, userID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, item := range slices.Concat(aiCreated, recent) {
		ids = append(ids, item.ID)
	}
	paths, err := folderPaths(r.Context(), h.db, userID, ids)
	if err != nil {
		return nil, err
	}
	for _, list := range [][]emptyFolder{aiCreated, recent} {
		for index := range list {
			list[index].Path = paths[list[index].ID]
		}
	}
	return map[string]any{"aiCreated": aiCreated, "recent": recent}, nil
}

func (h *Handler) removeEmptyFolders(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	ids, ok := idList(input["ids"], maxEmptyFolders)
	if !ok {
		return nil, fail(http.StatusBadRequest, "ids must be a list of 1–500 folder ids.", "INVALID_REQUEST")
	}
	removed, failed := 0, []failure{}
	for _, id := range ids {
		parent := ""
		// Read again for each: an earlier removal or an upload may have changed things.
		err := h.inTx(ctx, func(tx *sql.Tx) error {
			aiCreated, recent, err := emptyFolders(ctx, tx, userID)
			if err != nil || !slices.ContainsFunc(slices.Concat(aiCreated, recent), func(f emptyFolder) bool { return f.ID == id }) {
				return err
			}
			parent, err = h.options.Files.TrashTx(tx, userID, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		if parent == "" {
			failed = append(failed, failure{id, "NOT_EMPTY_OR_NOT_ALLOWED"})
			continue
		}
		removed++
		h.announceChanges(ctx, userID, parent)
	}
	return map[string]any{"removed": removed, "failed": failed}, nil
}
