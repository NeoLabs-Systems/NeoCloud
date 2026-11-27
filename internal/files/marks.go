package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type favoriteItem struct {
	node
	FavoritedAt string `json:"favoritedAt"`
	Path        string `json:"path"`
}

type recentItem struct {
	node
	LastOpenedAt *string `json:"lastOpenedAt"`
	Path         string  `json:"path"`
}

func (h *Handler) setFavorite(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.live(tx, uid, r.PathValue("id"))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if n.ParentID == nil {
		failTx(w, tx, apiError{400, "The root folder can’t be marked.", "ROOT_IMMUTABLE"})
		return
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO favorites(user_id,node_id,created_at) VALUES(?,?,strftime('%Y-%m-%d %H:%M:%f','now'))`, uid, n.ID); err != nil {
		failTx(w, tx, err)
		return
	}
	n, err = scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, n.ID, uid))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	httpx.WriteJSON(w, 200, n)
}

func (h *Handler) clearFavorite(w http.ResponseWriter, r *http.Request) {
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
	id := r.PathValue("id")
	var exists int
	if err = tx.QueryRow(`SELECT 1 FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		failTx(w, tx, apiError{404, "File or folder not found", "NOT_FOUND"})
		return
	} else if err != nil {
		failTx(w, tx, err)
		return
	}
	if _, err = tx.Exec(`DELETE FROM favorites WHERE user_id=? AND node_id=?`, uid, id); err != nil {
		failTx(w, tx, err)
		return
	}
	n, err := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, id, uid))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	httpx.WriteJSON(w, 200, n)
}

func pathString(path []map[string]string) string {
	parts := make([]string, 0, len(path))
	for _, entry := range path {
		if entry["name"] != "" {
			parts = append(parts, entry["name"])
		}
	}
	if len(parts) > 0 {
		return "/" + strings.Join(parts, "/")
	}
	return "/"
}

func (h *Handler) listFavorites(w http.ResponseWriter, r *http.Request) {
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
	rows, err := tx.Query(`WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION SELECT n.id FROM hidden h JOIN nodes n ON n.parent_id=h.id AND n.user_id=?) SELECT f.node_id,f.created_at FROM favorites f JOIN nodes n ON n.id=f.node_id WHERE f.user_id=? AND n.id NOT IN (SELECT id FROM hidden) AND (n.vault_id IS NULL OR EXISTS(SELECT 1 FROM vaults v WHERE v.folder_id=n.id)) ORDER BY f.created_at DESC,lower(n.name)`, uid, uid, uid)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	type fav struct{ id, at string }
	found := []fav{}
	for rows.Next() {
		var f fav
		if err = rows.Scan(&f.id, &f.at); err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		found = append(found, f)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	items := make([]favoriteItem, 0, len(found))
	for _, f := range found {
		n, e := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, f.id, uid))
		if e != nil {
			failTx(w, tx, e)
			return
		}
		p, e := h.path(tx, uid, n.ParentValue())
		if e != nil {
			failTx(w, tx, e)
			return
		}
		items = append(items, favoriteItem{node: n, FavoritedAt: f.at, Path: pathString(p)})
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) recordOpen(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.live(tx, uid, r.PathValue("id"))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if n.ParentID == nil {
		failTx(w, tx, apiError{400, "The root folder can’t be marked.", "ROOT_IMMUTABLE"})
		return
	}
	if _, err = tx.Exec(`INSERT INTO node_opens(user_id,node_id,last_opened_at) VALUES(?,?,strftime('%Y-%m-%d %H:%M:%f','now')) ON CONFLICT(user_id,node_id) DO UPDATE SET last_opened_at=excluded.last_opened_at,open_count=open_count+1`, uid, n.ID); err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (n node) ParentValue() string {
	if n.ParentID == nil {
		return ""
	}
	return *n.ParentID
}

func (h *Handler) recent(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	mode := r.URL.Query().Get("by")
	if mode == "" {
		mode = "opened"
	}
	if mode != "opened" && mode != "modified" {
		writeErr(w, 400, "by must be opened or modified.", "INVALID_PARAM")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	query := `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION SELECT n.id FROM hidden h JOIN nodes n ON n.parent_id=h.id AND n.user_id=?) SELECT n.id,COALESCE(o.last_opened_at,'') FROM nodes n LEFT JOIN node_opens o ON o.node_id=n.id AND o.user_id=n.user_id WHERE n.user_id=? AND n.type='file' AND n.vault_id IS NULL AND n.id NOT IN(SELECT id FROM hidden) ORDER BY n.modified_at DESC LIMIT ?`
	args := []any{uid, uid, uid, limit}
	if mode == "opened" {
		query = `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION SELECT n.id FROM hidden h JOIN nodes n ON n.parent_id=h.id AND n.user_id=?) SELECT n.id,o.last_opened_at FROM node_opens o JOIN nodes n ON n.id=o.node_id AND n.user_id=o.user_id WHERE o.user_id=? AND n.type='file' AND n.vault_id IS NULL AND n.id NOT IN(SELECT id FROM hidden) ORDER BY o.last_opened_at DESC,o.open_count DESC LIMIT ?`
		args = []any{uid, uid, uid, limit}
	}
	rows, err := tx.Query(query, args...)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	type recentRow struct {
		id string
		at sql.NullString
	}
	selected := []recentRow{}
	for rows.Next() {
		var item recentRow
		if err = rows.Scan(&item.id, &item.at); err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		selected = append(selected, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	if mode == "opened" && len(selected) < limit {
		exclude := make(map[string]bool, len(selected))
		for _, item := range selected {
			exclude[item.id] = true
		}
		excluded := make([]string, 0, len(selected))
		for _, item := range selected {
			excluded = append(excluded, item.id)
		}
		excludedJSON, _ := json.Marshal(excluded)
		extra, er := tx.Query(`WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION SELECT n.id FROM hidden h JOIN nodes n ON n.parent_id=h.id AND n.user_id=?) SELECT n.id,COALESCE(o.last_opened_at,'') FROM nodes n LEFT JOIN node_opens o ON o.node_id=n.id AND o.user_id=n.user_id WHERE n.user_id=? AND n.type='file' AND n.vault_id IS NULL AND n.id NOT IN(SELECT id FROM hidden) AND n.id NOT IN(SELECT value FROM json_each(?)) ORDER BY n.modified_at DESC LIMIT ?`, uid, uid, uid, string(excludedJSON), limit-len(selected))
		if er != nil {
			failTx(w, tx, er)
			return
		}
		for extra.Next() {
			var item recentRow
			if er = extra.Scan(&item.id, &item.at); er != nil {
				extra.Close()
				failTx(w, tx, er)
				return
			}
			if !exclude[item.id] {
				selected = append(selected, item)
				exclude[item.id] = true
			}
		}
		er = extra.Err()
		extra.Close()
		if er != nil {
			failTx(w, tx, er)
			return
		}
	}
	items := make([]recentItem, 0, len(selected))
	for _, item := range selected {
		n, er := scanNode(tx.QueryRow(nodeSelect+` WHERE n.id=? AND n.user_id=?`, item.id, uid))
		if er != nil {
			failTx(w, tx, er)
			return
		}
		p, er := h.path(tx, uid, n.ParentValue())
		if er != nil {
			failTx(w, tx, er)
			return
		}
		var opened *string
		if item.at.Valid && item.at.String != "" {
			opened = &item.at.String
		}
		items = append(items, recentItem{node: n, LastOpenedAt: opened, Path: pathString(p)})
	}
	if err = tx.Commit(); err != nil {
		failTx(w, tx, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}
