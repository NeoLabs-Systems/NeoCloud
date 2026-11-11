package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

func TestRemotePathProblem(t *testing.T) {
	for rel, bad := range map[string]bool{
		"a.txt": false, "dir/a.txt": false, "..a": false, "a..": false, "...": false, "x/.hidden": false,
		"": true, "..": true, "../x": true, "a/../../x": true, "a/..": true, ".": true, "./a": true,
		"a//b": true, "a/": true, "/etc/passwd": true, `a\b`: true, `..\x`: true, "a\x00b": true,
	} {
		if got := remotePathProblem(rel) != ""; got != bad {
			t.Errorf("remotePathProblem(%q) = %v, want %v", rel, got, bad)
		}
	}
}

func TestInsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	for full, want := range map[string]bool{
		filepath.Join(root, "a"):               true,
		filepath.Join(root, "a", "b"):          true,
		root:                                   false,
		filepath.Join(root, "..", "x"):         false,
		filepath.Join(root+"x", "a"):           false,
		filepath.Join(filepath.Dir(root), "a"): false,
	} {
		if got := insideRoot(root, full); got != want {
			t.Errorf("insideRoot(%q) = %v, want %v", full, got, want)
		}
	}
}

// A hostile server (or someone on the wire of an http:// server) sends
// paths that climb out of the pair. Nothing may be written outside it.
func TestRemotePathsCannotLeaveThePair(t *testing.T) {
	for _, direction := range []string{"both", "down"} {
		t.Run(direction, func(t *testing.T) {
			srv := newFakeServer(t)
			srv.mkdir("Shared")
			srv.put("Shared/ok.txt", "fine")
			srv.putRaw("Shared", "../escape.txt", "escaped")
			srv.mkdirRaw("Shared", "../escdir")
			srv.putRaw("Shared/../escdir", "inner.txt", "escaped too")
			srv.putRaw("Shared", `..\win.txt`, "backslash")
			h := newHarness(t, srv, direction)
			outside := filepath.Dir(h.root)
			before := listDir(t, outside)
			h.mustSync()

			if h.read("ok.txt") != "fine" {
				t.Fatal("the valid file was not synced")
			}
			for _, name := range []string{"escape.txt", "escdir", `..\win.txt`, "win.txt"} {
				if _, err := os.Lstat(filepath.Join(outside, name)); err == nil {
					t.Errorf("%s was written outside the pair", name)
				}
			}
			if after := listDir(t, outside); strings.Join(after, ",") != strings.Join(before, ",") {
				t.Errorf("folder above the pair changed: %v → %v", before, after)
			}
			invalid := map[string]bool{}
			for _, item := range h.engine.Status().PairState[h.pair.ID].NotSynced {
				if item.Reason == ReasonInvalidName {
					invalid[item.Path] = true
				}
			}
			for _, rel := range []string{"../escape.txt", "../escdir", "../escdir/inner.txt", `..\win.txt`} {
				if !invalid[rel] {
					t.Errorf("%s not reported as invalidName (got %v)", rel, invalid)
				}
			}
			entries, _ := h.db.Entries(h.pair.ID)
			for rel := range entries {
				if remotePathProblem(rel) != "" {
					t.Errorf("unsafe path %q was recorded", rel)
				}
			}
		})
	}
}

// Rows with unsafe paths (recorded before the client checked them) are
// forgotten; the file they point at outside the pair is not touched.
func TestUnsafeStoredRowsAreForgotten(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/ok.txt", "fine")
	h := newHarness(t, srv, "both")
	h.mustSync()
	victim := filepath.Join(filepath.Dir(h.root), "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(victim)
	if err := h.db.Put(h.pair.ID, state.Entry{Path: "../victim.txt", NodeID: "gone", Type: "file",
		Etag: "x", LocalSize: info.Size(), LocalMtime: info.ModTime().UnixNano()}); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	if data, err := os.ReadFile(victim); err != nil || string(data) != "keep me" {
		t.Fatalf("file outside the pair touched: %q %v", data, err)
	}
	if entries, _ := h.db.Entries(h.pair.ID); len(entries) != 1 {
		t.Fatalf("rows: %v", entries)
	}
	if len(h.trashed()) != 0 {
		t.Fatalf("trash: %v", h.trashed())
	}
}

// .neocloud in any case is left alone on both sides: a remote ".NeoCloud"
// would otherwise land in the marker and trash folder on a case-
// insensitive disk.
func TestMetadataFolderIgnoredInAnyCase(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/.NeoCloud/pair-id", "forged")
	srv.put("Shared/sub/.NEOCLOUD", "file")
	srv.put("Shared/a.txt", "a")
	h := newHarness(t, srv, "both")
	h.write(".Neocloud/local.txt", "local only")
	h.mustSync()
	if h.exists("sub/.NEOCLOUD") {
		t.Fatal("a remote .NEOCLOUD file was downloaded")
	}
	data, err := os.ReadFile(h.path(".neocloud/pair-id"))
	if err != nil || strings.TrimSpace(string(data)) != h.pair.ID {
		t.Fatalf("marker: %q %v", data, err)
	}
	remote := srv.files("Shared")
	if _, ok := remote[".Neocloud/local.txt"]; ok {
		t.Fatal("a local .Neocloud folder was uploaded")
	}
	if remote[".NeoCloud/pair-id"] == "" || remote["sub/.NEOCLOUD"] == "" {
		t.Fatalf("remote metadata-like names were touched: %v", remote)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
