package vaults

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/google/uuid"
)

const rootAlias = "root"

type folder struct {
	id, name string
	parent   sql.NullString
	vault    sql.NullString
}

func nodeNotFound() error {
	return apperr.New(http.StatusNotFound, "File or folder not found", "NOT_FOUND")
}

func vaultBoundary(message string) error {
	return apperr.New(http.StatusConflict, message, "VAULT_BOUNDARY")
}

// rootID is the account's root folder, made on first use.
func rootID(ctx context.Context, tx *sql.Tx, uid int64) (string, error) {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','',?)`, uuid.NewString(), uid, uuid.NewString()); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, uid).Scan(&id)
	return id, err
}

// liveFolder is a folder of the account with nothing above it in the trash.
// "root" names the account's root folder.
func liveFolder(ctx context.Context, tx *sql.Tx, uid int64, id string) (folder, error) {
	var f folder
	if id == rootAlias {
		resolved, err := rootID(ctx, tx, uid)
		if err != nil {
			return f, err
		}
		id = resolved
	}
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT id,type,name,parent_id,vault_id FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&f.id, &kind, &f.name, &f.parent, &f.vault)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nodeNotFound()
	}
	if err != nil {
		return f, err
	}
	var trashed bool
	if err = tx.QueryRowContext(ctx, `WITH RECURSIVE up(parent_id,trashed_at) AS (
		SELECT parent_id,trashed_at FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.parent_id,n.trashed_at FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL)`, id, uid, uid).Scan(&trashed); err != nil {
		return f, err
	}
	if trashed {
		return f, nodeNotFound()
	}
	if kind != "folder" {
		return f, apperr.New(http.StatusBadRequest, "That is not a folder.", "NOT_A_FOLDER")
	}
	return f, nil
}

// contains reports whether node is folder itself or lies below it.
func contains(ctx context.Context, tx *sql.Tx, uid int64, folderID, nodeID string) (bool, error) {
	var found bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id) AS (
		SELECT id,parent_id FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=?
	) SELECT EXISTS(SELECT 1 FROM up WHERE id=?)`, nodeID, uid, uid, folderID).Scan(&found)
	return found, err
}

// serializeNode is the node shape of the files API.
func serializeNode(ctx context.Context, tx *sql.Tx, uid int64, id string) (map[string]any, error) {
	var nodeType, name, etag, created, modified string
	var parent, mimeType, vault, signed, tags, fields, model sql.NullString
	var size, version int64
	var aiExcluded, favorite, shared bool
	err := tx.QueryRowContext(ctx, `SELECT n.parent_id,n.type,n.name,n.size,n.mime,n.etag,n.version,n.created_at,n.modified_at,n.vault_id,n.ai_excluded,
		EXISTS(SELECT 1 FROM favorites f WHERE f.user_id=n.user_id AND f.node_id=n.id),
		EXISTS(SELECT 1 FROM user_shares s WHERE s.node_id=n.id),
		(SELECT d.id FROM sign_documents d WHERE d.final_node_id=n.id LIMIT 1),
		(SELECT m.tags_json FROM ai_node_meta m WHERE m.node_id=n.id),
		(SELECT m.fields_json FROM ai_node_meta m WHERE m.node_id=n.id AND m.etag=n.etag AND m.fields_json<>'{}'),
		(SELECT m.model FROM ai_node_meta m WHERE m.node_id=n.id AND m.etag=n.etag)
		FROM nodes n WHERE n.id=? AND n.user_id=?`, id, uid).Scan(&parent, &nodeType, &name, &size, &mimeType, &etag, &version, &created, &modified, &vault, &aiExcluded, &favorite, &shared, &signed, &tags, &fields, &model)
	if err != nil {
		return nil, err
	}
	node := map[string]any{"id": id, "parentId": nullable(parent), "type": nodeType, "name": name, "size": size, "mime": nullable(mimeType), "etag": etag, "version": version, "createdAt": created, "modifiedAt": modified, "vaultId": nullable(vault), "aiExcluded": aiExcluded, "favorite": favorite, "shared": shared, "aiTags": []any{}, "aiMeta": nil}
	if signed.Valid {
		node["signed"], node["signedDocumentId"] = true, signed.String
	}
	var list []any
	if json.Unmarshal([]byte(tags.String), &list) == nil && list != nil {
		node["aiTags"] = list
	}
	var meta map[string]any
	if json.Unmarshal([]byte(fields.String), &meta) == nil && meta != nil {
		delete(meta, "camera")
		meta["model"] = nullable(model)
		node["aiMeta"] = meta
	}
	return node, nil
}

func nullable(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
