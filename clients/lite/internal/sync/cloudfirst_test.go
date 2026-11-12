package sync

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/netstate"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// A cloud-first pair (the whole cloud into a folder the app creates)
// downloads everything on its first pass and deletes nothing, and the
// icon files the app puts into that folder stay local.
func TestCloudFirstPairDownloadsEverything(t *testing.T) {
	srv := newFakeServer(t)
	for i := 0; i < 40; i++ {
		srv.put(fmt.Sprintf("Photos/%02d.jpg", i), fmt.Sprintf("photo %d", i))
	}
	srv.put("notes.txt", "notes")
	srv.mkdir("Empty")

	home := t.TempDir()
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.Server, c.Token, c.DeviceName = srv.srv.URL, "token", "dev"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	engine := New(cfg, db, log.New(io.Discard, "", 0))
	engine.conditions = func() netstate.Conditions { return netstate.Conditions{} }

	root := filepath.Join(t.TempDir(), "NeoCloud")
	pair, err := config.NewPair(root, "", config.Both, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	made, err := engine.AddPair(pair)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0] != root {
		t.Fatalf("made %v", made)
	}
	// What the folder icon leaves behind on macOS and Windows.
	os.WriteFile(filepath.Join(root, "Icon\r"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "desktop.ini"), []byte("[.ShellClassInfo]"), 0o644)

	h := &harness{t: t, srv: srv, root: root, db: db, engine: engine, pair: pair}
	h.mustSync()
	h.mustSync()
	for i := 0; i < 40; i++ {
		rel := fmt.Sprintf("Photos/%02d.jpg", i)
		if got := h.read(rel); got != fmt.Sprintf("photo %d", i) {
			t.Fatalf("%s = %q", rel, got)
		}
	}
	if h.read("notes.txt") != "notes" || !h.exists("Empty") {
		t.Error("root file or empty folder not downloaded")
	}
	if !h.exists("Icon\r") || !h.exists("desktop.ini") {
		t.Error("the folder icon files were removed")
	}
	for _, name := range []string{"Icon\r", "desktop.ini"} {
		if srv.lookup(name) != nil {
			t.Errorf("%q was uploaded", name)
		}
	}
	if len(srv.trash) != 0 || len(h.trashed()) != 0 {
		t.Errorf("the first pass deleted something: server trash %v, local trash %v", srv.trash, h.trashed())
	}

	// Two-way from then on.
	h.write("Photos/new.jpg", "from this computer")
	h.mustSync()
	if srv.lookup("Photos/new.jpg") == nil {
		t.Error("a local file was not uploaded")
	}
}

func TestIsOSJunk(t *testing.T) {
	for name, want := range map[string]bool{"Icon\r": true, "desktop.ini": true, "Desktop.INI": true, "Icon": false, "notes.txt": false} {
		if got := isOSJunk(name); got != want {
			t.Errorf("isOSJunk(%q) = %v", name, got)
		}
	}
}
