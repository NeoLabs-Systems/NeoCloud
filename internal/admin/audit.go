package admin

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const auditEventLimit = 200
const auditUsernameQueryBatch = 500

// AuditRoutes registers the read-only administration audit API separately
// from the user-management routes.
func (h *Handler) AuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/audit", h.listAuditEvents)
}

type auditUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type auditEntry struct {
	ID        int64      `json:"id"`
	Action    string     `json:"action"`
	Actor     *auditUser `json:"actor"`
	Subject   *auditUser `json:"subject"`
	Detail    any        `json:"detail"`
	CreatedAt string     `json:"createdAt"`
}

type auditRow struct {
	entry         auditEntry
	actorID       sql.NullInt64
	subjectID     sql.NullInt64
	detailUserIDs []int64
}

func (h *Handler) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT log.id,log.action,log.detail_json,log.created_at,
		actor.id,actor.username,subject.id,subject.username
		FROM access_audit_log log
		LEFT JOIN users actor ON actor.id=log.actor_user_id
		LEFT JOIN users subject ON subject.id=log.subject_user_id
		ORDER BY log.id DESC LIMIT ?`, auditEventLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
		return
	}
	entries := make([]auditRow, 0, auditEventLimit)
	allIDs := map[int64]struct{}{}
	for rows.Next() {
		var row auditRow
		var detailJSON string
		var actorName, subjectName sql.NullString
		if err = rows.Scan(&row.entry.ID, &row.entry.Action, &detailJSON, &row.entry.CreatedAt, &row.actorID, &actorName, &row.subjectID, &subjectName); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
			return
		}
		if row.actorID.Valid {
			row.entry.Actor = &auditUser{ID: row.actorID.Int64, Username: actorName.String}
		}
		if row.subjectID.Valid {
			row.entry.Subject = &auditUser{ID: row.subjectID.Int64, Username: subjectName.String}
		}
		if row.entry.Detail, err = parseAuditDetail(detailJSON); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
			return
		}
		row.detailUserIDs = detailUserIDs(row.entry.Detail)
		for _, id := range row.detailUserIDs {
			allIDs[id] = struct{}{}
		}
		entries = append(entries, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
		return
	}
	if err = rows.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
		return
	}

	usernames, err := h.auditUsernames(r, allIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list access events", "INTERNAL_ERROR")
		return
	}
	result := make([]auditEntry, len(entries))
	for i := range entries {
		result[i] = entries[i].entry
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": result, "usernames": usernames})
}

func detailUserIDs(detail any) []int64 {
	object, ok := detail.(map[string]any)
	if !ok {
		return nil
	}
	ids := make([]int64, 0)
	for key, value := range object {
		if !strings.HasSuffix(key, "UserId") {
			continue
		}
		number, ok := value.(json.Number)
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(string(number), 10, 64)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *Handler) auditUsernames(r *http.Request, ids map[int64]struct{}) (map[string]string, error) {
	result := make(map[string]string, len(ids))
	values := make([]int64, 0, len(ids))
	for id := range ids {
		values = append(values, id)
	}
	for start := 0; start < len(values); start += auditUsernameQueryBatch {
		end := min(start+auditUsernameQueryBatch, len(values))
		placeholders := strings.TrimSuffix(strings.Repeat("?,", end-start), ",")
		args := make([]any, end-start)
		for i, id := range values[start:end] {
			args[i] = id
		}
		rows, err := h.db.QueryContext(r.Context(), `SELECT id,username FROM users WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var username string
			if err = rows.Scan(&id, &username); err != nil {
				rows.Close()
				return nil, err
			}
			result[strconv.FormatInt(id, 10)] = username
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err = rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parseAuditDetail(raw string) (any, error) {
	if raw == "" {
		raw = "{}"
	}
	var detail any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&detail); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return detail, nil
}
