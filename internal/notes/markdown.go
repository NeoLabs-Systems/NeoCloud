package notes

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// A note is a Markdown file with optional YAML frontmatter. This reads and
// writes the YAML subset notes use (scalars and lists of scalars) and keeps any
// other key as it was, so a note written in another app keeps its properties.

const (
	maxTags       = 20
	maxTagLength  = 40
	snippetLength = 280
)

var colors = []string{"default", "red", "orange", "amber", "green", "teal", "blue", "purple", "gray"}

var (
	frontmatter     = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\z)`)
	propertyLine    = regexp.MustCompile(`^([A-Za-z_][\w-]*):(.*)$`)
	continuation    = regexp.MustCompile(`^(\s+|-\s)`)
	listMarker      = regexp.MustCompile(`^\s*-\s?`)
	lineBreak       = regexp.MustCompile(`\r?\n`)
	plainScalar     = regexp.MustCompile(`^\w[\w .\-/&+]*$`)
	taskLine        = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s?(.*)$`)
	blockMark       = regexp.MustCompile(`^\s*(#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)`)
	inlineMark      = regexp.MustCompile("(\\*\\*|__|\\*|_|~~|`)")
	markdownLink    = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	ruleOrCodeFence = regexp.MustCompile("^(-{3,}|\\*{3,}|```.*)$")
)

type properties struct {
	Tags     []string `json:"tags"`
	Pinned   bool     `json:"pinned"`
	Archived bool     `json:"archived"`
	Color    string   `json:"color"`
}

type document struct {
	properties
	// extra holds the frontmatter lines of keys notes don't know, verbatim.
	extra []string
	body  string
}

func unquote(text string) string {
	value := strings.TrimSpace(text)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func parseNote(text string) document {
	source := strings.TrimPrefix(text, "\ufeff")
	doc := document{properties: properties{Tags: []string{}, Color: "default"}, body: source}
	match := frontmatter.FindStringSubmatch(source)
	if match == nil {
		return doc
	}
	doc.body = source[len(match[0]):]
	lines := lineBreak.Split(match[1], -1)
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		key := propertyLine.FindStringSubmatch(line)
		if key == nil {
			doc.extra = append(doc.extra, line)
			continue
		}
		name, rest := key[1], strings.TrimSpace(key[2])
		block := []string{line}
		for index+1 < len(lines) && continuation.MatchString(lines[index+1]) {
			index++
			block = append(block, lines[index])
		}
		var values []string
		switch {
		case strings.HasPrefix(rest, "["):
			inner := ""
			if len(rest) >= 2 {
				inner = strings.TrimSpace(rest[1 : len(rest)-1])
			}
			for _, part := range strings.Split(inner, ",") {
				if value := unquote(part); value != "" {
					values = append(values, value)
				}
			}
		case len(block) > 1:
			for _, item := range block[1:] {
				if value := unquote(listMarker.ReplaceAllString(item, "")); value != "" {
					values = append(values, value)
				}
			}
		case rest != "":
			values = []string{unquote(rest)}
		}
		single := len(block) == 1 && !strings.HasPrefix(rest, "[")
		switch name {
		case "tags":
			doc.Tags = cleanTags(values)
		case "pinned":
			doc.Pinned = single && rest == "true"
		case "archived":
			doc.Archived = single && rest == "true"
		case "color":
			doc.Color = "default"
			if single && slices.Contains(colors, unquote(rest)) {
				doc.Color = unquote(rest)
			}
		default:
			doc.extra = append(doc.extra, block...)
		}
	}
	return doc
}

// cut shortens text to at most limit characters.
func cut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func cleanTags(values []string) []string {
	seen := map[string]bool{}
	tags := []string{}
	for _, raw := range values {
		tag := strings.TrimPrefix(strings.TrimSpace(raw), "#")
		tag = strings.NewReplacer("\r", " ", "\n", " ").Replace(tag)
		tag = strings.TrimSpace(cut(tag, maxTagLength))
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		if tags = append(tags, tag); len(tags) >= maxTags {
			break
		}
	}
	return tags
}

func quoteIfNeeded(text string) string {
	if plainScalar.MatchString(text) && text != "true" && text != "false" && text != "null" {
		return text
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(text)
	return strings.TrimRight(buffer.String(), "\n")
}

// serialize is the file text of a note; it has no frontmatter when there is
// nothing to say.
func serialize(doc document) string {
	var lines []string
	if len(doc.Tags) > 0 {
		lines = append(lines, "tags:")
		for _, tag := range doc.Tags {
			lines = append(lines, "  - "+quoteIfNeeded(tag))
		}
	}
	if doc.Pinned {
		lines = append(lines, "pinned: true")
	}
	if doc.Archived {
		lines = append(lines, "archived: true")
	}
	if doc.Color != "" && doc.Color != "default" {
		lines = append(lines, "color: "+doc.Color)
	}
	for _, line := range doc.extra {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return doc.body
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + doc.body
}

type checklist struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// summarize is what a card shows: a plain snippet and the checklist's progress.
func summarize(body string) (string, checklist) {
	var progress checklist
	var plain []string
	for _, line := range lineBreak.Split(body, -1) {
		text := line
		if task := taskLine.FindStringSubmatch(line); task != nil {
			progress.Total++
			if task[1] != " " {
				progress.Done++
			}
			text = task[2]
		}
		text = blockMark.ReplaceAllString(text, "")
		text = inlineMark.ReplaceAllString(text, "")
		text = strings.TrimSpace(markdownLink.ReplaceAllString(text, "$1"))
		if text != "" && !ruleOrCodeFence.MatchString(strings.TrimSpace(line)) {
			plain = append(plain, text)
		}
	}
	snippet := strings.Join(plain, " · ")
	if len([]rune(snippet)) > snippetLength {
		snippet = cut(snippet, snippetLength-1) + "…"
	}
	return snippet, progress
}
