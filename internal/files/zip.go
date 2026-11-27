package files

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	gopath "path"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

const maxZIPSelection = 1000

type zipSelection struct {
	id      string
	ownerID int64
	node    node
}

type zipArchiveEntry struct {
	node    node
	ownerID int64
	blobID  string
	path    string
}

func (h *Handler) downloadZIP(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	ids := parseZIPIDs(r.URL.Query().Get("ids"))
	if len(ids) == 0 {
		fail(w, apiError{http.StatusBadRequest, "Choose at least one file or folder.", "NOTHING_SELECTED"})
		return
	}
	if len(ids) > maxZIPSelection {
		fail(w, apiError{http.StatusBadRequest, "Too many items selected.", "TOO_MANY_ITEMS"})
		return
	}

	_, ownerID, err := h.resolveZIPSelections(r, uid, ids)
	if err != nil {
		fail(w, err)
		return
	}
	var dek []byte
	if r.Method != http.MethodHead {
		dek, err = h.keys.GetUserDEK(r.Context(), h.db, ownerID)
		if err != nil {
			fail(w, err)
			return
		}
	}
	// Recheck the grants and selected nodes after loading the account key. The
	// second transaction then supplies one consistent metadata snapshot while
	// the archive is streamed.
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	selections, err := h.resolveZIPSelectionsTx(tx, uid, ids, ownerID)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	entries, err := h.collectZIPEntries(r.Context(), tx, selections, uid)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	h.sendZIP(w, r, zipDownloadName(selections), entries, dek)
}

func (h *Handler) sendZIP(w http.ResponseWriter, r *http.Request, name string, entries []zipArchiveEntry, dek []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	archive := zip.NewWriter(w)
	for _, entry := range entries {
		if err := h.writeZIPEntry(archive, entry, dek); err != nil {
			abortResponse("ZIP download failed", err)
		}
	}
	if err := archive.Close(); err != nil {
		abortResponse("ZIP download failed", err)
	}
}

// SendSharedZIP streams one item of ownerID with everything live below it
// outside vaults. admit runs once the archive can be built and may refuse it;
// it is skipped for HEAD, which is not a download.
func (h *Handler) SendSharedZIP(w http.ResponseWriter, r *http.Request, ownerID int64, nodeID string, admit func() error) error {
	var selected node
	var entries []zipArchiveEntry
	err := h.inTx(r.Context(), func(tx *sql.Tx) (err error) {
		if selected, err = h.live(tx, ownerID, nodeID); err != nil {
			return err
		}
		// Viewer 0 is no account, so vault folders stay out.
		entries, err = h.collectZIPEntries(r.Context(), tx, []zipSelection{{id: selected.ID, ownerID: ownerID, node: selected}}, 0)
		return err
	})
	if err != nil {
		return err
	}
	var dek []byte
	if r.Method != http.MethodHead {
		if dek, err = h.keys.GetUserDEK(r.Context(), h.db, ownerID); err != nil {
			return err
		}
		if err = admit(); err != nil {
			return err
		}
	}
	h.sendZIP(w, r, selected.Name+".zip", entries, dek)
	return nil
}

func parseZIPIDs(raw string) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (h *Handler) resolveZIPSelections(r *http.Request, viewer int64, ids []string) ([]zipSelection, int64, error) {
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	selections := make([]zipSelection, 0, len(ids))
	ownerID := int64(0)
	for _, id := range ids {
		n, access, err := h.resolveReadNode(tx, viewer, id)
		if err != nil {
			return nil, 0, err
		}
		if n.Type != "file" && n.Type != "folder" {
			return nil, 0, apiError{http.StatusBadRequest, "Choose files or folders.", "INVALID_SELECTION"}
		}
		currentOwner := viewer
		if access != nil {
			currentOwner = access.ownerID
		}
		if ownerID != 0 && ownerID != currentOwner {
			return nil, 0, apiError{http.StatusBadRequest, "Download your own files and shared files separately.", "ZIP_MIXED_OWNERS"}
		}
		ownerID = currentOwner
		selections = append(selections, zipSelection{id: id, ownerID: currentOwner, node: n})
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	if ownerID == 0 {
		ownerID = viewer
	}
	return selections, ownerID, nil
}

func (h *Handler) resolveZIPSelectionsTx(tx *sql.Tx, viewer int64, ids []string, ownerID int64) ([]zipSelection, error) {
	selections := make([]zipSelection, 0, len(ids))
	for _, id := range ids {
		n, access, err := h.resolveReadNode(tx, viewer, id)
		if err != nil {
			return nil, err
		}
		currentOwner := viewer
		if access != nil {
			currentOwner = access.ownerID
		}
		if currentOwner != ownerID {
			return nil, apiError{http.StatusBadRequest, "Download your own files and shared files separately.", "ZIP_MIXED_OWNERS"}
		}
		selections = append(selections, zipSelection{id: id, ownerID: currentOwner, node: n})
	}
	return selections, nil
}

func zipDownloadName(selections []zipSelection) string {
	if len(selections) == 1 {
		n := selections[0].node
		if n.ParentID != nil {
			return n.Name + ".zip"
		}
	}
	return "NeoCloud.zip"
}

func (h *Handler) collectZIPEntries(ctx context.Context, tx *sql.Tx, selections []zipSelection, viewer int64) ([]zipArchiveEntry, error) {
	cols := strings.TrimSuffix(nodeSelect, " FROM nodes n")
	entryLimit := h.maxNodes
	if entryLimit < 1 {
		entryLimit = maxZIPSelection
	}
	query := `WITH RECURSIVE tree(id,depth,relative_path) AS (
		SELECT id,0,'' FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL
		UNION ALL
		SELECT child.id,tree.depth+1,CASE WHEN tree.depth=0 THEN child.name ELSE tree.relative_path||'/'||child.name END
		FROM nodes child JOIN tree ON child.parent_id=tree.id
		WHERE child.user_id=? AND child.trashed_at IS NULL
	) ` + cols + `,tree.depth,tree.relative_path,n.blob_id FROM tree JOIN nodes n ON n.id=tree.id
	WHERE (?=0 OR n.vault_id IS NULL) ORDER BY tree.depth,lower(n.name),n.id`
	entries := make([]zipArchiveEntry, 0)
	usedNames := map[string]struct{}{}
	for _, selection := range selections {
		isRoot := selection.node.ParentID == nil
		topName := selection.node.Name
		if !isRoot {
			for suffix := 1; ; suffix++ {
				if _, exists := usedNames[strings.ToLower(topName)]; !exists {
					usedNames[strings.ToLower(topName)] = struct{}{}
					break
				}
				topName = fmt.Sprintf("%s (%d)", selection.node.Name, suffix)
			}
		}
		rows, err := tx.QueryContext(ctx, query, selection.node.ID, selection.ownerID, selection.ownerID, boolToInt(selection.ownerID != viewer))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var depth int
			var relative string
			var blobID sql.NullString
			n, err := scanNode(rows, &depth, &relative, &blobID)
			if err != nil {
				rows.Close()
				return nil, err
			}
			if isRoot && depth == 0 {
				continue
			}
			path := relative
			if !isRoot {
				path = topName
				if depth > 0 {
					path += "/" + relative
				}
			}
			// A name that would climb out of the archive never goes into it.
			if gopath.Clean("/"+path) != "/"+path || strings.ContainsAny(path, "\\\x00") {
				rows.Close()
				return nil, apiError{http.StatusInternalServerError, "A file name cannot be added to the archive.", "ZIP_ENTRY_INVALID"}
			}
			if n.Type == "file" && (!blobID.Valid || n.Size < 0) {
				rows.Close()
				return nil, fmt.Errorf("invalid file metadata for ZIP entry %q", path)
			}
			if len(entries) >= entryLimit {
				rows.Close()
				return nil, apiError{http.StatusBadRequest, "Too many items selected.", "TOO_MANY_ITEMS"}
			}
			entries = append(entries, zipArchiveEntry{node: n, ownerID: selection.ownerID, blobID: blobID.String, path: path})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err = rows.Close(); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (h *Handler) writeZIPEntry(archive *zip.Writer, entry zipArchiveEntry, dek []byte) error {
	path := entry.path
	header := &zip.FileHeader{Name: path, Method: zip.Store}
	if parsed, err := time.Parse("2006-01-02 15:04:05", entry.node.ModifiedAt); err == nil {
		header.Modified = parsed.UTC()
	}
	if entry.node.Type == "folder" {
		header.Name = strings.TrimSuffix(path, "/") + "/"
		header.SetMode(os.ModeDir | 0o755)
		_, err := archive.CreateHeader(header)
		return err
	}
	header.SetMode(0o644)
	file, err := os.Open(blobPath(h.blobDir, entry.ownerID, entry.blobID))
	if err != nil {
		return err
	}
	writer, err := archive.CreateHeader(header)
	if err != nil {
		_ = file.Close()
		return err
	}
	end := entry.node.Size - 1
	if end < 0 {
		end = 0
	}
	_, decryptErr := cryptox.DecryptBlobRange(file, entry.node.Size, 0, end, dek, entry.blobID, writer)
	closeErr := file.Close()
	if decryptErr != nil {
		return decryptErr
	}
	return closeErr
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
