package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

func TestMatcher(t *testing.T) {
	m := NewMatcher([]string{"*.log", "node_modules/", "/build", "docs/*.pdf", "[", "  ", "# comment"})
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{".DS_Store", false, true},
		{"a/b/.DS_Store", false, true},
		{"Thumbs.db", false, true},
		{"desktop.ini", false, true},
		{"Icon\r", false, true}, // a macOS custom folder icon
		{"sub/Icon\r", false, true},
		{"Icon", false, false},
		{"sub/desktop.ini", false, true}, // a default pattern; the root one is always ignored
		{"~$report.docx", false, true},
		{"x.tmp", false, true},
		{"movie.mp4.ncpart", false, true},
		{".neocloud", true, true},
		{".neocloud/state", false, true},
		{".neocloud", false, true}, // the metadata folder: any type, any case
		{".NeoCloud", true, true},
		{".NEOCLOUD/trash/x", false, true},
		{"sub/.Neocloud", false, true},
		{".neocloudx", true, false},
		{"app.log", false, true},
		{"deep/er/app.log", false, true},
		{"node_modules", true, true},
		{"web/node_modules/react/index.js", false, true},
		{"node_modules", false, false},
		{"build", true, true},
		{"build/out.bin", false, true},
		{"src/build", true, false}, // anchored to the pair root
		{"docs/a.pdf", false, true},
		{"docs/sub/a.pdf", false, false},
		{"notes.txt", false, false},
		{"logs/readme.md", false, false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, c.isDir); got != c.want {
			t.Errorf("Match(%q, dir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestMatcherForPairSettings(t *testing.T) {
	off := false
	pair := config.Pair{Ignore: []string{"*.log"}}
	pair.UseDefaultIgnores, pair.IncludeHidden = &off, &off
	m := MatcherFor(pair)
	cases := map[string]bool{
		".DS_Store":       true, // a dot file now
		"Thumbs.db":       false,
		"x.tmp":           false,
		"app.log":         true,
		".git":            true,
		"src/.env":        true,
		"src/main.go":     false,
		"desktop.ini":     true, // the root's folder icon, always
		"sub/desktop.ini": false,
		"Icon\r":          true,
		".neocloud/x":     true,
	}
	for rel, want := range cases {
		if got := m.Match(rel, false); got != want {
			t.Errorf("Match(%q) = %v, want %v", rel, got, want)
		}
	}
	if !MatcherFor(config.Pair{}).Match("Thumbs.db", false) || MatcherFor(config.Pair{}).Match(".env", false) {
		t.Error("a pair without settings lost the defaults")
	}
}

// matcherWith is a matcher over in-memory .cloudignore files (dir → content).
func matcherWith(pair config.Pair, files map[string]string) (*Matcher, map[string]int) {
	m := MatcherFor(pair)
	loads := map[string]int{}
	m.load = func(dir string) ([]byte, error) {
		loads[dir]++
		if content, ok := files[dir]; ok {
			return []byte(content), nil
		}
		return nil, nil
	}
	return m, loads
}

func TestCloudIgnoreSemantics(t *testing.T) {
	m, loads := matcherWith(config.Pair{Ignore: []string{"*.bak", "secret/"}}, map[string]string{
		"": "# root rules\n" +
			"*.log\n" +
			"!keep.log\n" +
			"/build/\n" +
			"docs/*.pdf\n" +
			"**/cache/**\n" +
			"a/**/z.txt\n" +
			"tmp*/\n" +
			"\\#literal\n" +
			"\\!bang\n" +
			"trail   \n" +
			"space\\ \n" +
			"report[0-9].txt\n" +
			"excluded/\n" +
			"!excluded/inner.txt\n" +
			"!*.bak\n",
		"sub":      "!*.log\nlocal-only.txt\n/anchored.txt\n.cloudignore\n",
		"sub/deep": "*.log\n",
		"build":    "!*\n", // never read: build/ is ignored
		"excluded": "!*\n", // never read either
	})
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"x/app.log", false, true},
		{"keep.log", false, false},    // re-included
		{"x/keep.log", false, false},  // re-included at any depth
		{"sub/app.log", false, false}, // a deeper file overrides
		{"sub/deep/app.log", false, true},
		{"sub/deep/keep.log", false, true}, // deepest file wins over the root's "!keep.log"
		{"sub/local-only.txt", false, true},
		{"local-only.txt", false, false},
		{"sub/anchored.txt", false, true},
		{"sub/x/anchored.txt", false, false},
		{"build", true, true},
		{"build", false, false}, // trailing "/": folders only
		{"src/build", true, false},
		{"build/out.bin", false, true},
		{"docs/a.pdf", false, true},
		{"docs/sub/a.pdf", false, false},
		{"x/docs/a.pdf", false, false},
		{"cache/x", false, true},
		{"deep/cache/x/y", false, true},
		{"cache", true, false}, // "/**" matches what is inside, not the folder
		{"a/z.txt", false, true},
		{"a/b/c/z.txt", false, true},
		{"tmp1", true, true},
		{"tmp1", false, false},
		{"#literal", false, true},
		{"!bang", false, true},
		{"trail", false, true},
		{"space ", false, true},
		{"space", false, false},
		{"report7.txt", false, true},
		{"reportX.txt", false, false},
		// A parent that is excluded cannot be re-entered.
		{"excluded/inner.txt", false, true},
		// Settings come first; a .cloudignore can re-include them.
		{"old.bak", false, false},
		{"secret", true, true},
		// A .cloudignore can't ignore itself, or anything else's.
		{"sub/.cloudignore", false, false},
		{".cloudignore", false, false},
		// Fixed rules stay.
		{".neocloud/x", false, true},
		{"Icon\r", false, true},
		{"desktop.ini", false, true},
		{"notes.txt", false, false},
	}
	for _, c := range cases {
		if got := m.Match(c.path, c.isDir); got != c.want {
			t.Errorf("Match(%q, dir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
	if loads["build"] != 0 || loads["excluded"] != 0 {
		t.Errorf("read a .cloudignore inside an ignored folder: %v", loads)
	}
	for dir, n := range loads {
		if n != 1 {
			t.Errorf("%q loaded %d times", dir, n)
		}
	}
}

func TestCloudIgnoreExplain(t *testing.T) {
	m, _ := matcherWith(config.Pair{Ignore: []string{"*.bak"}}, map[string]string{
		"":    "\n*.log\n",
		"sub": "# c\n!keep.log\nbuild/\n",
	})
	v := m.Explain("x/app.log", false)
	if !v.Ignored || v.Rule == nil || v.Rule.Source != ".cloudignore" || v.Rule.Line != 2 || v.At != "x/app.log" {
		t.Fatalf("app.log: %+v", v)
	}
	if got := v.Reason(); got != "Ignored by .cloudignore (line 2: *.log)" {
		t.Errorf("reason = %q", got)
	}
	v = m.Explain("sub/keep.log", false)
	if v.Ignored || v.Rule == nil || v.Rule.Source != "sub/.cloudignore" || v.Rule.Line != 2 || !v.Rule.Negate {
		t.Errorf("keep.log: %+v", v)
	}
	v = m.Explain("sub/build/x/y.txt", false)
	if !v.Ignored || v.At != "sub/build" || v.Rule.Pattern != "build/" {
		t.Errorf("inside build: %+v", v)
	}
	if got := m.Explain("a.bak", false).Reason(); got != "Ignored by the folder settings (*.bak)" {
		t.Errorf("settings reason = %q", got)
	}
	if v := m.Explain(".neocloud", true); !v.Ignored || v.Rule != nil || v.Builtin == "" {
		t.Errorf(".neocloud: %+v", v)
	}
}

func TestCloudIgnoreAndHiddenFiles(t *testing.T) {
	off := false
	pair := config.Pair{}
	pair.IncludeHidden = &off
	m, _ := matcherWith(pair, map[string]string{"": "*.log\n"})
	cases := map[string]bool{
		".cloudignore":     false, // synced although hidden files are left out
		"sub/.cloudignore": false,
		".env":             true,
		".hidden/x":        true,
		"a.log":            true,
	}
	for rel, want := range cases {
		if got := m.Match(rel, false); got != want {
			t.Errorf("Match(%q) = %v, want %v", rel, got, want)
		}
	}
	if m.Match(".cloudignore", true) != true {
		t.Error("a folder named .cloudignore is just a hidden folder")
	}
	if m.hidesAttr("sub/.cloudignore", false) || !m.hidesAttr("x.txt", false) {
		t.Error("the hidden attribute must not hide a .cloudignore")
	}
}

func TestLocalMatcherReadsDisk(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, ".cloudignore"), []byte("*.log\n"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", ".cloudignore"), []byte("!*.log\r\n"), 0o644)
	m := LocalMatcher(config.Pair{Local: root})
	if !m.Match("a.log", false) || m.Match("sub/a.log", false) || m.Err() != nil {
		t.Errorf("local rules not applied (err %v)", m.Err())
	}
}
