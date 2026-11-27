// Package accountdelete contains the transactional account erasure shared by
// administrator and self-service deletion.
package accountdelete

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
)

// ListUploadIDs records upload staging directories for post-commit cleanup.
func ListUploadIDs(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM upload_sessions WHERE user_id=? OR created_by=?`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// SharePartners returns the accounts that shared with userID or that userID
// shared with, mapped to the deduplicated node IDs they could see.
func SharePartners(ctx context.Context, tx *sql.Tx, userID int64) (map[int64][]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT grantee_id AS partner, node_id FROM user_shares WHERE owner_id=?
		UNION ALL
		SELECT owner_id AS partner, node_id FROM user_shares WHERE grantee_id=?`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	partners := map[int64][]string{}
	seen := map[int64]map[string]bool{}
	for rows.Next() {
		var partnerID int64
		var nodeID string
		if err := rows.Scan(&partnerID, &nodeID); err != nil {
			return nil, err
		}
		if seen[partnerID] == nil {
			seen[partnerID] = map[string]bool{}
		}
		if !seen[partnerID][nodeID] {
			seen[partnerID][nodeID] = true
			partners[partnerID] = append(partners[partnerID], nodeID)
		}
	}
	return partners, rows.Err()
}

// NotifySharePartners tells each former share partner to refresh both its
// share list and the affected folders after an account erasure commits.
func NotifySharePartners(notify func(int64, string, map[string]any), partners map[int64][]string) {
	if notify == nil {
		return
	}
	for partnerID, folderIDs := range partners {
		notify(partnerID, "shares:changed", nil)
		notify(partnerID, "files:changed", map[string]any{"folderIds": folderIDs})
	}
}

// DeleteRows removes all database-owned account state in the caller's
// transaction, writes the account deletion audit record, then removes user.
func DeleteRows(ctx context.Context, tx *sql.Tx, userID, actorID int64) error {
	uid := strconv.FormatInt(userID, 10)
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM user_shares WHERE owner_id=? OR grantee_id=?`, []any{userID, userID}},
		{`DELETE FROM shares WHERE user_id=?`, []any{userID}},
		{`DELETE FROM search_docs WHERE user_id=?`, []any{userID}},
		{`DELETE FROM vaults WHERE user_id=?`, []any{userID}},
		{`DELETE FROM sign_documents WHERE owner_id=?`, []any{userID}},
		{`DELETE FROM sign_profile_images WHERE user_id=?`, []any{userID}},
		{`DELETE FROM sign_profiles WHERE user_id=?`, []any{userID}},
		{`DELETE FROM handoff_offers WHERE user_id=?`, []any{userID}},
		{`DELETE FROM import_jobs WHERE user_id=?`, []any{userID}},
		{`DELETE FROM external_mounts WHERE user_id=?`, []any{userID}},
		{`DELETE FROM upload_sessions WHERE user_id=? OR created_by=?`, []any{userID, userID}},
		{`DELETE FROM favorites WHERE user_id=?`, []any{userID}},
		{`DELETE FROM node_comments WHERE user_id=?`, []any{userID}},
		{`DELETE FROM activity WHERE owner_id=? OR actor_id=?`, []any{userID, userID}},
		{`DELETE FROM node_opens WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_jobs WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_suggestions WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_node_meta WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_summaries WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_rules WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_move_stats WHERE user_id=?`, []any{userID}},
		{`DELETE FROM ai_folder_profiles WHERE user_id=?`, []any{userID}},
		{`DELETE FROM nodes WHERE user_id=? AND parent_id IS NULL`, []any{userID}},
		{`DELETE FROM nodes WHERE user_id=?`, []any{userID}},
		{`DELETE FROM previews WHERE user_id=?`, []any{userID}},
		{`DELETE FROM blobs WHERE user_id=?`, []any{userID}},
		{`DELETE FROM changes WHERE user_id=?`, []any{userID}},
		{`DELETE FROM device_pairings WHERE user_id=?`, []any{userID}},
		{`DELETE FROM device_authorizations WHERE user_id=?`, []any{userID}},
		{`DELETE FROM user_qr_login_challenges WHERE approved_by_user_id=?`, []any{userID}},
		{`DELETE FROM device_tokens WHERE user_id=?`, []any{userID}},
		{`DELETE FROM oauth_refresh_tokens WHERE user_id=?`, []any{userID}},
		{`DELETE FROM oauth_access_tokens WHERE user_id=?`, []any{userID}},
		{`DELETE FROM oauth_codes WHERE user_id=?`, []any{userID}},
		{`DELETE FROM user_sessions WHERE user_id=?`, []any{userID}},
		{`DELETE FROM sessions WHERE CAST(json_extract(sess,'$.userId') AS TEXT)=?`, []any{uid}},
		{`DELETE FROM user_webauthn_credentials WHERE user_id=?`, []any{userID}},
		{`DELETE FROM user_recovery_codes WHERE user_id=?`, []any{userID}},
		{`DELETE FROM user_two_factor WHERE user_id=?`, []any{userID}},
		{`DELETE FROM user_settings WHERE user_id=?`, []any{userID}},
		{`DELETE FROM login_failures WHERE account_key IN (?,?)`, []any{"user:" + uid, "confirm:user:" + uid}},
	} {
		if _, err := tx.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			return err
		}
	}
	actor := any(actorID)
	self := actorID == userID
	if self {
		actor = nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.delete',?,?,?)`, actor, userID, `{"userId":`+uid+`,"self":`+strconv.FormatBool(self)+`}`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, userID)
	return err
}

// CleanupStorage removes account blob, preview, and incomplete upload staging
// files after the erasure transaction has committed.
func CleanupStorage(blobDir, previewDir, uploadTmpDir string, userID int64, uploads []string) {
	userComponent := strconv.FormatInt(userID, 10)
	_ = os.RemoveAll(filepath.Join(blobDir, userComponent))
	_ = os.RemoveAll(filepath.Join(previewDir, userComponent))
	partsDir := filepath.Join(uploadTmpDir, "parts")
	for _, uploadID := range uploads {
		if uploadID == "" || filepath.Base(uploadID) != uploadID || uploadID == "." || uploadID == ".." {
			continue
		}
		_ = os.RemoveAll(filepath.Join(partsDir, uploadID))
	}
}
