package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

// The one structured call per file that auto-title, tags and sorting share. The
// result is kept per node and content (ai_node_meta), so a second feature asks
// nothing again.

const maxExistingTags = 60

type extraction struct {
	fields fields
	model  string
	// wait: the indexer is about to read the file. skipped: nothing a model can
	// read.
	wait    bool
	skipped string
}

func existingTags(ctx context.Context, q querier, userID int64) ([]string, error) {
	return queryStrings(ctx, q, `SELECT DISTINCT lower(j.value) FROM ai_node_meta m,json_each(m.tags_json) j WHERE m.user_id=? LIMIT ?`, userID, maxExistingTags)
}

func acceptedTags(ctx context.Context, q querier, nodeID string) ([]string, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT tags_json FROM ai_node_meta WHERE node_id=?`, nodeID).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	tags := []string{}
	if raw != "" && json.Unmarshal([]byte(raw), &tags) != nil {
		tags = []string{}
	}
	return tags, nil
}

func searchText(f fields, tags []string) string {
	words := append([]string{}, tags...)
	for _, value := range []string{f.Issuer, f.Subject, f.Type, f.Caption} {
		if value != "" {
			words = append(words, value)
		}
	}
	return strings.ToLower(strings.Join(words, " "))
}

func (h *Handler) cachedExtraction(ctx context.Context, userID int64, n node) (*extraction, error) {
	var raw string
	var model sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT fields_json,model FROM ai_node_meta WHERE node_id=? AND user_id=? AND etag=?`, n.ID, userID, n.ETag).Scan(&raw, &model)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if json.Unmarshal([]byte(raw), &values) != nil || len(values) == 0 {
		return nil, nil
	}
	return &extraction{fields: decodeFields(values), model: model.String}, nil
}

func (h *Handler) saveExtraction(ctx context.Context, userID int64, n node, f fields, model string) error {
	return h.inTx(ctx, func(tx *sql.Tx) error {
		tags, err := acceptedTags(ctx, tx, n.ID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_node_meta(node_id,user_id,etag,fields_json,tags_json,search_text,model,updated_at)
			VALUES(?,?,?,?,?,?,?,datetime('now')) ON CONFLICT(node_id) DO UPDATE SET etag=excluded.etag,fields_json=excluded.fields_json,
			search_text=excluded.search_text,model=excluded.model,updated_at=excluded.updated_at`,
			n.ID, userID, n.ETag, jsonText(f.encode()), jsonText(tags), searchText(f, tags), nullableString(model))
		return err
	})
}

// saveTags stores the tags the owner accepted for a node and refreshes its
// search words.
func saveTags(ctx context.Context, tx *sql.Tx, userID int64, n node, tags []string) error {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT fields_json FROM ai_node_meta WHERE node_id=?`, n.ID).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var values map[string]any
	_ = json.Unmarshal([]byte(raw), &values)
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_node_meta(node_id,user_id,etag,tags_json,search_text,updated_at)
		VALUES(?,?,?,?,?,datetime('now')) ON CONFLICT(node_id) DO UPDATE SET tags_json=excluded.tags_json,
		search_text=excluded.search_text,updated_at=excluded.updated_at`,
		n.ID, userID, n.ETag, jsonText(append([]string{}, tags...)), searchText(decodeFields(values), tags))
	return err
}

func (h *Handler) extract(ctx context.Context, userID int64, n node, feature string, waitForIndex bool) (extraction, error) {
	cached, err := h.cachedExtraction(ctx, userID, n)
	if err != nil {
		return extraction{}, err
	}
	if cached != nil {
		return *cached, nil
	}
	input, err := h.readFileInput(ctx, userID, n, waitForIndex)
	if err != nil {
		return extraction{}, err
	}
	switch input.kind {
	case "wait":
		return extraction{wait: true}, nil
	case "none":
		return extraction{skipped: input.reason}, nil
	}
	tags, err := existingTags(ctx, h.db, userID)
	if err != nil {
		return extraction{}, err
	}
	system := llm.Message{Role: "system", Content: extractionInstructions(tags)}
	modified, _, _ := strings.Cut(n.ModifiedAt, " ")
	describe := "File name: " + n.Name + "\nModified: " + modified
	role, user := "text", llm.Message{Role: "user", Content: describe + "\n\nContent:\n" + input.text}
	text := &input.text
	if input.kind == "image" {
		role, user, text = "vision", imageMessage(input.image, describe), nil
	}
	data, model, err := h.chatJSON(ctx, userID, feature, role, []llm.Message{system, user})
	if err != nil {
		if role == "vision" && codeOf(err) == codeModelsUnavailable {
			return extraction{skipped: "AI_NO_VISION"}, nil
		}
		return extraction{}, err
	}
	f := normalizeExtraction(data, text, n, input.exif)
	if err = h.saveExtraction(ctx, userID, n, f, model); err != nil {
		return extraction{}, err
	}
	return extraction{fields: f, model: model}, nil
}
