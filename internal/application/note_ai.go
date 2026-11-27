package application

import (
	"context"
	"errors"

	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/settings"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// noteAI gives note help the account's AI policy and models.
type noteAI struct {
	policy *ai.Handler
	chat   *settings.Handler
}

func (n noteAI) Check(ctx context.Context, userID int64, nodeID string) error {
	err := n.policy.CheckDirectFeature(ctx, userID, nodeID, "summaries")
	var refused *ai.DirectFeatureError
	if errors.As(err, &refused) {
		return apperr.New(refused.Status, refused.Message, refused.Code)
	}
	return err
}

func noteAIFailure(err error) error {
	message := settings.AIErrorMessage(err)
	if message == "" {
		message = "The AI model could not help with this note."
	}
	return apperr.New(settings.AIErrorStatus(err), message, settings.AIChatErrorCode(err))
}

func (n noteAI) Chat(ctx context.Context, userID int64, role string, messages []llm.Message) (string, string, error) {
	answer, model, err := n.chat.ChatForUserRole(ctx, userID, "notes", role, messages)
	if err != nil {
		return "", "", noteAIFailure(err)
	}
	return answer, model, nil
}

func (n noteAI) ChatJSON(ctx context.Context, userID int64, role string, messages []llm.Message) (map[string]any, string, error) {
	data, model, err := n.chat.ChatJSONForUserRole(ctx, userID, "notes", role, messages)
	if err != nil {
		return nil, "", noteAIFailure(err)
	}
	return data, model, nil
}
