package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAdminAIConfigPersistsAndUpdatesLivePolicy(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	envFile := filepath.Join(t.TempDir(), "settings.env")
	var liveEnabled bool
	var liveBudget int64
	changed := 0
	h, err := New(Options{
		DB: db.W, EnvFile: envFile, AIEnabled: true, AIMonthlyTokens: 1200,
		OnAIConfigChanged: func(enabled bool, budget int64) { liveEnabled, liveBudget, changed = enabled, budget, changed+1 },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	get := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/ai", "")
	var initial struct {
		Enabled       bool  `json:"enabled"`
		MonthlyTokens int64 `json:"monthlyTokens"`
	}
	if get.Code != http.StatusOK || json.Unmarshal(get.Body.Bytes(), &initial) != nil || !initial.Enabled || initial.MonthlyTokens != 1200 {
		t.Fatalf("initial config=%d %s", get.Code, get.Body.String())
	}
	updated := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/ai", `{"enabled":false,"monthlyTokens":0}`)
	var result struct {
		Enabled       bool   `json:"enabled"`
		MonthlyTokens *int64 `json:"monthlyTokens"`
	}
	if updated.Code != http.StatusOK || json.Unmarshal(updated.Body.Bytes(), &result) != nil || result.Enabled || result.MonthlyTokens != nil {
		t.Fatalf("updated config=%d %s", updated.Code, updated.Body.String())
	}
	if changed != 1 || liveEnabled || liveBudget != 0 {
		t.Fatalf("live policy enabled=%v budget=%d callbacks=%d", liveEnabled, liveBudget, changed)
	}
	stored, err := os.ReadFile(envFile)
	if err != nil || !containsEnvValue(string(stored), aiEnabledEnv, "false") || !containsEnvValue(string(stored), aiBudgetEnv, "") {
		t.Fatalf("persisted config=%q err=%v", stored, err)
	}
	invalid := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/ai", `{"monthlyTokens":1.25}`)
	if invalid.Code != http.StatusBadRequest || changed != 1 {
		t.Fatalf("invalid config=%d callbacks=%d %s", invalid.Code, changed, invalid.Body.String())
	}
}

func containsEnvValue(contents, key, value string) bool {
	for _, line := range splitLines(contents) {
		if line == key+"="+value {
			return true
		}
	}
	return false
}

func splitLines(value string) []string {
	var result []string
	start := 0
	for i, r := range value {
		if r == '\n' {
			result = append(result, value[start:i])
			start = i + 1
		}
	}
	if start < len(value) {
		result = append(result, value[start:])
	}
	return result
}

func TestAdminAIUsageGroupsMonthlyUsageAndBudgets(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	if _, err := db.W.Exec(`UPDATE users SET ai_monthly_tokens=0 WHERE id=2;
INSERT INTO ai_usage(user_id,feature,provider,model,input_tokens,output_tokens,total_tokens,latency_ms,ok,error_code,created_at)
VALUES(1,'organize','openai','gpt-test',3,4,7,40,1,NULL,datetime('now')),
      (1,'organize','openai','gpt-test',1,2,3,60,0,'UPSTREAM',datetime('now')),
      (2,'summary','anthropic','claude-test',5,2,7,80,1,NULL,datetime('now'));`); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{
		DB: db.W, AIMonthlyTokens: 100,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/ai/usage", "")
	if response.Code != http.StatusOK {
		t.Fatalf("usage=%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Since    string           `json:"since"`
		Users    []map[string]any `json:"users"`
		Features []map[string]any `json:"features"`
		Models   []map[string]any `json:"models"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Since == "" || len(result.Users) != 2 || len(result.Features) != 2 || len(result.Models) != 2 {
		t.Fatalf("usage groups=%s", response.Body.String())
	}
	if result.Users[0]["userId"] != float64(1) || result.Users[0]["tokens"] != float64(10) || result.Users[0]["calls"] != float64(2) || result.Users[0]["errors"] != float64(1) || result.Users[0]["limit"] != float64(100) {
		t.Fatalf("top user usage=%#v", result.Users[0])
	}
	if result.Users[1]["limit"] != nil {
		t.Fatalf("zero user budget should mean unlimited: %#v", result.Users[1])
	}
}
