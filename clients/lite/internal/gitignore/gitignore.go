// Package gitignore parses and matches patterns with the syntax of git's
// ignore files (https://git-scm.com/docs/gitignore). It only knows single
// patterns: which files apply to a path, and in which order, is up to the
// caller.
package gitignore

import "strings"

// Pattern is one rule of an ignore file.
type Pattern struct {
	// Line is the 1-based line it was read from (0 when it was not read
	// from a file).
	Line int
	// Text is the line as written, without the trailing spaces git drops.
	Text string
	// Negate is set for "!pattern": a match re-includes the path.
	Negate bool
	// DirOnly is set for "pattern/": it only matches folders.
	DirOnly bool
	// anchored patterns (a slash at the start or in the middle) match the
	// path relative to the ignore file's folder; the others match the
	// last path component, at any depth.
	anchored bool
	glob     []rune
}

// Parse reads an ignore file: one pattern per line, blank lines and
// "#" comments skipped. A UTF-8 byte order mark and CRLF line ends are
// accepted.
func Parse(content []byte) []Pattern {
	text := strings.TrimPrefix(string(content), "\xef\xbb\xbf")
	var patterns []Pattern
	for i, line := range strings.Split(text, "\n") {
		if p, ok := ParseLine(strings.TrimSuffix(line, "\r")); ok {
			p.Line = i + 1
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// ParseLine reads one line. It reports false for blank lines, comments
// and lines that hold no pattern ("!", "/").
func ParseLine(line string) (Pattern, bool) {
	line = trimTrailingSpaces(line)
	if line == "" || line[0] == '#' {
		return Pattern{}, false
	}
	p := Pattern{Text: line}
	if line[0] == '!' {
		p.Negate = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		p.DirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if strings.Contains(line, "/") {
		p.anchored = true
		line = strings.TrimPrefix(line, "/")
	}
	if line == "" {
		return Pattern{}, false
	}
	p.glob = []rune(line)
	return p, true
}

// trimTrailingSpaces drops trailing spaces unless a backslash quotes them.
func trimTrailingSpaces(line string) string {
	end := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			continue
		case '\\':
			i++ // the quoted character stays
		}
		end = min(i+1, len(line))
	}
	return line[:end]
}

// Match reports whether the pattern matches rel, a "/"-separated path
// relative to the folder of the pattern's ignore file. isDir says whether
// rel is a folder. A match of a negated pattern means "re-include".
func (p Pattern) Match(rel string, isDir bool) bool {
	if p.DirOnly && !isDir {
		return false
	}
	if !p.anchored {
		rel = rel[strings.LastIndex(rel, "/")+1:]
	}
	return dowild(p.glob, 0, []rune(rel), 0) == wmMatch
}

// Results of dowild. The two abort results cut the backtracking short,
// as in git's wildmatch.c, which this follows (with WM_PATHNAME: wildcards
// other than "**" never match a slash).
const (
	wmMatch = iota
	wmNoMatch
	wmAbortAll
	wmAbortToStarStar
)

func dowild(pat []rune, pi int, text []rune, ti int) int {
	for ; pi < len(pat); pi, ti = pi+1, ti+1 {
		pc := pat[pi]
		if ti >= len(text) && pc != '*' {
			return wmAbortAll
		}
		switch pc {
		case '\\':
			pi++
			if pi >= len(pat) || text[ti] != pat[pi] {
				return wmNoMatch // a trailing backslash never matches
			}
		case '?':
			if text[ti] == '/' {
				return wmNoMatch
			}
		case '*':
			pi++
			matchSlash := false
			if pi < len(pat) && pat[pi] == '*' {
				before := pi - 2
				for pi < len(pat) && pat[pi] == '*' {
					pi++
				}
				// "**" is special only as a whole path component; other
				// runs of asterisks are a plain "*".
				if (before < 0 || pat[before] == '/') && (pi == len(pat) || pat[pi] == '/') {
					// "**/" also matches no folder at all.
					if pi < len(pat) && dowild(pat, pi+1, text, ti) == wmMatch {
						return wmMatch
					}
					matchSlash = true
				}
			}
			if pi == len(pat) {
				if !matchSlash && containsSlash(text[ti:]) {
					return wmNoMatch
				}
				return wmMatch
			}
			if !matchSlash && pat[pi] == '/' {
				slash := indexSlash(text[ti:])
				if slash < 0 {
					return wmNoMatch
				}
				ti += slash
				continue // the loop steps over the slash on both sides
			}
			for ; ti < len(text); ti++ {
				matched := dowild(pat, pi, text, ti)
				if matched != wmNoMatch {
					if !matchSlash || matched != wmAbortToStarStar {
						return matched
					}
				} else if !matchSlash && text[ti] == '/' {
					return wmAbortToStarStar
				}
			}
			return wmAbortAll
		case '[':
			end, ok, abort := matchClass(pat, pi, text[ti])
			if abort {
				return wmAbortAll
			}
			if !ok || text[ti] == '/' {
				return wmNoMatch
			}
			pi = end
		default:
			if text[ti] != pc {
				return wmNoMatch
			}
		}
	}
	if ti == len(text) {
		return wmMatch
	}
	return wmNoMatch
}

// matchClass matches c against the bracket expression starting at
// pat[start] ('['). It returns the index of the closing ']', whether c
// is in the set, and abort for an unterminated expression, which makes
// the whole pattern fail (as in git).
func matchClass(pat []rune, start int, c rune) (end int, ok, abort bool) {
	pi := start + 1
	if pi >= len(pat) {
		return 0, false, true
	}
	negated := pat[pi] == '!' || pat[pi] == '^'
	if negated {
		pi++
	}
	matched := false
	var prev rune = -1
	for first := true; ; first = false {
		if pi >= len(pat) {
			return 0, false, true
		}
		pc := pat[pi]
		if pc == ']' && !first {
			break
		}
		switch {
		case pc == '\\':
			pi++
			if pi >= len(pat) {
				return 0, false, true
			}
			if c == pat[pi] {
				matched = true
			}
			prev = pat[pi]
		case pc == '-' && prev >= 0 && pi+1 < len(pat) && pat[pi+1] != ']':
			pi++
			hi := pat[pi]
			if hi == '\\' {
				pi++
				if pi >= len(pat) {
					return 0, false, true
				}
				hi = pat[pi]
			}
			if prev <= c && c <= hi {
				matched = true
			}
			prev = -1
		case pc == '[' && pi+1 < len(pat) && pat[pi+1] == ':':
			name, next, found := className(pat, pi+2)
			if !found {
				// No ":]": the '[' is an ordinary member of the set.
				if c == '[' {
					matched = true
				}
				prev = '['
				break
			}
			in, known := inClass(name, c)
			if !known {
				return 0, false, true
			}
			if in {
				matched = true
			}
			pi = next
			prev = -1
		default:
			if c == pc {
				matched = true
			}
			prev = pc
		}
		pi++
	}
	return pi, matched != negated, false
}

// className reads a "[:name:]" whose name starts at pat[from]. next is
// the index of its closing ']'.
func className(pat []rune, from int) (name string, next int, found bool) {
	for i := from; i < len(pat); i++ {
		if pat[i] == ']' {
			if i > from && pat[i-1] == ':' {
				return string(pat[from : i-1]), i, true
			}
			return "", 0, false
		}
	}
	return "", 0, false
}

// inClass tests c against a POSIX character class (ASCII, like git).
func inClass(name string, c rune) (in, known bool) {
	isUpper := 'A' <= c && c <= 'Z'
	isLower := 'a' <= c && c <= 'z'
	isDigit := '0' <= c && c <= '9'
	isPrint := 0x20 <= c && c < 0x7f
	switch name {
	case "alnum":
		return isUpper || isLower || isDigit, true
	case "alpha":
		return isUpper || isLower, true
	case "blank":
		return c == ' ' || c == '\t', true
	case "cntrl":
		return c < 0x20 || c == 0x7f, true
	case "digit":
		return isDigit, true
	case "graph":
		return isPrint && c != ' ', true
	case "lower":
		return isLower, true
	case "print":
		return isPrint, true
	case "punct":
		return isPrint && c != ' ' && !isUpper && !isLower && !isDigit, true
	case "space":
		return c == ' ' || ('\t' <= c && c <= '\r'), true
	case "upper":
		return isUpper, true
	case "xdigit":
		return isDigit || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F'), true
	}
	return false, false
}

func containsSlash(text []rune) bool { return indexSlash(text) >= 0 }

func indexSlash(text []rune) int {
	for i, r := range text {
		if r == '/' {
			return i
		}
	}
	return -1
}
