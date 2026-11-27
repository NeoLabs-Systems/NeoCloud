package ai

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Where a file belongs. Restraint first: "keep it where it is" is the default,
// a move must be clearly better.
//
//   - A file at the top level or in an inbox-like folder may go into any folder
//     it may move to; one in a folder the person made stays in its subtree.
//   - Photos and videos only go into the existing photo tree, by year and month
//     when the person sorts it that way, never into a new folder. Documents are
//     never filed into the photo tree.
//   - Cheapest signal first: the owner's rules, then embedding similarity (a
//     clear winner decides), then the text model among the ten closest folders.

const (
	topCandidates          = 10
	decisiveScore          = 0.92
	decisiveMargin         = 0.05
	minConfidence          = 0.6
	minNewFolderConfidence = 0.7
	photoConfidence        = 0.8
)

// Kinds of pictures that are documents, filed like documents.
var documentTypes = []string{"invoice", "receipt", "contract", "payslip", "letter", "statement", "certificate", "manual", "report", "notes", "presentation", "spreadsheet", "scan"}

// moveTarget is the destination of a move suggestion. A new folder has no ID
// yet.
type moveTarget struct {
	ID       string
	ParentID string
	Name     string
	Path     string
	Key      string
}

func (t moveTarget) encode() map[string]any {
	if t.ID != "" {
		return map[string]any{"id": t.ID, "path": t.Path}
	}
	return map[string]any{"id": nil, "parentId": t.ParentID, "name": t.Name, "path": t.Path}
}

type move struct {
	to         moveTarget
	confidence float64
	reason     string
	source     string
	ruleID     int64
	model      string
}

func isImageOrVideo(n node) bool {
	return (strings.HasPrefix(n.MIME, "image/") || strings.HasPrefix(n.MIME, "video/")) && n.MIME != "image/svg+xml"
}

func segments(path string) []string {
	return strings.FieldsFunc(path, func(char rune) bool { return char == '/' })
}

func inMediaTree(f folder) bool { return slices.ContainsFunc(segments(f.Path), isMediaName) }

func within(f, ancestor folder) bool {
	prefix := ancestor.Path
	if prefix == "/" {
		prefix = ""
	}
	return f.ID == ancestor.ID || strings.HasPrefix(f.Path, prefix+"/")
}

func mediaRoot(n node, f fields, folders []folder) *folder {
	wanted := []string{"photos"}
	if f.Type == "screenshot" {
		wanted = []string{"screenshots", "photos"}
	} else if strings.HasPrefix(n.MIME, "video/") {
		wanted = []string{"videos", "photos"}
	}
	var roots []folder
	for _, candidate := range folders {
		parents := segments(candidate.Path)
		if candidate.Path != "/" && isMediaName(candidate.Name) && !slices.ContainsFunc(parents[:len(parents)-1], isMediaName) {
			roots = append(roots, candidate)
		}
	}
	for _, name := range wanted {
		if index := slices.IndexFunc(roots, func(root folder) bool { return conceptOf(root.Name) == name }); index >= 0 {
			return &roots[index]
		}
	}
	return nil
}

// datedFolder is the year (and month) folder below the photo root, when the
// person has them.
func datedFolder(folders []folder, root folder, date string) folder {
	if len(date) < 7 {
		return root
	}
	year, _ := strconv.Atoi(date[:4])
	month, _ := strconv.Atoi(date[5:7])
	child := func(parent string, matches func(string) bool) *folder {
		index := slices.IndexFunc(folders, func(f folder) bool { return f.ParentID == parent && matches(f.Name) })
		if index < 0 {
			return nil
		}
		return &folders[index]
	}
	yearFolder := child(root.ID, func(name string) bool { return isYearName(name, year) })
	if yearFolder == nil {
		return root
	}
	if month >= 1 && month <= 12 {
		if found := child(yearFolder.ID, func(name string) bool { return isMonthName(name, month) }); found != nil {
			return *found
		}
	}
	return *yearFolder
}

func photoMove(n node, f fields, folders []folder) *move {
	root := mediaRoot(n, f, folders)
	if root == nil {
		return nil
	}
	target := datedFolder(folders, *root, f.Date)
	if target.ID == n.ParentID {
		return nil
	}
	return &move{to: moveTarget{ID: target.ID, Path: target.Path}, confidence: photoConfidence, reason: "Photos and videos are kept in " + root.Path, source: "ai"}
}

// parseChoice is the index a 1-based answer names, or -1.
func parseChoice(value any, count int) int {
	chosen, ok := number(value)
	if !ok || chosen != math.Trunc(chosen) || chosen < 1 || chosen > float64(count) {
		return -1
	}
	return int(chosen) - 1
}

// decideMove is a move for a file with its extraction, or nil when it stays.
func (h *Handler) decideMove(ctx context.Context, settings account, n node, f fields, allowNewFolders bool) (*move, error) {
	folders, err := candidateFolders(ctx, h.db, settings.id, n.ParentID)
	if err != nil {
		return nil, err
	}
	find := func(id string) *folder {
		index := slices.IndexFunc(folders, func(candidate folder) bool { return candidate.ID == id })
		if index < 0 {
			return nil
		}
		return &folders[index]
	}
	rule, err := matchingRule(ctx, h.db, settings.id, n.Name, f)
	if err != nil {
		return nil, err
	}
	if rule != nil {
		if target := find(rule.target); target != nil {
			if target.ID == n.ParentID {
				return nil, nil
			}
			return &move{to: moveTarget{ID: target.ID, Path: target.Path}, confidence: 1, reason: "Rule: " + rule.name, source: "rule", ruleID: rule.id}, nil
		}
	}
	current := find(n.ParentID)
	if current == nil {
		return nil, nil
	}
	open := current.Path == "/" || isInboxName(current.Name)
	if isImageOrVideo(n) && !slices.Contains(documentTypes, f.Type) {
		if !open {
			return nil, nil
		}
		return photoMove(n, f, folders), nil
	}
	var scoped []folder
	for _, candidate := range folders {
		if !inMediaTree(candidate) && (open || within(candidate, *current)) {
			scoped = append(scoped, candidate)
		}
	}
	allowNew := allowNewFolders && !isImageOrVideo(n)
	if len(scoped) == 0 || len(scoped) < 2 && !allowNew {
		return nil, nil
	}
	ranked, embeddingModel, err := h.rankFolders(ctx, settings.id, n, f, scoped)
	if err != nil {
		return nil, err
	}
	if embeddingModel != "" && len(ranked) > 1 && ranked[0].score >= decisiveScore && ranked[0].score-ranked[1].score >= decisiveMargin {
		best := ranked[0].folder
		if best.ID == n.ParentID {
			return nil, nil
		}
		return &move{to: moveTarget{ID: best.ID, Path: best.Path}, confidence: ranked[0].score, reason: "Similar to the files in " + best.Path, source: "ai", model: embeddingModel}, nil
	}
	candidates := make([]folder, min(len(ranked), topCandidates))
	for index := range candidates {
		candidates[index] = ranked[index].folder
	}
	names := make([]string, len(folders))
	for index, item := range folders {
		names[index] = item.Name
	}
	language := folderLanguage(names)
	for _, fallback := range []string{settings.text("language"), f.Language, "en"} {
		if language == "" {
			language = fallback
		}
	}
	answer, model, err := h.chatJSON(ctx, settings.id, "sort", "text", sortMessages(n.Name, f, candidates, allowNew, language))
	if err != nil {
		return nil, err
	}
	decision := &move{confidence: clamp(answer["confidence"], 0.5), reason: shortText(answer["reason"], 200), source: "ai", model: model}
	if index := parseChoice(answer["choice"], len(candidates)); index >= 0 {
		target := candidates[index]
		if target.ID == n.ParentID || decision.confidence < minConfidence {
			return nil, nil
		}
		decision.to = moveTarget{ID: target.ID, Path: target.Path}
		return decision, nil
	}
	proposed, _ := answer["newFolder"].(string)
	if !allowNew || proposed == "" || decision.confidence < minNewFolderConfidence {
		return nil, nil
	}
	parent := *current
	if open {
		parent = folders[0]
	}
	if index := parseChoice(answer["parent"], len(candidates)); index >= 0 {
		if slices.ContainsFunc(scoped, func(item folder) bool { return item.ID == candidates[index].ID }) {
			parent = candidates[index]
		}
	}
	existing, name, err := h.resolveNewFolder(ctx, settings.id, proposed, parent, scoped, folders, language)
	if err != nil {
		return nil, err
	}
	switch {
	case existing != nil:
		if existing.ID == n.ParentID {
			return nil, nil
		}
		decision.to = moveTarget{ID: existing.ID, Path: existing.Path}
	case name != "":
		decision.to = moveTarget{ParentID: parent.ID, Name: name, Path: strings.TrimSuffix(parent.Path, "/") + "/" + name, Key: folderKey(cleanNewFolderName(proposed))}
	default:
		return nil, nil
	}
	return decision, nil
}
