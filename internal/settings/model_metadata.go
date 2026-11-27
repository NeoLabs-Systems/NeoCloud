package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

// Model is one model an account can see, as the apps read it.
type Model struct {
	ID                  string   `json:"id"`
	ModelID             string   `json:"modelId"`
	Provider            string   `json:"provider"`
	Label               string   `json:"label"`
	Kind                string   `json:"kind"`
	CreatedAt           *int64   `json:"createdAt"`
	SupportsTools       *bool    `json:"supportsTools"`
	SupportsVision      *bool    `json:"supportsVision"`
	Purpose             string   `json:"purpose"`
	PriceTier           *string  `json:"priceTier"`
	InputCostPerM       *float64 `json:"inputCostPerM"`
	Available           bool     `json:"available"`
	ProviderStatus      string   `json:"providerStatus"`
	ProviderStatusLabel string   `json:"providerStatusLabel"`
	IsBYOK              bool     `json:"isByok"`
	BYOKLabel           string   `json:"byokLabel"`
	AdminGranted        bool     `json:"adminGranted"`
}

// AdminModel is a model of the server's own catalog, for the admin's visibility
// and grant editors.
type AdminModel = Model

// AdminModelCatalog discovers models with the server's credentials only.
func (h *Handler) AdminModelCatalog(ctx context.Context) ([]AdminModel, error) {
	return h.supportedModels(ctx, 0)
}

// supportedModels is every model an account (0: the server) can see. Available
// is false for a model the admin turned off, unless granted to the account.
func (h *Handler) supportedModels(ctx context.Context, userID int64) ([]Model, error) {
	access, err := h.providerAccess(ctx, userID)
	if err != nil {
		return nil, err
	}
	return h.modelsFor(ctx, userID, access, h.providerHealth(ctx, access))
}

// A NeoRouter offers each kind of model only while that work goes through it.
var routerRoleByKind = map[string]string{kindChat: "chat", kindEmbedding: "embeddings", kindTranscription: "transcription"}

func (h *Handler) modelsFor(ctx context.Context, userID int64, access providerAccess, states []providerState) ([]Model, error) {
	granted, err := h.grantedModelIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	catalogs := make([][]listedModel, len(states))
	var workers sync.WaitGroup
	for i, state := range states {
		runtime, ok := access.runtime(state.ID)
		if !state.Available || !ok {
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			catalogs[i] = h.catalog(ctx, runtime)
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, catalog := range catalogs {
		if len(catalog) > 0 && states[i].ID != "ollama" {
			h.refreshReference(ctx)
			break
		}
	}
	models := []Model{}
	for i, state := range states {
		seen := map[string]bool{}
		for _, listed := range catalogs[i] {
			id := strings.TrimSpace(listed.ID)
			kind := listed.Kind
			if kind != kindEmbedding && kind != kindTranscription {
				kind = kindChat
			}
			// NeoRouter's "auto" is one id in several kinds.
			if id == "" || seen[kind+"\x00"+id] || state.ID == neorouter.ProviderID && !access.router.Handles(routerRoleByKind[kind]) {
				continue
			}
			seen[kind+"\x00"+id] = true
			models = append(models, h.describeModel(state, listed, id, kind, granted))
		}
	}
	return models, nil
}

func (h *Handler) describeModel(state providerState, listed listedModel, modelID, kind string, granted map[string]bool) Model {
	model := Model{
		ID: llm.SelectionID(state.ID, modelID), ModelID: modelID, Provider: state.ID, Label: modelLabel(state.ID, listed), Kind: kind,
		CreatedAt: listed.CreatedAt, SupportsTools: listed.SupportsTools, SupportsVision: listed.SupportsVision, Purpose: "general",
		ProviderStatus: state.Status, ProviderStatusLabel: state.StatusLabel, IsBYOK: state.IsBYOK, BYOKLabel: state.CustomLabel,
	}
	tier := listed.PriceTier
	// Local models cost nothing per token; every other model is looked up in the
	// reference catalog by name.
	if state.ID == "ollama" {
		model.InputCostPerM = new(float64)
	} else if facts, ok := h.reference.Lookup(modelID); ok {
		model.InputCostPerM = facts.InputCostPerM
		if facts.CreatedAtMs != nil {
			model.CreatedAt = facts.CreatedAtMs
		}
		if model.SupportsTools == nil {
			model.SupportsTools = facts.SupportsTools
		}
		if model.SupportsVision == nil {
			model.SupportsVision = facts.SupportsVision
		}
	}
	if tier == "" {
		tier = llm.PriceTierForCost(model.InputCostPerM)
	}
	if tier != "" {
		model.PriceTier = &tier
	}
	if tier == "cheap" {
		model.Purpose = "fast"
	}
	disabled := h.modelDisabled(model.ID) || h.modelDisabled(modelID)
	model.AdminGranted = disabled && (granted[model.ID] || granted[modelID])
	model.Available = state.Available && (!disabled || model.AdminGranted)
	return model
}

var providerSuffix = map[string]string{
	"openai": "OpenAI", "openai-compatible": "Custom OpenAI-compatible", "anthropic": "Anthropic",
	"google": "Google", "openrouter": "OpenRouter", "ollama": "Ollama / Local",
}

func modelLabel(provider string, listed listedModel) string {
	name := strings.TrimSpace(listed.Name)
	if name == "" || provider == "openai" || provider == "ollama" {
		name = listed.ID
	}
	if suffix := providerSuffix[provider]; suffix != "" {
		return name + " (" + suffix + ")"
	}
	return name
}

// grantedModelIDs are the models an admin allowed one account beyond what the
// server enables for everyone.
func (h *Handler) grantedModelIDs(ctx context.Context, userID int64) (map[string]bool, error) {
	granted := map[string]bool{}
	if userID < 1 {
		return granted, nil
	}
	var raw sql.NullString
	if err := h.db.QueryRowContext(ctx, `SELECT allowed_models FROM users WHERE id=?`, userID).Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var ids []any
	_ = json.Unmarshal([]byte(raw.String), &ids)
	for _, value := range ids {
		if id, ok := value.(string); ok {
			granted[strings.TrimSpace(id)] = true
		}
	}
	return granted, nil
}

// refreshReference loads the public catalog that supplies the facts most
// provider lists leave out. The request carries no key and nothing about users.
func (h *Handler) refreshReference(ctx context.Context) {
	if strings.EqualFold(h.reference.URL, "off") {
		return
	}
	// An address the admin set may be on the LAN.
	client := transport.Config{TrustedBaseURL: h.reference.URL != ""}.Client()
	if err := h.reference.EnsureFresh(ctx, client); err == nil {
		if err := h.reference.Err(); err != nil {
			h.log.Warn("Could not refresh the model reference catalog", "error", llm.SanitizeDetail(err.Error()))
		}
	}
}

// ResolveAIRoles picks the model each kind of work would use for an account.
func (h *Handler) ResolveAIRoles(ctx context.Context, userID int64, settings map[string]any) (map[string]ai.RoleStatus, error) {
	discovered, err := h.supportedModels(ctx, userID)
	if err != nil {
		return nil, err
	}
	return ai.ResolveRoleModels(routerModels(discovered), settings), nil
}

func routerModels(discovered []Model) []ai.Model {
	models := make([]ai.Model, 0, len(discovered))
	for _, model := range discovered {
		entry := ai.Model{ID: model.ID, Provider: model.Provider, ModelID: model.ModelID, Kind: model.Kind, Available: model.Available}
		entry.SupportsVision = model.SupportsVision != nil && *model.SupportsVision
		if model.PriceTier != nil {
			entry.PriceTier = *model.PriceTier
		}
		if model.CreatedAt != nil {
			entry.CreatedAt = *model.CreatedAt
		}
		models = append(models, entry)
	}
	return models
}

func (h *Handler) modelMetadata(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.principal(w, r)
	if !ok {
		return
	}
	models, err := h.supportedModels(r.Context(), userID)
	if err != nil {
		metadataError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func writeJSON(w http.ResponseWriter, status int, value any) { httpx.WriteJSON(w, status, value) }

func metadataError(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not load AI model information.", "code": "INTERNAL_ERROR"})
}
