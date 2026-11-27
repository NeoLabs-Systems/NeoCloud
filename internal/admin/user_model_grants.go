package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const maxUserModelGrants = 200

func (h *Handler) getUserModelGrants(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var raw sql.NullString
	var budget sql.NullInt64
	if err := h.db.QueryRowContext(r.Context(), `SELECT allowed_models,ai_monthly_tokens FROM users WHERE id=?`, userID).Scan(&raw, &budget); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "Could not read user model settings", "INTERNAL_ERROR")
		}
		return
	}
	monthlyTokens := any(nil)
	if budget.Valid {
		monthlyTokens = budget.Int64
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"modelIds": parseGrantedModelIDs(raw.String), "monthlyTokens": monthlyTokens})
}

func (h *Handler) setUserModelGrants(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		ModelIDs json.RawMessage `json:"modelIds"`
	}
	if !readActionBody(w, r, &body) {
		return
	}
	wanted, valid := parseRequestedModelIDs(body.ModelIDs)
	if !valid {
		writeError(w, http.StatusBadRequest, "modelIds must be an array of model ids, or null", "INVALID_VALUE")
		return
	}
	if len(wanted) > maxUserModelGrants {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("At most %d models can be granted to one account", maxUserModelGrants), "INVALID_VALUE")
		return
	}
	catalog := make([]string, 0)
	if h.adminModelCatalog != nil {
		models, err := h.adminModelCatalog(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not load model catalog", "INTERNAL_ERROR")
			return
		}
		for _, model := range models {
			if id := strings.TrimSpace(model.ID); id != "" {
				catalog = append(catalog, id)
			}
		}
	}
	known := make(map[string]bool, len(catalog))
	for _, id := range catalog {
		known[id] = true
	}
	unknown := make([]string, 0)
	for _, id := range wanted {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		if len(unknown) > 10 {
			unknown = unknown[:10]
		}
		writeError(w, http.StatusBadRequest, "Unknown model ids: "+strings.Join(unknown, ", "), "UNKNOWN_MODEL")
		return
	}
	encoded, err := json.Marshal(wanted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not encode model grants", "INTERNAL_ERROR")
		return
	}
	var stored any
	if len(wanted) > 0 {
		stored = string(encoded)
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update model grants", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var current sql.NullString
	if err = tx.QueryRowContext(r.Context(), `SELECT allowed_models FROM users WHERE id=?`, userID).Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "Could not read user model settings", "INTERNAL_ERROR")
		}
		return
	}
	before := uniqueModelIDs(parseGrantedModelIDs(current.String))
	beforeSet := make(map[string]bool, len(before))
	wantedSet := make(map[string]bool, len(wanted))
	for _, id := range before {
		beforeSet[id] = true
	}
	for _, id := range wanted {
		wantedSet[id] = true
	}
	granted := make([]string, 0)
	for _, id := range wanted {
		if !beforeSet[id] {
			granted = append(granted, id)
		}
	}
	revoked := make([]string, 0)
	for _, id := range before {
		if !wantedSet[id] {
			revoked = append(revoked, id)
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET allowed_models=? WHERE id=?`, stored, userID); err == nil && (len(granted) > 0 || len(revoked) > 0) {
		var detail []byte
		detail, err = json.Marshal(map[string]any{"granted": granted, "revoked": revoked})
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.models_changed',?,?,?)`, actorID, userID, string(detail))
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update model grants", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update model grants", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "modelIds": wanted})
}

func (h *Handler) setUserAIBudget(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		MonthlyTokens json.RawMessage `json:"monthlyTokens"`
	}
	if !readActionBody(w, r, &body) {
		return
	}
	monthlyTokens, valid := parseBudget(body.MonthlyTokens)
	if !valid {
		writeError(w, http.StatusBadRequest, "monthlyTokens must be a whole number of tokens, or null for no limit.", "INVALID_VALUE")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account AI budget", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE users SET ai_monthly_tokens=? WHERE id=?`, monthlyTokens, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account AI budget", "INTERNAL_ERROR")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account AI budget", "INTERNAL_ERROR")
		return
	}
	if changed == 0 {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	var auditValue any
	if monthlyTokens.Valid {
		auditValue = monthlyTokens.Int64
	}
	detail, err := json.Marshal(map[string]any{"monthlyTokens": auditValue})
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.ai_budget_changed',?,?,?)`, actorID, userID, string(detail))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account AI budget", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account AI budget", "INTERNAL_ERROR")
		return
	}
	var value any
	if monthlyTokens.Valid {
		value = monthlyTokens.Int64
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"userId": userID, "monthlyTokens": value})
}

func parseRequestedModelIDs(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, true
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			ids = append(ids, "null")
		} else {
			ids = append(ids, fmt.Sprint(value))
		}
	}
	return uniqueModelIDs(ids), true
}

func parseGrantedModelIDs(raw string) []string {
	var values []any
	if json.Unmarshal([]byte(raw), &values) != nil || values == nil {
		return []string{}
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			ids = append(ids, "null")
		} else if id := strings.TrimSpace(fmt.Sprint(value)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func parseBudget(raw json.RawMessage) (sql.NullInt64, bool) {
	if len(raw) == 0 {
		return sql.NullInt64{}, false
	}
	if string(raw) == "null" {
		return sql.NullInt64{}, true
	}
	value, ok := new(big.Rat).SetString(string(raw))
	if !ok || !value.IsInt() || value.Sign() < 0 || !value.Num().IsInt64() {
		return sql.NullInt64{}, false
	}
	return sql.NullInt64{Int64: value.Num().Int64(), Valid: true}, true
}
