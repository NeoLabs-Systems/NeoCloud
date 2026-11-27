package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

func newBYOKMux(t *testing.T) (*http.ServeMux, *cryptox.Keyring, *sql.DB) {
	t.Helper()
	h, db := newSettingsTest(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	h.keys = keys
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, keys, db.W
}

func byokRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), testPrincipalKey{}, Principal{UserID: 1}))
}

func TestBYOKSaveListsWithoutExposingKeyAndClearRemovesIt(t *testing.T) {
	mux, keys, db := newBYOKMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, byokRequest(http.MethodPut, "/api/settings/byok/openai", `{"apiKey":" sk-test-secret ","baseUrl":"https://8.8.8.8/v1","label":" My endpoint "}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, byokRequest(http.MethodGet, "/api/settings/byok/openai", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-test-secret") {
		t.Fatal("BYOK response exposed saved API key")
	}
	var result struct {
		Provider map[string]any `json:"provider"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Provider["configured"] != true || result.Provider["baseUrl"] != "https://8.8.8.8/v1" || result.Provider["customLabel"] != "My endpoint" {
		t.Fatalf("provider response=%v", result.Provider)
	}

	var stored string
	if err := db.QueryRow(`SELECT value FROM user_settings WHERE user_id=1 AND key=?`, providerSecretsSetting).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "sk-test-secret") {
		t.Fatal("API key was stored in plaintext")
	}
	plain, err := keys.DecryptValue(stored)
	if err != nil {
		t.Fatal(err)
	}
	var secretMap map[string]string
	if err := json.Unmarshal([]byte(plain), &secretMap); err != nil {
		t.Fatal(err)
	}
	if secretMap["openai"] != "sk-test-secret" {
		t.Fatalf("decrypted provider keys=%v", secretMap)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, byokRequest(http.MethodDelete, "/api/settings/byok/openai", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := db.QueryRow(`SELECT value FROM user_settings WHERE user_id=1 AND key=?`, providerSecretsSetting).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	plain, err = keys.DecryptValue(stored)
	if err != nil {
		t.Fatal(err)
	}
	secretMap = nil
	if err := json.Unmarshal([]byte(plain), &secretMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := secretMap["openai"]; ok {
		t.Fatalf("cleared API key remains: %v", secretMap)
	}
}

func TestBYOKRejectsPrivateURLsAndUnknownProviders(t *testing.T) {
	mux, _, _ := newBYOKMux(t)
	for _, tc := range []struct{ path, body string }{
		{"/api/settings/byok/openai", `{"apiKey":"sk-test","baseUrl":"http://127.0.0.1:8080"}`},
		{"/api/settings/byok/openai-compatible", `{"apiKey":"sk-test"}`},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, byokRequest(http.MethodPut, tc.path, tc.body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("path=%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, byokRequest(http.MethodGet, "/api/settings/byok/neorouter", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unsupported provider status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBYOKTestRequiresConfiguredCredential(t *testing.T) {
	mux, _, _ := newBYOKMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, byokRequest(http.MethodPost, "/api/settings/byok/openai/test", `{}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "API_KEY_REQUIRED") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
