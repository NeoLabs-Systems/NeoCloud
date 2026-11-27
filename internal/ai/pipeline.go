package ai

import (
	"context"
	"slices"
	"strings"
)

// What one job does for one file: the shared extraction, then a rename, tag and
// move suggestion as asked and switched on, then auto-apply when the person
// wants it. Renames need a meaningless name unless the person asked for this
// file; camera photos get tags but no new name unless asked. Nothing is created
// or moved here.

type outcome struct {
	created int
	// wait: the indexer reads the file soon. skipped: why nothing was done.
	wait    bool
	skipped string
}

func renameReason(f fields) string {
	var what []string
	if f.Type != "" {
		what = append(what, f.Type)
	}
	if f.Issuer != "" {
		what = append(what, "from "+f.Issuer)
	}
	var parts []string
	if len(what) > 0 {
		parts = append(parts, strings.Join(what, " "))
	}
	if f.DateSource != "" {
		parts = append(parts, "date from "+f.DateSource)
	}
	return strings.Join(parts, ", ")
}

func (h *Handler) renameDraft(ctx context.Context, settings account, n node, f fields, model string) (*draft, error) {
	template, ok := settings.settings["ai_naming_template"].(string)
	if !ok {
		template = defaultTemplate
	}
	rendered := renderName(template, f, n.Name)
	if rendered == "" || rendered == n.Name {
		return nil, nil
	}
	name, err := h.options.Files.FreeName(ctx, n.ParentID, rendered, n.ID)
	if err != nil {
		return nil, err
	}
	return &draft{kind: "rename", from: n.Name, to: name, reason: renameReason(f), confidence: nameConfidence(template, f), model: model, signal: signalOf(f)}, nil
}

func (h *Handler) tagDraft(ctx context.Context, userID int64, n node, f fields, model string, allowNew bool) (*draft, error) {
	present, err := acceptedTags(ctx, h.db, n.ID)
	if err != nil {
		return nil, err
	}
	known, err := queryStrings(ctx, h.db, `SELECT DISTINCT j.value FROM ai_node_meta m,json_each(m.tags_json) j WHERE m.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	merged := slices.Clone(present)
	for _, tag := range f.Tags {
		if (allowNew || slices.Contains(known, tag)) && !slices.Contains(merged, tag) {
			merged = append(merged, tag)
		}
	}
	if len(merged) == len(present) {
		return nil, nil
	}
	return &draft{kind: "tags", from: present, to: merged, reason: f.Type, confidence: min(f.Confidence["tags"], f.Overall), model: model, signal: signalOf(f)}, nil
}

func (h *Handler) moveDraft(ctx context.Context, settings account, n node, f fields) (*draft, error) {
	decision, err := h.decideMove(ctx, settings, n, f, settings.features()["newFolders"])
	if err != nil || decision == nil {
		return nil, err
	}
	path, err := folderPath(ctx, h.db, settings.id, n.ParentID)
	if err != nil {
		return nil, err
	}
	result := &draft{kind: "move", from: folderRef{n.ParentID, path}, to: decision.to.encode(), reason: decision.reason, confidence: decision.confidence,
		model: decision.model, source: decision.source, ruleID: decision.ruleID, signal: signalOf(f)}
	if decision.to.ID == "" {
		result.move = &decision.to
	}
	return result, nil
}

// suggestForFile makes the suggestions for one of the owner's files. batch
// groups new-folder votes (the job's batch, or uploads).
func (h *Handler) suggestForFile(ctx context.Context, userID int64, nodeID string, kinds []string, explicit, waitForIndex bool, batch string) (outcome, error) {
	if err := assertAllowed(ctx, h.db, userID, nodeID); err != nil {
		return outcome{}, err
	}
	n, err := liveNode(ctx, h.db, userID, nodeID, "")
	if err != nil {
		return outcome{}, err
	}
	if n.Type != "file" {
		return outcome{skipped: "NOT_A_FILE"}, nil
	}
	settings, err := readSettings(ctx, h.db, userID)
	if err != nil {
		return outcome{}, err
	}
	if settings.skipsType(n.Name) {
		return outcome{skipped: "AI_TYPE_SKIPPED"}, nil
	}
	enabled := settings.features()
	wanted := map[string]bool{}
	for _, kind := range kinds {
		if enabled[kind] {
			wanted[kind] = true
		}
	}
	if len(wanted) == 0 {
		return outcome{skipped: "AI_FEATURE_OFF"}, nil
	}
	class := classifyName(n.Name)
	wantName := wanted["names"] && (explicit || class.meaningless)
	if !wantName && !wanted["tags"] && !wanted["folders"] {
		return outcome{}, nil
	}
	feature := "sort"
	if wantName {
		feature = "title"
	} else if wanted["tags"] {
		feature = "tags"
	}
	extracted, err := h.extract(ctx, userID, n, feature, waitForIndex)
	if err != nil || extracted.wait || extracted.skipped != "" {
		return outcome{wait: extracted.wait, skipped: extracted.skipped}, err
	}
	f, model := extracted.fields, extracted.model
	var drafts []*draft
	add := func(d *draft, err error) error {
		if d != nil {
			drafts = append(drafts, d)
		}
		return err
	}
	if wantName && (explicit || !(f.Camera && class.camera)) {
		if err = add(h.renameDraft(ctx, settings, n, f, model)); err != nil {
			return outcome{}, err
		}
	}
	if wanted["tags"] {
		if err = add(h.tagDraft(ctx, userID, n, f, model, enabled["newTags"])); err != nil {
			return outcome{}, err
		}
	}
	if wanted["folders"] {
		if err = add(h.moveDraft(ctx, settings, n, f)); err != nil {
			return outcome{}, err
		}
	}
	// The file may have changed while the model was thinking.
	after, err := liveNode(ctx, h.db, userID, n.ID, "")
	if err != nil {
		return outcome{}, err
	}
	if after.ETag != n.ETag || after.Name != n.Name || after.ParentID != n.ParentID {
		return outcome{skipped: "CHANGED"}, nil
	}
	var ready []readyDraft
	for _, d := range drafts {
		if d.move == nil {
			ready = append(ready, readyDraft{n, *d})
			continue
		}
		released, err := h.holdNewFolderMove(ctx, userID, n, *d, batch)
		if err != nil {
			return outcome{}, err
		}
		ready = append(ready, released...)
	}
	result := outcome{}
	var high []string
	for _, entry := range ready {
		id, tier, err := h.createSuggestion(ctx, userID, entry.node, entry.draft)
		if err != nil {
			return outcome{}, err
		}
		if id != "" {
			result.created++
			if tier == "high" {
				high = append(high, id)
			}
		}
	}
	if enabled["autoApplyHighConfidence"] {
		h.autoApply(ctx, userID, high)
	}
	return result, nil
}
