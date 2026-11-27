package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/llm/anthropic"
	"github.com/NeoLabs-Systems/neolabs-go/llm/google"
	"github.com/NeoLabs-Systems/neolabs-go/llm/ollama"
	"github.com/NeoLabs-Systems/neolabs-go/llm/openaichat"
)

const (
	catalogLifetime         = 5 * time.Minute
	permanentCatalogBackoff = 30 * time.Minute
	discoveryTimeout        = 10 * time.Second
	maxCachedCatalogs       = 256

	kindChat          = "chat"
	kindEmbedding     = "embedding"
	kindTranscription = "transcription"
)

// listedModel is one entry of a provider's catalog; unknowns stay nil or "".
type listedModel struct {
	ID, Name, Kind, PriceTier     string
	CreatedAt                     *int64
	SupportsTools, SupportsVision *bool
}

type cachedCatalog struct {
	models  []listedModel
	expires time.Time
}

// Models OpenAI lists that neither chat nor embed nor transcribe.
var openAIDrop = regexp.MustCompile(`(?i)dall-e|tts|moderat|realtime|audio|search-api|-image-|babbage|davinci-002|^sora|-instruct|deep-research|computer-use|gpt-live`)

var (
	embeddingID     = regexp.MustCompile(`(?i)embed`)
	transcriptionID = regexp.MustCompile(`(?i)whisper|transcribe`)
	permanentError  = regexp.MustCompile(`(?i)401|403|unauthorized|forbidden|credits|spending`)
	autoModelID     = regexp.MustCompile(`^auto(-(free|plan|paid))?(:|$)`)
	autoModelName   = regexp.MustCompile(`^Auto \((.*)\)$`)
)

// kindFromModelID reads the kind of an entry in an OpenAI-style /models list,
// which states none.
func kindFromModelID(id string) string {
	switch {
	case embeddingID.MatchString(id):
		return kindEmbedding
	case transcriptionID.MatchString(id):
		return kindTranscription
	}
	return kindChat
}

// catalog is a provider's models, cached per key and address. A failed refresh
// keeps serving the previous list and backs off.
func (h *Handler) catalog(ctx context.Context, r providerRuntime) []listedModel {
	digest := sha256.Sum256([]byte(r.ID + "\x00" + r.APIKey + "\x00" + r.BaseURL))
	key := hex.EncodeToString(digest[:])
	h.cacheMu.Lock()
	existing, ok := h.catalogs[key]
	h.cacheMu.Unlock()
	if ok && h.now().Before(existing.expires) {
		return existing.models
	}
	models, err := h.refreshes.Do(ctx, key, func() ([]listedModel, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discoveryTimeout)
		defer cancel()
		models, err := h.listModels(refreshCtx, r)
		entry := cachedCatalog{models: models, expires: h.now().Add(catalogLifetime)}
		if err != nil {
			h.log.Warn("Could not refresh a model catalog", "provider", r.ID, "error", llm.SanitizeDetail(err.Error()))
			entry.models = existing.models
			if status := llm.AsError(err); status != nil && (status.Status == 401 || status.Status == 403) || permanentError.MatchString(err.Error()) {
				entry.expires = h.now().Add(permanentCatalogBackoff)
			}
		}
		h.cacheMu.Lock()
		if len(h.catalogs) >= maxCachedCatalogs {
			clear(h.catalogs)
		}
		h.catalogs[key] = entry
		h.cacheMu.Unlock()
		return entry.models, nil
	})
	if err != nil {
		return nil
	}
	return models
}

// listModels asks a provider for its live catalog.
func (h *Handler) listModels(ctx context.Context, r providerRuntime) ([]listedModel, error) {
	config := r.transport()
	switch r.ID {
	case "openai":
		list, err := openaichat.New(config).ModelList(ctx, "")
		return openAIModels(list.Data, func(id string) bool { return !openAIDrop.MatchString(id) }), err
	case "openai-compatible":
		list, err := openaichat.NewCompatible(config).ModelList(ctx, "")
		return openAIModels(list.Data, func(string) bool { return true }), err
	case "anthropic":
		entries, err := anthropic.New(config).Catalog(ctx)
		models := make([]listedModel, 0, len(entries))
		for _, entry := range entries {
			// Every current Claude model reads images.
			models = append(models, fromFacts(entry.ID, entry.Name, kindChat, entry.Raw, boolPointer(true)))
		}
		return models, err
	case "google":
		return h.googleModels(ctx, r)
	case "openrouter":
		return h.openRouterModels(ctx, r)
	case neorouter.ProviderID:
		return neoRouterModels(ctx, r)
	case "ollama":
		// The Ollama catalog reads an unreachable server as empty, so reachability
		// is checked first to keep the previous list through an outage.
		if probe := askOllama(ctx, r); !probe.healthy {
			return nil, errors.New(probe.reason)
		}
		entries, err := ollama.New(ollama.Config{Config: config}).Catalog(ctx)
		models := make([]listedModel, 0, len(entries))
		for _, entry := range entries {
			kind := string(entry.Kind)
			if kind == "" {
				kind = kindChat
				if embeddingID.MatchString(entry.ID) {
					kind = kindEmbedding
				}
			}
			models = append(models, fromFacts(entry.ID, entry.Name, kind, entry.Raw, nil))
		}
		return models, err
	}
	return nil, errors.New("settings: unknown provider " + r.ID)
}

func fromFacts(id, name, kind string, raw map[string]any, vision *bool) listedModel {
	facts := llm.ReadCatalogFacts(raw)
	if vision == nil {
		vision = facts.SupportsVision
	}
	return listedModel{ID: id, Name: name, Kind: kind, CreatedAt: facts.CreatedAtMs, SupportsTools: facts.SupportsTools, SupportsVision: vision}
}

func openAIModels(data []map[string]any, keep func(string) bool) []listedModel {
	models := make([]listedModel, 0, len(data))
	for _, raw := range data {
		id, _ := raw["id"].(string)
		if strings.TrimSpace(id) == "" || !keep(id) {
			continue
		}
		name, _ := raw["name"].(string)
		kind := kindFromModelID(id)
		if declared := llm.ExplicitModelKind(raw); declared != "" {
			kind = string(declared)
		}
		models = append(models, fromFacts(id, name, kind, raw, nil))
	}
	return models
}

func (h *Handler) googleModels(ctx context.Context, r providerRuntime) ([]listedModel, error) {
	provider := google.New(r.transport(), nil)
	chat, err := provider.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]listedModel, 0, len(chat))
	seen := map[string]bool{}
	for _, entry := range chat {
		seen[entry.ID] = true
		// Gemini models are multimodal; Gemma models served here are not.
		models = append(models, listedModel{ID: entry.ID, Name: entry.Name, Kind: kindChat, SupportsVision: boolPointer(strings.HasPrefix(entry.ID, "gemini-"))})
	}
	embeddings, err := provider.EmbeddingModels(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range embeddings {
		if !seen[entry.ID] {
			models = append(models, listedModel{ID: entry.ID, Name: entry.Name, Kind: kindEmbedding})
		}
	}
	return models, nil
}

var openRouterAttribution = map[string]string{"HTTP-Referer": "https://github.com/NeoLabs-Systems/NeoCloud", "X-Title": "NeoCloud"}

// openRouterModels lists chat models and feeds the reference catalog with
// OpenRouter's prices and modalities.
func (h *Handler) openRouterModels(ctx context.Context, r providerRuntime) ([]listedModel, error) {
	config := r.transport()
	config.Headers = openRouterAttribution
	entries, err := openaichat.NewOpenRouter(config, nil).Catalog(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]listedModel, 0, len(entries))
	reference := make([]any, 0, len(entries))
	for _, entry := range entries {
		architecture, _ := entry.Raw["architecture"].(map[string]any)
		if outputs, ok := architecture["output_modalities"].([]any); ok && !containsValue(outputs, "text") {
			continue
		}
		reference = append(reference, entry.Raw)
		models = append(models, fromFacts(entry.ID, entry.Name, kindChat, entry.Raw, nil))
	}
	h.reference.Record(reference)
	return models, nil
}

// neoRouterModels lists the chat, embedding and speech-to-text models of a
// NeoRouter, whose own list carries prices and abilities. A kind it cannot list
// is left out; only when none can be listed is that an error.
func neoRouterModels(ctx context.Context, r providerRuntime) ([]listedModel, error) {
	provider := openaichat.NewNeoRouter(r.transport(), nil)
	var models []listedModel
	var failure error
	listed := false
	for _, kind := range []string{kindChat, kindEmbedding, kindTranscription} {
		list, err := provider.ModelList(ctx, kind)
		if err != nil {
			failure = err
			continue
		}
		listed = true
		for _, raw := range list.Data {
			if id, _ := raw["id"].(string); id != "" {
				models = append(models, neoRouterModel(id, kind, raw))
			}
		}
	}
	if !listed {
		return nil, failure
	}
	return models, nil
}

func neoRouterModel(id, kind string, raw map[string]any) listedModel {
	name, _ := raw["name"].(string)
	if name == "" {
		name = id
	}
	if autoModelID.MatchString(id) {
		// "auto" alone is not a valid selection id, so plain auto is listed as
		// "auto:default". NeoRouter sends images only to models that see them.
		if !strings.Contains(id, ":") {
			id += ":default"
		}
		label := "NeoRouter " + name
		if match := autoModelName.FindStringSubmatch(name); match != nil {
			label = "NeoRouter Auto · " + strings.Join(strings.Split(match[1], ", "), " · ")
		}
		return listedModel{ID: id, Name: label, Kind: kind, SupportsTools: boolPointer(true), SupportsVision: boolPointer(kind == kindChat)}
	}
	facts, _ := raw["neorouter"].(map[string]any)
	model := fromFacts(id, name, kind, raw, nil)
	model.CreatedAt = nil
	if tools, ok := facts["tools"].(bool); ok {
		model.SupportsTools = &tools
	}
	if vision, ok := facts["vision"].(bool); ok {
		model.SupportsVision = &vision
	}
	free, _ := facts["free"].(bool)
	plan, _ := facts["plan"].(bool)
	if free || plan {
		model.PriceTier = "free"
	} else {
		model.PriceTier = llm.PriceTierForCost(llm.ReadCatalogFacts(raw).InputCostPerM)
	}
	return model
}

func countTags(body string) int {
	var payload struct {
		Models []json.RawMessage `json:"models"`
	}
	_ = json.Unmarshal([]byte(body), &payload)
	return len(payload.Models)
}

func containsValue(values []any, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func boolPointer(value bool) *bool { return &value }
