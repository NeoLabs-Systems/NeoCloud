package files

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const maxSafeSearchInteger int64 = 9007199254740991

type searchItem struct {
	node
	Path    string `json:"path"`
	Match   string `json:"match,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

var searchTypeConditions = map[string]string{
	"folder":   `n.type='folder'`,
	"image":    `(n.type='file' AND (lower(n.mime) LIKE 'image/%' OR lower(n.name) LIKE '%.cr2' OR lower(n.name) LIKE '%.cr3' OR lower(n.name) LIKE '%.nef' OR lower(n.name) LIKE '%.arw' OR lower(n.name) LIKE '%.dng' OR lower(n.name) LIKE '%.raf' OR lower(n.name) LIKE '%.orf' OR lower(n.name) LIKE '%.rw2' OR lower(n.name) LIKE '%.psd' OR lower(n.name) LIKE '%.ico'))`,
	"video":    `(n.type='file' AND (lower(n.mime) LIKE 'video/%' OR lower(n.name) LIKE '%.3gp' OR lower(n.name) LIKE '%.mts' OR lower(n.name) LIKE '%.m2ts' OR lower(n.name) LIKE '%.wmv' OR lower(n.name) LIKE '%.flv' OR lower(n.name) LIKE '%.mpg' OR lower(n.name) LIKE '%.mpeg' OR lower(n.name) LIKE '%.vob'))`,
	"audio":    `(n.type='file' AND (lower(n.mime) LIKE 'audio/%' OR lower(n.name) LIKE '%.aac' OR lower(n.name) LIKE '%.aif' OR lower(n.name) LIKE '%.aiff' OR lower(n.name) LIKE '%.wma' OR lower(n.name) LIKE '%.mid' OR lower(n.name) LIKE '%.midi' OR lower(n.name) LIKE '%.alac'))`,
	"pdf":      `(n.type='file' AND lower(n.mime)='application/pdf')`,
	"document": `(n.type='file' AND (lower(n.mime) LIKE 'text/%' OR lower(n.mime) LIKE 'application/vnd.openxmlformats-officedocument.%' OR lower(n.mime) LIKE 'application/vnd.oasis.opendocument.%' OR lower(n.mime) IN ('application/msword','application/vnd.ms-excel','application/vnd.ms-powerpoint','application/rtf','application/json','application/xml') OR lower(n.name) LIKE '%.pages' OR lower(n.name) LIKE '%.numbers' OR lower(n.name) LIKE '%.key' OR lower(n.name) LIKE '%.epub' OR lower(n.name) LIKE '%.tex'))`,
	"archive":  `(n.type='file' AND (lower(n.mime) IN ('application/zip','application/gzip','application/x-tar','application/x-7z-compressed','application/vnd.rar') OR lower(n.name) LIKE '%.bz2' OR lower(n.name) LIKE '%.xz' OR lower(n.name) LIKE '%.tgz' OR lower(n.name) LIKE '%.zst' OR lower(n.name) LIKE '%.dmg' OR lower(n.name) LIKE '%.iso'))`,
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	raw := strings.TrimSpace(strings.ToLower(q.Get("q")))
	if len([]rune(raw)) > 200 {
		raw = string([]rune(raw)[:200])
	}
	contentSearch := flagTrue(q.Get("content"))
	// In content search quoted words are a phrase, and the quote marks do not
	// count toward names.
	contentQuery := raw
	if contentSearch {
		raw = strings.Join(strings.Fields(strings.ReplaceAll(raw, `"`, " ")), " ")
	}
	if raw == "" && strings.TrimSpace(q.Get("type")) == "" && strings.TrimSpace(q.Get("after")) == "" && strings.TrimSpace(q.Get("before")) == "" && strings.TrimSpace(q.Get("minSize")) == "" && strings.TrimSpace(q.Get("maxSize")) == "" && strings.TrimSpace(q.Get("parentId")) == "" {
		httpx.WriteJSON(w, 200, map[string]any{"items": []searchItem{}})
		return
	}
	limit := 100
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 500 {
		limit = 500
	}
	indexer := h.currentSearchIndexer()
	where := []string{"n.user_id=?", "n.parent_id IS NOT NULL", "n.vault_id IS NULL", "n.id NOT IN (SELECT id FROM hidden)"}
	with := "WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?)"
	// hidden's first placeholder precedes the WHERE clause placeholders.
	args := []any{uid, uid, uid}
	searchScope := searchindex.ContentScope{}
	if parent := strings.TrimSpace(q.Get("parentId")); parent != "" {
		tx, err := h.db.BeginTx(r.Context(), nil)
		if err != nil {
			fail(w, err)
			return
		}
		folder, err := h.live(tx, uid, parent)
		if err != nil {
			failTx(w, tx, err)
			return
		}
		if folder.Type != "folder" {
			failTx(w, tx, apiError{400, "That is not a folder.", "NOT_A_FOLDER"})
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, err)
			return
		}
		with = `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?), scope(id) AS (SELECT id FROM nodes WHERE parent_id=? AND trashed_at IS NULL UNION ALL SELECT n.id FROM nodes n JOIN scope s ON n.parent_id=s.id WHERE n.trashed_at IS NULL)`
		args = []any{uid, uid, folder.ID, uid}
		where = where[:3]
		where = append(where, "n.id IN (SELECT id FROM scope)")
		searchScope.CTE = `scope(id) AS (SELECT id FROM nodes WHERE parent_id=? AND trashed_at IS NULL UNION ALL SELECT n.id FROM nodes n JOIN scope s ON n.parent_id=s.id WHERE n.trashed_at IS NULL)`
		searchScope.CTEArgs = []any{folder.ID}
		searchScope.Where = `n.id IN (SELECT id FROM scope)`
	}
	scopeWhere := append([]string{}, where...)
	scopeArgs := append([]any{}, args...)
	if raw != "" {
		terms := strings.Fields(raw)
		if len(terms) > 8 {
			terms = terms[:8]
		}
		for _, term := range terms {
			pattern := "%" + escapeLike(term) + "%"
			where = append(where, "(lower(n.name) LIKE ? ESCAPE '\\' OR EXISTS(SELECT 1 FROM ai_node_meta m WHERE m.user_id=n.user_id AND m.node_id=n.id AND lower(m.search_text) LIKE ? ESCAPE '\\'))")
			args = append(args, pattern, pattern)
		}
	}
	types := uniqueCSV(q.Get("type"))
	if len(types) > 0 {
		conditions := []string{}
		for _, typ := range types {
			condition, valid := searchTypeConditions[typ]
			if !valid {
				writeErr(w, 400, "type must be one of image, video, audio, pdf, document, archive, folder.", "INVALID_TYPE")
				return
			}
			conditions = append(conditions, condition)
		}
		condition := "(" + strings.Join(conditions, " OR ") + ")"
		where = append(where, condition)
		scopeWhere = append(scopeWhere, condition)
		if searchScope.Where != "" {
			searchScope.Where += " AND "
		}
		searchScope.Where += condition
	}
	for _, filter := range []struct{ key, op string }{{"after", ">="}, {"before", "<"}} {
		if rawDate := q.Get(filter.key); rawDate != "" {
			parsed, err := parseSearchDate(rawDate)
			if err != nil {
				writeErr(w, 400, filter.key+" must be a date.", "INVALID_DATE")
				return
			}
			where = append(where, "n.modified_at "+filter.op+" ?")
			args = append(args, parsed)
			scopeWhere = append(scopeWhere, "n.modified_at "+filter.op+" ?")
			scopeArgs = append(scopeArgs, parsed)
			if searchScope.Where != "" {
				searchScope.Where += " AND "
			}
			searchScope.Where += "n.modified_at " + filter.op + " ?"
			searchScope.Args = append(searchScope.Args, parsed)
		}
	}
	for _, filter := range []struct{ key, op string }{{"minSize", ">="}, {"maxSize", "<="}} {
		if rawSize := q.Get(filter.key); rawSize != "" {
			size, err := strconv.ParseInt(rawSize, 10, 64)
			if err != nil || size < 0 || size > maxSafeSearchInteger {
				writeErr(w, 400, filter.key+" must be a byte count.", "INVALID_SIZE")
				return
			}
			where = append(where, "n.size "+filter.op+" ?")
			args = append(args, size)
			scopeWhere = append(scopeWhere, "n.size "+filter.op+" ?")
			scopeArgs = append(scopeArgs, size)
			if searchScope.Where != "" {
				searchScope.Where += " AND "
			}
			searchScope.Where += "n.size " + filter.op + " ?"
			searchScope.Args = append(searchScope.Args, size)
		}
	}
	rank := ""
	if raw != "" {
		prefix := escapeLike(raw) + "%"
		rank = `CASE WHEN lower(n.name)=? OR (lower(n.name) LIKE ? ESCAPE '\' AND lower(n.name) NOT LIKE ? ESCAPE '\') THEN 0 WHEN lower(n.name) LIKE ? ESCAPE '\' THEN 1 WHEN lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' OR lower(n.name) LIKE ? ESCAPE '\' THEN 2 ELSE 3 END,`
		stem := escapeLike(raw) + ".%"
		args = append(args, raw, stem, stem+".%", prefix, "% "+prefix, "%-"+prefix, `%\_`+prefix, "%."+prefix, "%("+prefix)
	}
	args = append(args, limit)
	columns := strings.TrimSuffix(nodeSelect, " FROM nodes n")
	query := fmt.Sprintf(`%s %s FROM nodes n WHERE %s ORDER BY %s n.modified_at DESC,lower(n.name),n.id LIMIT ?`, with, columns, strings.Join(where, " AND "), rank)
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	nodes := []node{}
	for rows.Next() {
		n, scanErr := scanNode(rows)
		if scanErr != nil {
			rows.Close()
			fail(w, scanErr)
			return
		}
		nodes = append(nodes, n)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		fail(w, err)
		return
	}
	rows.Close()
	contentMatches := map[string]string{}
	contentNodes := []node{}
	if contentSearch && contentQuery != "" && indexer != nil {
		nameIDs := make([]string, 0, len(nodes))
		for _, n := range nodes {
			nameIDs = append(nameIDs, n.ID)
		}
		found, searchErr := indexer.ContentSearchScoped(r.Context(), uid, searchindex.ParseQuery(contentQuery), nameIDs, searchScope, limit)
		if searchErr != nil {
			fail(w, searchErr)
			return
		}
		contentIDs := make([]string, 0, len(found.Rows))
		for _, hit := range found.Rows {
			contentMatches[hit.NodeID] = hit.Snippet
			contentIDs = append(contentIDs, hit.NodeID)
		}
		if len(contentIDs) > 0 {
			idsJSON, marshalErr := json.Marshal(contentIDs)
			if marshalErr != nil {
				writeErr(w, http.StatusInternalServerError, "Could not search file contents.", "INTERNAL_ERROR")
				return
			}
			columns := strings.TrimSuffix(nodeSelect, " FROM nodes n")
			hitsQuery := fmt.Sprintf(`%s %s FROM nodes n WHERE %s AND n.id IN (SELECT value FROM json_each(?))`, with, columns, strings.Join(scopeWhere, " AND "))
			contentArgs := append(append([]any{}, scopeArgs...), string(idsJSON))
			contentRows, queryErr := h.db.QueryContext(r.Context(), hitsQuery, contentArgs...)
			if queryErr != nil {
				fail(w, queryErr)
				return
			}
			for contentRows.Next() {
				n, scanErr := scanNode(contentRows)
				if scanErr != nil {
					contentRows.Close()
					fail(w, scanErr)
					return
				}
				contentNodes = append(contentNodes, n)
			}
			if queryErr = contentRows.Err(); queryErr != nil {
				contentRows.Close()
				fail(w, queryErr)
				return
			}
			contentRows.Close()
			rank := make(map[string]int, len(contentIDs))
			for i, id := range contentIDs {
				rank[id] = i
			}
			sort.Slice(contentNodes, func(i, j int) bool { return rank[contentNodes[i].ID] < rank[contentNodes[j].ID] })
		}
		for id, snippet := range found.Snippets {
			contentMatches[id] = snippet
		}
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]searchItem, 0, len(nodes))
	for _, n := range nodes {
		parentID := "root"
		if n.ParentID != nil {
			parentID = *n.ParentID
		}
		path, pathErr := h.path(tx, uid, parentID)
		if pathErr != nil {
			failTx(w, tx, pathErr)
			return
		}
		item := searchItem{node: n, Path: pathString(path)}
		if contentSearch {
			item.Match = "name"
			if snippet, matched := contentMatches[n.ID]; matched {
				item.Match = "both"
				item.Snippet = snippet
			}
		}
		items = append(items, item)
	}
	for _, n := range contentNodes {
		parentID := "root"
		if n.ParentID != nil {
			parentID = *n.ParentID
		}
		path, pathErr := h.path(tx, uid, parentID)
		if pathErr != nil {
			failTx(w, tx, pathErr)
			return
		}
		items = append(items, searchItem{node: n, Path: pathString(path), Match: "content", Snippet: contentMatches[n.ID]})
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "%", "\\%")
	return strings.ReplaceAll(value, "_", "\\_")
}
func uniqueCSV(value string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(strings.ToLower(value), ",") {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}
func flagTrue(value string) bool { return value == "1" || strings.EqualFold(value, "true") }
func parseSearchDate(value string) (string, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", time.DateOnly} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t.UTC().Format("2006-01-02 15:04:05"), nil
		}
	}
	return "", fmt.Errorf("invalid date")
}
