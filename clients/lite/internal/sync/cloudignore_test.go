package sync

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// noConflictCopies fails when either side holds a conflict copy.
func (h *harness) noConflictCopies() {
	h.t.Helper()
	for rel := range h.srv.files("Shared") {
		if strings.Contains(rel, "(conflict") {
			h.t.Errorf("conflict copy on the server: %s", rel)
		}
	}
	for _, rel := range h.localFiles() {
		if strings.Contains(rel, "(conflict") {
			h.t.Errorf("conflict copy locally: %s", rel)
		}
	}
}

// localFiles lists the files in the pair's folder, outside .neocloud.
func (h *harness) localFiles() []string {
	var out []string
	filepath.WalkDir(h.root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == metaDir {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(h.root, full)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func (h *harness) tracked(rel string) bool {
	h.t.Helper()
	entries, err := h.db.Entries(h.pair.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	_, ok := entries[rel]
	return ok
}

// A .cloudignore that only exists in the cloud applies before anything
// it ignores is downloaded: content requests for those files would fail.
func TestRemoteCloudIgnoreAppliesOnFirstPass(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/.cloudignore", "*.log\nbuild/\n!keep.log\n")
	srv.put("Shared/a.log", "log")
	srv.put("Shared/keep.log", "kept")
	srv.put("Shared/build/out.bin", "binary")
	srv.put("Shared/notes.txt", "notes")
	srv.put("Shared/sub/.cloudignore", "*.txt\n")
	srv.put("Shared/sub/x.txt", "x")
	srv.put("Shared/sub/y.md", "y")
	srv.failContent = "a.log"
	h := newHarness(t, srv, config.Both)
	h.mustSync()

	for _, rel := range []string{".cloudignore", "keep.log", "notes.txt", "sub/.cloudignore", "sub/y.md"} {
		if !h.exists(rel) {
			t.Errorf("%s was not downloaded", rel)
		}
	}
	for _, rel := range []string{"a.log", "build", "sub/x.txt"} {
		if h.exists(rel) {
			t.Errorf("%s was downloaded although ignored", rel)
		}
	}
	report, _ := h.engine.PairReport(h.pair.ID)
	reasons := map[string]string{}
	for _, item := range report.Ignored {
		reasons[item.Path] = item.Detail
	}
	if reasons["a.log"] != "Ignored by .cloudignore (line 1: *.log)" || reasons["build"] != "Ignored by .cloudignore (line 2: build/)" ||
		reasons["sub/x.txt"] != "Ignored by sub/.cloudignore (line 1: *.txt)" {
		t.Errorf("ignored report = %v", reasons)
	}
	if len(report.NotSynced) != 0 {
		t.Errorf("ignored items count as left out: %v", report.NotSynced)
	}
}

// Ignoring files that were synced stops syncing them; both copies stay,
// and later changes on either side stay on that side.
func TestNewlyIgnoredFilesAreLeftAlone(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Both)
	h.write("a.log", "log v1")
	h.write("build/out.bin", "bin v1")
	h.write("notes.txt", "notes")
	h.mustSync()
	before := srv.files("Shared")

	h.write(".cloudignore", "*.log\nbuild/\n")
	h.mustSync()
	h.mustSync()
	after := srv.files("Shared")
	for rel, etag := range before {
		if after[rel] != etag {
			t.Errorf("server copy of %s changed or went: %q → %q", rel, etag, after[rel])
		}
	}
	if _, ok := after[".cloudignore"]; !ok {
		t.Error(".cloudignore was not uploaded")
	}
	for _, rel := range []string{"a.log", "build/out.bin", "notes.txt"} {
		if !h.exists(rel) {
			t.Errorf("local %s went", rel)
		}
	}
	if len(h.trashed()) != 0 {
		t.Errorf("trash = %v", h.trashed())
	}
	if h.tracked("a.log") || h.tracked("build") || h.tracked("build/out.bin") || !h.tracked("notes.txt") {
		t.Error("ignored paths are still tracked (or notes.txt is not)")
	}

	// From now on each side keeps its own copy.
	h.write("a.log", "log v2 local")
	srv.put("Shared/build/out.bin", "bin v2 remote")
	srv.del("Shared/a.log")
	h.mustSync()
	if h.read("a.log") != "log v2 local" || h.read("build/out.bin") != "bin v1" {
		t.Error("an ignored file was synced")
	}
	if _, ok := srv.files("Shared")["a.log"]; ok {
		t.Error("an ignored file was uploaded")
	}
}

// A rule that arrives from another device does the same: nothing is
// deleted on this one either.
func TestRemoteRuleOnSyncedFilesDeletesNothing(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Both)
	h.write("a.log", "log")
	h.write("notes.txt", "notes")
	h.mustSync()
	srv.put("Shared/.cloudignore", "*.log\n")
	h.mustSync()
	if !h.exists("a.log") || !h.exists(".cloudignore") || len(h.trashed()) != 0 {
		t.Errorf("local side changed: a.log %v, .cloudignore %v, trash %v", h.exists("a.log"), h.exists(".cloudignore"), h.trashed())
	}
	if _, ok := srv.files("Shared")["a.log"]; !ok {
		t.Error("server copy of a.log went")
	}
	if h.tracked("a.log") {
		t.Error("a.log still tracked")
	}
}

// Un-ignoring merges both sides like a first sync: equal files are
// recorded, one-sided ones copied, nothing deleted, no conflict copies.
func TestUnignoreMergesWithoutDeletes(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Both)
	h.write("logs/a.log", "same")
	h.write("notes.txt", "notes")
	h.mustSync()
	h.write(".cloudignore", "logs/\n")
	h.mustSync()

	// While ignored: one new file on each side.
	h.write("logs/local.log", "local only")
	srv.put("Shared/logs/remote.log", "remote only")
	h.mustSync()
	if h.exists("logs/remote.log") {
		t.Fatal("downloaded into an ignored folder")
	}

	if err := os.Remove(h.path(".cloudignore")); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	h.mustSync()
	files := srv.files("Shared")
	for _, rel := range []string{"logs/a.log", "logs/local.log", "logs/remote.log", "notes.txt"} {
		if !h.exists(rel) {
			t.Errorf("local %s missing", rel)
		}
		if _, ok := files[rel]; !ok {
			t.Errorf("server %s missing", rel)
		}
	}
	if _, ok := files[".cloudignore"]; ok {
		t.Error(".cloudignore still on the server")
	}
	if !h.tracked("logs/a.log") || !h.tracked("logs/remote.log") {
		t.Error("un-ignored files are not tracked again")
	}
	if len(h.trashed()) != 0 {
		t.Errorf("trash = %v", h.trashed())
	}
	h.noConflictCopies()
}

// A .cloudignore syncs although hidden files are left out.
func TestCloudIgnoreSyncsWithHiddenFilesOff(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Both)
	off := false
	h.configure(func(p *config.Pair) { p.IncludeHidden = &off })
	h.write(".cloudignore", "*.log\n")
	h.write("sub/.cloudignore", "!*.log\n")
	h.write("sub/a.log", "kept")
	h.write("a.log", "ignored")
	h.write(".env", "hidden")
	h.mustSync()
	files := srv.files("Shared")
	for _, rel := range []string{".cloudignore", "sub/.cloudignore", "sub/a.log"} {
		if _, ok := files[rel]; !ok {
			t.Errorf("%s not uploaded", rel)
		}
	}
	for _, rel := range []string{".env", "a.log"} {
		if _, ok := files[rel]; ok {
			t.Errorf("%s uploaded", rel)
		}
	}
}

// Both sides changed the .cloudignore: a conflict like any file, and the
// cloud's rules apply while the local version waits as a conflict copy.
func TestCloudIgnoreConflict(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Both)
	h.write(".cloudignore", "*.tmp\n")
	h.write("a.log", "a")
	h.write("b.bak", "b")
	h.mustSync()
	h.write(".cloudignore", "*.log\n")
	srv.put("Shared/.cloudignore", "*.bak\n")
	h.mustSync()
	if got := h.read(".cloudignore"); got != "*.bak\n" {
		t.Errorf(".cloudignore = %q, want the cloud's", got)
	}
	if h.tracked("b.bak") || !h.tracked("a.log") {
		t.Error("the cloud's rules were not the ones applied")
	}
	copies := 0
	for _, rel := range h.localFiles() {
		if strings.HasPrefix(rel, ".cloudignore (conflict") {
			copies++
		}
	}
	if copies != 1 {
		t.Errorf("%d conflict copies of .cloudignore", copies)
	}
}

// An unreadable cloud .cloudignore stops the pass before it acts.
func TestUnreadableRemoteCloudIgnoreStopsThePass(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/.cloudignore", "*.log\n")
	srv.put("Shared/a.log", "log")
	srv.put("Shared/notes.txt", "notes")
	srv.failContent = ".cloudignore"
	h := newHarness(t, srv, config.Both)
	if err := h.sync(); err == nil {
		t.Fatal("sync succeeded without the rules")
	}
	if h.exists("a.log") || h.exists("notes.txt") {
		t.Error("files were downloaded without knowing the rules")
	}
}
