package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A config.toml from before the folder settings loads with the old
// behaviour: every default, and the old delete guard (500 files, 50 %).
func TestOldConfigKeepsItsBehaviour(t *testing.T) {
	dir := t.TempDir()
	old := `server = "https://cloud.test"
token = "nc_x"

[[pairs]]
  id = "abc"
  local = "/tmp/x"
  remote = "Docs"
  direction = "both"
  ignore = ["*.log"]
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(old), 0o600)
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	pair := cfg.Pairs[0]
	if !pair.MirrorDeletes() || !pair.DefaultIgnores() || !pair.Hidden() || !pair.FollowsNetworkRules() ||
		pair.ConflictPolicy() != ConflictKeepBoth || pair.NewFolderPolicy() != NewFoldersSync ||
		pair.TrashDays() != 30 || pair.MaxFileSize() != 0 || len(pair.ExcludePaths) != 0 {
		t.Errorf("pair defaults: %+v", pair.PairSettings)
	}
	if files, percent := cfg.Guard(pair); files != 500 || percent != 50 {
		t.Errorf("guard %d/%d", files, percent)
	}
	if cfg.BigFolderMB(pair) != 500 || cfg.NotifyLevel() != NotifyErrors || !cfg.Sidebar() || cfg.ProxyMode() != ProxySystem {
		t.Errorf("global defaults: %+v", cfg.GlobalSettings)
	}
}

func TestPairSettingsRoundTripAndValidate(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ten, off := 10, false
	err := store.Update(func(c *Config) error {
		pair := Pair{ID: "p", Local: "/x", Direction: Both}
		pair.ExcludePaths = []string{"/Photos/", "Photos/2024", "Videos"}
		pair.DeleteGuardFiles, pair.PropagateDeletes, pair.Conflict = &ten, &off, ConflictNewer
		pair.Mode = "on_demand"
		if err := pair.Validate(); err == nil {
			t.Error("on_demand accepted")
		}
		pair.Mode = ""
		if err := pair.Validate(); err != nil {
			return err
		}
		c.Pairs = append(c.Pairs, pair)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(dir)
	pair := reopened.Get().Pairs[0]
	if len(pair.ExcludePaths) != 2 || pair.ExcludePaths[0] != "Photos" || pair.MirrorDeletes() || *pair.DeleteGuardFiles != 10 {
		t.Errorf("round trip: %+v", pair.PairSettings)
	}
	if !pair.Excluded("Photos/2024/x.jpg") || pair.Excluded("Photoshop") {
		t.Error("Excluded")
	}
	for _, bad := range []PairSettings{
		{NewFolders: "maybe"}, {Conflict: "coin"}, {Symlinks: "follow"}, {MaxFileSizeMB: -1},
		{ExcludePaths: []string{"../x"}}, {DeleteGuardPercent: new(0)},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	for _, bad := range []GlobalSettings{{Proxy: ProxyManual, ProxyURL: "nope"}, {Notifications: "loud"}, {Proxy: "pac"}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := (GlobalSettings{Proxy: ProxyManual, ProxyURL: "socks5://p:1080"}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestOnDemandFallsBackToMirrorOnLoad(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[[pairs]]\n  id = \"a\"\n  local = \"/x\"\n  mode = \"on_demand\"\n"), 0o600)
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := store.Get().Pairs[0].Mode; mode != ModeMirror {
		t.Errorf("mode %q", mode)
	}
}

func TestWholeCloudPairAsksForNewFolders(t *testing.T) {
	whole, _ := NewPair(t.TempDir(), "/", "", nil, false)
	sub, _ := NewPair(t.TempDir(), "Docs", "", nil, false)
	if whole.NewFolderPolicy() != NewFoldersAsk || sub.NewFolderPolicy() != NewFoldersSync {
		t.Errorf("%q %q", whole.NewFolders, sub.NewFolders)
	}
}
