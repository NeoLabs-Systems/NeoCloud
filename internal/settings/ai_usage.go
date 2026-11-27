package settings

// Whether AI may run for an account (the admin's switch, the person's opt-in and
// their monthly token budget) and what each call cost: who, which feature, which
// model, how many tokens, how long and whether it worked. Never a prompt, a file
// name or an answer.

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

func (h *Handler) assertAIUsable(ctx context.Context, userID int64, settings map[string]any) error {
	if h.aiPolicy == nil {
		return nil
	}
	enabled, defaultMonthlyTokens := h.aiPolicy()
	if !enabled {
		return &AIChatError{Code: "AI_DISABLED", Status: http.StatusForbidden, Message: "AI features aren’t turned on."}
	}
	if settings["ai_opt_in"] != true {
		return &AIChatError{Code: "AI_NOT_OPTED_IN", Status: http.StatusForbidden, Message: "Turn on AI features in Settings › AI first."}
	}
	var used int64
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_tokens),0) FROM ai_usage WHERE user_id=? AND created_at>=strftime('%Y-%m-01 00:00:00','now')`, userID).Scan(&used); err != nil {
		return err
	}
	var override sql.NullInt64
	if err := h.db.QueryRowContext(ctx, `SELECT ai_monthly_tokens FROM users WHERE id=?`, userID).Scan(&override); err != nil {
		return err
	}
	limit := defaultMonthlyTokens
	if override.Valid {
		limit = override.Int64
	}
	if limit > 0 && used >= limit {
		return &AIChatError{Code: "AI_BUDGET_EXCEEDED", Status: http.StatusTooManyRequests, Message: "This month’s AI budget is used up. It resets on the 1st."}
	}
	return nil
}

type usageRecord struct {
	userID                   int64
	feature, provider, model string
	usage                    *llm.Usage
	started                  time.Time
	errorCode                string
}

// recordAIUsage writes the row even when the caller has gone away: the tokens
// were spent.
func (h *Handler) recordAIUsage(ctx context.Context, call usageRecord) error {
	var input, output, total int
	if call.usage != nil {
		input, output = max(0, call.usage.InputTokens), max(0, call.usage.OutputTokens)
		total = input + output
		// Gemini counts thinking apart from the output tokens.
		if call.provider == "google" {
			total += max(0, call.usage.ReasoningTokens)
		}
	}
	var code any
	if call.errorCode != "" {
		code = call.errorCode
	}
	ok := 0
	if call.errorCode == "" {
		ok = 1
	}
	_, err := h.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO ai_usage(user_id,feature,provider,model,input_tokens,output_tokens,total_tokens,latency_ms,ok,error_code)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, call.userID, call.feature, call.provider, call.model, input, output, total, max(0, time.Since(call.started).Milliseconds()), ok, code)
	return err
}
