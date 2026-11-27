package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/settings"
)

func TestAdminModelVisibilityReconcilesAndPersistsPolicy(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	envFile := filepath.Join(t.TempDir(), "settings.env")
	if err := os.WriteFile(envFile, []byte("NEOCLOUD_DISABLED_MODELS=provider::old\nNEOCLOUD_KNOWN_MODELS=provider::old\nNEOCLOUD_AUTO_ENABLE_NEW_MODELS=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	models := []settings.AdminModel{
		{ID: "provider::old", ModelID: "old", Provider: "provider", Available: true},
		{ID: "provider::new", ModelID: "new", Provider: "provider", Available: true},
	}
	var applied []string
	h, err := New(Options{
		DB: db.W, EnvFile: envFile,
		DisabledModelIDs: []string{"provider::old"}, KnownModelIDs: []string{"provider::old"},
		KnownModelsConfigured: true, AutoEnableNewModels: false, AutoEnableNewModelsConfigured: true,
		AdminModelCatalog:       func(_ context.Context) ([]settings.AdminModel, error) { return models, nil },
		OnDisabledModelsChanged: func(ids []string) { applied = append([]string(nil), ids...) },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/models", "")
	if response.Code != http.StatusOK {
		t.Fatalf("model catalog=%d %s", response.Code, response.Body.String())
	}
	var catalog struct {
		Models              []settings.AdminModel `json:"models"`
		DisabledModels      []string              `json:"disabledModels"`
		AutoEnableNewModels bool                  `json:"autoEnableNewModels"`
		OK                  *bool                 `json:"ok"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.OK != nil || !reflect.DeepEqual(catalog.DisabledModels, []string{"provider::old", "provider::new"}) || catalog.AutoEnableNewModels {
		t.Fatalf("GET shape/policy = %s", response.Body.String())
	}
	if len(catalog.Models) != 2 || catalog.Models[0].Available || catalog.Models[1].Available || catalog.Models[0].ID != "provider::old" || catalog.Models[1].ID != "provider::new" {
		t.Fatalf("new model not disabled in returned catalog: %#v", catalog.Models)
	}
	if !reflect.DeepEqual(applied, []string{"provider::old", "provider::new"}) {
		t.Fatalf("live settings disabled ids=%v", applied)
	}
	env, err := os.ReadFile(envFile)
	if err != nil || !strings.Contains(string(env), "NEOCLOUD_KNOWN_MODELS=provider::old,provider::new") || !strings.Contains(string(env), "NEOCLOUD_DISABLED_MODELS=provider::old,provider::new") {
		t.Fatalf("reconciled env=%q err=%v", env, err)
	}

	// A transient empty provider catalog must not prune known or disabled IDs.
	models = nil
	empty := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/models", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"provider::new"`) {
		t.Fatalf("empty catalog pruned saved policy: %d %s", empty.Code, empty.Body.String())
	}

	updated := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/models/config", `{"disabledModels":[" provider::new ","provider::new"],"autoEnableNewModels":true}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update visibility=%d %s", updated.Code, updated.Body.String())
	}
	var result struct {
		OK                  bool     `json:"ok"`
		Models              []any    `json:"models"`
		DisabledModels      []string `json:"disabledModels"`
		AutoEnableNewModels bool     `json:"autoEnableNewModels"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Models != nil || !reflect.DeepEqual(result.DisabledModels, []string{"provider::new"}) || !result.AutoEnableNewModels {
		t.Fatalf("PUT shape/policy=%s", updated.Body.String())
	}
	env, err = os.ReadFile(envFile)
	if err != nil || !strings.Contains(string(env), "NEOCLOUD_DISABLED_MODELS=provider::new") || !strings.Contains(string(env), "NEOCLOUD_AUTO_ENABLE_NEW_MODELS=true") {
		t.Fatalf("updated env=%q err=%v", env, err)
	}
}

func TestAdminModelVisibilityDoesNotChangeMemoryWhenEnvWriteFails(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	h, err := New(Options{
		DB: db.W, EnvFile: t.TempDir(),
		DisabledModelIDs: []string{"provider::old"}, AutoEnableNewModels: true,
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
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/models/config", `{"disabledModels":["provider::new"],"autoEnableNewModels":false}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed write=%d %s", response.Code, response.Body.String())
	}
	response = adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/models", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"provider::old"`) || strings.Contains(response.Body.String(), `"provider::new"`) {
		t.Fatalf("failed write changed live state: %d %s", response.Code, response.Body.String())
	}
}
