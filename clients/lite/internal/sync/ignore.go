package sync

import (
	"path"
	"strings"
)

// DefaultIgnore is the list every pair starts with (docs/clients.md).
// The .neocloud metadata folder is not a pattern: Match always leaves it
// alone, whatever its case.
var DefaultIgnore = []string{".DS_Store", "Thumbs.db", "desktop.ini", "~$*", "*.tmp", "*.ncpart"}

// Matcher decides which relative paths a pair leaves alone.
//
// Pattern rules, kept close to .gitignore so they feel familiar:
//   - no "/" inside: matched against every path component ("*.log" hides
//     any .log file, "node_modules" hides that folder wherever it is);
//   - a "/" inside or a leading "/": matched against the path from the
//     pair root ("/build" or "docs/*.pdf");
//   - a trailing "/": only matches folders.
//
// Matching a folder hides everything below it.
type Matcher struct {
	patterns []pattern
}

type pattern struct {
	glob     string
	anchored bool
	dirOnly  bool
}

// NewMatcher combines the defaults with a pair's own patterns. Invalid
// globs are dropped rather than failing the whole pair.
func NewMatcher(extra []string) *Matcher {
	m := &Matcher{}
	for _, raw := range append(append([]string(nil), DefaultIgnore...), extra...) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		p := pattern{}
		if strings.HasSuffix(raw, "/") {
			p.dirOnly = true
			raw = strings.TrimRight(raw, "/")
		}
		if strings.Contains(raw, "/") {
			p.anchored = true
			raw = strings.TrimLeft(raw, "/")
		}
		if _, err := path.Match(raw, ""); err != nil || raw == "" {
			continue
		}
		p.glob = raw
		m.patterns = append(m.patterns, p)
	}
	return m
}

// Match reports whether rel ("/"-separated, relative to the pair root) or
// any folder above it is ignored. isDir says whether rel itself is a folder.
//
// A component named .neocloud in any case, file or folder, is always
// ignored: the pair's marker and trash live there, and on a case-
// insensitive disk a remote ".NeoCloud" would land in the same folder.
func (m *Matcher) Match(rel string, isDir bool) bool {
	parts := strings.Split(rel, "/")
	for i := range parts {
		if strings.EqualFold(parts[i], metaDir) {
			return true
		}
		componentIsDir := i < len(parts)-1 || isDir
		prefix := strings.Join(parts[:i+1], "/")
		for _, p := range m.patterns {
			if p.dirOnly && !componentIsDir {
				continue
			}
			subject := parts[i]
			if p.anchored {
				subject = prefix
			}
			if ok, _ := path.Match(p.glob, subject); ok {
				return true
			}
		}
	}
	return false
}
