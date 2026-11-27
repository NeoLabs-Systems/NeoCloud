package files

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type storageNode struct {
	node
	Path string `json:"path"`
}
type typeUsage struct {
	Type  string `json:"type"`
	Bytes int64  `json:"bytes"`
	Count int    `json:"count"`
}
type folderUsage struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}
type duplicateGroup struct {
	SHA256      string        `json:"sha256"`
	Size        int64         `json:"size"`
	Reclaimable int64         `json:"reclaimable"`
	Nodes       []storageNode `json:"nodes"`
}

func (h *Handler) storageBreakdown(w http.ResponseWriter, r *http.Request) {
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
	var total int64
	if err = tx.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, uid).Scan(&total); err != nil {
		failTx(w, tx, err)
		return
	}
	var trashBytes, versionBytes int64
	err = tx.QueryRow(`WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?) SELECT COALESCE(SUM(size),0) FROM nodes WHERE user_id=? AND type='file' AND id IN (SELECT id FROM hidden)`, uid, uid, uid).Scan(&trashBytes)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.QueryRow(`SELECT COALESCE(SUM(v.size),0) FROM file_versions v JOIN nodes n ON n.id=v.node_id WHERE n.user_id=?`, uid).Scan(&versionBytes); err != nil {
		failTx(w, tx, err)
		return
	}
	columns := strings.TrimSuffix(nodeSelect, " FROM nodes n")
	rows, err := tx.Query(`WITH RECURSIVE tree(id,depth,relative_path) AS (SELECT id,0,'' FROM nodes WHERE user_id=? AND parent_id IS NULL UNION ALL SELECT n.id,t.depth+1,CASE WHEN t.depth=0 THEN n.name ELSE t.relative_path||'/'||n.name END FROM nodes n JOIN tree t ON n.parent_id=t.id WHERE n.user_id=? AND n.trashed_at IS NULL) `+columns+`,tree.depth,tree.relative_path FROM tree JOIN nodes n ON n.id=tree.id ORDER BY tree.depth,lower(n.name)`, uid, uid)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	byType := map[string]*typeUsage{}
	typeOrder := []string{}
	byFolder := map[string]*folderUsage{}
	folderOrder := []string{}
	files := []storageNode{}
	var fileBytes int64
	rootID := ""
	for rows.Next() {
		var depth int
		var relative string
		n, err := scanNode(rows, &depth, &relative)
		if err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		if depth == 0 {
			rootID = n.ID
			continue
		}
		if depth == 1 && n.Type == "folder" {
			byFolder[relative] = &folderUsage{ID: n.ID, Name: n.Name}
			folderOrder = append(folderOrder, relative)
			continue
		}
		if n.Type != "file" {
			continue
		}
		files = append(files, storageNode{node: n})
		fileBytes += n.Size
		kind := storageType(n)
		entry := byType[kind]
		if entry == nil {
			entry = &typeUsage{Type: kind}
			byType[kind] = entry
			typeOrder = append(typeOrder, kind)
		}
		entry.Bytes += n.Size
		entry.Count++
		if depth == 1 {
			entry := byFolder[""]
			if entry == nil {
				entry = &folderUsage{ID: rootID}
				byFolder[""] = entry
				folderOrder = append(folderOrder, "")
			}
			entry.Bytes += n.Size
		} else {
			top := strings.SplitN(relative, "/", 2)[0]
			if entry := byFolder[top]; entry != nil {
				entry.Bytes += n.Size
			}
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	sort.Slice(files, func(i, j int) bool {
		if files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	largest := []storageNode{}
	for _, f := range files {
		if f.VaultID != nil {
			continue
		}
		if len(largest) == 20 {
			break
		}
		parent := "root"
		if f.ParentID != nil {
			parent = *f.ParentID
		}
		path, err := h.path(tx, uid, parent)
		if err != nil {
			failTx(w, tx, err)
			return
		}
		largest = append(largest, storageNode{node: f.node, Path: pathString(path)})
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	byTypes := make([]typeUsage, 0, len(typeOrder))
	for _, key := range typeOrder {
		byTypes = append(byTypes, *byType[key])
	}
	sort.SliceStable(byTypes, func(i, j int) bool { return byTypes[i].Bytes > byTypes[j].Bytes })
	folders := make([]folderUsage, 0, len(folderOrder))
	for _, key := range folderOrder {
		folders = append(folders, *byFolder[key])
	}
	sort.SliceStable(folders, func(i, j int) bool { return folders[i].Bytes > folders[j].Bytes })
	httpx.WriteJSON(w, 200, map[string]any{"total": total, "files": fileBytes, "trash": trashBytes, "versions": versionBytes, "byType": byTypes, "byFolder": folders, "largest": largest})
}

func storageType(n node) string {
	if n.VaultID != nil {
		return "vault"
	}
	mime := ""
	if n.Mime != nil {
		mime = strings.ToLower(*n.Mime)
	}
	ext := strings.ToLower(filepath.Ext(n.Name))
	switch {
	case strings.HasPrefix(mime, "image/") || hasExt(ext, ".cr2 .cr3 .nef .arw .dng .raf .orf .rw2 .psd .ico"):
		return "image"
	case strings.HasPrefix(mime, "video/") || hasExt(ext, ".3gp .mts .m2ts .wmv .flv .mpg .mpeg .vob"):
		return "video"
	case strings.HasPrefix(mime, "audio/") || hasExt(ext, ".aac .aif .aiff .wma .mid .midi .alac"):
		return "audio"
	case mime == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mime, "text/") || strings.HasPrefix(mime, "application/vnd.openxmlformats-officedocument.") || strings.HasPrefix(mime, "application/vnd.oasis.opendocument.") || hasString(mime, "application/msword application/vnd.ms-excel application/vnd.ms-powerpoint application/rtf application/json application/xml") || hasExt(ext, ".pages .numbers .key .epub .tex"):
		return "document"
	case hasString(mime, "application/zip application/gzip application/x-tar application/x-7z-compressed application/vnd.rar") || hasExt(ext, ".bz2 .xz .tgz .zst .dmg .iso"):
		return "archive"
	default:
		return "other"
	}
}
func hasExt(value, list string) bool {
	for _, item := range strings.Fields(list) {
		if value == item {
			return true
		}
	}
	return false
}
func hasString(value, list string) bool {
	for _, item := range strings.Fields(list) {
		if value == item {
			return true
		}
	}
	return false
}

type duplicateAccumulator struct {
	sha   string
	size  int64
	blobs map[string]bool
	nodes []node
	waste int64
}

func (h *Handler) storageDuplicates(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	columns := strings.TrimSuffix(nodeSelect, " FROM nodes n")
	rows, err := h.db.QueryContext(r.Context(), `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?) `+columns+`,n.blob_id,b.sha256 FROM nodes n JOIN blobs b ON b.id=n.blob_id WHERE n.user_id=? AND n.type='file' AND n.size>0 AND n.vault_id IS NULL AND n.id NOT IN (SELECT id FROM hidden) ORDER BY n.modified_at DESC`, uid, uid, uid)
	if err != nil {
		fail(w, err)
		return
	}
	groups := map[string]*duplicateAccumulator{}
	groupOrder := []*duplicateAccumulator{}
	for rows.Next() {
		var blob, sha string
		n, err := scanNode(rows, &blob, &sha)
		if err != nil {
			rows.Close()
			fail(w, err)
			return
		}
		g := groups[sha]
		if g == nil {
			g = &duplicateAccumulator{sha: sha, size: n.Size, blobs: map[string]bool{}}
			groups[sha] = g
			groupOrder = append(groupOrder, g)
		}
		g.blobs[blob] = true
		g.nodes = append(g.nodes, n)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		fail(w, err)
		return
	}
	rows.Close()
	ranked := []*duplicateAccumulator{}
	var reclaimable int64
	for _, g := range groupOrder {
		if len(g.nodes) < 2 {
			continue
		}
		g.waste = int64(len(g.blobs)-1) * g.size
		reclaimable += g.waste
		ranked = append(ranked, g)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].waste != ranked[j].waste {
			return ranked[i].waste > ranked[j].waste
		}
		return ranked[i].size*int64(len(ranked[i].nodes)) > ranked[j].size*int64(len(ranked[j].nodes))
	})
	if len(ranked) > 200 {
		ranked = ranked[:200]
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]duplicateGroup, 0, len(ranked))
	for _, g := range ranked {
		entry := duplicateGroup{SHA256: g.sha, Size: g.size, Reclaimable: g.waste, Nodes: []storageNode{}}
		for _, n := range g.nodes {
			parent := "root"
			if n.ParentID != nil {
				parent = *n.ParentID
			}
			path, err := h.path(tx, uid, parent)
			if err != nil {
				failTx(w, tx, err)
				return
			}
			entry.Nodes = append(entry.Nodes, storageNode{node: n, Path: pathString(path)})
		}
		out = append(out, entry)
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"groups": out, "reclaimable": reclaimable})
}
