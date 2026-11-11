package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreRoundTripAndPairs(t *testing.T) {
	dir := t.TempDir()
	local := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := NewPair(local, "/Documents/Work/", "", []string{"*.log"})
	if err != nil {
		t.Fatal(err)
	}
	if pair.Remote != "Documents/Work" || pair.Direction != Both {
		t.Errorf("pair not normalized: %+v", pair)
	}
	err = store.Update(func(c *Config) error {
		c.Server, c.Token = "https://cloud.test", "nc_secret"
		return c.AddPair(pair)
	})
	if err != nil {
		t.Fatal(err)
	}
	nested, _ := NewPair(local, "x", Up, nil)
	nested.Local = filepath.Join(local, "sub")
	if err := store.Update(func(c *Config) error { return c.AddPair(nested) }); err == nil {
		t.Error("overlapping pair accepted")
	}

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix permission bits; the file is protected by the user profile ACL.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v, want 0600 (it holds the token)", info.Mode().Perm())
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := reopened.Get()
	if !cfg.LoggedIn() || len(cfg.Pairs) != 1 || cfg.Pairs[0].Ignore[0] != "*.log" {
		t.Errorf("round trip lost data: %+v", cfg)
	}
	if _, err := NewPair(local, "", "sideways", nil); err == nil {
		t.Error("bad direction accepted")
	}
	if _, err := NewPair(filepath.Join(local, "missing"), "", "", nil); err == nil {
		t.Error("missing local folder accepted")
	}
}
