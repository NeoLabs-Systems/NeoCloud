package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuggestLocal(t *testing.T) {
	home := t.TempDir()
	at := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	suggest := func(parent, remote string, pairs ...Pair) string {
		t.Helper()
		got, err := SuggestLocal(home, parent, remote, pairs)
		if err != nil {
			t.Fatalf("SuggestLocal(%q, %q): %v", parent, remote, err)
		}
		return got
	}

	if got := suggest("", ""); got != at("NeoCloud") {
		t.Errorf("whole cloud: %s", got)
	}
	if got := suggest("", "/Work/Photos/"); got != at("NeoCloud", "Photos") {
		t.Errorf("a folder: %s", got)
	}
	if got := suggest("", `Work/a:b?`); got != at("NeoCloud", "a_b_") {
		t.Errorf("a name Windows refuses: %s", got)
	}

	// A taken name gets a number, files count too.
	os.MkdirAll(at("NeoCloud", "Photos"), 0o755)
	os.WriteFile(at("NeoCloud 2"), nil, 0o644)
	if got := suggest("", ""); got != at("NeoCloud 3") {
		t.Errorf("whole cloud with NeoCloud taken: %s", got)
	}
	if got := suggest("", "Photos"); got != at("NeoCloud", "Photos 2") {
		t.Errorf("folder with its name taken: %s", got)
	}

	// The whole cloud never goes around a pair (that would overlap).
	nested := Pair{Local: at("Elsewhere", "NeoCloud")}
	if got := suggest(at("Elsewhere"), "", nested); got != at("Elsewhere", "NeoCloud 2") {
		t.Errorf("around a pair: %s", got)
	}

	// With ~/NeoCloud synced itself, a single folder goes next to it.
	whole := Pair{Local: at("NeoCloud")}
	if got := suggest("", "Photos", whole); got != at("NeoCloud Photos") {
		t.Errorf("next to a synced NeoCloud folder: %s", got)
	}
	// A parent the user picked inside a pair is refused.
	if _, err := SuggestLocal(home, at("NeoCloud", "Photos"), "Docs", []Pair{whole}); err == nil {
		t.Error("a parent inside a pair was accepted")
	}
	if got := suggest(at("Elsewhere"), "Docs"); got != at("Elsewhere", "Docs") {
		t.Errorf("a parent the user picked: %s", got)
	}
}

func TestNewPairCreate(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "NeoCloud", "Photos")
	if _, err := NewPair(target, "Photos", "", nil, false); err == nil {
		t.Error("a missing folder was accepted without create")
	}
	pair, err := NewPair(target, "Photos", "", nil, true)
	if err != nil || !pair.Created {
		t.Fatalf("create: %+v %v", pair, err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("NewPair made the folder before the pair was accepted")
	}
	made, err := CreateLocal(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 || made[0] != filepath.Join(base, "NeoCloud") || made[1] != target {
		t.Errorf("made %v", made)
	}
	if _, err := NewPair(target, "Photos", "", nil, true); err == nil {
		t.Error("create accepted an existing folder")
	}
	if again, err := CreateLocal(target); err != nil || len(again) != 0 {
		t.Errorf("CreateLocal on an existing folder: %v %v", again, err)
	}
}

func TestSidebarFolder(t *testing.T) {
	home := t.TempDir()
	user := Pair{Local: filepath.Join(home, "Documents")}
	sub := Pair{Local: filepath.Join(home, "NeoCloud", "Photos"), Created: true}
	whole := Pair{Local: filepath.Join(home, "NeoCloud 2"), Created: true}
	custom := Pair{Local: filepath.Join(home, "Work", "Docs"), Created: true}
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{Pairs: []Pair{user}}, ""},
		{Config{Pairs: []Pair{user, sub}}, filepath.Join(home, "NeoCloud")},
		{Config{Pairs: []Pair{whole, sub}}, whole.Local},
		{Config{Pairs: []Pair{custom}}, custom.Local},
		{Config{Pairs: []Pair{whole}, GlobalSettings: GlobalSettings{FileManagerSidebar: new(false)}}, ""},
	}
	for i, c := range cases {
		if got := c.cfg.SidebarFolder(); got != c.want {
			t.Errorf("case %d: %q, want %q", i, got, c.want)
		}
	}
}
