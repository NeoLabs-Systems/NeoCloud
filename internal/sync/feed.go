package sync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
)

// Feed follows what an account can see change: its own journal, the journals of
// the accounts that share folders with it, and the shares themselves. It reads
// the database, so changes made by any process are seen.
type Feed struct {
	db     *sql.DB
	userID int64
	own    int64
	owners map[int64]int64
	shares string
}

// Follow starts a feed at the current position.
func Follow(ctx context.Context, db *sql.DB, userID int64) (*Feed, error) {
	return follow(ctx, db, userID)
}

func follow(ctx context.Context, db *sql.DB, userID int64) (*Feed, error) {
	f := &Feed{db: db, userID: userID, owners: map[int64]int64{}}
	var err error
	if f.own, err = f.head(ctx, userID); err != nil {
		return nil, err
	}
	if f.shares, err = f.shareSignature(ctx); err != nil {
		return nil, err
	}
	if _, err = f.sharedFolders(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

// Folders lists the folders whose listing changed since the last call.
func (f *Feed) Folders(ctx context.Context) ([]string, error) {
	own, next, err := f.journal(ctx, f.userID, f.own)
	if err != nil {
		return nil, err
	}
	f.own = next
	shared, err := f.sharedFolders(ctx)
	if err != nil {
		return nil, err
	}
	return append(own, shared...), nil
}

// SharesChanged reports whether a share the account gave or received was added,
// changed, removed or expired since the last call.
func (f *Feed) SharesChanged(ctx context.Context) (bool, error) {
	next, err := f.shareSignature(ctx)
	if err != nil || next == f.shares {
		return false, err
	}
	f.shares = next
	return true, nil
}

// sharedFolders follows the journals of the accounts sharing with this one. An
// account that starts sharing is followed from its current position.
func (f *Feed) sharedFolders(ctx context.Context) ([]string, error) {
	owners, err := f.sharedOwners(ctx)
	if err != nil {
		return nil, err
	}
	folders := map[string]bool{}
	cursors := make(map[int64]int64, len(owners))
	for _, ownerID := range owners {
		cursor, known := f.owners[ownerID]
		if !known {
			if cursors[ownerID], err = f.head(ctx, ownerID); err != nil {
				return nil, err
			}
			continue
		}
		changed, next, err := f.visibleChanges(ctx, ownerID, cursor)
		if err != nil {
			return nil, err
		}
		cursors[ownerID] = next
		for _, folderID := range changed {
			folders[folderID] = true
		}
	}
	f.owners = cursors
	return slices.Sorted(maps.Keys(folders)), nil
}

// journal reads the next page of an account's journal and names the folders it
// touched: each change's old and new parent.
func (f *Feed) journal(ctx context.Context, ownerID, after int64) ([]string, int64, error) {
	rows, err := f.db.QueryContext(ctx, `SELECT c.seq,n.parent_id,c.parent_ids_json FROM changes c
		LEFT JOIN nodes n ON n.id=c.node_id AND n.user_id=c.user_id
		WHERE c.user_id=? AND c.seq>? ORDER BY c.seq LIMIT 1001`, ownerID, after)
	if err != nil {
		return nil, after, err
	}
	type candidate struct {
		seq      int64
		parentID sql.NullString
		parents  string
	}
	items := make([]candidate, 0, 1001)
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.seq, &item.parentID, &item.parents); err != nil {
			rows.Close()
			return nil, after, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, after, err
	}
	if err = rows.Close(); err != nil {
		return nil, after, err
	}
	if len(items) > 1000 {
		items = items[:1000]
	}
	next := after
	candidateSet := map[string]bool{}
	for _, item := range items {
		next = item.seq
		var parentIDs []string
		if err = json.Unmarshal([]byte(item.parents), &parentIDs); err != nil {
			parentIDs = nil
		}
		for _, parentID := range parentIDs {
			if parentID != "" {
				candidateSet[parentID] = true
			}
		}
		if item.parentID.Valid && item.parentID.String != "" {
			candidateSet[item.parentID.String] = true
		}
	}
	return slices.Sorted(maps.Keys(candidateSet)), next, nil
}

func (f *Feed) sharedOwners(ctx context.Context) ([]int64, error) {
	rows, err := f.db.QueryContext(ctx, `SELECT DISTINCT s.owner_id FROM user_shares s
		JOIN users u ON u.id=s.owner_id AND u.disabled_at IS NULL
		WHERE s.grantee_id=? AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now')) ORDER BY s.owner_id`, f.userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := []int64{}
	for rows.Next() {
		var ownerID int64
		if err = rows.Scan(&ownerID); err != nil {
			return nil, err
		}
		owners = append(owners, ownerID)
	}
	return owners, rows.Err()
}

func (f *Feed) head(ctx context.Context, ownerID int64) (int64, error) {
	var cursor int64
	err := f.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, ownerID).Scan(&cursor)
	return cursor, err
}

// visibleChanges names the folders of another account's journal after the
// cursor that the viewer's grants cover.
func (f *Feed) visibleChanges(ctx context.Context, ownerID, after int64) ([]string, int64, error) {
	candidates, next, err := f.journal(ctx, ownerID, after)
	if err != nil {
		return nil, after, err
	}
	folders := []string{}
	if len(candidates) == 0 {
		return folders, next, nil
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		return nil, after, err
	}
	rows, err := f.db.QueryContext(ctx, `WITH RECURSIVE candidates(folder_id) AS (
			SELECT value FROM json_each(?)
		), up(folder_id,id,parent_id,trashed_at,vault_id) AS (
			SELECT c.folder_id,n.id,n.parent_id,n.trashed_at,n.vault_id
			FROM candidates c JOIN nodes n ON n.id=c.folder_id AND n.user_id=? AND n.type='folder'
			UNION ALL
			SELECT up.folder_id,p.id,p.parent_id,p.trashed_at,p.vault_id
			FROM up JOIN nodes p ON p.id=up.parent_id AND p.user_id=?
		), permissions AS (
			SELECT up.folder_id,
				MAX(CASE WHEN s.id IS NOT NULL AND (s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now')) THEN 1 ELSE 0 END) AS granted,
				MAX(CASE WHEN up.trashed_at IS NOT NULL OR up.vault_id IS NOT NULL THEN 1 ELSE 0 END) AS hidden
			FROM up LEFT JOIN user_shares s ON s.node_id=up.id AND s.owner_id=? AND s.grantee_id=?
			GROUP BY up.folder_id
		) SELECT folder_id FROM permissions WHERE granted=1 AND hidden=0`, string(encoded), ownerID, ownerID, ownerID, f.userID)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	for rows.Next() {
		var folderID string
		if err := rows.Scan(&folderID); err != nil {
			return nil, after, err
		}
		folders = append(folders, folderID)
	}
	return folders, next, rows.Err()
}

// shareSignature contains only share metadata visible to the authenticated
// account, whether they are the grantee or owner. Polling this durable view
// sends a generic invalidation event without disclosing row contents or
// relying on process-local pub/sub.
func (f *Feed) shareSignature(ctx context.Context) (string, error) {
	rows, err := f.db.QueryContext(ctx, `SELECT role,id,owner_id,node_id,grantee_id,permission,expires_at,active,
		name,etag,modified_at,trashed_at,vault_id,peer_name FROM (
			SELECT 'grantee' AS role,s.id,s.owner_id,s.node_id,s.grantee_id,s.permission,COALESCE(s.expires_at,'') AS expires_at,
				(s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now')) AS active,
				COALESCE(n.name,'') AS name,COALESCE(n.etag,'') AS etag,COALESCE(n.modified_at,'') AS modified_at,
				COALESCE(n.trashed_at,'') AS trashed_at,COALESCE(n.vault_id,'') AS vault_id,
				COALESCE(NULLIF(owner.display_name,''),owner.username,'') AS peer_name
			FROM user_shares s LEFT JOIN nodes n ON n.id=s.node_id AND n.user_id=s.owner_id
			LEFT JOIN users owner ON owner.id=s.owner_id WHERE s.grantee_id=?
			UNION ALL
			SELECT 'owner' AS role,s.id,s.owner_id,s.node_id,s.grantee_id,s.permission,COALESCE(s.expires_at,'') AS expires_at,
				(s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now')) AS active,
				COALESCE(n.name,'') AS name,COALESCE(n.etag,'') AS etag,COALESCE(n.modified_at,'') AS modified_at,
				COALESCE(n.trashed_at,'') AS trashed_at,COALESCE(n.vault_id,'') AS vault_id,
				COALESCE(NULLIF(grantee.display_name,''),grantee.username,'') AS peer_name
			FROM user_shares s LEFT JOIN nodes n ON n.id=s.node_id AND n.user_id=s.owner_id
			LEFT JOIN users grantee ON grantee.id=s.grantee_id WHERE s.owner_id=?
		) ORDER BY role,id`, f.userID, f.userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var role, nodeID, permission, expires, name, etag, modified, trashed, vault, peerName string
		var id, ownerID, granteeID int64
		var active int64
		if err = rows.Scan(&role, &id, &ownerID, &nodeID, &granteeID, &permission, &expires, &active, &name, &etag, &modified, &trashed, &vault, &peerName); err != nil {
			return "", err
		}
		encoded, marshalErr := json.Marshal([]any{role, id, ownerID, nodeID, granteeID, permission, expires, active, name, etag, modified, trashed, vault, peerName})
		if marshalErr != nil {
			return "", marshalErr
		}
		_, _ = hash.Write(encoded)
		_, _ = hash.Write([]byte{'\n'})
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
