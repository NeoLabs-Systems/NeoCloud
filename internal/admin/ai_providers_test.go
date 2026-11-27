package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/settings"
)

func TestAdminProvidersMasksSecretsValidatesURLsAndUpdatesLiveCatalog(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	envFile := filepath.Join(t.TempDir(), "server.env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer very-secret-token" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"runtime-model"}]}`)
	}))
	defer provider.Close()
	settingsHandler, err := settings.New(settings.Options{
		DB: db.W,
		ResolvePrincipal: func(r *http.Request) (settings.Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return settings.Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{
		DB: db.W, EnvFile: envFile,
		ProviderKeys:     map[string]string{"openai": "abcd1234"},
		ProviderBaseURLs: map[string]string{"ollama": "http://localhost:11434"},
		OnProviderChanged: func(key, value string) {
			if !settingsHandler.SetServerProviderValue(key, value) {
				t.Errorf("settings rejected allowlisted key %q", key)
			}
		},
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
	listed := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/providers", "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "abcd1234") {
		t.Fatalf("provider list exposed credentials: %d %s", listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), `"hint":"••••••••"`) || !strings.Contains(listed.Body.String(), `"hint":"http://localhost:11434"`) {
		t.Fatalf("provider masks/URL hints=%s", listed.Body.String())
	}
	for _, body := range []string{
		`{"key":"NEOCLOUD_UNKNOWN_API_KEY","value":"secret"}`,
		`{"key":"NEOCLOUD_OLLAMA_URL","value":"ftp://localhost:11434"}`,
		`{"key":"NEOCLOUD_OLLAMA_URL","value":"https://user:pass@example.test/v1"}`,
		`{"key":"NEOCLOUD_OLLAMA_URL","value":"https://example.test:invalid/v1"}`,
	} {
		bad := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/providers", body)
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("invalid provider setting %s=%d %s", body, bad.Code, bad.Body.String())
		}
	}
	clearedByZero := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/providers", `{"key":"NEOCLOUD_OPENAI_API_KEY","value":0}`)
	if clearedByZero.Code != http.StatusOK {
		t.Fatalf("Node-compatible falsey value=%d %s", clearedByZero.Code, clearedByZero.Body.String())
	}
	updatedKey := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/providers", `{"key":"NEOCLOUD_OPENAI_API_KEY","value":"very-secret-token"}`)
	if updatedKey.Code != http.StatusOK || updatedKey.Body.String() != `{"ok":true}` {
		t.Fatalf("update key=%d %s", updatedKey.Code, updatedKey.Body.String())
	}
	updatedURL := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/providers", `{"key":"NEOCLOUD_OPENAI_BASE_URL","value":"`+provider.URL+`/v1"}`)
	if updatedURL.Code != http.StatusOK {
		t.Fatalf("update URL=%d %s", updatedURL.Code, updatedURL.Body.String())
	}
	catalog, err := settingsHandler.AdminModelCatalog(context.Background())
	if err != nil || len(catalog) != 1 || catalog[0].ID != "openai::runtime-model" || providerCalls == 0 {
		t.Fatalf("live catalog=%#v calls=%d err=%v", catalog, providerCalls, err)
	}
	listed = adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/providers", "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "very-secret-token") || !strings.Contains(listed.Body.String(), maskProviderSecret("very-secret-token")) || !strings.Contains(listed.Body.String(), provider.URL+"/v1") {
		t.Fatalf("updated provider list leaked secret or lost URL: %d %s", listed.Code, listed.Body.String())
	}
	env, err := os.ReadFile(envFile)
	if err != nil || !strings.Contains(string(env), "NEOCLOUD_OPENAI_API_KEY=very-secret-token") || !strings.Contains(string(env), "NEOCLOUD_OPENAI_BASE_URL="+provider.URL+"/v1") {
		t.Fatalf("provider env=%q err=%v", env, err)
	}
}
