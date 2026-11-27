package files

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type sharedReadAccess struct {
	ownerID     int64
	rootID      string
	permission  string
	writeRootID string
	node        node
	chain       []ancestor
}

type ancestor struct {
	id      string
	name    string
	parent  sql.NullString
	trashed sql.NullString
	vault   sql.NullString
	depth   int
}

// resolveReadNode preserves owner behavior and resolves a member's visible
// shared ancestor chain. Inactive owners, expired grants, trashed ancestry,
// and vaults are indistinguishable from missing nodes.
func (h *Handler) resolveReadNode(tx *sql.Tx, viewer int64, id string) (node, *sharedReadAccess, error) {
	if id == "root" {
		n, err := h.live(tx, viewer, id)
		return n, nil, err
	}
	var owner int64
	err := tx.QueryRow(`SELECT user_id FROM nodes WHERE id=?`, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	if err != nil {
		return node{}, nil, err
	}
	if owner == viewer {
		n, err := h.live(tx, viewer, id)
		return n, nil, err
	}
	var active int
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, owner).Scan(&active); err != nil {
		return node{}, nil, err
	}
	if active == 0 {
		return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	rows, err := tx.Query(`WITH RECURSIVE up(id,parent_id,name,trashed_at,vault_id,depth) AS (
		SELECT id,parent_id,name,trashed_at,vault_id,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id,n.name,n.trashed_at,n.vault_id,up.depth+1
		FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT id,parent_id,name,trashed_at,vault_id,depth FROM up ORDER BY depth`, id, owner, owner)
	if err != nil {
		return node{}, nil, err
	}
	chain := []ancestor{}
	for rows.Next() {
		var a ancestor
		if err = rows.Scan(&a.id, &a.parent, &a.name, &a.trashed, &a.vault, &a.depth); err != nil {
			rows.Close()
			return node{}, nil, err
		}
		if a.trashed.Valid || a.vault.Valid {
			rows.Close()
			return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
		}
		chain = append(chain, a)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return node{}, nil, err
	}
	if err = rows.Close(); err != nil {
		return node{}, nil, err
	}
	if len(chain) == 0 {
		return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	ids, err := json.Marshal(func() []string {
		out := make([]string, 0, len(chain))
		for _, a := range chain {
			out = append(out, a.id)
		}
		return out
	}())
	if err != nil {
		return node{}, nil, err
	}
	grants, err := tx.Query(`SELECT node_id,permission FROM user_shares WHERE owner_id=? AND grantee_id=?
		AND node_id IN (SELECT value FROM json_each(?)) AND (expires_at IS NULL OR datetime(expires_at)>datetime('now'))`, owner, viewer, string(ids))
	if err != nil {
		return node{}, nil, err
	}
	rootDepth, writeDepth := -1, -1
	for grants.Next() {
		var grantID, permission string
		if err = grants.Scan(&grantID, &permission); err != nil {
			grants.Close()
			return node{}, nil, err
		}
		for _, a := range chain {
			if a.id == grantID {
				if a.depth > rootDepth {
					rootDepth = a.depth
				}
				if permission == "write" && a.depth > writeDepth {
					writeDepth = a.depth
				}
			}
		}
	}
	if err = grants.Err(); err != nil {
		grants.Close()
		return node{}, nil, err
	}
	if err = grants.Close(); err != nil {
		return node{}, nil, err
	}
	if rootDepth < 0 {
		return node{}, nil, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	rootID := ""
	for _, a := range chain {
		if a.depth == rootDepth {
			rootID = a.id
			break
		}
	}
	writeRootID := ""
	for _, a := range chain {
		if a.depth == writeDepth {
			writeRootID = a.id
			break
		}
	}
	n, err := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, owner))
	if err != nil {
		return node{}, nil, err
	}
	permission := "read"
	if writeDepth >= 0 {
		permission = "write"
	}
	return n, &sharedReadAccess{ownerID: owner, rootID: rootID, permission: permission, writeRootID: writeRootID, node: n, chain: chain}, nil
}

func memberNodeView(n node, shareRoot string) map[string]any {
	b, _ := json.Marshal(n)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	delete(out, "shared")
	delete(out, "aiTags")
	delete(out, "aiMeta")
	out["favorite"] = false
	if n.ID == shareRoot {
		out["parentId"] = nil
	}
	return out
}

func memberPath(access *sharedReadAccess) []map[string]string {
	path := []map[string]string{}
	if access == nil {
		return path
	}
	start := -1
	for i, a := range access.chain {
		if a.id == access.rootID {
			start = i
			break
		}
	}
	if start < 0 {
		return path
	}
	for i := start; i >= 0; i-- {
		path = append(path, map[string]string{"id": access.chain[i].id, "name": access.chain[i].name})
	}
	return path
}

func (a *sharedReadAccess) hideVaultChildCount(tx *sql.Tx) (int, error) {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE user_id=? AND parent_id=? AND trashed_at IS NULL AND vault_id IS NOT NULL`, a.ownerID, a.node.ID).Scan(&count)
	return count, err
}

func (a *sharedReadAccess) visibleChildren(items []node) []any {
	out := make([]any, 0, len(items))
	for _, n := range items {
		if n.VaultID != nil {
			continue
		}
		out = append(out, memberNodeView(n, a.rootID))
	}
	return out
}
