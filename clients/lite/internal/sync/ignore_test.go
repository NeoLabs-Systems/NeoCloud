package sync

import (
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
