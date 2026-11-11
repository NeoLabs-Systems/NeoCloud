package sync

import "testing"

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
