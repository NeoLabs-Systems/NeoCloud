package notes

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"golang.org/x/text/unicode/norm"
)

// Notes are Markdown files under the account's /Notes folder; a subfolder is a
// notebook. Every change goes through the file services, so it keeps the change
// journal, versions, quota and trash, and nothing inside a vault is reached.

const (
	notesFolder  = "Notes"
	maxNoteBytes = 1 << 20
	maxTitle     = 120
)

var (
	titleSeparators = regexp.MustCompile(`[\x00-\x1f\x7f/\\]`)
	whitespace      = regexp.MustCompile(`\s+`)
	titleNoise      = regexp.MustCompile(`^[#>\-*+\s]+|\[[ xX]\]\s*`)
	numbered        = regexp.MustCompile(` \(\d+\)$`)
)

type note struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Notebook   string    `json:"notebook"`
	Snippet    string    `json:"snippet"`
	Checklist  checklist `json:"checklist"`
	Tags       []string  `json:"tags"`
	Pinned     bool      `json:"pinned"`
	Archived   bool      `json:"archived"`
	Color      string    `json:"color"`
	Size       int64     `json:"size"`
	ModifiedAt string    `json:"modifiedAt"`
	Revision   string    `json:"revision"`
}

type noteWithBody struct {
	note
	Body string `json:"body"`
}

type encryptedNotebook struct {
	VaultID  string `json:"vaultId"`
	FolderID string `json:"folderId"`
	Name     string `json:"name"`
}

type tagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type listing struct {
	RootID             *string             `json:"rootId"`
	Notes              []note              `json:"notes"`
	Notebooks          []string            `json:"notebooks"`
	EncryptedNotebooks []encryptedNotebook `json:"encryptedNotebooks"`
	Tags               []tagCount          `json:"tags"`
}

// entry is a row under /Notes with its path relative to that folder.
type entry struct {
	agentapi.NotesFile
	rel string
}

func (e entry) isNote() bool {
	return !e.Vault && e.Type == "file" && e.Size <= maxNoteBytes && strings.EqualFold(path.Ext(e.Name), ".md")
}

func (e entry) folder() string {
	if index := strings.LastIndex(e.rel, "/"); index >= 0 {
		return e.rel[:index]
	}
	return ""
}

func titleOf(name string) string {
	if strings.EqualFold(path.Ext(name), ".md") {
		return name[:len(name)-3]
	}
	return name
}

// scan is everything live under /Notes, vault contents included but marked.
func (h *Handler) scan(ctx context.Context, userID int64) ([]entry, error) {
	rows, err := h.files.NotesTree(ctx, userID)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(rows))
	for _, row := range rows {
		_, rel, _ := strings.Cut(row.Path, "/")
		entries = append(entries, entry{NotesFile: row, rel: rel})
	}
	return entries, nil
}

func noteNotFound() error { return apperr.New(http.StatusNotFound, "Note not found", "NOT_FOUND") }

// find is a note of the account by id: a live .md file under /Notes. Another
// account's id answers like a missing one.
func (h *Handler) find(ctx context.Context, userID int64, id string) (entry, error) {
	entries, err := h.scan(ctx, userID)
	if err != nil {
		return entry{}, err
	}
	for _, candidate := range entries {
		if candidate.ID == id && candidate.isNote() {
			return candidate, nil
		}
	}
	return entry{}, noteNotFound()
}

func (h *Handler) read(ctx context.Context, userID int64, id string) (document, error) {
	data, _, err := h.files.ReadNote(ctx, userID, id)
	if err != nil {
		return document{}, err
	}
	return parseNote(string(data)), nil
}

func contentOf(doc document) content {
	snippet, progress := summarize(doc.body)
	return content{properties: doc.properties, snippet: snippet, checklist: progress}
}

func cardOf(e entry, c content) note {
	return note{
		ID: e.ID, Title: titleOf(e.Name), Notebook: e.folder(), Snippet: c.snippet, Checklist: c.checklist,
		Tags: c.Tags, Pinned: c.Pinned, Archived: c.Archived, Color: c.Color,
		Size: e.Size, ModifiedAt: e.ModifiedAt, Revision: e.Revision,
	}
}

func (h *Handler) card(ctx context.Context, userID int64, e entry) (note, error) {
	if kept, ok := h.cache.get(userID, e.ID, e.Revision); ok {
		return cardOf(e, kept), nil
	}
	doc, err := h.read(ctx, userID, e.ID)
	if err != nil {
		return note{}, err
	}
	value := contentOf(doc)
	h.cache.put(userID, e.ID, e.Revision, value)
	return cardOf(e, value), nil
}

func compareFold(a, b string) int {
	if order := strings.Compare(strings.ToLower(a), strings.ToLower(b)); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

type filter struct {
	query, tag string
	notebook   *string
	archived   bool
}

func (f filter) matches(card note) bool {
	if card.Archived != f.archived || f.notebook != nil && card.Notebook != *f.notebook {
		return false
	}
	if f.tag != "" && !slices.ContainsFunc(card.Tags, func(tag string) bool { return strings.ToLower(tag) == f.tag }) {
		return false
	}
	if f.query == "" {
		return true
	}
	return slices.ContainsFunc(append([]string{card.Title, card.Snippet}, card.Tags...), func(text string) bool {
		return strings.Contains(strings.ToLower(text), f.query)
	})
}

// listNotes is the account's notes as cards, pinned first, then the newest.
func (h *Handler) listNotes(ctx context.Context, userID int64, f filter) (listing, error) {
	result := listing{Notes: []note{}, Notebooks: []string{}, EncryptedNotebooks: []encryptedNotebook{}, Tags: []tagCount{}}
	root, err := h.files.NotesRoot(ctx, userID)
	if err != nil {
		return result, err
	}
	if root != "" {
		result.RootID = &root
	}
	entries, err := h.scan(ctx, userID)
	if err != nil {
		return result, err
	}
	counts := map[string]int{}
	for _, e := range entries {
		switch {
		case e.VaultID != "":
			result.EncryptedNotebooks = append(result.EncryptedNotebooks, encryptedNotebook{VaultID: e.VaultID, FolderID: e.ID, Name: e.rel})
		case !e.Vault && e.Type == "folder":
			result.Notebooks = append(result.Notebooks, e.rel)
		case e.isNote():
			card, err := h.card(ctx, userID, e)
			if err != nil {
				return result, err
			}
			if !card.Archived {
				for _, tag := range card.Tags {
					counts[tag]++
				}
			}
			if f.matches(card) {
				result.Notes = append(result.Notes, card)
			}
		}
	}
	slices.SortStableFunc(result.Notes, func(a, b note) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		return strings.Compare(b.ModifiedAt, a.ModifiedAt)
	})
	slices.SortFunc(result.Notebooks, compareFold)
	slices.SortFunc(result.EncryptedNotebooks, func(a, b encryptedNotebook) int { return compareFold(a.Name, b.Name) })
	for name, count := range counts {
		result.Tags = append(result.Tags, tagCount{Name: name, Count: count})
	}
	slices.SortFunc(result.Tags, func(a, b tagCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return compareFold(a.Name, b.Name)
	})
	return result, nil
}

func (h *Handler) getNote(ctx context.Context, userID int64, id string) (noteWithBody, error) {
	e, err := h.find(ctx, userID, id)
	if err != nil {
		return noteWithBody{}, err
	}
	doc, err := h.read(ctx, userID, e.ID)
	if err != nil {
		return noteWithBody{}, err
	}
	value := contentOf(doc)
	h.cache.put(userID, e.ID, e.Revision, value)
	return noteWithBody{note: cardOf(e, value), Body: doc.body}, nil
}

// fileTitle is a file name for a title: no separators or control characters.
func fileTitle(raw string) string {
	title := titleSeparators.ReplaceAllString(norm.NFC.String(raw), " ")
	title = strings.TrimSpace(whitespace.ReplaceAllString(title, " "))
	return strings.TrimSpace(cut(strings.TrimLeft(title, "."), maxTitle))
}

func defaultTitle(body string, now time.Time) string {
	for _, line := range lineBreak.Split(body, -1) {
		if first := strings.TrimSpace(titleNoise.ReplaceAllString(line, "")); first != "" {
			if title := fileTitle(cut(first, 60)); title != "" {
				return title
			}
			break
		}
	}
	return "Note " + now.UTC().Format("2006-01-02 15.04")
}

func invalidRequest(message string) error {
	return apperr.New(http.StatusBadRequest, message, "INVALID_REQUEST")
}

func notebookParts(value any) ([]string, error) {
	text, ok := value.(string)
	if !ok && value != nil {
		return nil, invalidRequest("notebook must be text.")
	}
	var parts []string
	for _, part := range strings.Split(text, "/") {
		if title := fileTitle(part); title != "" {
			parts = append(parts, title)
		}
	}
	return parts, nil
}

func notebookPath(parts []string) string {
	return "/" + strings.Join(append([]string{notesFolder}, parts...), "/")
}

func bodyText(value any) (string, error) {
	text, ok := value.(string)
	if !ok && value != nil {
		return "", invalidRequest("body must be text.")
	}
	return text, nil
}

// applyProperties changes the properties named in input.
func applyProperties(props *properties, input map[string]any) error {
	if value, ok := input["tags"]; ok {
		list, isList := value.([]any)
		if !isList {
			return invalidRequest("tags must be a list.")
		}
		names := make([]string, len(list))
		for i, item := range list {
			names[i] = jsvalue.String(item)
		}
		props.Tags = cleanTags(names)
	}
	for key, target := range map[string]*bool{"pinned": &props.Pinned, "archived": &props.Archived} {
		if value, ok := input[key]; ok {
			flag, isBool := value.(bool)
			if !isBool {
				return invalidRequest(key + " must be true or false.")
			}
			*target = flag
		}
	}
	if value, ok := input["color"]; ok {
		name, _ := value.(string)
		props.Color = "default"
		if slices.Contains(colors, name) {
			props.Color = name
		}
	}
	return nil
}

func assertFits(text string) error {
	if len(text) > maxNoteBytes {
		return apperr.New(http.StatusRequestEntityTooLarge, "This note is too long (1 MB at most).", "TOO_LARGE")
	}
	return nil
}

// freeName is "Title.md", or "Title (2).md" when that is taken in the folder.
func freeName(entries []entry, parts []string, title string) (string, error) {
	folder := strings.Join(parts, "/")
	taken := map[string]bool{}
	for _, e := range entries {
		if strings.EqualFold(e.folder(), folder) {
			taken[strings.ToLower(e.Name)] = true
		}
	}
	name := title + ".md"
	stem := numbered.ReplaceAllString(title, "")
	for number := 2; number < 1000; number++ {
		if !taken[strings.ToLower(name)] {
			return name, nil
		}
		name = fmt.Sprintf("%s (%d).md", stem, number)
	}
	return "", apperr.New(http.StatusConflict, "Too many notes with this title.", "ALREADY_EXISTS")
}

func (h *Handler) createNote(ctx context.Context, userID int64, input map[string]any) (noteWithBody, error) {
	var none noteWithBody
	body, err := bodyText(input["body"])
	if err != nil {
		return none, err
	}
	parts, err := notebookParts(input["notebook"])
	if err != nil {
		return none, err
	}
	title := fileTitle(jsvalue.String(input["title"]))
	if title == "" {
		title = defaultTitle(body, time.Now())
	}
	doc := document{properties: properties{Tags: []string{}, Color: "default"}, body: body}
	if err = applyProperties(&doc.properties, input); err != nil {
		return none, err
	}
	text := serialize(doc)
	if err = assertFits(text); err != nil {
		return none, err
	}
	entries, err := h.scan(ctx, userID)
	if err != nil {
		return none, err
	}
	name, err := freeName(entries, parts, title)
	if err != nil {
		return none, err
	}
	file, err := h.files.WriteNote(ctx, userID, notebookPath(parts)+"/"+name, []byte(text), false, "")
	if err != nil {
		return none, err
	}
	return h.getNote(ctx, userID, file.ID)
}

// updateNote changes the body and properties, renames the file by title and
// moves it by notebook. baseRevision refuses a change to a note that changed
// since it was read.
func (h *Handler) updateNote(ctx context.Context, userID int64, id string, input map[string]any) (noteWithBody, error) {
	var none noteWithBody
	e, err := h.find(ctx, userID, id)
	if err != nil {
		return none, err
	}
	base, isText := input["baseRevision"].(string)
	if !isText && input["baseRevision"] != nil {
		return none, invalidRequest("baseRevision must be text.")
	}
	if base != "" && base != e.Revision {
		return none, apperr.New(http.StatusConflict, "The file changed since that revision.", "CONFLICT")
	}
	touchesText := false
	for _, key := range []string{"body", "tags", "pinned", "archived", "color"} {
		_, present := input[key]
		touchesText = touchesText || present
	}
	if touchesText {
		doc, err := h.read(ctx, userID, e.ID)
		if err != nil {
			return none, err
		}
		if err = applyProperties(&doc.properties, input); err != nil {
			return none, err
		}
		if value, ok := input["body"]; ok {
			if doc.body, err = bodyText(value); err != nil {
				return none, err
			}
		}
		text := serialize(doc)
		if err = assertFits(text); err != nil {
			return none, err
		}
		if _, err = h.files.WriteNote(ctx, userID, "/"+e.Path, []byte(text), true, e.Revision); err != nil {
			return none, err
		}
	}
	title := titleOf(e.Name)
	if value, ok := input["title"]; ok {
		if next := fileTitle(jsvalue.String(value)); next != "" {
			title = next
		}
	}
	folder := "/" + path.Dir(e.Path)
	moved := false
	if value, ok := input["notebook"]; ok {
		parts, err := notebookParts(value)
		if err != nil {
			return none, err
		}
		if moved = strings.Join(parts, "/") != e.folder(); moved {
			folder = notebookPath(parts)
			if _, _, err = h.files.EnsureNotesFolder(ctx, userID, folder); err != nil {
				return none, err
			}
		}
	}
	if moved || title != titleOf(e.Name) {
		if err = h.files.MoveNote(ctx, userID, e.ID, title+".md", folder); err != nil {
			return none, err
		}
	}
	return h.getNote(ctx, userID, e.ID)
}

// createNotebook makes a folder under /Notes; making it again changes nothing.
func (h *Handler) createNotebook(ctx context.Context, userID int64, input map[string]any) (map[string]any, error) {
	parts, err := notebookParts(input["name"])
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, apperr.New(http.StatusBadRequest, "Give the notebook a name.", "INVALID_NOTEBOOK")
	}
	_, created, err := h.files.EnsureNotesFolder(ctx, userID, notebookPath(parts))
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": strings.Join(parts, "/"), "created": created}, nil
}

func lower(text string) string { return strings.ToLower(strings.TrimSpace(text)) }
