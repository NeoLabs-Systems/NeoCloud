package signpdf

import (
	"math"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	lineHeight = 1.25
	// baseline is the distance from the top of a line box to its baseline, in
	// em (Noto Sans).
	baseline     = 1.013
	sizeStep     = 0.5
	maxTextRunes = 5000
)

// sanitize normalises to NFC, unifies line breaks and replaces every character
// the fonts cannot draw by "?".
func (f fonts) sanitize(input string) string {
	if runes := []rune(input); len(runes) > maxTextRunes {
		input = string(runes[:maxTextRunes])
	}
	input = strings.ReplaceAll(strings.ReplaceAll(norm.NFC.String(input), "\r\n", "\n"), "\r", "\n")
	var out strings.Builder
	for _, r := range strings.ReplaceAll(input, "\t", " ") {
		switch {
		case r == '\n':
			out.WriteRune(r)
		case r < 0x20 || r >= 0x7f && r < 0xa0 || r == 0x200b || r == 0xfeff:
		case f.supports(r):
			out.WriteRune(r)
		default:
			out.WriteByte('?')
		}
	}
	return out.String()
}

// wrap is a greedy word wrap that honours "\n" and splits over-long words.
func wrap(text string, f *font, size, maxWidth float64) []string {
	space := f.width(" ", size)
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line, lineWidth := "", 0.0
		for _, word := range strings.Split(paragraph, " ") {
			if word == "" {
				continue
			}
			wordWidth := f.width(word, size)
			if line != "" && lineWidth+space+wordWidth <= maxWidth {
				line += " " + word
				lineWidth += space + wordWidth
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			if wordWidth <= maxWidth {
				line, lineWidth = word, wordWidth
				continue
			}
			var piece []rune
			pieceWidth := 0.0
			for _, r := range word {
				w := f.width(string(r), size)
				if len(piece) > 0 && pieceWidth+w > maxWidth {
					lines = append(lines, string(piece))
					piece, pieceWidth = piece[:0], 0
				}
				piece = append(piece, r)
				pieceWidth += w
			}
			line, lineWidth = string(piece), pieceWidth
		}
		lines = append(lines, line)
	}
	return lines
}

func ellipsize(line string, f *font, size, maxWidth float64) string {
	runes := []rune(line)
	for len(runes) > 0 && f.width(string(runes)+"…", size) > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// fitText finds the largest size from base down to least at which text fits;
// at the least size what still does not fit is cut with an ellipsis.
func fitText(text string, f *font, maxWidth, maxHeight, base, least float64, allowWrap bool) (float64, []string) {
	layout := func(size float64) []string {
		if allowWrap {
			return wrap(text, f, size, maxWidth)
		}
		return []string{text}
	}
	fits := func(size float64, lines []string) bool {
		return float64(len(lines))*size*lineHeight <= maxHeight+0.01 &&
			!slices.ContainsFunc(lines, func(l string) bool { return f.width(l, size) > maxWidth+0.01 })
	}
	for size := base; size >= least-1e-6; size -= sizeStep {
		if lines := layout(size); fits(size, lines) {
			return size, lines
		}
	}
	var lines []string
	if allowWrap {
		lines = wrap(text, f, least, maxWidth)
	} else {
		lines = []string{strings.ReplaceAll(text, "\n", " ")}
	}
	keep := int(math.Max(1, math.Floor(maxHeight/(least*lineHeight))))
	truncated := len(lines) > keep
	lines = lines[:min(len(lines), keep)]
	last := len(lines) - 1
	if truncated || f.width(lines[last], least) > maxWidth {
		lines[last] = ellipsize(lines[last], f, least, maxWidth)
	}
	return least, lines
}
