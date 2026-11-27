package vaults

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// NCV1: 12-byte header, then a 16-byte tag per 64 KiB segment (an empty file
// is one empty segment).
const (
	segmentBytes int64 = 65536
	headerBytes  int64 = 12
	tagBytes     int64 = 16
)

func vaultFileSize(size int64) int64 {
	return headerBytes + max(int64(1), (size+segmentBytes-1)/segmentBytes)*tagBytes + size
}

// problemsOf is why a folder can't become a vault, ignoring room. Members and
// public links count on the folder, anything in it and any folder above it:
// people who can see the folder would lose its contents.
func problemsOf(ctx context.Context, tx *sql.Tx, uid int64, f folder) ([]string, error) {
	var nested, shared, linked bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE
		down(id,depth,vault_id) AS (
			SELECT id,0,vault_id FROM nodes WHERE id=? AND user_id=?
			UNION ALL SELECT n.id,d.depth+1,n.vault_id FROM nodes n JOIN down d ON n.parent_id=d.id WHERE n.user_id=? AND n.trashed_at IS NULL
		),
		up(id,parent_id) AS (
			SELECT id,parent_id FROM nodes WHERE id=? AND user_id=?
			UNION ALL SELECT n.id,n.parent_id FROM nodes n JOIN up u ON n.id=u.parent_id WHERE n.user_id=?
		),
		targets(id) AS (SELECT id FROM down UNION SELECT id FROM up)
		SELECT
			EXISTS(SELECT 1 FROM down WHERE depth>0 AND vault_id IS NOT NULL),
			EXISTS(SELECT 1 FROM user_shares WHERE node_id IN (SELECT id FROM targets) AND (expires_at IS NULL OR datetime(expires_at)>datetime('now'))),
			EXISTS(SELECT 1 FROM shares WHERE node_id IN (SELECT id FROM targets) AND revoked_at IS NULL AND (expires_at IS NULL OR datetime(expires_at)>datetime('now')))`,
		f.id, uid, uid, f.id, uid, uid).Scan(&nested, &shared, &linked)
	if err != nil {
		return nil, err
	}
	problems := []string{}
	for _, check := range []struct {
		found bool
		code  string
	}{{!f.parent.Valid, "ROOT"}, {f.vault.Valid, "IN_VAULT"}, {nested, "CONTAINS_VAULT"}, {shared, "SHARED"}, {linked, "PUBLIC_LINK"}} {
		if check.found {
			problems = append(problems, check.code)
		}
	}
	return problems, nil
}

// freeBytes is the room left in the account's quota; nil when unlimited.
func (h *Handler) freeBytes(ctx context.Context, tx *sql.Tx, uid int64) (*int64, error) {
	var used, reserved int64
	var quota sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT used_bytes,quota_bytes FROM users WHERE id=?`, uid).Scan(&used, &quota); err != nil {
		return nil, err
	}
	limit := h.defaultQuota()
	if quota.Valid {
		limit = quota.Int64
	}
	if limit < 0 {
		return nil, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND datetime(expires_at)>datetime('now')`, uid).Scan(&reserved); err != nil {
		return nil, err
	}
	free := max(int64(0), limit-used-reserved)
	return &free, nil
}

type conversion struct {
	FolderID   string   `json:"folderId"`
	Name       string   `json:"name"`
	ParentID   any      `json:"parentId"`
	Files      int64    `json:"files"`
	Folders    int64    `json:"folders"`
	Bytes      int64    `json:"bytes"`
	VaultBytes int64    `json:"vaultBytes"`
	FreeBytes  *int64   `json:"freeBytes"`
	Cursor     int64    `json:"cursor"`
	Problems   []string `json:"problems"`
}

// conversionCheck says what turning a folder into a vault involves and what
// rules it out.
func (h *Handler) conversionCheck(w http.ResponseWriter, r *http.Request, uid int64) error {
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := liveFolder(ctx, tx, uid, r.PathValue("folderId"))
	if err != nil {
		return err
	}
	result := conversion{FolderID: f.id, Name: f.name, ParentID: nullable(f.parent)}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE down(id,type,size,depth) AS (
		SELECT id,type,size,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.type,n.size,d.depth+1 FROM nodes n JOIN down d ON n.parent_id=d.id WHERE n.user_id=? AND n.trashed_at IS NULL
	) SELECT type,size FROM down WHERE depth>0`, f.id, uid, uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var size int64
		if err = rows.Scan(&kind, &size); err != nil {
			return err
		}
		if kind != "file" {
			result.Folders++
			continue
		}
		result.Files++
		result.Bytes += size
		result.VaultBytes += vaultFileSize(size)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if result.Problems, err = problemsOf(ctx, tx, uid, f); err != nil {
		return err
	}
	if result.FreeBytes, err = h.freeBytes(ctx, tx, uid); err != nil {
		return err
	}
	if result.FreeBytes != nil && *result.FreeBytes < result.VaultBytes {
		result.Problems = append(result.Problems, "QUOTA")
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=?`, uid).Scan(&result.Cursor); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}
