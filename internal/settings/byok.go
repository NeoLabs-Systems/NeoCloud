package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const (
	providerConfigsSetting = "ai_provider_configs"
	providerSecretsSetting = "ai_provider_api_keys"
)

type byokConfig struct {
	BaseURL string `json:"baseUrl"`
	Label   string `json:"label"`
}

// NeoRouter is connected with its own sign-in, not a pasted key.
func byokDefinition(id string) (providerDefinition, bool) {
	definition, ok := definitionFor(id)
	return definition, ok && definition.Authentication != "oauth"
}

func (h *Handler) byokRoutes(mux *http.ServeMux) {
	// Saving and testing a key may make the server call a typed address.
	limiter := ratelimit.New(ratelimit.Config{
		Window:  15 * time.Minute,
		Max:     30,
		Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"},
	})
	mux.HandleFunc("GET /api/settings/byok", h.listBYOK)
	mux.HandleFunc("GET /api/settings/byok/{providerId}", h.getBYOK)
	mux.Handle("PUT /api/settings/byok/{providerId}", limiter.Middleware(http.HandlerFunc(h.saveBYOK)))
	mux.Handle("DELETE /api/settings/byok/{providerId}", limiter.Middleware(http.HandlerFunc(h.clearBYOK)))
	mux.Handle("POST /api/settings/byok/{providerId}/test", limiter.Middleware(http.HandlerFunc(h.testBYOK)))
}

// readBYOK returns a person's provider settings. It covers every provider,
// NeoRouter included, so a write keeps what another feature stored.
func (h *Handler) readBYOK(ctx context.Context, userID int64) (map[string]byokConfig, map[string]string, error) {
	read := func(key string) (string, error) {
		var value string
		err := h.db.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id=? AND key=?`, userID, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return value, err
	}
	rawConfigs, err := read(providerConfigsSetting)
	if err != nil {
		return nil, nil, err
	}
	rawSecrets, err := read(providerSecretsSetting)
	if err != nil {
		return nil, nil, err
	}
	var stored map[string]byokConfig
	_ = json.Unmarshal([]byte(rawConfigs), &stored)
	configs := make(map[string]byokConfig, len(providerDefinitions))
	for _, definition := range providerDefinitions {
		configs[definition.ID] = normalizeBYOKConfig(definition, stored[definition.ID])
	}
	secrets := map[string]string{}
	if rawSecrets != "" && h.keys != nil {
		// Unreadable data holds no keys.
		plain, err := h.keys.DecryptValue(rawSecrets)
		if err != nil || json.Unmarshal([]byte(plain), &secrets) != nil || secrets == nil {
			secrets = map[string]string{}
		}
	}
	return configs, secrets, nil
}

func normalizeBYOKConfig(d providerDefinition, c byokConfig) byokConfig {
	if !d.SupportsBaseURL {
		c.BaseURL = ""
	}
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Label = strings.TrimSpace(c.Label)
	if runes := []rune(c.Label); len(runes) > 80 {
		c.Label = string(runes[:80])
	}
	return c
}

func (h *Handler) writeBYOK(ctx context.Context, userID int64, configs map[string]byokConfig, secrets map[string]string) error {
	configJSON, err := json.Marshal(configs)
	if err != nil {
		return err
	}
	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	sealed, err := h.keys.EncryptValue(string(secretsJSON))
	if err != nil {
		return err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{providerConfigsSetting: string(configJSON), providerSecretsSetting: sealed} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings(user_id,key,value,updated_at) VALUES(?,?,?,datetime('now')) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, userID, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type byokView struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Description      string `json:"description"`
	SupportsAPIKey   bool   `json:"supportsApiKey"`
	SupportsBaseURL  bool   `json:"supportsBaseUrl"`
	RequiresBaseURL  bool   `json:"requiresBaseUrl"`
	DefaultBaseURL   string `json:"defaultBaseUrl"`
	IsCustomEndpoint bool   `json:"isCustomEndpoint"`
	Configured       bool   `json:"configured"`
	BaseURL          string `json:"baseUrl"`
	CustomLabel      string `json:"customLabel"`
}

// byokViews describes the providers a person can bring a key for. Only their own
// address and label are shown, never the server's.
func (h *Handler) byokViews(ctx context.Context, userID int64) ([]byokView, error) {
	access, err := h.providerAccess(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]byokView, 0, len(access.runtimes))
	for _, r := range access.runtimes {
		if _, ok := byokDefinition(r.ID); !ok {
			continue
		}
		view := byokView{ID: r.ID, Label: r.providerDefinition.Label, Description: r.Description, SupportsAPIKey: r.SupportsAPIKey,
			SupportsBaseURL: r.SupportsBaseURL, RequiresBaseURL: r.RequiresBaseURL, DefaultBaseURL: r.DefaultBaseURL,
			IsCustomEndpoint: r.ID == "openai-compatible", Configured: r.BYOK}
		if r.BYOK {
			view.BaseURL, view.CustomLabel = r.BaseURL, r.Label
		}
		views = append(views, view)
	}
	return views, nil
}

func (h *Handler) byokPrincipal(w http.ResponseWriter, r *http.Request) (int64, bool) {
	uid, ok := h.principal(w, r)
	if ok && h.keys == nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "BYOK encryption is unavailable.", "code": "SERVICE_UNAVAILABLE"})
		return 0, false
	}
	return uid, ok
}

// byokRequest resolves the account and the provider a route names.
func (h *Handler) byokRequest(w http.ResponseWriter, r *http.Request) (int64, providerDefinition, bool) {
	uid, ok := h.byokPrincipal(w, r)
	if !ok {
		return 0, providerDefinition{}, false
	}
	definition, ok := byokDefinition(r.PathValue("providerId"))
	if !ok {
		byokRefused(w, http.StatusNotFound, "Unknown or unsupported provider.", "UNKNOWN_PROVIDER")
	}
	return uid, definition, ok
}

func (h *Handler) listBYOK(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.byokPrincipal(w, r)
	if !ok {
		return
	}
	views, err := h.byokViews(r.Context(), uid)
	if err != nil {
		byokInternalError(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": views})
}

func (h *Handler) getBYOK(w http.ResponseWriter, r *http.Request) {
	uid, d, ok := h.byokRequest(w, r)
	if !ok {
		return
	}
	views, err := h.byokViews(r.Context(), uid)
	if err != nil {
		byokInternalError(w)
		return
	}
	for _, view := range views {
		if view.ID == d.ID {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider": view})
			return
		}
	}
	byokInternalError(w)
}

// byokBody is what a save or test carries; nil fields were not sent.
type byokBody struct {
	APIKey         string
	BaseURL, Label *string
}

func readBYOKBody(w http.ResponseWriter, r *http.Request) (byokBody, bool) {
	var raw map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		byokRefused(w, http.StatusBadRequest, "Request must be a JSON object.", "INVALID_SETTINGS")
		return byokBody{}, false
	}
	text := func(key string) *string {
		value, ok := raw[key].(string)
		if !ok {
			return nil
		}
		value = strings.TrimSpace(value)
		return &value
	}
	body := byokBody{BaseURL: text("baseUrl"), Label: text("label")}
	if key := text("apiKey"); key != nil {
		body.APIKey = *key
	}
	return body, true
}

func (b byokBody) baseURL() string {
	if b.BaseURL == nil {
		return ""
	}
	return *b.BaseURL
}

// A provider without a key (Ollama) is only ever "yours" by pointing it at your
// own address, so that address is mandatory.
func baseURLRequired(d providerDefinition) bool { return d.RequiresBaseURL || !d.SupportsAPIKey }

// checkBYOKBaseURL refuses an address that resolves into the server's own
// network, unless it is the one the admin configured. Calls are checked again at
// dial time.
func (h *Handler) checkBYOKBaseURL(w http.ResponseWriter, r *http.Request, d providerDefinition, baseURL string) bool {
	if !isHTTPURL(baseURL) {
		byokRefused(w, http.StatusBadRequest, "Base URL must be a valid http(s) URL.", "INVALID_URL")
		return false
	}
	if _, serverURL := h.serverProviderConfig(d.ID); serverURL != "" && baseURL == strings.TrimSpace(serverURL) {
		return true
	}
	if err := neorouter.CheckPublicURL(r.Context(), baseURL, nil); err != nil {
		byokRefused(w, http.StatusBadRequest, err.Error(), "URL_NOT_ALLOWED")
		return false
	}
	return true
}

func (h *Handler) saveBYOK(w http.ResponseWriter, r *http.Request) {
	uid, d, ok := h.byokRequest(w, r)
	if !ok {
		return
	}
	body, ok := readBYOKBody(w, r)
	if !ok {
		return
	}
	if d.SupportsAPIKey && body.APIKey == "" {
		byokRefused(w, http.StatusBadRequest, "An API key is required.", "API_KEY_REQUIRED")
		return
	}
	if baseURLRequired(d) && body.baseURL() == "" {
		byokRefused(w, http.StatusBadRequest, "A base URL is required for this provider.", "BASE_URL_REQUIRED")
		return
	}
	if body.baseURL() != "" && !h.checkBYOKBaseURL(w, r, d, body.baseURL()) {
		return
	}
	configs, secrets, err := h.readBYOK(r.Context(), uid)
	if err != nil {
		byokInternalError(w)
		return
	}
	if d.SupportsAPIKey {
		secrets[d.ID] = body.APIKey
	}
	if d.SupportsBaseURL {
		config := configs[d.ID]
		if body.BaseURL != nil {
			config.BaseURL = *body.BaseURL
		}
		if body.Label != nil {
			config.Label = *body.Label
		}
		configs[d.ID] = normalizeBYOKConfig(d, config)
	}
	if err := h.writeBYOK(r.Context(), uid, configs, secrets); err != nil {
		byokInternalError(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "configured": true})
}

func (h *Handler) clearBYOK(w http.ResponseWriter, r *http.Request) {
	uid, d, ok := h.byokRequest(w, r)
	if !ok {
		return
	}
	configs, secrets, err := h.readBYOK(r.Context(), uid)
	if err != nil {
		byokInternalError(w)
		return
	}
	if d.SupportsAPIKey {
		delete(secrets, d.ID)
	}
	if d.SupportsBaseURL {
		config := configs[d.ID]
		config.BaseURL = ""
		if !d.SupportsAPIKey {
			config.Label = ""
		}
		configs[d.ID] = config
	}
	if err := h.writeBYOK(r.Context(), uid, configs, secrets); err != nil {
		byokInternalError(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "configured": false})
}

// testBYOK tries a key or endpoint the person is about to save, or the saved
// setup when the body names none, by listing models. Nothing is stored.
func (h *Handler) testBYOK(w http.ResponseWriter, r *http.Request) {
	uid, d, ok := h.byokRequest(w, r)
	if !ok {
		return
	}
	body, ok := readBYOKBody(w, r)
	if !ok {
		return
	}
	if body.baseURL() != "" && body.APIKey == "" && d.SupportsAPIKey {
		byokRefused(w, http.StatusBadRequest, "Testing a custom base URL requires the API key for that endpoint.", "API_KEY_REQUIRED")
		return
	}
	access, err := h.providerAccess(r.Context(), uid)
	if err != nil {
		byokInternalError(w)
		return
	}
	runtime, _ := access.runtime(d.ID)
	if body.APIKey != "" {
		runtime.APIKey = body.APIKey
	}
	if body.baseURL() != "" {
		_, serverURL := h.serverProviderConfig(d.ID)
		runtime.BaseURL, runtime.Trusted = body.baseURL(), body.baseURL() == strings.TrimSpace(serverURL)
	}
	if d.SupportsAPIKey && runtime.APIKey == "" {
		byokRefused(w, http.StatusBadRequest, "No API key to test.", "API_KEY_REQUIRED")
		return
	}
	if baseURLRequired(d) && runtime.BaseURL == "" {
		byokRefused(w, http.StatusBadRequest, "A base URL is required.", "BASE_URL_REQUIRED")
		return
	}
	if runtime.BaseURL != "" && !h.checkBYOKBaseURL(w, r, d, runtime.BaseURL) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	models, err := h.listModels(ctx, runtime)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "ok": false, "error": llm.SanitizeDetail(err.Error())})
		return
	}
	message := "Connected, but no models were returned."
	if len(models) > 0 {
		message = fmt.Sprintf("Connected — found %d model(s).", len(models))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "ok": true, "message": message})
}

func byokRefused(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

func byokInternalError(w http.ResponseWriter) {
	byokRefused(w, http.StatusInternalServerError, "Could not read or update provider settings.", "INTERNAL_ERROR")
}
