package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/netstate"
)

// Tests of the per-pair settings (docs/clients.md "Folder settings").

func notSyncedReason(h *harness, rel string) string {
	for _, item := range h.engine.Status().PairState[h.pair.ID].NotSynced {
		if item.Path == rel {
			return item.Reason
		}
	}
	return ""
}

func TestExcludePaths(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/Photos/a.jpg", "photo")
	srv.put("Shared/Docs/b.txt", "doc")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	if h.read("Photos/a.jpg") != "photo" {
		t.Fatal("not downloaded")
	}

	h.configure(func(p *config.Pair) { p.ExcludePaths = []string{"/Photos/"} })
	h.mustSync()
	if h.exists("Photos") {
		t.Error("the excluded folder stayed on this computer")
	}
	if h.trashed()["Photos/a.jpg"] != "photo" {
		t.Errorf("the local copy did not go to the trash: %v", h.trashed())
	}
	if srv.lookup("Shared/Photos/a.jpg") == nil || len(srv.trash) != 0 {
		t.Fatal("excluding a folder touched the cloud")
	}

	srv.put("Shared/Photos/new.jpg", "new")
	h.write("Docs/c.txt", "still synced")
	h.mustSync()
	if h.exists("Photos/new.jpg") {
		t.Error("a file in an excluded folder was downloaded")
	}
	if srv.lookup("Shared/Docs/c.txt") == nil {
		t.Error("the rest of the pair stopped syncing")
	}

	// A local folder of that name with new content is kept, not uploaded.
	h.write("Photos/mine.txt", "only here")
	h.mustSync()
	if h.read("Photos/mine.txt") != "only here" || srv.lookup("Shared/Photos/mine.txt") != nil {
		t.Error("new local content in an excluded folder was moved or uploaded")
	}
	if got := notSyncedReason(h, "Photos"); got != ReasonExcluded {
		t.Errorf("not listed as kept: %q", got)
	}

	// Ticking it again brings it back.
	h.configure(func(p *config.Pair) { p.ExcludePaths = nil })
	h.mustSync()
	if h.read("Photos/a.jpg") != "photo" || srv.lookup("Shared/Photos/mine.txt") == nil {
		t.Error("an included folder did not sync again")
	}
}

func TestExcludeInUploadOnlyPairKeepsLocal(t *testing.T) {
	srv := newFakeServer(t)
	h := newHarness(t, srv, config.Up)
	h.write("Raw/big.bin", "raw")
	h.mustSync()
	h.configure(func(p *config.Pair) { p.ExcludePaths = []string{"Raw"} })
	h.write("Raw/more.bin", "more")
	h.mustSync()
	if h.read("Raw/big.bin") != "raw" {
		t.Error("an upload-only pair moved its own files to the trash")
	}
	if srv.lookup("Shared/Raw/more.bin") != nil || srv.lookup("Shared/Raw/big.bin") == nil || len(srv.trash) != 0 {
		t.Error("the excluded folder was uploaded or deleted in the cloud")
	}
}

func TestMaxFileSize(t *testing.T) {
	srv := newFakeServer(t)
	big := strings.Repeat("x", 1<<20+1)
	srv.put("Shared/remote-big.bin", big)
	srv.put("Shared/remote-small.txt", "small")
	h := newHarness(t, srv, config.Both)
	h.configure(func(p *config.Pair) { p.MaxFileSizeMB = 1 })
	h.write("local-big.bin", big)
	h.write("local-small.txt", "small")
	h.mustSync()
	if h.exists("remote-big.bin") || srv.lookup("Shared/local-big.bin") != nil {
		t.Error("a file over the limit was synced")
	}
	if !h.exists("remote-small.txt") || srv.lookup("Shared/local-small.txt") == nil {
		t.Error("small files were not synced")
	}
	for _, rel := range []string{"remote-big.bin", "local-big.bin"} {
		if got := notSyncedReason(h, rel); got != ReasonTooLarge {
			t.Errorf("%s not listed as too large: %q", rel, got)
		}
	}
}

func TestIncludeHiddenOff(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/.synced-before", "x")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	off := false
	h.configure(func(p *config.Pair) { p.IncludeHidden = &off })
	srv.put("Shared/.secret", "remote")
	h.write(".env", "local")
	h.write("visible.txt", "v")
	h.mustSync()
	if h.exists(".secret") || srv.lookup("Shared/.env") != nil {
		t.Error("a dot file was synced")
	}
	if !h.exists(".synced-before") || srv.lookup("Shared/.synced-before") == nil {
		t.Error("turning hidden files off deleted one on a side")
	}
	if srv.lookup("Shared/visible.txt") == nil {
		t.Error("a visible file was not synced")
	}
}

// conflicted syncs "doc.txt", then changes it on both sides.
func conflicted(t *testing.T, policy string) *harness {
	srv := newFakeServer(t)
	srv.put("Shared/doc.txt", "base")
	h := newHarness(t, srv, config.Both)
	h.configure(func(p *config.Pair) { p.Conflict = policy })
	h.mustSync()
	h.write("doc.txt", "local edit")
	srv.put("Shared/doc.txt", "remote edit")
	return h
}

func remoteContent(h *harness, rel string) string {
	node := h.srv.lookup(rel)
	if node == nil {
		return ""
	}
	return string(h.srv.blobs[node.Etag])
}

func TestConflictPolicies(t *testing.T) {
	t.Run("keep_both", func(t *testing.T) {
		h := conflicted(t, config.ConflictKeepBoth)
		h.mustSync()
		copies, _ := filepath.Glob(h.path("doc (conflict *"))
		if h.read("doc.txt") != "remote edit" || len(copies) != 1 {
			t.Errorf("doc.txt %q, copies %v", h.read("doc.txt"), copies)
		}
	})
	t.Run("prefer_local", func(t *testing.T) {
		h := conflicted(t, config.ConflictPreferLocal)
		h.mustSync()
		if remoteContent(h, "Shared/doc.txt") != "local edit" || h.read("doc.txt") != "local edit" {
			t.Errorf("cloud %q", remoteContent(h, "Shared/doc.txt"))
		}
		if h.trashed()["doc.txt"] != "remote edit" {
			t.Errorf("the cloud version is not in the local trash: %v", h.trashed())
		}
	})
	t.Run("prefer_remote", func(t *testing.T) {
		h := conflicted(t, config.ConflictPreferRemote)
		h.mustSync()
		if h.read("doc.txt") != "remote edit" || remoteContent(h, "Shared/doc.txt") != "remote edit" {
			t.Errorf("local %q", h.read("doc.txt"))
		}
		if h.trashed()["doc.txt"] != "local edit" {
			t.Errorf("the local version is not in the trash: %v", h.trashed())
		}
	})
	t.Run("newer", func(t *testing.T) {
		h := conflicted(t, config.ConflictNewer) // the local edit is newer than the cloud's 2026-01-01
		h.mustSync()
		if remoteContent(h, "Shared/doc.txt") != "local edit" {
			t.Error("the newer local edit lost")
		}
		h2 := conflicted(t, config.ConflictNewer)
		old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		os.Chtimes(h2.path("doc.txt"), old, old)
		h2.mustSync()
		if h2.read("doc.txt") != "remote edit" {
			t.Error("the newer cloud edit lost")
		}
	})
	t.Run("issues view choice", func(t *testing.T) {
		h := conflicted(t, config.ConflictKeepBoth)
		if err := h.engine.Resolve(h.pair.ID, "doc.txt", ResolveLocal); err != nil {
			t.Fatal(err)
		}
		h.mustSync()
		copies, _ := filepath.Glob(h.path("doc (conflict *"))
		if remoteContent(h, "Shared/doc.txt") != "local edit" || len(copies) != 0 {
			t.Errorf("keep local: cloud %q, copies %v", remoteContent(h, "Shared/doc.txt"), copies)
		}
	})
}

func TestPropagateDeletesOff(t *testing.T) {
	off := false
	t.Run("upload only is a backup", func(t *testing.T) {
		srv := newFakeServer(t)
		h := newHarness(t, srv, config.Up)
		h.configure(func(p *config.Pair) { p.PropagateDeletes = &off })
		h.write("keep.txt", "k")
		h.mustSync()
		os.Remove(h.path("keep.txt"))
		h.mustSync()
		h.mustSync()
		if srv.lookup("Shared/keep.txt") == nil || len(srv.trash) != 0 {
			t.Error("a local delete reached the cloud")
		}
		if h.exists("keep.txt") {
			t.Error("an upload-only pair wrote locally")
		}
	})
	t.Run("both ways restores", func(t *testing.T) {
		srv := newFakeServer(t)
		srv.put("Shared/a.txt", "a")
		srv.put("Shared/b.txt", "b")
		h := newHarness(t, srv, config.Both)
		h.configure(func(p *config.Pair) { p.PropagateDeletes = &off })
		h.mustSync()
		os.Remove(h.path("a.txt"))
		srv.del("Shared/b.txt")
		h.mustSync()
		if h.read("a.txt") != "a" || srv.lookup("Shared/b.txt") == nil {
			t.Error("a deleted file did not come back from the other side")
		}
	})
}

func TestNewFolders(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/Old/a.txt", "a")
	h := newHarness(t, srv, config.Both)
	h.configure(func(p *config.Pair) { p.NewFolders = config.NewFoldersAsk })
	h.mustSync() // the first sync takes everything
	if !h.exists("Old/a.txt") {
		t.Fatal("first sync held a folder back")
	}
	srv.put("Shared/Videos/v.mp4", "video")
	h.mustSync()
	asks := h.engine.Status().PairState[h.pair.ID].NewFolders
	if h.exists("Videos") || len(asks) != 1 || asks[0].Path != "Videos" || asks[0].Size != 5 {
		t.Fatalf("not held for a prompt: %v", asks)
	}
	if err := h.engine.DecideNewFolder(h.pair.ID, "Videos", true); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	if !h.exists("Videos/v.mp4") || len(h.engine.Status().PairState[h.pair.ID].NewFolders) != 0 {
		t.Error("an accepted folder did not sync")
	}

	srv.put("Shared/Junk/j.bin", "j")
	h.mustSync()
	if err := h.engine.DecideNewFolder(h.pair.ID, "Junk", false); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	if pair, _ := h.engine.cfg.Get().Pair(h.pair.ID); h.exists("Junk") || len(pair.ExcludePaths) != 1 || pair.ExcludePaths[0] != "Junk" {
		t.Errorf("a declined folder: excludes %v", pair.ExcludePaths)
	}

	// skip excludes on its own; a big folder asks even with sync.
	h.configure(func(p *config.Pair) { p.NewFolders = config.NewFoldersSkip })
	srv.put("Shared/Skipped/s.txt", "s")
	h.mustSync()
	if pair, _ := h.engine.cfg.Get().Pair(h.pair.ID); h.exists("Skipped") || !contains(pair.ExcludePaths, "Skipped") {
		t.Errorf("skip: excludes %v", pair.ExcludePaths)
	}
	one := 1
	h.configure(func(p *config.Pair) { p.NewFolders, p.BigFolderPromptMB = config.NewFoldersSync, &one })
	srv.put("Shared/Big/b.bin", strings.Repeat("b", 1<<20+1))
	srv.put("Shared/Small/s.txt", "s")
	h.mustSync()
	asks = h.engine.Status().PairState[h.pair.ID].NewFolders
	if h.exists("Big") || !h.exists("Small/s.txt") || len(asks) != 1 || asks[0].Path != "Big" {
		t.Errorf("big folder: %v", asks)
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func TestNetworkRules(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/a.txt", "a")
	h := newHarness(t, srv, config.Both)
	h.engine.conditions = func() netstate.Conditions { return netstate.Conditions{Metered: true} }
	h.mustSync()
	if h.exists("a.txt") {
		t.Fatal("synced on a metered connection")
	}
	if status := h.engine.Status(); status.State != StatePaused || status.PairState[h.pair.ID].Held == "" {
		t.Errorf("status %+v", status)
	}
	off := false
	h.configure(func(p *config.Pair) { p.RespectNetworkRules = &off })
	h.mustSync()
	if !h.exists("a.txt") {
		t.Error("a pair that ignores the network rules did not sync")
	}
}
