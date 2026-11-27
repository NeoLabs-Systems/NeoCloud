package ai

import (
	"context"
	"encoding/base64"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// Every model call goes through Models, which checks that AI is on, the person
// opted in and their monthly budget isn't used up. Callers here check the files
// they read with assertAllowed before their content reaches a prompt.

const codeModelsUnavailable = "AI_MODELS_UNAVAILABLE"

func (h *Handler) modelError(err error) error {
	if h.options.ModelError != nil {
		return h.options.ModelError(err)
	}
	return err
}

func (h *Handler) chatJSON(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (map[string]any, string, error) {
	data, model, err := h.options.Models.ChatJSONForUserRole(ctx, userID, feature, role, messages)
	if err != nil {
		return nil, "", h.modelError(err)
	}
	return data, model, nil
}

func (h *Handler) chat(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (string, string, error) {
	text, model, err := h.options.Models.ChatForUserRole(ctx, userID, feature, role, messages)
	if err != nil {
		return "", "", h.modelError(err)
	}
	return text, model, nil
}

// embed returns one vector per text, or no vectors when there is no embedding
// model.
func (h *Handler) embed(ctx context.Context, userID int64, texts []string) ([][]float32, string, error) {
	const batch = 64
	vectors := make([][]float32, 0, len(texts))
	model := ""
	for start := 0; start < len(texts); start += batch {
		answer, served, err := h.options.Models.EmbedForUser(ctx, userID, "embed", texts[start:min(start+batch, len(texts))])
		if err != nil {
			if err = h.modelError(err); codeOf(err) == codeModelsUnavailable {
				return nil, "", nil
			}
			return nil, "", err
		}
		vectors, model = append(vectors, answer...), served
	}
	return vectors, model, nil
}

func imageMessage(jpeg []byte, prompt string) llm.Message {
	return llm.Message{Role: "user", Content: []any{
		map[string]any{"type": "text", "text": prompt},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)}},
	}}
}
