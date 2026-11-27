package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/settings"
)

func TestAdminUserModelGrantsValidateCatalogAndAuditAtomically(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	models := []settings.AdminModel{{ID: "openai::gpt-test", ModelID: "gpt-test", Provider: "openai"}}
	h, err := New(Options{
		DB:                db.W,
		AdminModelCatalog: func(context.Context) ([]settings.AdminModel, error) { return models, nil },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.UserActionRoutes(mux)
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/users/2/allowed-models", "")
	var initial struct {
		ModelIDs      []string `json:"modelIds"`
		MonthlyTokens *int64   `json:"monthlyTokens"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &initial) != nil || len(initial.ModelIDs) != 0 || initial.MonthlyTokens != nil {
		t.Fatalf("initial grants=%d %s", response.Code, response.Body.String())
	}
	response = adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/allowed-models", `{"modelIds":["openai::gpt-test"]}`)
	var granted struct {
		OK       bool     `json:"ok"`
		ModelIDs []string `json:"modelIds"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &granted) != nil || !granted.OK || len(granted.ModelIDs) != 1 || granted.ModelIDs[0] != "openai::gpt-test" {
		t.Fatalf("set grants=%d %s", response.Code, response.Body.String())
	}
	var stored string
	if err = db.W.QueryRow(`SELECT allowed_models FROM users WHERE id=2`).Scan(&stored); err != nil || stored != `["openai::gpt-test"]` {
		t.Fatalf("stored grants=%q err=%v", stored, err)
	}
	var auditCount int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.models_changed' AND actor_user_id=1 AND subject_user_id=2`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("grant audit count=%d err=%v", auditCount, err)
	}
	response = adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/allowed-models", `{"modelIds":["openai::unknown"]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown grant=%d %s", response.Code, response.Body.String())
	}
	if err = db.W.QueryRow(`SELECT allowed_models FROM users WHERE id=2`).Scan(&stored); err != nil || stored != `["openai::gpt-test"]` {
		t.Fatalf("unknown model changed grants=%q err=%v", stored, err)
	}
}

func TestAdminUserAIBudgetValidatesAndAudits(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	h, err := New(Options{
		DB: db.W,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.UserActionRoutes(mux)
	for _, body := range []string{`{"monthlyTokens":-1}`, `{"monthlyTokens":1.5}`, `{"monthlyTokens":"10"}`, `{}`} {
		if response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/ai-budget", body); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid budget %s: %d %s", body, response.Code, response.Body.String())
		}
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/ai-budget", `{"monthlyTokens":0}`)
	var unlimited struct {
		MonthlyTokens int64 `json:"monthlyTokens"`
		UserID        int64 `json:"userId"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &unlimited) != nil || unlimited.UserID != 2 || unlimited.MonthlyTokens != 0 {
		t.Fatalf("unlimited budget=%d %s", response.Code, response.Body.String())
	}
	var budget int64
	if err = db.W.QueryRow(`SELECT ai_monthly_tokens FROM users WHERE id=2`).Scan(&budget); err != nil || budget != 0 {
		t.Fatalf("stored unlimited budget=%d err=%v", budget, err)
	}
	response = adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/users/2/ai-budget", `{"monthlyTokens":null}`)
	if response.Code != http.StatusOK {
		t.Fatalf("inherited budget=%d %s", response.Code, response.Body.String())
	}
	var nullCount int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='account.ai_budget_changed' AND actor_user_id=1 AND subject_user_id=2`).Scan(&nullCount); err != nil || nullCount != 2 {
		t.Fatalf("budget audit count=%d err=%v", nullCount, err)
	}
	var got map[string]any
	if err = json.Unmarshal(response.Body.Bytes(), &got); err != nil || got["monthlyTokens"] != nil {
		t.Fatalf("null budget response=%s err=%v", response.Body.String(), err)
	}
}
