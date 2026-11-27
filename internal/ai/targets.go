package ai

import (
	"context"
	"net/http"
	"strings"
)

// Where AI may suggest moving a file: one of the owner's own live folders,
// never in a vault or inside a folder excluded from AI, and never across the
// border of a shared folder: a move suggestion must not change who can see the
// file.

const (
	maxCandidateFolders = 1000
	maxFolderDepth      = 64
)

const sharedSQL = `(EXISTS(SELECT 1 FROM user_shares s WHERE s.node_id=n.id) OR EXISTS(SELECT 1 FROM shares l WHERE l.node_id=n.id AND l.revoked_at IS NULL))`

type folder struct {
	ID, ParentID, Name, Path string
}

// shareScope is the shared folders above (and including) a folder, root first,
// as one key.
func shareScope(ctx context.Context, q querier, userID int64, folderID string) (string, error) {
	ids, err := queryStrings(ctx, q, `WITH RECURSIVE up(id,parent_id,depth) AS (
		SELECT id,parent_id,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id,up.depth+1 FROM nodes p JOIN up ON p.id=up.parent_id
	) SELECT n.id FROM up JOIN nodes n ON n.id=up.id WHERE `+sharedSQL+` ORDER BY up.depth DESC`, folderID, userID)
	return strings.Join(ids, ","), err
}

// candidateFolders is every folder a file in fromFolderID may be moved to, by
// depth and then path.
func candidateFolders(ctx context.Context, q querier, userID int64, fromFolderID string) ([]folder, error) {
	scope, err := shareScope(ctx, q, userID, fromFolderID)
	if err != nil {
		return nil, err
	}
	type row struct {
		folder
		scope string
	}
	rows, err := queryAll(ctx, q, func(s scanner) (row, error) {
		var item row
		err := s.Scan(&item.ID, &item.ParentID, &item.Name, &item.Path, &item.scope)
		return item, err
	}, `WITH RECURSIVE down(id,parent_id,name,path,scope,depth) AS (
		SELECT n.id,n.parent_id,n.name,'',CASE WHEN `+sharedSQL+` THEN n.id ELSE '' END,0 FROM nodes n WHERE n.user_id=? AND n.parent_id IS NULL
		UNION ALL SELECT n.id,n.parent_id,n.name,down.path||'/'||n.name,
			CASE WHEN `+sharedSQL+` THEN (CASE WHEN down.scope='' THEN n.id ELSE down.scope||','||n.id END) ELSE down.scope END,down.depth+1
		FROM nodes n JOIN down ON n.parent_id=down.id
		WHERE n.type='folder' AND n.trashed_at IS NULL AND n.vault_id IS NULL AND n.ai_excluded=0 AND down.depth<?
	) SELECT id,COALESCE(parent_id,''),name,path,scope FROM down ORDER BY depth,lower(path) LIMIT ?`, userID, maxFolderDepth, maxCandidateFolders)
	if err != nil {
		return nil, err
	}
	folders := []folder{}
	for _, item := range rows {
		if item.scope == scope {
			if item.Path == "" {
				item.Path = "/"
			}
			folders = append(folders, item.folder)
		}
	}
	return folders, nil
}

func targetNotAllowed() error {
	return fail(http.StatusConflict, "AI can’t move the file there.", "AI_TARGET_NOT_ALLOWED")
}

// filingFolder is a live folder of the owner that AI may file into.
func filingFolder(ctx context.Context, q querier, userID int64, folderID string) (node, error) {
	found, err := liveNode(ctx, q, userID, folderID, "folder")
	if err != nil {
		return node{}, err
	}
	if err = assertAllowed(ctx, q, userID, found.ID); err != nil {
		if statusOf(err) == http.StatusForbidden {
			return node{}, targetNotAllowed()
		}
		return node{}, err
	}
	return found, nil
}

// assertMoveTarget refuses unless the file in fromFolderID may move to folderID.
func assertMoveTarget(ctx context.Context, q querier, userID int64, fromFolderID, folderID string) (node, error) {
	found, err := filingFolder(ctx, q, userID, folderID)
	if err != nil {
		return node{}, err
	}
	to, err := shareScope(ctx, q, userID, found.ID)
	if err != nil {
		return node{}, err
	}
	from, err := shareScope(ctx, q, userID, fromFolderID)
	if err != nil {
		return node{}, err
	}
	if to != from {
		return node{}, fail(http.StatusConflict, "Items can’t be moved across the border of a shared folder.", "SHARE_BOUNDARY")
	}
	return found, nil
}

// folderPaths are the paths ("/Finance/Telekom", "/" for the root) of folders.
func folderPaths(ctx context.Context, q querier, userID int64, folderIDs []string) (map[string]string, error) {
	type row struct {
		start, name string
		root        bool
	}
	rows, err := queryAll(ctx, q, func(s scanner) (row, error) {
		var item row
		err := s.Scan(&item.start, &item.name, &item.root)
		return item, err
	}, `WITH RECURSIVE up(start,id,parent_id,name,depth) AS (
		SELECT n.id,n.id,n.parent_id,n.name,0 FROM json_each(?) j CROSS JOIN nodes n ON n.id=j.value AND n.user_id=?
		UNION ALL SELECT up.start,p.id,p.parent_id,p.name,up.depth+1 FROM nodes p JOIN up ON p.id=up.parent_id
	) SELECT start,name,parent_id IS NULL FROM up ORDER BY start,depth DESC`, jsonText(folderIDs), userID)
	if err != nil {
		return nil, err
	}
	names := map[string][]string{}
	for _, item := range rows {
		if !item.root {
			names[item.start] = append(names[item.start], item.name)
		}
	}
	paths := make(map[string]string, len(folderIDs))
	for _, id := range folderIDs {
		paths[id] = "/" + strings.Join(names[id], "/")
	}
	return paths, nil
}

func folderPath(ctx context.Context, q querier, userID int64, folderID string) (string, error) {
	paths, err := folderPaths(ctx, q, userID, []string{folderID})
	return paths[folderID], err
}
