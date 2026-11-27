package settings

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// EmbedForUser returns one vector per text from the account's embedding model,
// under the same policy, retries and metering as a chat call.
func (h *Handler) EmbedForUser(ctx context.Context, userID int64, feature string, texts []string) ([][]float32, string, error) {
	if userID < 1 || feature == "" || len(texts) == 0 {
		return nil, "", &AIChatError{Code: "INVALID_REQUEST", Status: http.StatusBadRequest, Err: errors.New("settings: invalid embedding request")}
	}
	model, err := h.prepare(ctx, userID, "embedding")
	if err != nil {
		return nil, "", err
	}
	embedder, ok := model.provider.(llm.EmbeddingProvider)
	if !ok {
		return nil, "", &AIChatError{Code: "AI_MODELS_UNAVAILABLE", Status: http.StatusServiceUnavailable, Message: "Provider '" + model.name + "' does not serve embeddings."}
	}
	started := time.Now()
	result, err := llm.Retry(ctx, llm.DefaultRetryPolicy(), func(int) (*llm.EmbeddingResult, error) {
		attempt, cancel := context.WithTimeout(ctx, aiAttemptTimeout)
		defer cancel()
		result, err := embedder.Embed(attempt, texts, llm.Options{Model: embeddingModelID(model.name, model.modelID)})
		if err == nil && len(result.Vectors) != len(texts) {
			err = &llm.Error{Provider: model.name, Message: "The provider returned the wrong number of embeddings."}
		}
		return result, err
	})
	call := usageRecord{userID: userID, feature: feature, provider: model.name, model: model.modelID, started: started}
	if err != nil {
		return nil, "", h.providerFailed(ctx, call, err)
	}
	call.usage = result.Usage
	if err := h.recordAIUsage(ctx, call); err != nil {
		return nil, "", err
	}
	return result.Vectors, model.id, nil
}
