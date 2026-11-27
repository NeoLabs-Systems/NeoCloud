package files

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type copySource struct {
	id, parentID, kind, name, mime, blobID, etag string
	size                                         int64
	vaultID                                      sql.NullString
	depth                                        int
}

func (h *Handler) copyAcrossAccounts(ctx context.Context, actorID int64, source node, sourceOwner int64, target node, targetOwner int64, targetAccess *sharedReadAccess, requestedName *string, mode string) (node, error) {
	policy := h.storagePolicy()
	if source.ParentID == nil {
		return node{}, apiError{400, "The root folder can’t be copied.", "ROOT_IMMUTABLE"}
	}
	if source.VaultID != nil || target.VaultID != nil {
		return node{}, vaultBoundary()
	}
	name := source.Name
	var err error
	if requestedName != nil {
		name, err = validateName(*requestedName)
		if err != nil {
			return node{}, err
		}
	}
	if target.Type != "folder" {
		return node{}, apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
	}

	// Load the visible source subtree and enforce destination capacity before
	// decrypting or writing any content.
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return node{}, err
	}
	if _, access, e := h.resolveReadNode(tx, actorID, source.ID); e != nil || ownerFromAccess(actorID, access) != sourceOwner {
		tx.Rollback()
		if e != nil {
			return node{}, e
		}
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	resolvedTarget, targetGrant, err := h.resolveReadNode(tx, actorID, target.ID)
	if err != nil {
		tx.Rollback()
		return node{}, err
	}
	resolvedTargetOwner := actorID
	if targetGrant != nil {
		if targetGrant.permission != "write" {
			tx.Rollback()
			return node{}, readOnlyShare()
		}
		resolvedTargetOwner = targetGrant.ownerID
	}
	if resolvedTargetOwner != targetOwner {
		tx.Rollback()
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	target = resolvedTarget

	maxCopy := h.maxCopyNodes
	if maxCopy <= 0 {
		maxCopy = 50_000
	}
	rows, err := tx.Query(`WITH RECURSIVE tree(id,parent_id,type,name,size,mime,blob_id,etag,vault_id,depth) AS (
		SELECT id,parent_id,type,name,size,mime,blob_id,etag,vault_id,0 FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL
		UNION ALL SELECT n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.blob_id,n.etag,n.vault_id,t.depth+1 FROM nodes n JOIN tree t ON n.parent_id=t.id WHERE n.user_id=? AND n.trashed_at IS NULL
	) SELECT id,parent_id,type,name,size,mime,blob_id,etag,vault_id,depth FROM tree WHERE vault_id IS NULL ORDER BY depth,id LIMIT ?`, source.ID, sourceOwner, sourceOwner, maxCopy+1)
	if err != nil {
		tx.Rollback()
		return node{}, err
	}
	nodes := make([]copySource, 0)
	var totalBytes int64
	for rows.Next() {
		var old copySource
		var parent, mime, blob sql.NullString
		if err = rows.Scan(&old.id, &parent, &old.kind, &old.name, &old.size, &mime, &blob, &old.etag, &old.vaultID, &old.depth); err != nil {
			rows.Close()
			tx.Rollback()
			return node{}, err
		}
		if parent.Valid {
			old.parentID = parent.String
		}
		if mime.Valid {
			old.mime = mime.String
		}
		if blob.Valid {
			old.blobID = blob.String
		}
		nodes = append(nodes, old)
		if old.kind == "file" {
			if old.size > int64(^uint64(0)>>1)-totalBytes {
				rows.Close()
				tx.Rollback()
				return node{}, apiError{413, "Not enough storage left for this copy.", "QUOTA_EXCEEDED"}
			}
			totalBytes += old.size
			if policy.MaxUploadBytes > 0 && old.size > policy.MaxUploadBytes {
				rows.Close()
				tx.Rollback()
				return node{}, apiError{413, "This file is larger than the server allows.", "FILE_TOO_LARGE"}
			}
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		tx.Rollback()
		return node{}, err
	}
	rows.Close()
	if len(nodes) == 0 || nodes[0].id != source.ID {
		tx.Rollback()
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	if len(nodes) > maxCopy {
		tx.Rollback()
		return node{}, copyTooLarge(maxCopy)
	}
	var quota sql.NullInt64
	var used, reserved int64
	if err = tx.QueryRow(`SELECT quota_bytes,used_bytes FROM users WHERE id=?`, targetOwner).Scan(&quota, &used); err == nil {
		err = tx.QueryRow(`SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND datetime(expires_at)>datetime('now')`, targetOwner).Scan(&reserved)
	}
	quotaBytes := policy.DefaultQuotaBytes
	if quota.Valid {
		quotaBytes = quota.Int64
	}
	if err != nil {
		tx.Rollback()
		return node{}, err
	}
	if quotaBytes >= 0 && (totalBytes > quotaBytes-used-reserved) {
		tx.Rollback()
		return node{}, apiError{413, "Not enough storage left for this copy.", "QUOTA_EXCEEDED"}
	}
	addNodes := len(nodes)
	if source.Type == "file" {
		addNodes = 1
	}
	if mode == "replace" {
		var existingType string
		e := tx.QueryRow(`SELECT type FROM nodes WHERE user_id=? AND parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, targetOwner, target.ID, name).Scan(&existingType)
		if e == nil {
			if source.Type == "folder" && existingType == "folder" {
				addNodes--
			} else if source.Type == "file" && existingType == "file" {
				addNodes = 0
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			tx.Rollback()
			return node{}, e
		}
	}
	if err = h.checkNodeRoom(tx, targetOwner, addNodes); err != nil {
		tx.Rollback()
		return node{}, err
	}
	if err = tx.Commit(); err != nil {
		return node{}, err
	}

	if err = h.CheckDiskSpace(totalBytes); err != nil {
		return node{}, err
	}

	// Decrypt and re-encrypt every file before changing the destination tree.
	// A damaged source or failed write therefore leaves no partial folders.
	staged := make(map[string]uploadedBlob)
	placed := make([]string, 0, len(nodes))
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, path := range placed {
			_ = os.Remove(path)
		}
		for _, blob := range staged {
			if blob.tempPath != "" {
				_ = os.Remove(blob.tempPath)
			}
		}
	}()
	for _, old := range nodes {
		if old.kind != "file" {
			continue
		}
		blob, e := h.reencryptCopyBlob(ctx, sourceOwner, targetOwner, old)
		if e != nil {
			return node{}, e
		}
		staged[old.id] = blob
	}
	for id, blob := range staged {
		final, e := h.placeUpload(targetOwner, blob)
		if e != nil {
			return node{}, e
		}
		blob.finalPath = final
		blob.tempPath = ""
		staged[id] = blob
		placed = append(placed, final)
	}
	if source.Type == "file" {
		checkTx, e := h.db.BeginTx(ctx, nil)
		if e != nil {
			return node{}, e
		}
		if _, access, sourceErr := h.resolveReadNode(checkTx, actorID, source.ID); sourceErr != nil || ownerFromAccess(actorID, access) != sourceOwner {
			checkTx.Rollback()
			if sourceErr != nil {
				return node{}, sourceErr
			}
			return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
		}
		if e = checkTx.Commit(); e != nil {
			return node{}, e
		}
		blob := staged[source.ID]
		created, orphans, e := h.commitUpload(ctx, targetOwner, actorID, target.ID, name, mode, "", "", blob, nil, nil)
		if e != nil {
			return node{}, e
		}
		h.RemoveBlobFiles(targetOwner, orphans)
		committed = true
		return created, nil
	}

	tx, err = h.db.BeginTx(ctx, nil)
	if err != nil {
		return node{}, err
	}
	defer tx.Rollback()
	if _, access, e := h.resolveReadNode(tx, actorID, source.ID); e != nil || ownerFromAccess(actorID, access) != sourceOwner {
		if e != nil {
			return node{}, e
		}
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}
	parent, targetGrant, err := h.resolveReadNode(tx, actorID, target.ID)
	if err != nil {
		return node{}, err
	}
	if parent.Type != "folder" {
		return node{}, apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
	}
	if targetGrant != nil && (targetGrant.ownerID != targetOwner || targetGrant.permission != "write") {
		return node{}, apiError{404, "File or folder not found", "NOT_FOUND"}
	}

	newIDs := make(map[string]string, len(nodes))
	var created node
	for _, old := range nodes {
		newParent, newName := parent.ID, old.name
		if old.id != source.ID {
			var found bool
			newParent, found = newIDs[old.parentID]
			if !found {
				return node{}, apiError{409, "The folder contains a protected vault boundary.", "VAULT_BOUNDARY"}
			}
		}
		if old.id == source.ID {
			newName = name
		}
		var existingID, existingType string
		e := tx.QueryRow(`SELECT id,type FROM nodes WHERE user_id=? AND parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, targetOwner, newParent, newName).Scan(&existingID, &existingType)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return node{}, e
		}
		if old.id == source.ID && e == nil {
			if mode == "rename" {
				newName, err = h.freeName(tx, newParent, newName, "")
				if err != nil {
					return node{}, err
				}
			} else if mode == "replace" && old.kind == "folder" && existingType == "folder" {
				newIDs[old.id] = existingID
				created, err = scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, existingID, targetOwner))
				if err != nil {
					return node{}, err
				}
				continue
			} else {
				return node{}, apiError{409, "That name already exists here.", "NAME_EXISTS"}
			}
		} else if old.id != source.ID && e == nil {
			newName, err = h.freeName(tx, newParent, newName, "")
			if err != nil {
				return node{}, err
			}
		}
		id := randomID()
		var etag string
		var mime, blobID any
		var size int64
		if old.kind == "folder" {
			etag = randomID()
		} else {
			blob := staged[old.id]
			if err = h.recordBlobTx(tx, targetOwner, blob, ""); err != nil {
				return node{}, err
			}
			size, mime, blobID, etag = blob.size, mimeForName(newName), blob.id, blob.hash
		}
		if err = h.checkNodeRoom(tx, targetOwner, 1); err != nil {
			return node{}, err
		}
		if _, err = tx.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag,vault_id) VALUES(?,?,?,?,?,?,?,?,?,NULL)`, id, targetOwner, newParent, old.kind, newName, size, mime, blobID, etag); err != nil {
			return node{}, err
		}
		if err = recordChange(tx, targetOwner, id, "create", newParent); err != nil {
			return node{}, err
		}
		kind := "folder"
		if old.kind == "file" {
			kind = "upload"
		}
		if err = recordActivity(tx, targetOwner, actorID, kind, id, nil); err != nil {
			return node{}, err
		}
		if err = touchFolder(tx, newParent); err != nil {
			return node{}, err
		}
		newIDs[old.id] = id
		if old.id == source.ID {
			created, err = scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, targetOwner))
			if err != nil {
				return node{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return node{}, err
	}
	committed = true
	return created, nil
}

func copyTooLarge(limit int) error {
	return apiError{413, fmt.Sprintf("A copy can hold at most %d files and folders.", limit), "COPY_TOO_LARGE"}
}

func ownerFromAccess(viewer int64, access *sharedReadAccess) int64 {
	if access == nil {
		return viewer
	}
	return access.ownerID
}

func (h *Handler) reencryptCopyBlob(ctx context.Context, sourceOwner, targetOwner int64, source copySource) (uploadedBlob, error) {
	path := blobPath(h.blobDir, sourceOwner, source.blobID)
	file, err := os.Open(path)
	if err != nil {
		return uploadedBlob{}, err
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, sourceOwner)
	if err != nil {
		file.Close()
		return uploadedBlob{}, err
	}
	reader, writer := io.Pipe()
	decryptDone := make(chan error, 1)
	go func() {
		var decryptErr error
		if source.size == 0 {
			_, decryptErr = cryptox.DecryptBlobRange(file, 0, 0, 0, dek, source.blobID, writer)
		} else {
			_, decryptErr = cryptox.DecryptBlobRange(file, source.size, 0, source.size-1, dek, source.blobID, writer)
		}
		_ = file.Close()
		_ = writer.CloseWithError(decryptErr)
		decryptDone <- decryptErr
	}()
	blob, encryptErr := h.encryptStream(ctx, targetOwner, reader, randomID(), source.size+1, source.size, apiError{413, "Not enough storage left for this copy.", "QUOTA_EXCEEDED"})
	_ = reader.CloseWithError(encryptErr)
	decryptErr := <-decryptDone
	if encryptErr != nil {
		return uploadedBlob{}, encryptErr
	}
	if decryptErr != nil {
		_ = os.Remove(blob.tempPath)
		return uploadedBlob{}, fmt.Errorf("decrypt source file for copy: %w", decryptErr)
	}
	return blob, nil
}

// copyNode clones a subtree without decrypting or duplicating its encrypted
// blobs. Each copied file acquires a new blob reference in the same transaction
// as its new node, so quota bytes remain unchanged until one side is replaced.
func (h *Handler) copyNode(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.auth(w, r)
	if !ok {
		return
	}
	uid := actorID
	var body struct {
		ParentID *string `json:"parentId"`
		Name     *string `json:"name"`
		Conflict string  `json:"conflict"`
	}
	if !decode(w, r, &body) {
		return
	}
	mode := body.Conflict
	if mode == "" {
		mode = "rename"
	}
	if mode != "fail" && mode != "rename" && mode != "replace" {
		writeErr(w, 400, "conflict must be fail, rename or replace.", "CONFLICT_MODE_INVALID")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	source, sourceAccess, err := h.resolveReadNode(tx, uid, r.PathValue("id"))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if source.ParentID == nil {
		failTx(w, tx, apiError{400, "The root folder can’t be copied.", "ROOT_IMMUTABLE"})
		return
	}
	parentID := *source.ParentID
	if body.ParentID != nil && *body.ParentID != "" {
		parentID = *body.ParentID
	}
	target, targetAccess, err := h.resolveReadNode(tx, uid, parentID)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	sourceOwner := uid
	if sourceAccess != nil {
		sourceOwner = sourceAccess.ownerID
	}
	targetOwner := uid
	if targetAccess != nil {
		if targetAccess.permission != "write" {
			failTx(w, tx, readOnlyShare())
			return
		}
		targetOwner = targetAccess.ownerID
	}
	if sourceOwner != targetOwner {
		if target.Type != "folder" {
			failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, err)
			return
		}
		created, copyErr := h.copyAcrossAccounts(r.Context(), actorID, source, sourceOwner, target, targetOwner, targetAccess, body.Name, mode)
		if copyErr != nil {
			fail(w, copyErr)
			return
		}
		if targetAccess != nil {
			httpx.WriteJSON(w, http.StatusCreated, memberNodeView(created, targetAccess.rootID))
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
		return
	}
	uid = sourceOwner
	if target.Type != "folder" {
		failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
		return
	}
	if source.Type == "folder" {
		var self int
		err = tx.QueryRow(`WITH RECURSIVE up(id,parent_id) AS(SELECT id,parent_id FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id,n.parent_id FROM nodes n JOIN up ON n.id=up.parent_id) SELECT COUNT(*) FROM up WHERE id=?`, target.ID, uid, source.ID).Scan(&self)
		if err != nil {
			failTx(w, tx, err)
			return
		}
		if self > 0 {
			failTx(w, tx, apiError{400, "A folder can’t be copied into itself.", "COPY_INTO_SELF"})
			return
		}
	}
	if !sameOptionalID(source.VaultID, target.VaultID) {
		failTx(w, tx, vaultBoundary())
		return
	}
	name := source.Name
	if body.Name != nil {
		name, err = validateName(*body.Name)
		if err != nil {
			failTx(w, tx, err)
			return
		}
	}
	existing, found, err := childByName(tx, target.ID, name, "")
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if found {
		if mode != "rename" {
			failTx(w, tx, nameExists(existing.name))
			return
		}
		if name, err = h.freeName(tx, target.ID, name, ""); err != nil {
			failTx(w, tx, err)
			return
		}
	}
	created, err := h.copyTreeTx(tx, uid, actorID, source, target, name)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	if targetAccess != nil {
		httpx.WriteJSON(w, http.StatusCreated, memberNodeView(created, targetAccess.rootID))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

// copyTreeTx copies source (a file, or a folder with everything outside nested
// vaults) into target under name. Copies share their blobs.
func (h *Handler) copyTreeTx(tx *sql.Tx, uid, actorID int64, source, target node, name string) (node, error) {
	var err error
	maxCopy := h.maxCopyNodes
	if maxCopy <= 0 {
		maxCopy = 50_000
	}
	if source.Type == "folder" {
		var count int
		err = tx.QueryRow(`WITH RECURSIVE tree(id) AS (SELECT id FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL UNION ALL SELECT n.id FROM nodes n JOIN tree t ON n.parent_id=t.id WHERE n.user_id=? AND n.trashed_at IS NULL LIMIT ?) SELECT COUNT(*) FROM tree`, source.ID, uid, uid, maxCopy+1).Scan(&count)
		if err != nil {
			return node{}, err
		}
		if count > maxCopy {
			return node{}, copyTooLarge(maxCopy)
		}
	}
	rows, err := tx.Query(`WITH RECURSIVE tree(id,parent_id,type,name,size,mime,blob_id,etag,vault_id,depth) AS (SELECT id,parent_id,type,name,size,mime,blob_id,etag,vault_id,0 FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL UNION ALL SELECT n.id,n.parent_id,n.type,n.name,n.size,n.mime,n.blob_id,n.etag,n.vault_id,t.depth+1 FROM nodes n JOIN tree t ON n.parent_id=t.id WHERE n.user_id=? AND n.trashed_at IS NULL) SELECT id,parent_id,type,name,size,mime,blob_id,etag,vault_id,depth FROM tree WHERE vault_id IS ? ORDER BY depth,id`, source.ID, uid, uid, source.VaultID)
	if err != nil {
		return node{}, err
	}
	nodes := []copySource{}
	for rows.Next() {
		var n copySource
		var parent, mime, blob sql.NullString
		if err = rows.Scan(&n.id, &parent, &n.kind, &n.name, &n.size, &mime, &blob, &n.etag, &n.vaultID, &n.depth); err != nil {
			rows.Close()
			return node{}, err
		}
		if parent.Valid {
			n.parentID = parent.String
		}
		if mime.Valid {
			n.mime = mime.String
		}
		if blob.Valid {
			n.blobID = blob.String
		}
		nodes = append(nodes, n)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return node{}, err
	}
	rows.Close()
	if err = h.checkNodeRoom(tx, uid, len(nodes)); err != nil {
		return node{}, err
	}
	newIDs := make(map[string]string, len(nodes))
	var created node
	for _, old := range nodes {
		newParent := target.ID
		newName := old.name
		if old.id != source.ID {
			var found bool
			newParent, found = newIDs[old.parentID]
			if !found {
				return node{}, apiError{409, "The folder contains a protected vault boundary.", "VAULT_BOUNDARY"}
			}
		} else {
			newName = name
		}
		id := randomID()
		etag := old.etag
		if old.kind == "folder" {
			etag = randomID()
		}
		var mime any
		if old.kind == "file" {
			mime = mimeForName(newName)
		}
		_, err = tx.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag,vault_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, uid, newParent, old.kind, newName, old.size, mime, nullString(old.blobID), etag, nullDBString(target.VaultID))
		if err != nil {
			return node{}, err
		}
		if old.blobID != "" {
			result, e := tx.Exec(`UPDATE blobs SET ref_count=ref_count+1 WHERE id=? AND user_id=?`, old.blobID, uid)
			if e != nil {
				return node{}, e
			}
			if count, _ := result.RowsAffected(); count != 1 {
				return node{}, apiError{409, "The file’s stored data is unavailable.", "BLOB_NOT_FOUND"}
			}
		}
		if err = recordChange(tx, uid, id, "create", newParent); err != nil {
			return node{}, err
		}
		newIDs[old.id] = id
		if old.id == source.ID {
			created, err = scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, uid))
			if err != nil {
				return node{}, err
			}
		}
	}
	if err = recordActivity(tx, uid, actorID, "copy", created.ID, nil); err != nil {
		return node{}, err
	}
	if err = touchFolder(tx, target.ID); err != nil {
		return node{}, err
	}
	return created, nil
}

func sameOptionalID(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func nullDBString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
