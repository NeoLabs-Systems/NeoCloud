package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const (
	aiEnabledEnv = "NEOCLOUD_AI_ENABLED"
	aiBudgetEnv  = "NEOCLOUD_AI_MONTHLY_TOKENS"
)

func (h *Handler) aiConfig() (bool, int64) {
	h.aiConfigMu.RLock()
	defer h.aiConfigMu.RUnlock()
	return h.aiEnabled, h.aiMonthlyTokens
}

func (h *Handler) getAdminAIConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	enabled, budget := h.aiConfig()
	var monthlyTokens any
	if budget > 0 {
		monthlyTokens = budget
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "monthlyTokens": monthlyTokens})
}

func (h *Handler) updateAdminAIConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	var enabledUpdate *bool
	if raw, exists := body["enabled"]; exists {
		value, valid := raw.(bool)
		if !valid {
			writeError(w, http.StatusBadRequest, "enabled must be true or false.", "INVALID_VALUE")
			return
		}
		enabledUpdate = &value
	}
	var budgetUpdate *int64
	if raw, exists := body["monthlyTokens"]; exists {
		encoded, err := json.Marshal(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "monthlyTokens must be a whole number of tokens, or null for no limit.", "INVALID_VALUE")
			return
		}
		parsed, valid := parseBudget(encoded)
		if !valid {
			writeError(w, http.StatusBadRequest, "monthlyTokens must be a whole number of tokens, or null for no limit.", "INVALID_VALUE")
			return
		}
		budget := int64(0)
		if parsed.Valid {
			budget = parsed.Int64
		}
		budgetUpdate = &budget
	}
	h.aiConfigMu.Lock()
	enabled, budget := h.aiEnabled, h.aiMonthlyTokens
	updates := make(map[string]string, 2)
	if enabledUpdate != nil {
		enabled = *enabledUpdate
		updates[aiEnabledEnv] = "false"
		if enabled {
			updates[aiEnabledEnv] = "true"
		}
	}
	if budgetUpdate != nil {
		budget = *budgetUpdate
		updates[aiBudgetEnv] = ""
		if budget > 0 {
			updates[aiBudgetEnv] = strconv.FormatInt(budget, 10)
		}
	}
	if len(updates) > 0 {
		if err := h.persistSettings(updates); err != nil {
			h.aiConfigMu.Unlock()
			writeError(w, http.StatusInternalServerError, "Could not save AI settings.", "INTERNAL_ERROR")
			return
		}
		h.aiEnabled = enabled
		h.aiMonthlyTokens = budget
		if h.onAIConfigChanged != nil {
			h.onAIConfigChanged(enabled, budget)
		}
		h.aiConfigMu.Unlock()
	} else {
		h.aiConfigMu.Unlock()
	}
	var monthlyTokens any
	if budget > 0 {
		monthlyTokens = budget
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "monthlyTokens": monthlyTokens})
}

func (h *Handler) getAdminAIUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	const monthStart = "strftime('%Y-%m-01 00:00:00','now')"
	var since string
	if err := h.db.QueryRowContext(r.Context(), "SELECT "+monthStart).Scan(&since); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	users := make([]map[string]any, 0)
	rows, err := h.db.QueryContext(r.Context(), `SELECT u.id,u.username,u.ai_monthly_tokens,
		COALESCE(SUM(a.total_tokens),0),COUNT(a.id),COALESCE(SUM(1-a.ok),0)
		FROM ai_usage a JOIN users u ON u.id=a.user_id WHERE a.created_at>=`+monthStart+`
		GROUP BY u.id ORDER BY COALESCE(SUM(a.total_tokens),0) DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	_, serverDefault := h.aiConfig()
	for rows.Next() {
		var userID int64
		var username string
		var override sql.NullInt64
		var tokens, calls, errorsCount int64
		if err = rows.Scan(&userID, &username, &override, &tokens, &calls, &errorsCount); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
			return
		}
		var limit any
		if override.Valid {
			if override.Int64 > 0 {
				limit = override.Int64
			}
		} else if serverDefault > 0 {
			limit = serverDefault
		}
		users = append(users, map[string]any{"userId": userID, "username": username, "tokens": tokens, "calls": calls, "errors": errorsCount, "limit": limit})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	if err = rows.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	features, err := h.aiUsageGroups(r, `SELECT feature,COALESCE(SUM(total_tokens),0),COUNT(*) FROM ai_usage WHERE created_at>=`+monthStart+` GROUP BY feature ORDER BY COALESCE(SUM(total_tokens),0) DESC`, []string{"feature", "tokens", "calls"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	models, err := h.aiUsageGroups(r, `SELECT provider,model,COALESCE(SUM(total_tokens),0),COUNT(*),CAST(COALESCE(AVG(latency_ms),0) AS INTEGER) FROM ai_usage WHERE created_at>=`+monthStart+` GROUP BY provider,model ORDER BY COALESCE(SUM(total_tokens),0) DESC`, []string{"provider", "model", "tokens", "calls", "avgLatencyMs"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load AI usage.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"since": since, "users": users, "features": features, "models": models})
}

func (h *Handler) aiUsageGroups(r *http.Request, query string, fields []string) ([]map[string]any, error) {
	rows, err := h.db.QueryContext(r.Context(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	values := make([]any, len(fields))
	ptrs := make([]any, len(fields))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(fields))
		for i, field := range fields {
			item[field] = values[i]
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
