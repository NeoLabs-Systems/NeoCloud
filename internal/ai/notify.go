package ai

import (
	"context"
	"log/slog"
	"slices"
)

// announceChanges tells open sessions to refresh folders: the owner's, and
// those of members of a shared folder above a changed one (each told only about
// the folders it can see).
func (h *Handler) announceChanges(ctx context.Context, userID int64, folderIDs ...string) {
	var folders []string
	for _, id := range folderIDs {
		if id != "" && !slices.Contains(folders, id) {
			folders = append(folders, id)
		}
	}
	if len(folders) == 0 || h.options.NotifyEvent == nil {
		return
	}
	h.options.NotifyEvent(userID, "files:changed", map[string]any{"folderIds": folders})
	ctx = context.WithoutCancel(ctx)
	watching := map[int64][]string{}
	for _, folderID := range folders {
		grantees, err := queryAll(ctx, h.db, func(s scanner) (int64, error) {
			var id int64
			err := s.Scan(&id)
			return id, err
		}, `WITH RECURSIVE up(id,parent_id,trashed_at) AS (
			SELECT id,parent_id,trashed_at FROM nodes WHERE id=? AND user_id=?
			UNION ALL SELECT n.id,n.parent_id,n.trashed_at FROM nodes n JOIN up ON n.id=up.parent_id
		) SELECT DISTINCT s.grantee_id FROM user_shares s JOIN up ON up.id=s.node_id
		WHERE s.owner_id=? AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now'))
			AND NOT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL)
			AND (SELECT vault_id FROM nodes WHERE id=?) IS NULL`, folderID, userID, userID, folderID)
		if err != nil {
			slog.Error("find members watching a folder", "error", err)
			return
		}
		for _, grantee := range grantees {
			watching[grantee] = append(watching[grantee], folderID)
		}
	}
	for grantee, visible := range watching {
		h.options.NotifyEvent(grantee, "files:changed", map[string]any{"folderIds": visible})
	}
}
