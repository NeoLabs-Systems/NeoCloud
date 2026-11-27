package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The tree mutations. Each runs inside the caller's transaction against the
// owner's tree; actorID is who did it, which differs from the owner when a
// member works in a shared folder.

const maxPathDepth = 64

func vaultBoundary() error {
	return apiError{409, "Items can’t be moved or copied into or out of a vault.", "VAULT_BOUNDARY"}
}

func nameExists(existing string) error {
	return apiError{409, fmt.Sprintf("“%s” already exists here.", existing), "NAME_EXISTS"}
}

func notInTrash() error { return apiError{404, "Not in the trash", "NOT_FOUND"} }

type child struct{ id, name, kind string }

// childByName finds the live item of that name in a folder, ignoring case and
// the node ignoreID.
func childByName(tx *sql.Tx, parentID, name, ignoreID string) (child, bool, error) {
	var found child
	err := tx.QueryRow(`SELECT id,name,type FROM nodes WHERE parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL AND id<>?`, parentID, name, ignoreID).Scan(&found.id, &found.name, &found.kind)
	if errors.Is(err, sql.ErrNoRows) {
		return child{}, false, nil
	}
	return found, err == nil, err
}

func nodeByID(tx *sql.Tx, ownerID int64, id string) (node, error) {
	return scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, ownerID))
}

// containingVault is the vault a node sits in, read from its parent: a vault's
// own folder sits outside it and may move like any folder.
func containingVault(tx *sql.Tx, n node) (sql.NullString, error) {
	var vault sql.NullString
	if n.ParentID == nil {
		return vault, nil
	}
	err := tx.QueryRow(`SELECT vault_id FROM nodes WHERE id=?`, *n.ParentID).Scan(&vault)
	return vault, err
}

// createFolderTx makes a folder in parent. With conflict "replace" an existing
// folder of that name is returned as it is, "rename" picks a free name.
func (h *Handler) createFolderTx(tx *sql.Tx, ownerID, actorID int64, parent node, rawName, conflict string) (node, bool, error) {
	name, err := validateName(rawName)
	if err != nil {
		return node{}, false, err
	}
	existing, found, err := childByName(tx, parent.ID, name, "")
	if err != nil {
		return node{}, false, err
	}
	if found {
		if conflict == "replace" && existing.kind == "folder" {
			n, err := nodeByID(tx, ownerID, existing.id)
			return n, false, err
		}
		if conflict != "rename" {
			return node{}, false, nameExists(existing.name)
		}
		if name, err = h.freeName(tx, parent.ID, name, ""); err != nil {
			return node{}, false, err
		}
	}
	if err = h.checkNodeRoom(tx, ownerID, 1); err != nil {
		return node{}, false, err
	}
	id := randomID()
	if _, err = tx.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES(?,?,?,'folder',?,?,?)`, id, ownerID, parent.ID, name, randomID(), parent.VaultID); err != nil {
		return node{}, false, err
	}
	if err = recordChange(tx, ownerID, id, "create", parent.ID); err != nil {
		return node{}, false, err
	}
	if err = recordActivity(tx, ownerID, actorID, "folder", id, nil); err != nil {
		return node{}, false, err
	}
	if err = touchFolder(tx, parent.ID); err != nil {
		return node{}, false, err
	}
	created, err := nodeByID(tx, ownerID, id)
	return created, true, err
}

func pathParts(relative string) ([]string, error) {
	parts := strings.FieldsFunc(relative, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return nil, apiError{400, "path is empty.", "PATH_EMPTY"}
	}
	if len(parts) > maxPathDepth {
		return nil, apiError{400, "path is too deep.", "PATH_TOO_DEEP"}
	}
	return parts, nil
}

// ensurePathTx makes sure every folder along relative ("a/b/c") exists below
// parent and returns the deepest one.
func (h *Handler) ensurePathTx(tx *sql.Tx, ownerID, actorID int64, parent node, relative string) (node, error) {
	parts, err := pathParts(relative)
	if err != nil {
		return node{}, err
	}
	current := parent
	for _, part := range parts {
		next, _, err := h.createFolderTx(tx, ownerID, actorID, current, part, "replace")
		if err != nil {
			return node{}, err
		}
		current = next
	}
	return current, nil
}

// Moved is where an UpdateTx left a node.
type Moved struct {
	Name             string
	ParentID         string
	PreviousParentID string
}

// UpdateTx renames and/or moves an owner's live node inside the caller's
// transaction, with the same journal and activity entries as PATCH
// /api/nodes/{id}. A nil name or parent keeps it. A taken name is numbered when
// numbered is set and refused otherwise.
func (h *Handler) UpdateTx(tx *sql.Tx, userID int64, nodeID string, newName, newParent *string, numbered bool) (Moved, error) {
	n, err := h.live(tx, userID, nodeID)
	if err != nil {
		return Moved{}, err
	}
	return h.updateTx(tx, userID, userID, n, newName, newParent, numbered)
}

func (h *Handler) updateTx(tx *sql.Tx, ownerID, actorID int64, n node, newName, newParent *string, numbered bool) (Moved, error) {
	if n.ParentID == nil {
		return Moved{}, apiError{400, "The root folder can’t be renamed or moved.", "ROOT_IMMUTABLE"}
	}
	previous, target := *n.ParentID, *n.ParentID
	if newParent != nil {
		parent, err := h.live(tx, ownerID, *newParent)
		if err != nil {
			return Moved{}, err
		}
		if parent.Type != "folder" {
			return Moved{}, apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
		}
		var inside int
		if err = tx.QueryRow(`WITH RECURSIVE up(id,parent_id) AS(SELECT id,parent_id FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT p.id,p.parent_id FROM nodes p JOIN up ON p.id=up.parent_id) SELECT COUNT(*) FROM up WHERE id=?`, parent.ID, ownerID, n.ID).Scan(&inside); err != nil {
			return Moved{}, err
		}
		if inside > 0 {
			return Moved{}, apiError{400, "A folder can’t be moved into itself.", "MOVE_INTO_SELF"}
		}
		vault, err := containingVault(tx, n)
		if err != nil {
			return Moved{}, err
		}
		if (parent.VaultID == nil) == vault.Valid || parent.VaultID != nil && *parent.VaultID != vault.String {
			return Moved{}, vaultBoundary()
		}
		target = parent.ID
	}
	name := n.Name
	if newName != nil {
		var err error
		if name, err = validateName(*newName); err != nil {
			return Moved{}, err
		}
	}
	result := Moved{Name: name, ParentID: target, PreviousParentID: previous}
	moved := target != previous
	if !moved && name == n.Name {
		return result, nil
	}
	existing, found, err := childByName(tx, target, name, n.ID)
	if err != nil {
		return Moved{}, err
	}
	if found {
		if !numbered {
			return Moved{}, nameExists(existing.name)
		}
		if name, err = h.freeName(tx, target, name, n.ID); err != nil {
			return Moved{}, err
		}
		result.Name = name
	}
	if _, err = tx.Exec(`UPDATE nodes SET parent_id=?,name=?,mime=CASE WHEN type='file' THEN ? ELSE NULL END WHERE id=? AND user_id=?`, target, name, mimeForName(name), n.ID, ownerID); err != nil {
		return Moved{}, err
	}
	op, folders, detail := "move", []string{previous, target}, map[string]any(nil)
	if !moved {
		op, folders, detail = "rename", []string{target}, map[string]any{"from": n.Name}
	}
	if err = recordChange(tx, ownerID, n.ID, op, folders...); err != nil {
		return Moved{}, err
	}
	if err = recordActivity(tx, ownerID, actorID, op, n.ID, detail); err != nil {
		return Moved{}, err
	}
	if err = touchFolder(tx, target); err != nil {
		return Moved{}, err
	}
	if moved {
		err = touchFolder(tx, previous)
	}
	return result, err
}

// EnsureFolderTx returns the child folder of that name, creating it when there
// is none.
func (h *Handler) EnsureFolderTx(tx *sql.Tx, userID int64, parentID, rawName string) (string, bool, error) {
	parent, err := h.live(tx, userID, parentID)
	if err != nil {
		return "", false, err
	}
	if parent.Type != "folder" {
		return "", false, apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
	}
	folder, created, err := h.createFolderTx(tx, userID, userID, parent, rawName, "replace")
	return folder.ID, created, err
}

// TrashTx moves an owner's live node to the trash and returns its folder.
func (h *Handler) TrashTx(tx *sql.Tx, userID int64, nodeID string) (string, error) {
	n, err := h.live(tx, userID, nodeID)
	if err != nil {
		return "", err
	}
	return h.trashTx(tx, userID, userID, n)
}

func (h *Handler) trashTx(tx *sql.Tx, ownerID, actorID int64, n node) (string, error) {
	if n.ParentID == nil {
		return "", apiError{400, "The root folder can’t be deleted.", "ROOT_IMMUTABLE"}
	}
	if _, err := tx.Exec(`UPDATE nodes SET trashed_at=datetime('now') WHERE id=? AND user_id=?`, n.ID, ownerID); err != nil {
		return "", err
	}
	if err := recordChange(tx, ownerID, n.ID, "trash", *n.ParentID); err != nil {
		return "", err
	}
	if err := recordActivity(tx, ownerID, actorID, "trash", n.ID, nil); err != nil {
		return "", err
	}
	return *n.ParentID, touchFolder(tx, *n.ParentID)
}

// restoreTx puts a trashed node back where it was. If that folder is gone or
// itself in the trash it lands in the root, and a taken name gets "(1)".
func (h *Handler) restoreTx(tx *sql.Tx, ownerID, actorID int64, nodeID string) (node, error) {
	n, err := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=? AND n.trashed_at IS NOT NULL`, nodeID, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return node{}, notInTrash()
	}
	if err != nil {
		return node{}, err
	}
	parentID := ""
	if n.ParentID != nil {
		_, err = h.live(tx, ownerID, *n.ParentID)
		var missing apiError
		switch {
		case err == nil:
			parentID = *n.ParentID
		case !errors.As(err, &missing):
			return node{}, err
		}
	}
	if parentID == "" {
		// Ciphertext can't land in the root: the vault folder comes back first.
		if n.ParentID != nil && n.VaultID != nil {
			var vaultFolder bool
			if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM vaults WHERE folder_id=?)`, n.ID).Scan(&vaultFolder); err != nil {
				return node{}, err
			}
			if !vaultFolder {
				return node{}, apiError{409, "Restore the vault folder first.", "VAULT_BOUNDARY"}
			}
		}
		root, err := h.root(tx, ownerID)
		if err != nil {
			return node{}, err
		}
		parentID = root.ID
	}
	name, err := h.freeName(tx, parentID, n.Name, n.ID)
	if err != nil {
		return node{}, err
	}
	if _, err = tx.Exec(`UPDATE nodes SET trashed_at=NULL,parent_id=?,name=? WHERE id=? AND user_id=?`, parentID, name, n.ID, ownerID); err != nil {
		return node{}, err
	}
	if err = recordChange(tx, ownerID, n.ID, "restore", parentID); err != nil {
		return node{}, err
	}
	if err = touchFolder(tx, parentID); err != nil {
		return node{}, err
	}
	if err = recordActivity(tx, ownerID, actorID, "restore", n.ID, nil); err != nil {
		return node{}, err
	}
	return nodeByID(tx, ownerID, n.ID)
}

// NodeViews are the API representations of the owner's nodes among ids,
// trashed ones included, by id.
func (h *Handler) NodeViews(ctx context.Context, userID int64, ids []string) (map[string]any, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, nodeSelect+` WHERE n.user_id=? AND n.id IN (SELECT value FROM json_each(?))`, userID, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := map[string]any{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		views[n.ID] = n
	}
	return views, rows.Err()
}

// FreeName is the first of name, "name (1)", … not taken in a folder by
// anything but ignoreID.
func (h *Handler) FreeName(ctx context.Context, parentID, name, ignoreID string) (string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	return h.freeName(tx, parentID, name, ignoreID)
}
