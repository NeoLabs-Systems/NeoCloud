package settings

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

// The AI providers NeoCloud can use. Server-wide credentials come from the env
// file; a person's own keys (BYOK) are stored encrypted in user_settings.

type providerDefinition struct {
	ID, Label, Description, Authentication           string
	SupportsAPIKey, SupportsBaseURL, RequiresBaseURL bool
	DefaultBaseURL                                   string
}

var providerDefinitions = []providerDefinition{
	{ID: "openai", Label: "OpenAI", Description: "GPT models for text and images, plus OpenAI embeddings.", Authentication: "api_key", SupportsAPIKey: true, SupportsBaseURL: true},
	{ID: "openai-compatible", Label: "Custom OpenAI-compatible", Description: "Models exposed by a custom OpenAI-compatible endpoint (vLLM, LM Studio, LiteLLM, …).", Authentication: "api_key", SupportsAPIKey: true, SupportsBaseURL: true, RequiresBaseURL: true},
	{ID: "anthropic", Label: "Anthropic", Description: "Claude models for long documents and careful summaries.", Authentication: "api_key", SupportsAPIKey: true, SupportsBaseURL: true},
	{ID: "google", Label: "Google", Description: "Gemini models with large context windows and image understanding.", Authentication: "api_key", SupportsAPIKey: true},
	{ID: "openrouter", Label: "OpenRouter", Description: "Hundreds of models through one API, including free ones. Get a key at openrouter.ai.", Authentication: "api_key", SupportsAPIKey: true, SupportsBaseURL: true, DefaultBaseURL: "https://openrouter.ai/api/v1"},
	// Connected through "Connect NeoRouter", not a pasted key.
	{ID: neorouter.ProviderID, Label: "NeoRouter", Description: "Your NeoRouter: the models of all its providers through one connection.", Authentication: "oauth", SupportsAPIKey: true, SupportsBaseURL: true, RequiresBaseURL: true},
	// Off until an address is set: a file server should not probe its own
	// localhost for a model server nobody configured.
	{ID: "ollama", Label: "Ollama", Description: "Local models running on an Ollama server.", Authentication: "local", SupportsBaseURL: true, RequiresBaseURL: true},
}

func definitionFor(id string) (providerDefinition, bool) {
	for _, definition := range providerDefinitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return providerDefinition{}, false
}

// providerRuntime is what one account would call a provider with.
type providerRuntime struct {
	providerDefinition
	APIKey, BaseURL, Label string
	BYOK                   bool
	// Trusted marks an address the admin configured, which may be on the LAN.
	Trusted bool
}

func (r providerRuntime) usable() bool {
	return (!r.SupportsAPIKey || r.APIKey != "") && (!r.RequiresBaseURL || isHTTPURL(r.BaseURL))
}

func (r providerRuntime) transport() transport.Config {
	return transport.Config{APIKey: r.APIKey, BaseURL: r.BaseURL, TrustedBaseURL: r.Trusted}
}

type providerAccess struct {
	runtimes []providerRuntime
	router   neorouter.Connection
}

func (a providerAccess) runtime(id string) (providerRuntime, bool) {
	for _, runtime := range a.runtimes {
		if runtime.ID == id {
			return runtime, true
		}
	}
	return providerRuntime{}, false
}

func (h *Handler) routerConnection(ctx context.Context, userID int64) (neorouter.Connection, error) {
	if h.neoRouter == nil {
		return neorouter.Connection{}, nil
	}
	return h.neoRouter(ctx, userID)
}

// providerAccess resolves every provider for one account; userID 0 is the
// server itself.
func (h *Handler) providerAccess(ctx context.Context, userID int64) (providerAccess, error) {
	var access providerAccess
	configs, secrets := map[string]byokConfig{}, map[string]string{}
	var err error
	if userID > 0 {
		if configs, secrets, err = h.readBYOK(ctx, userID); err != nil {
			return access, err
		}
	}
	if access.router, err = h.routerConnection(ctx, userID); err != nil {
		return access, err
	}
	serverRouter, err := h.routerConnection(ctx, 0)
	if err != nil {
		return access, err
	}
	operator := map[string]bool{originOf(serverRouter.APIBase): true}
	for _, definition := range providerDefinitions {
		_, baseURL := h.serverProviderConfig(definition.ID)
		operator[originOf(baseURL)] = true
	}
	delete(operator, "")
	for _, definition := range providerDefinitions {
		serverKey, serverURL := h.serverProviderConfig(definition.ID)
		ownURL := configs[definition.ID].BaseURL
		if definition.ID == neorouter.ProviderID {
			serverKey, serverURL = serverRouter.APIKey, serverRouter.APIBase
			ownURL = neorouter.APIBase(ownURL)
		}
		serverKey, serverURL = strings.TrimSpace(serverKey), strings.TrimSpace(serverURL)
		// A person's own address is not allowed to point into the server's
		// network unless it is the one the admin configured.
		if ownURL != "" && ownURL != definition.DefaultBaseURL && ownURL != serverURL {
			if parsed, parseErr := url.Parse(ownURL); parseErr != nil || neorouter.PrivateName(parsed.Hostname()) {
				ownURL = ""
			}
		}
		ownKey := strings.TrimSpace(secrets[definition.ID])
		runtime := providerRuntime{providerDefinition: definition, Label: configs[definition.ID].Label}
		if definition.SupportsAPIKey {
			runtime.BYOK = ownKey != ""
			runtime.APIKey = firstSet(ownKey, serverKey)
		} else {
			runtime.BYOK = ownURL != ""
		}
		// A stored user address belongs to that user's own key. The server key
		// is sent only to the operator's address.
		if definition.SupportsBaseURL {
			runtime.BaseURL = firstSet(serverURL, definition.DefaultBaseURL)
			if runtime.BYOK {
				runtime.BaseURL = firstSet(ownURL, runtime.BaseURL)
			}
		}
		runtime.Trusted = operator[originOf(runtime.BaseURL)]
		access.runtimes = append(access.runtimes, runtime)
	}
	return access, nil
}

func firstSet(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func originOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}

type providerState struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	Authentication       string `json:"authentication"`
	SupportsAPIKey       bool   `json:"supportsApiKey"`
	SupportsBaseURL      bool   `json:"supportsBaseUrl"`
	RequiresBaseURL      bool   `json:"requiresBaseUrl"`
	DefaultBaseURL       string `json:"defaultBaseUrl"`
	Available            bool   `json:"available"`
	CredentialConfigured bool   `json:"credentialConfigured"`
	BaseURLConfigured    bool   `json:"baseUrlConfigured"`
	BaseURL              string `json:"baseUrl"`
	CustomLabel          string `json:"customLabel"`
	IsBYOK               bool   `json:"isByok"`
	Status               string `json:"status"`
	StatusLabel          string `json:"statusLabel"`
	AvailabilityReason   string `json:"availabilityReason"`
	Connected            bool   `json:"connected"`
	Healthy              bool   `json:"healthy"`
	ModelCount           int    `json:"modelCount"`
	AvailableModelCount  int    `json:"availableModelCount"`
}

// providerHealth says whether each provider can be called. A configured Ollama
// is probed for reachability.
func (h *Handler) providerHealth(ctx context.Context, access providerAccess) []providerState {
	states := make([]providerState, 0, len(access.runtimes))
	for _, r := range access.runtimes {
		state := providerState{
			ID: r.ID, Label: r.Label, Description: r.Description, Authentication: r.Authentication,
			SupportsAPIKey: r.SupportsAPIKey, SupportsBaseURL: r.SupportsBaseURL, RequiresBaseURL: r.RequiresBaseURL,
			DefaultBaseURL: r.DefaultBaseURL, CredentialConfigured: r.APIKey != "", BaseURLConfigured: r.BaseURL != "",
			BaseURL: r.BaseURL, CustomLabel: r.Label, IsBYOK: r.BYOK, Available: r.usable(),
		}
		state.Label = r.providerDefinition.Label
		switch {
		case r.SupportsAPIKey && r.APIKey == "":
			state.Status, state.StatusLabel, state.AvailabilityReason = "needs_setup", "Setup Needed", "Credentials for this provider are not available on this server yet."
		case r.RequiresBaseURL && r.BaseURL == "":
			state.Status, state.StatusLabel, state.AvailabilityReason = "needs_setup", "Setup Needed", "An address (base URL) is required for this provider."
		case r.RequiresBaseURL && !isHTTPURL(r.BaseURL):
			state.Status, state.StatusLabel, state.AvailabilityReason = "needs_setup", "Setup Needed", "The configured base URL must be an HTTP or HTTPS URL without embedded credentials."
		case r.ID == "ollama":
			probe := h.probeOllama(ctx, r)
			state.Available, state.Status, state.StatusLabel, state.AvailabilityReason = probe.healthy, "offline", "Offline", probe.reason
			if probe.healthy {
				state.Status, state.StatusLabel = "healthy", "Healthy"
			}
		default:
			state.Status, state.StatusLabel, state.AvailabilityReason = "healthy", "Healthy", "Credentials for this provider are available."
		}
		state.Connected, state.Healthy = state.Available, state.Available
		states = append(states, state)
	}
	return states
}

const ollamaProbeTTL = 30 * time.Second

type ollamaProbe struct {
	healthy bool
	reason  string
	expires time.Time
}

// probeOllama keeps the outcome briefly, so an offline server does not add a
// timeout to every catalog read.
func (h *Handler) probeOllama(ctx context.Context, r providerRuntime) ollamaProbe {
	h.cacheMu.Lock()
	cached, ok := h.probes[r.BaseURL]
	h.cacheMu.Unlock()
	if ok && h.now().Before(cached.expires) {
		return cached
	}
	probe := askOllama(ctx, r)
	if ctx.Err() != nil {
		return probe
	}
	probe.expires = h.now().Add(ollamaProbeTTL)
	h.cacheMu.Lock()
	if len(h.probes) >= maxCachedCatalogs {
		clear(h.probes)
	}
	h.probes[r.BaseURL] = probe
	h.cacheMu.Unlock()
	return probe
}

func askOllama(ctx context.Context, r providerRuntime) ollamaProbe {
	response, err := transport.FetchText(ctx, r.transport().Client(), transport.FetchOptions{
		URL: strings.TrimRight(r.BaseURL, "/") + "/api/tags", MaxBytes: 2 << 20, Service: "Ollama health check", Timeout: 1500 * time.Millisecond,
	})
	if err != nil {
		return ollamaProbe{reason: "Could not reach Ollama at the configured address."}
	}
	if !response.OK() {
		return ollamaProbe{reason: fmt.Sprintf("Ollama returned HTTP %d.", response.Status)}
	}
	count := countTags(response.Text)
	if count == 0 {
		return ollamaProbe{healthy: true, reason: "Connected to Ollama, but no local models were reported."}
	}
	return ollamaProbe{healthy: true, reason: fmt.Sprintf("Connected to Ollama with %d local model(s).", count)}
}

func (h *Handler) providerMetadata(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.principal(w, r)
	if !ok {
		return
	}
	access, err := h.providerAccess(r.Context(), userID)
	if err != nil {
		metadataError(w)
		return
	}
	var admin int
	if err := h.db.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=? AND disabled_at IS NULL`, userID).Scan(&admin); err != nil {
		metadataError(w)
		return
	}
	states := h.providerHealth(r.Context(), access)
	models, err := h.modelsFor(r.Context(), userID, access, states)
	if err != nil {
		metadataError(w)
		return
	}
	for i := range states {
		state := &states[i]
		for _, model := range models {
			if model.Provider != state.ID {
				continue
			}
			state.ModelCount++
			if model.Available {
				state.AvailableModelCount++
			}
		}
		// The operator's addresses are not people's business.
		if admin != 1 && !state.IsBYOK && state.BaseURL != "" {
			state.AvailabilityReason = strings.ReplaceAll(state.AvailabilityReason, state.BaseURL, "the configured address")
			state.BaseURL = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": states})
}
