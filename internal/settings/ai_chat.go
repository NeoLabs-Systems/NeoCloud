package settings

// The one way NeoCloud's features talk to a model. Each call checks that AI is
// on and the person opted in, checks their monthly budget, resolves the role's
// model, retries transient provider failures and records the usage as metadata
// only. Callers check the files they read before their content reaches a prompt.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/llm/providers"
)

const (
	aiJSONInstruction = "Reply with a single JSON object and nothing else."
	aiMaxOutputTokens = 4096
	aiAttemptTimeout  = 5 * time.Minute
)

// AIChatError is a refused or failed model call with its API code and status.
// Message is what a client is told; Error() is the underlying detail.
type AIChatError struct {
	Code    string
	Status  int
	Message string
	Err     error
}

func (e *AIChatError) Error() string {
	switch {
	case e.Err != nil:
		return e.Err.Error()
	case e.Message != "":
		return e.Message
	}
	return e.Code
}

func (e *AIChatError) Unwrap() error { return e.Err }

func AIChatErrorCode(err error) string {
	var chatErr *AIChatError
	if errors.As(err, &chatErr) {
		return chatErr.Code
	}
	return "PROVIDER_ERROR"
}

// AIErrorStatus is the HTTP status a failed model call answers with.
func AIErrorStatus(err error) int {
	var chatErr *AIChatError
	if errors.As(err, &chatErr) && chatErr.Status != 0 {
		return chatErr.Status
	}
	return http.StatusBadGateway
}

// AIErrorMessage is what a client is told about a failed model call, or "" when
// the failure is not a model call's.
func AIErrorMessage(err error) string {
	var chatErr *AIChatError
	if errors.As(err, &chatErr) {
		return chatErr.Message
	}
	return ""
}

// AIRequest is one chat call. Role is "text", "smart" or "vision"; a vision
// message carries OpenAI-style text and image_url data parts.
type AIRequest struct {
	Feature     string
	Role        string
	Messages    []llm.Message
	JSON        bool
	MaxTokens   int
	Temperature *float64
}

// AIAnswer is a model's reply. Model is the selected provider::model;
// ServedModel is what a router actually answered with.
type AIAnswer struct {
	Content     string
	Data        map[string]any
	Provider    string
	Model       string
	ServedModel string
	Usage       *llm.Usage
}

type preparedModel struct {
	provider llm.Provider
	id       string
	name     string
	modelID  string
}

// prepare applies the policy and resolves the role to a callable provider.
func (h *Handler) prepare(ctx context.Context, userID int64, role string) (preparedModel, error) {
	var prepared preparedModel
	settings, err := h.readAISettings(ctx, userID)
	if err != nil {
		return prepared, err
	}
	if err := h.assertAIUsable(ctx, userID, settings); err != nil {
		return prepared, err
	}
	unavailable := &AIChatError{Code: "AI_MODELS_UNAVAILABLE", Status: http.StatusServiceUnavailable,
		Message: "No AI model is available for " + role + " work yet. Ask the person who runs your NeoCloud, or add your own key in Settings › AI."}
	access, err := h.providerAccess(ctx, userID)
	if err != nil {
		return prepared, err
	}
	models, err := h.modelsFor(ctx, userID, access, h.providerHealth(ctx, access))
	if err != nil {
		return prepared, err
	}
	status := ai.ResolveRoleModels(routerModels(models), settings)[role]
	if !status.Ready || status.Model == nil {
		return prepared, unavailable
	}
	name, modelID, ok := strings.Cut(*status.Model, "::")
	runtime, known := access.runtime(name)
	if !ok || !known || !runtime.usable() {
		return prepared, unavailable
	}
	config := providers.Config{Config: runtime.transport(), Caches: h.caches}
	if name == "openrouter" {
		config.Headers = openRouterAttribution
	}
	provider, err := providers.New(name, config)
	if err != nil {
		return prepared, err
	}
	return preparedModel{provider: provider, id: *status.Model, name: name, modelID: modelID}, nil
}

// CompleteForUser runs one chat call for an account.
func (h *Handler) CompleteForUser(ctx context.Context, userID int64, request AIRequest) (*AIAnswer, error) {
	if userID < 1 || request.Feature == "" || (request.Role != "text" && request.Role != "smart" && request.Role != "vision") {
		return nil, &AIChatError{Code: "INVALID_REQUEST", Status: http.StatusBadRequest, Err: errors.New("settings: invalid AI chat request")}
	}
	model, err := h.prepare(ctx, userID, request.Role)
	if err != nil {
		return nil, err
	}
	messages := request.Messages
	if request.JSON {
		messages = append([]llm.Message{{Role: "system", Content: aiJSONInstruction}}, messages...)
	}
	options := llm.Options{Model: model.modelID, JSON: request.JSON, MaxTokens: request.MaxTokens, Temperature: request.Temperature}
	if options.MaxTokens <= 0 {
		options.MaxTokens = aiMaxOutputTokens
	}
	started := time.Now()
	result, err := llm.Retry(ctx, llm.DefaultRetryPolicy(), func(int) (*llm.Result, error) {
		attempt, cancel := context.WithTimeout(ctx, aiAttemptTimeout)
		defer cancel()
		return model.provider.Chat(attempt, messages, nil, options)
	})
	call := usageRecord{userID: userID, feature: request.Feature, provider: model.name, model: model.modelID, started: started}
	if err != nil {
		return nil, h.providerFailed(ctx, call, err)
	}
	answer := &AIAnswer{Content: result.Content, Provider: model.name, Model: model.id, ServedModel: firstSet(result.Model, model.modelID), Usage: result.Usage}
	call.usage = result.Usage
	if request.JSON {
		if answer.Data = parseAIJSON(result.Content); answer.Data == nil {
			call.errorCode = "AI_BAD_JSON"
			if err := h.recordAIUsage(ctx, call); err != nil {
				return nil, err
			}
			return nil, &AIChatError{Code: "AI_BAD_JSON", Status: http.StatusBadGateway, Message: "The AI model did not answer with valid JSON."}
		}
	}
	if err := h.recordAIUsage(ctx, call); err != nil {
		return nil, err
	}
	return answer, nil
}

var (
	publicCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	systemCode = regexp.MustCompile(`^(?:E[A-Z0-9]+|ERR_[A-Z0-9_]+|SQLITE_[A-Z0-9_]+)$`)
)

// providerFailed records a failed call and words it for the client. A caller
// that went away is not the provider's failure and is not recorded.
func (h *Handler) providerFailed(ctx context.Context, call usageRecord, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	call.errorCode = "PROVIDER_ERROR"
	if apiErr := llm.AsError(err); apiErr != nil {
		if publicCode.MatchString(apiErr.Code) && !systemCode.MatchString(apiErr.Code) {
			call.errorCode = apiErr.Code
		} else if apiErr.Status != 0 {
			call.errorCode = "HTTP_" + strconv.Itoa(apiErr.Status)
		}
	}
	h.log.Warn("An AI call failed", "feature", call.feature, "model", call.provider+"::"+call.model, "code", call.errorCode)
	// Provider text can echo a key back.
	detail := llm.SanitizeDetail(err.Error())
	failure := &AIChatError{Code: "AI_PROVIDER_ERROR", Status: http.StatusBadGateway,
		Message: "The AI provider could not answer: " + detail, Err: errors.New(detail)}
	if recordErr := h.recordAIUsage(ctx, call); recordErr != nil {
		return errors.Join(failure, recordErr)
	}
	return failure
}

// ChatJSONForUser asks the account's text model for a JSON object.
func (h *Handler) ChatJSONForUser(ctx context.Context, userID int64, feature string, messages []llm.Message) (map[string]any, string, error) {
	return h.ChatJSONForUserRole(ctx, userID, feature, "text", messages)
}

// ChatJSONForUserRole asks the role's model for a JSON object.
func (h *Handler) ChatJSONForUserRole(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (map[string]any, string, error) {
	answer, err := h.CompleteForUser(ctx, userID, AIRequest{Feature: feature, Role: role, Messages: messages, JSON: true})
	if err != nil {
		return nil, "", err
	}
	return answer.Data, answer.Model, nil
}

// ChatForUserRole asks the role's model for text.
func (h *Handler) ChatForUserRole(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (string, string, error) {
	answer, err := h.CompleteForUser(ctx, userID, AIRequest{Feature: feature, Role: role, Messages: messages})
	if err != nil {
		return "", "", err
	}
	return answer.Content, answer.Model, nil
}

func (h *Handler) readAISettings(ctx context.Context, userID int64) (map[string]any, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT key,value FROM user_settings WHERE user_id=? AND key IN ('ai_opt_in','ai_model_text','ai_model_smart','ai_model_vision','ai_model_embedding','ai_model_transcription')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]any{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		if json.Unmarshal([]byte(raw), &value) == nil {
			settings[key] = value
		}
	}
	return settings, rows.Err()
}

// parseAIJSON reads a JSON object, with the code fences some models add anyway.
func parseAIJSON(raw string) map[string]any {
	start, end := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return nil
	}
	var result map[string]any
	if json.Unmarshal([]byte(raw[start:end+1]), &result) != nil {
		return nil
	}
	return result
}

// embeddingModelID maps a selection to what the endpoint takes: NeoRouter
// embeds with plain "auto", the profile its key was approved with.
func embeddingModelID(provider, modelID string) string {
	if provider == neorouter.ProviderID && modelID == "auto:default" {
		return "auto"
	}
	return modelID
}
