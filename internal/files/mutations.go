package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// writeNode answers with a node the way the caller may see it.
func writeNode(w http.ResponseWriter, status int, n node, shared *sharedReadAccess) {
	if shared != nil {
		httpx.WriteJSON(w, status, memberNodeView(n, shared.rootID))
		return
	}
	httpx.WriteJSON(w, status, n)
}

// writeTarget resolves a node the caller wants to change, in their own tree or
// in a folder shared with them for writing, and the tree it belongs to.
func (h *Handler) writeTarget(tx *sql.Tx, userID int64, id string) (node, *sharedReadAccess, int64, error) {
	n, shared, err := h.resolveReadNode(tx, userID, id)
	if err != nil {
		return node{}, nil, 0, err
	}
	if shared == nil {
		return n, nil, userID, nil
	}
	if shared.permission != "write" {
		return node{}, nil, 0, readOnlyShare()
	}
	return n, shared, shared.ownerID, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	var b struct {
		ParentID *string `json:"parentId"`
		Name     string  `json:"name"`
		Path     string  `json:"path"`
		Conflict string  `json:"conflict"`
	}
	if !decode(w, r, &b) {
		return
	}
	parentID := "root"
	if b.ParentID != nil {
		parentID = *b.ParentID
	}
	var created node
	var shared *sharedReadAccess
	err := h.inTx(r.Context(), func(tx *sql.Tx) error {
		parent, access, owner, err := h.writeTarget(tx, uid, parentID)
		if err != nil {
			return err
		}
		if parent.Type != "folder" {
			return apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
		}
		shared = access
		if b.Path != "" {
			created, err = h.ensurePathTx(tx, owner, uid, parent, b.Path)
			return err
		}
		if err = validateConflictMode(b.Conflict); err != nil {
			return err
		}
		created, _, err = h.createFolderTx(tx, owner, uid, parent, b.Name, b.Conflict)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeNode(w, http.StatusCreated, created, shared)
}

// freeName is name, or the first "name (n)" free in the folder.
func (h *Handler) freeName(tx *sql.Tx, parentID, name, ignoreID string) (string, error) {
	stem, extension := splitExtension(name)
	stem = strings.TrimSuffix(stem, numberedSuffix(stem))
	candidate := name
	for number := 1; number <= 10000; number++ {
		_, taken, err := childByName(tx, parentID, candidate, ignoreID)
		if err != nil || !taken {
			return candidate, err
		}
		candidate = fmt.Sprintf("%s (%d)%s", stem, number, extension)
	}
	return "", apiError{409, "Too many items with that name.", "NAME_EXISTS"}
}

// splitExtension splits "report.final.pdf" before ".pdf"; dotfiles keep their
// name.
func splitExtension(name string) (string, string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return name, ""
	}
	return name[:index], name[index:]
}

func numberedSuffix(s string) string {
	i := strings.LastIndex(s, " (")
	if i < 0 || !strings.HasSuffix(s, ")") {
		return ""
	}
	if _, e := strconv.ParseUint(s[i+2:len(s)-1], 10, 64); e == nil {
		return s[i:]
	}
	return ""
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	var b struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parentId"`
		Conflict string  `json:"conflict"`
	}
	if !decode(w, r, &b) {
		return
	}
	if err := validateConflictMode(b.Conflict); err != nil {
		fail(w, err)
		return
	}
	var updated node
	var shared *sharedReadAccess
	err := h.inTx(r.Context(), func(tx *sql.Tx) error {
		n, access, owner, err := h.writeTarget(tx, uid, r.PathValue("id"))
		if err != nil {
			return err
		}
		shared = access
		if access != nil && n.ID == access.writeRootID {
			return shareRootImmutable()
		}
		parentID := b.ParentID
		if parentID != nil && access != nil && *parentID == "" {
			return shareBoundary()
		}
		if parentID != nil {
			// A move stays inside the caller's own tree or inside one share.
			target, targetAccess, _, err := h.writeTarget(tx, uid, *parentID)
			if err != nil {
				return err
			}
			if (access == nil) != (targetAccess == nil) || access != nil && (targetAccess.ownerID != owner || targetAccess.writeRootID != access.writeRootID) {
				return shareBoundary()
			}
			parentID = &target.ID
		}
		if _, err = h.updateTx(tx, owner, uid, n, b.Name, parentID, b.Conflict == "rename"); err != nil {
			return err
		}
		updated, err = nodeByID(tx, owner, n.ID)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeNode(w, http.StatusOK, updated, shared)
}

func mimeForName(name string) any {
	_, extension := splitExtension(name)
	value, ok := mimeTypes[strings.ToLower(extension)]
	if !ok {
		value = "application/octet-stream"
	}
	return value
}

var mimeTypes = map[string]string{
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".log": "text/plain",
	".json": "application/json", ".xml": "application/xml", ".yaml": "text/yaml", ".yml": "text/yaml",
	".html": "text/html", ".htm": "text/html", ".css": "text/css", ".js": "text/javascript", ".ts": "text/plain", ".dart": "text/plain", ".py": "text/x-python", ".sh": "text/x-shellscript",
	".pdf": "application/pdf", ".doc": "application/msword", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".xls": "application/vnd.ms-excel", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".ppt": "application/vnd.ms-powerpoint", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".odt": "application/vnd.oasis.opendocument.text", ".ods": "application/vnd.oasis.opendocument.spreadsheet", ".odp": "application/vnd.oasis.opendocument.presentation", ".rtf": "application/rtf",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml", ".heic": "image/heic", ".heif": "image/heif", ".bmp": "image/bmp", ".tif": "image/tiff", ".tiff": "image/tiff", ".avif": "image/avif",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav", ".flac": "audio/flac", ".ogg": "audio/ogg", ".opus": "audio/opus",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".mkv": "video/x-matroska", ".avi": "video/x-msvideo",
	".zip": "application/zip", ".gz": "application/gzip", ".tar": "application/x-tar", ".7z": "application/x-7z-compressed", ".rar": "application/vnd.rar",
}

func (h *Handler) trash(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	err := h.inTx(r.Context(), func(tx *sql.Tx) error {
		n, shared, owner, err := h.writeTarget(tx, uid, r.PathValue("id"))
		if err != nil {
			return err
		}
		if shared != nil && n.ID == shared.writeRootID {
			return shareRootImmutable()
		}
		_, err = h.trashTx(tx, owner, uid, n)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}

// restore brings a node back from the caller's trash, or, for a member, what
// was trashed inside a folder shared with them for writing, into that folder.
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var restored node
	var shared *sharedReadAccess
	err := h.inTx(r.Context(), func(tx *sql.Tx) error {
		var owner int64
		var parentID, trashedAt, vaultID sql.NullString
		err := tx.QueryRow(`SELECT user_id,parent_id,trashed_at,vault_id FROM nodes WHERE id=?`, id).Scan(&owner, &parentID, &trashedAt, &vaultID)
		if errors.Is(err, sql.ErrNoRows) {
			return notInTrash()
		}
		if err != nil {
			return err
		}
		if owner != uid {
			if !trashedAt.Valid || vaultID.Valid || !parentID.Valid {
				return notInTrash()
			}
			if _, shared, _, err = h.writeTarget(tx, uid, parentID.String); err != nil {
				return err
			}
			if shared == nil {
				return notInTrash()
			}
		}
		restored, err = h.restoreTx(tx, owner, uid, id)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeNode(w, http.StatusOK, restored, shared)
}

type trashItem struct {
	node
	OriginalPath string  `json:"originalPath"`
	ExpiresAt    *string `json:"expiresAt"`
}

func (h *Handler) listTrash(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	retentionDays := h.storagePolicy().TrashRetentionDays
	rows, err := tx.Query(`SELECT id, CASE WHEN ?=0 THEN NULL ELSE datetime(trashed_at, printf('+%d days', ?)) END FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL ORDER BY trashed_at DESC`, retentionDays, retentionDays, uid)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	type trashed struct {
		id      string
		expires sql.NullString
	}
	entries := []trashed{}
	for rows.Next() {
		var entry trashed
		if err = rows.Scan(&entry.id, &entry.expires); err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		entries = append(entries, entry)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	items := make([]trashItem, 0, len(entries))
	for _, entry := range entries {
		n, er := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, entry.id, uid))
		if er != nil {
			failTx(w, tx, er)
			return
		}
		path := "/"
		if n.ParentID != nil {
			chain, e := h.path(tx, uid, *n.ParentID)
			if e != nil {
				failTx(w, tx, e)
				return
			}
			path = pathString(chain)
		}
		var expires *string
		if entry.expires.Valid {
			expires = &entry.expires.String
		}
		items = append(items, trashItem{node: n, OriginalPath: path, ExpiresAt: expires})
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) purge(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	orphans, err := h.purgeOne(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	h.RemoveBlobFiles(uid, orphans)
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}

func (h *Handler) emptyTrash(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL`, uid)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	orphans := []string{}
	for _, id := range ids {
		var exists int
		err = tx.QueryRow(`SELECT 1 FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			failTx(w, tx, err)
			return
		}
		removed, er := purgeSubtreeTx(tx, uid, id)
		if er != nil {
			failTx(w, tx, er)
			return
		}
		orphans = append(orphans, removed...)
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	h.RemoveBlobFiles(uid, orphans)
	httpx.WriteJSON(w, 200, map[string]any{"success": true, "removed": len(ids)})
}

func (h *Handler) purgeOne(ctx context.Context, uid int64, id string) ([]string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var trashed sql.NullString
	if err = tx.QueryRow(`SELECT trashed_at FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&trashed); errors.Is(err, sql.ErrNoRows) || err == nil && !trashed.Valid {
		return nil, apiError{404, "Not in the trash", "NOT_FOUND"}
	} else if err != nil {
		return nil, err
	}
	orphans, err := purgeSubtreeTx(tx, uid, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return orphans, nil
}

func purgeSubtreeTx(tx *sql.Tx, uid int64, id string) ([]string, error) {
	var parentID sql.NullString
	if err := tx.QueryRow(`SELECT parent_id FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&parentID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`WITH RECURSIVE down(id) AS (SELECT id FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id FROM nodes n JOIN down d ON n.parent_id=d.id WHERE n.user_id=?) SELECT n.id,n.blob_id FROM down JOIN nodes n ON n.id=down.id`, id, uid, uid)
	if err != nil {
		return nil, err
	}
	type entry struct {
		id   string
		blob sql.NullString
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.blob); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(entries) == 0 {
		return nil, apiError{404, "Not in the trash", "NOT_FOUND"}
	}
	ids := make([]string, 0, len(entries))
	blobIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.id)
		if entry.blob.Valid {
			blobIDs = append(blobIDs, entry.blob.String)
		}
	}
	encoded, _ := json.Marshal(ids)
	versions, err := tx.Query(`SELECT blob_id FROM file_versions WHERE node_id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	for versions.Next() {
		var id string
		if err = versions.Scan(&id); err != nil {
			versions.Close()
			return nil, err
		}
		blobIDs = append(blobIDs, id)
	}
	if err = versions.Err(); err != nil {
		versions.Close()
		return nil, err
	}
	versions.Close()
	if err = recordChange(tx, uid, id, "delete", parentID.String); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`DELETE FROM nodes WHERE id=? AND user_id=?`, id, uid); err != nil {
		return nil, err
	}
	orphans := []string{}
	for _, blobID := range blobIDs {
		var size int64
		var refs int
		if err = tx.QueryRow(`SELECT size,ref_count FROM blobs WHERE id=? AND user_id=?`, blobID, uid).Scan(&size, &refs); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return nil, err
		}
		if refs > 1 {
			_, err = tx.Exec(`UPDATE blobs SET ref_count=ref_count-1 WHERE id=? AND user_id=?`, blobID, uid)
		} else {
			_, err = tx.Exec(`DELETE FROM blobs WHERE id=? AND user_id=?`, blobID, uid)
			if err == nil {
				_, err = tx.Exec(`UPDATE users SET used_bytes=MAX(0,used_bytes-?) WHERE id=?`, size, uid)
				orphans = append(orphans, blobID)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return orphans, nil
}

// RemoveBlobFiles deletes the encrypted files of blobs whose rows are gone,
// together with every preview made from them. Call it after the commit that
// dropped them; what can't be removed only wastes space.
func (h *Handler) RemoveBlobFiles(uid int64, ids []string) {
	for _, id := range ids {
		if len(id) != 36 || strings.Trim(id, "0123456789abcdef-") != "" {
			continue
		}
		_ = os.Remove(filepath.Join(h.blobDir, strconv.FormatInt(uid, 10), id[:2], id))
		_ = os.RemoveAll(filepath.Join(h.previewDir, strconv.FormatInt(uid, 10), id))
	}
}
