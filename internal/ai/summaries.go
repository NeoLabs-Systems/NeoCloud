package ai

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// A summary of one file on demand, from the smart role (images: the vision
// role). Kept per content (etag), encrypted under the owner's key and labelled
// with the model that wrote it.

const maxSummaryRunes = 4000

// Summary is one file's summary as the apps read it.
type Summary struct {
	Summary   string `json:"summary"`
	Model     string `json:"model"`
	ETag      string `json:"etag"`
	CreatedAt string `json:"createdAt"`
	Cached    bool   `json:"cached"`
}

func summaryAAD(nodeID string) []byte { return []byte("ai-summary:" + nodeID) }

func (h *Handler) summarize(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	if err = h.assertUsable(settings); err != nil {
		return nil, err
	}
	if err = settings.assertFeature("summaries"); err != nil {
		return nil, err
	}
	n, err := liveNode(ctx, h.db, userID, r.PathValue("id"), "file")
	if err != nil {
		return nil, err
	}
	if err = assertAllowed(ctx, h.db, userID, n.ID); err != nil {
		return nil, err
	}
	if err = settings.assertTypeAllowed(n.Name); err != nil {
		return nil, err
	}
	if input["refresh"] != true {
		kept := Summary{ETag: n.ETag, Cached: true}
		var sealed []byte
		err = h.db.QueryRowContext(ctx, `SELECT summary_enc,model,created_at FROM ai_summaries WHERE node_id=? AND user_id=? AND etag=?`, n.ID, userID, n.ETag).Scan(&sealed, &kept.Model, &kept.CreatedAt)
		if err == nil {
			dek, err := h.options.Keys.GetUserDEK(ctx, h.db, userID)
			if err != nil {
				return nil, err
			}
			plain, err := cryptox.OpenBytes(dek, sealed, summaryAAD(n.ID))
			if err != nil {
				return nil, err
			}
			kept.Summary = string(plain)
			return kept, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	content, err := h.readFileInput(ctx, userID, n, false)
	if err != nil {
		return nil, err
	}
	role, user := "smart", llm.Message{Role: "user", Content: "File name: " + n.Name + "\n\nContent:\n" + content.text}
	switch content.kind {
	case "text":
	case "image":
		role, user = "vision", imageMessage(content.image, "File name: "+n.Name)
	default:
		return nil, fail(http.StatusUnprocessableEntity, "There is no text in this file to summarize.", "AI_NO_CONTENT")
	}
	answer, model, err := h.chat(ctx, userID, "summary", role, []llm.Message{{Role: "system", Content: summaryInstructions}, user})
	if err != nil {
		return nil, err
	}
	text := truncate(strings.TrimSpace(answer), maxSummaryRunes)
	if text == "" {
		return nil, fail(http.StatusBadGateway, "The AI model returned no summary.", "AI_PROVIDER_ERROR")
	}
	dek, err := h.options.Keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	sealed, err := cryptox.SealBytes(dek, []byte(text), summaryAAD(n.ID))
	if err != nil {
		return nil, err
	}
	result := Summary{Summary: text, Model: model, ETag: n.ETag}
	err = h.db.QueryRowContext(ctx, `INSERT INTO ai_summaries(node_id,user_id,etag,summary_enc,model,created_at) VALUES(?,?,?,?,?,datetime('now'))
		ON CONFLICT(node_id) DO UPDATE SET etag=excluded.etag,summary_enc=excluded.summary_enc,model=excluded.model,created_at=excluded.created_at
		RETURNING created_at`, n.ID, userID, n.ETag, sealed, model).Scan(&result.CreatedAt)
	if err != nil {
		return nil, err
	}
	return result, nil
}
