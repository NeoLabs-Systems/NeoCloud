package searchindex

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Turning what a user typed into an FTS5 query, and finding the matches again
// in the decrypted text for snippets. The index is detail=column, which cannot
// answer phrase queries; a phrase (quoted, or a word like "e-mail" that splits
// into several tokens) is searched as all of its tokens and then checked in
// the text.

const (
	snippetBefore = 60
	snippetLength = 220
)

// Term is a word (a prefix) or a quoted phrase of a query.
type Term struct {
	Tokens []string
	Prefix bool
}

func ParseQuery(query string) []Term {
	text := []rune(query)
	if len(text) > 200 {
		text = text[:200]
	}
	terms := []Term{}
	for pos := 0; pos < len(text) && len(terms) < 8; {
		for pos < len(text) && unicode.IsSpace(text[pos]) {
			pos++
		}
		if pos >= len(text) {
			break
		}
		phrase := text[pos] == '"'
		var value strings.Builder
		if phrase {
			pos++
			for pos < len(text) && text[pos] != '"' {
				value.WriteRune(text[pos])
				pos++
			}
			if pos < len(text) && text[pos] == '"' {
				pos++
			}
		} else {
			for pos < len(text) && !unicode.IsSpace(text[pos]) && text[pos] != '"' {
				value.WriteRune(text[pos])
				pos++
			}
		}
		tokens := tokensOf(value.String())
		if len(tokens) > 0 {
			terms = append(terms, Term{Tokens: tokens, Prefix: !phrase})
		}
	}
	budget := 16
	out := []Term{}
	for _, term := range terms {
		if budget <= 0 {
			break
		}
		if len(term.Tokens) > budget {
			term.Tokens = term.Tokens[:budget]
		}
		budget -= len(term.Tokens)
		out = append(out, term)
	}
	return out
}

// tokensOf splits text the way FTS5's unicode61 tokenizer with
// remove_diacritics does: letters, digits and private-use characters, case
// and accents folded.
func tokensOf(text string) []string {
	tokens := []string{}
	for _, token := range tokensOfWithPositions(text) {
		tokens = append(tokens, token.folded)
	}
	return tokens
}
func MatchExpression(terms []Term) string {
	parts := []string{}
	for _, term := range terms {
		for index, token := range term.Tokens {
			quoted := `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
			if term.Prefix && index == len(term.Tokens)-1 {
				quoted += "*"
			}
			parts = append(parts, quoted)
		}
	}
	return strings.Join(parts, " AND ")
}

func needsVerification(terms []Term) bool {
	for _, t := range terms {
		if len(t.Tokens) > 1 {
			return true
		}
	}
	return false
}
func matchesAll(text string, terms []Term) bool {
	tokens := tokensOfWithPositions(text)
	for _, term := range terms {
		found := false
		for i := 0; i+len(term.Tokens) <= len(tokens); i++ {
			if termMatches(term, tokens, i) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type locatedToken struct {
	start, end int
	folded     string
}
type textMatch struct{ start, end int }

func tokensOfWithPositions(text string) []locatedToken {
	out := []locatedToken{}
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		folded := strings.ToLower(norm.NFD.String(text[start:end]))
		var b strings.Builder
		for _, r := range folded {
			if !unicode.Is(unicode.M, r) {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			out = append(out, locatedToken{start: start, end: end, folded: b.String()})
		}
		start = -1
	}
	for offset, r := range text {
		keep := unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.M, r)
		if keep {
			if start < 0 {
				start = offset
			}
		} else {
			flush(offset)
		}
	}
	flush(len(text))
	return out
}
func termMatches(term Term, tokens []locatedToken, start int) bool {
	if len(term.Tokens) == 0 || start+len(term.Tokens) > len(tokens) {
		return false
	}
	for j, want := range term.Tokens {
		got := tokens[start+j].folded
		if term.Prefix && j == len(term.Tokens)-1 {
			if !strings.HasPrefix(got, want) {
				return false
			}
		} else if got != want {
			return false
		}
	}
	return true
}
func findMatches(text string, terms []Term) []textMatch {
	tokens := tokensOfWithPositions(text)
	matches := []textMatch{}
	for start := range tokens {
		for _, term := range terms {
			if termMatches(term, tokens, start) {
				matches = append(matches, textMatch{start: tokens[start].start, end: tokens[start+len(term.Tokens)-1].end})
			}
		}
	}
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].start == matches[b].start {
			return matches[a].end < matches[b].end
		}
		return matches[a].start < matches[b].start
	})
	return matches
}

// MakeSnippet is a short excerpt around the first match, matches wrapped in
// \u0002 … \u0003, with "…" where the text was cut. The start of the text
// when nothing matches.
func MakeSnippet(text string, terms []Term) string {
	start := 0
	if matches := findMatches(text, terms); len(matches) > 0 {
		first := matches[0].start
		start = back(text, first, snippetBefore)
		// Begin at a word boundary.
		if start > 0 {
			if space := strings.LastIndex(text[:start], " "); space >= 0 && space+1 > back(text, first, 2*snippetBefore) {
				start = space + 1
			}
		}
	}
	end := forward(text, start, snippetLength)
	if end < len(text) {
		if space := strings.Index(text[end:], " "); space >= 0 && space < 30 {
			end += space
		}
	}
	window := text[start:end]
	var out strings.Builder
	cursor := 0
	for _, m := range findMatches(window, terms) {
		if m.start < cursor {
			continue
		}
		out.WriteString(cleanText(window[cursor:m.start]))
		out.WriteRune('\u0002')
		out.WriteString(cleanText(window[m.start:m.end]))
		out.WriteRune('\u0003')
		cursor = m.end
	}
	out.WriteString(cleanText(window[cursor:]))
	// The markers are not white space, so trimming leaves them alone.
	snippet := strings.Trim(out.String(), " ")
	if start > 0 {
		snippet = "… " + snippet
	}
	if end < len(text) {
		snippet += " …"
	}
	return snippet
}

// back and forward step up to count characters from a byte offset.
func back(text string, offset, count int) int {
	for ; count > 0 && offset > 0; count-- {
		_, size := utf8.DecodeLastRuneInString(text[:offset])
		offset -= size
	}
	return offset
}

func forward(text string, offset, count int) int {
	for ; count > 0 && offset < len(text); count-- {
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}
	return offset
}

// cleanText drops control characters (so that the text cannot fake a
// highlight) and collapses runs of white space to one space.
func cleanText(text string) string {
	var out strings.Builder
	space := false
	for _, r := range text {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			if !space {
				out.WriteByte(' ')
				space = true
			}
			continue
		}
		out.WriteRune(r)
		space = false
	}
	return out.String()
}
