package sync

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	gosync "sync"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/gitignore"
)

// DefaultIgnore is the list every pair starts with (docs/clients.md),
// unless its use_default_ignores is off.
var DefaultIgnore = []string{".DS_Store", "Thumbs.db", "desktop.ini", "~$*", "*.tmp", "*.ncpart"}

// CloudIgnoreName is the per-folder ignore file. It works like a
// .gitignore and syncs like any other file, so every device of the
// account applies the same rules.
const CloudIgnoreName = ".cloudignore"

// maxCloudIgnoreSize caps what is read of one .cloudignore.
const maxCloudIgnoreSize = 1 << 20

// iconFile is where macOS keeps a folder's custom icon (the NeoCloud
// icon of a folder the app created uses it). It holds no data of its
// own, so it is never synced, whatever the patterns say.
const iconFile = "Icon\r"

// Sources of the rules that do not come from a .cloudignore.
const (
	SourceDefaults = "default patterns"
	SourceSettings = "folder settings"
)

// Matcher decides which relative paths a pair leaves alone, with the
// rules of .gitignore (https://git-scm.com/docs/gitignore):
//
//   - the defaults and the pair's own patterns come first, like
//     .git/info/exclude; then the .cloudignore of the pair root, then
//     those of deeper folders, each relative to its own folder. The last
//     pattern that matches decides, so deeper files override shallower
//     ones and "!pattern" re-includes;
//   - a path inside an ignored folder is ignored, whatever comes later
//     (git does not look into an ignored folder, so its .cloudignore is
//     not read either);
//   - patterns never ignore a .cloudignore itself: it always syncs, so
//     every device sees the same rules.
//
// Some things are ignored whatever the patterns say: the .neocloud
// metadata folder, macOS folder icons and the root desktop.ini, and,
// when the pair leaves hidden files out, names starting with a dot (but
// not a .cloudignore).
type Matcher struct {
	base []rule
	// noDotFiles hides every name starting with "."; hiddenAttr also
	// skips files Windows marks hidden or system (include_hidden off).
	noDotFiles bool
	hiddenAttr bool
	// load returns the content of the .cloudignore in dir ("" is the
	// root), or nil when there is none. Nil: no .cloudignore files.
	load func(dir string) ([]byte, error)

	mu    gosync.Mutex
	files map[string][]rule  // dir → its .cloudignore rules, once loaded
	dirs  map[string]Verdict // verdicts of folders, once worked out
	err   error              // the first load error
}

type rule struct {
	pattern gitignore.Pattern
	info    Rule
	dir     string // the folder the rule is relative to
}

func newRule(p gitignore.Pattern, source, dir string) rule {
	return rule{pattern: p, info: Rule{Source: source, Line: p.Line, Pattern: p.Text, Negate: p.Negate}, dir: dir}
}

// Rule names a pattern for people: where it comes from and what it says.
type Rule struct {
	// Source is the pair-relative path of its .cloudignore, or
	// SourceDefaults / SourceSettings.
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Pattern string `json:"pattern"`
	Negate  bool   `json:"negate"`
}

// FromFile reports whether the rule comes from a .cloudignore.
func (r Rule) FromFile() bool { return r.Source != SourceDefaults && r.Source != SourceSettings }

// Verdict is the outcome of matching one path.
type Verdict struct {
	Ignored bool
	// At is the path the decision was made at: the path itself, or the
	// ignored folder it is in.
	At string
	// Rule is the pattern that decided (for a path that is not ignored,
	// the "!pattern" that re-included it), or nil.
	Rule *Rule
	// Builtin says why a fixed rule ignores the path ("" otherwise).
	Builtin string
}

// Reason says in a sentence why the path is ignored.
func (v Verdict) Reason() string {
	switch {
	case !v.Ignored:
		return ""
	case v.Rule == nil:
		return "Ignored: " + v.Builtin
	case v.Rule.FromFile():
		return fmt.Sprintf("Ignored by %s (line %d: %s)", v.Rule.Source, v.Rule.Line, v.Rule.Pattern)
	}
	return fmt.Sprintf("Ignored by the %s (%s)", v.Rule.Source, v.Rule.Pattern)
}

// NewMatcher combines the defaults with extra patterns.
func NewMatcher(extra []string) *Matcher {
	m := &Matcher{}
	m.addBase(DefaultIgnore, SourceDefaults)
	m.addBase(extra, SourceSettings)
	return m
}

// MatcherFor is the matcher of a pair's settings: its patterns, the
// defaults unless turned off, and hidden files unless included. It does
// not read .cloudignore files; see LocalMatcher.
func MatcherFor(pair config.Pair) *Matcher {
	m := &Matcher{}
	if pair.DefaultIgnores() {
		m.addBase(DefaultIgnore, SourceDefaults)
	}
	m.addBase(pair.Ignore, SourceSettings)
	m.noDotFiles = !pair.Hidden()
	m.hiddenAttr = !pair.Hidden()
	return m
}

// LocalMatcher is MatcherFor plus the .cloudignore files in the pair's
// local folder, as they are on disk now (the settings page and
// `check-ignore`; a sync pass also weighs the cloud's copies).
func LocalMatcher(pair config.Pair) *Matcher {
	m := MatcherFor(pair)
	m.load = func(dir string) ([]byte, error) {
		data, _, err := readCloudIgnore(filepath.Join(pair.Local, filepath.FromSlash(path.Join(dir, CloudIgnoreName))))
		return data, err
	}
	return m
}

// addBase adds settings patterns. Leading spaces are dropped: they come
// from a form or a command line, not a file.
func (m *Matcher) addBase(patterns []string, source string) {
	for i, raw := range patterns {
		p, ok := gitignore.ParseLine(strings.TrimLeft(raw, " \t"))
		if !ok {
			continue
		}
		p.Line = i + 1
		m.base = append(m.base, newRule(p, source, ""))
	}
}

// Err is the first error reading a .cloudignore. A pass that saw one
// must not act: its rules may be incomplete.
func (m *Matcher) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// Match reports whether rel ("/"-separated, relative to the pair root) or
// any folder above it is ignored. isDir says whether rel itself is a folder.
func (m *Matcher) Match(rel string, isDir bool) bool {
	return m.Explain(rel, isDir).Ignored
}

// hidesAttr reports whether the Windows hidden or system attribute keeps
// the item at rel out (include_hidden off). A .cloudignore always syncs.
func (m *Matcher) hidesAttr(rel string, isDir bool) bool {
	return m.hiddenAttr && !isCloudIgnore(rel, isDir)
}

func isCloudIgnore(rel string, isDir bool) bool {
	return !isDir && path.Base(rel) == CloudIgnoreName
}

// Explain matches rel and says why.
func (m *Matcher) Explain(rel string, isDir bool) Verdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	parts := strings.Split(rel, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		dir := i < len(parts)-1 || isDir
		if dir {
			if v, ok := m.dirs[prefix]; ok {
				if v.Ignored || i == len(parts)-1 {
					return v
				}
				continue
			}
		}
		v := m.decide(prefix, i == 0, dir)
		if dir {
			if m.dirs == nil {
				m.dirs = map[string]Verdict{}
			}
			m.dirs[prefix] = v
		}
		if v.Ignored || i == len(parts)-1 {
			return v
		}
	}
	return Verdict{}
}

// decide works out one path whose folders above are known not to be
// ignored.
func (m *Matcher) decide(rel string, top, isDir bool) Verdict {
	name := path.Base(rel)
	builtin := ""
	switch {
	case strings.EqualFold(name, metaDir):
		// The pair's marker and trash live there, and on a case-
		// insensitive disk a remote ".NeoCloud" would land in it.
		builtin = "NeoCloud's own folder"
	case name == iconFile:
		builtin = "a folder icon (macOS)"
	case top && !isDir && strings.EqualFold(name, "desktop.ini"):
		builtin = "the folder icon (Windows)"
	case m.noDotFiles && strings.HasPrefix(name, ".") && !isCloudIgnore(rel, isDir):
		builtin = "a hidden file (Include hidden files is off)"
	}
	if builtin != "" {
		return Verdict{Ignored: true, At: rel, Builtin: builtin}
	}
	if isCloudIgnore(rel, isDir) {
		return Verdict{}
	}
	r := m.lastMatch(rel, isDir)
	if r == nil {
		return Verdict{}
	}
	found := r.info
	return Verdict{Ignored: !found.Negate, At: rel, Rule: &found}
}

// lastMatch finds the pattern that decides rel: the last matching one of
// the deepest .cloudignore, then shallower ones, then the settings.
func (m *Matcher) lastMatch(rel string, isDir bool) *rule {
	parent := parentDir(rel)
	var dirs []string
	if m.load != nil {
		dirs = append(dirs, "")
		if parent != "" {
			parts := strings.Split(parent, "/")
			for i := range parts {
				dirs = append(dirs, strings.Join(parts[:i+1], "/"))
			}
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		rules := m.rulesIn(dirs[i])
		sub := rel
		if dirs[i] != "" {
			sub = rel[len(dirs[i])+1:]
		}
		for j := len(rules) - 1; j >= 0; j-- {
			if rules[j].pattern.Match(sub, isDir) {
				return &rules[j]
			}
		}
	}
	for j := len(m.base) - 1; j >= 0; j-- {
		if m.base[j].pattern.Match(rel, isDir) {
			return &m.base[j]
		}
	}
	return nil
}

// rulesIn loads the .cloudignore of dir once.
func (m *Matcher) rulesIn(dir string) []rule {
	if rules, ok := m.files[dir]; ok {
		return rules
	}
	if m.files == nil {
		m.files = map[string][]rule{}
	}
	data, err := m.load(dir)
	if err != nil && m.err == nil {
		m.err = fmt.Errorf("reading %s: %w", path.Join(dir, CloudIgnoreName), err)
	}
	source := path.Join(dir, CloudIgnoreName)
	var rules []rule
	for _, p := range gitignore.Parse(data) {
		rules = append(rules, newRule(p, source, dir))
	}
	m.files[dir] = rules
	return rules
}

// readCloudIgnore reads a local .cloudignore. A missing one, or something
// else under that name (a folder, a link), has no rules and no error.
func readCloudIgnore(full string) ([]byte, *localItem, error) {
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCloudIgnoreSize))
	if err != nil {
		return nil, nil, err
	}
	return data, &localItem{size: info.Size(), mtime: info.ModTime().UnixNano()}, nil
}
