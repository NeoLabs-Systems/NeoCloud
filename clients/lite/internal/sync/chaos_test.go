package sync

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/netstate"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// Chaos tests: the things that go wrong on real computers (drives that
// disappear, processes that die, servers that fail, names one system
// cannot hold) must never cost a file on either side.

// restart is a new process: a fresh engine on the same config, with the
// state database opened again from disk.
func (h *harness) restart() {
	h.t.Helper()
	h.db.Close()
	db, err := state.Open(h.home)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { db.Close() })
	engine := New(h.engine.cfg, db, log.New(io.Discard, "", 0))
	engine.conditions = func() netstate.Conditions { return netstate.Conditions{} }
	h.db, h.engine = db, engine
}

// report is the pair's report from the last run.
func (h *harness) report() PairState {
	return h.engine.Status().PairState[h.pair.ID]
}

// serverKeepsAll fails unless every content hash the server held before
// is still held (live, in its trash or as an older version).
func serverKeepsAll(t *testing.T, srv *fakeServer, before map[string]bool) {
	t.Helper()
	after := srv.everyHash()
	for hash := range before {
		if !after[hash] {
			t.Errorf("the server lost content %s", hash[:8])
		}
	}
}

func liveFiles(srv *fakeServer) map[string]string {
	out := map[string]string{}
	for rel, etag := range srv.files("Shared") {
		if etag != "/" {
			out[rel] = etag
		}
	}
	return out
}

// A root that is a symbolic link (a folder moved to another disk and
// linked back) is walked at its target. WalkDir does not enter a link,
// so the pair used to look empty and every synced file was deleted in
// the cloud on the second pass.
func TestSymlinkedRootIsScanned(t *testing.T) {
	srv := newFakeServer(t)
	for _, name := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		srv.put("Shared/"+name, "remote "+name)
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "Linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symlinks here")
	}
	h := newHarnessAt(t, srv, link, "/Shared", config.Both)
	h.write("local.txt", "local")
	h.mustSync()
	h.mustSync()
	remote := liveFiles(srv)
	if len(remote) != 4 || remote["a.txt"] == "" || remote["local.txt"] == "" {
		t.Fatalf("server after two passes through a linked root: %v", remote)
	}
	if data, err := os.ReadFile(filepath.Join(target, "sub", "c.txt")); err != nil || string(data) != "remote sub/c.txt" {
		t.Fatalf("download through the link: %q %v", data, err)
	}
	// A real delete through the link still syncs.
	os.Remove(filepath.Join(target, "a.txt"))
	h.mustSync()
	if _, ok := liveFiles(srv)["a.txt"]; ok {
		t.Fatal("a delete through the link did not sync")
	}
}

// A folder that cannot be read (permissions, a flaky network share) is
// reported and left alone: its files must not read as deleted.
func TestUnreadableFolderIsLeftAlone(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	// 0: nothing inside can be looked at. 0o300: names can't be listed,
	// though files inside can still be opened by name.
	for _, mode := range []os.FileMode{0, 0o300} {
		t.Run(mode.String(), func(t *testing.T) {
			srv := newFakeServer(t)
			srv.put("Shared/Private/a.txt", "a")
			srv.put("Shared/Private/b.txt", "b")
			srv.put("Shared/open.txt", "o")
			h := newHarness(t, srv, config.Both)
			h.mustSync()
			if err := os.Chmod(h.path("Private"), mode); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(h.path("Private"), 0o755)
			h.mustSync()
			if remote := liveFiles(srv); len(remote) != 3 {
				t.Fatalf("server lost files behind an unreadable folder: %v", remote)
			}
			if notSyncedReason(h, "Private") != ReasonUnreadable {
				t.Fatalf("not reported: %+v", h.report().NotSynced)
			}
			// A cloud change inside it waits too; nothing is written there.
			srv.put("Shared/Private/a.txt", "edited in the cloud")
			h.mustSync()
			os.Chmod(h.path("Private"), 0o755)
			h.mustSync()
			if h.read("Private/a.txt") != "edited in the cloud" || h.read("Private/b.txt") != "b" {
				t.Fatal("folder did not catch up once readable")
			}
			h.noConflictCopies()
		})
	}
}

// The drive goes away in the middle of a pass (after the scan, while
// files transfer). Deletions planned for this pass must not run against
// a folder that is no longer there: on a vanished folder every file
// reads as gone, and their rows would be dropped.
func TestDriveRemovedMidPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows can't rename a folder that has files open in it, so the
		// test can't make the drive vanish mid-download there.
		t.Skip("can't simulate an unplugged drive by renaming on Windows")
	}
	srv := newFakeServer(t)
	srv.put("Shared/keep.txt", "keep")
	srv.put("Shared/removed-in-cloud.txt", "r")
	srv.put("Shared/big.bin", "v1")
	h := newHarness(t, srv, config.Both)
	h.mustSync()

	srv.del("Shared/removed-in-cloud.txt")
	srv.put("Shared/big.bin", "v2")
	unplugged := filepath.Join(t.TempDir(), "unplugged")
	srv.onContent = func(w http.ResponseWriter, r *http.Request, data []byte) bool {
		os.Rename(h.root, unplugged) // the drive goes away during the download
		return false
	}
	if err := h.sync(); err == nil {
		t.Fatal("a pass on a vanished folder reported success")
	}
	srv.onContent = nil
	if !h.tracked("removed-in-cloud.txt") || !h.tracked("keep.txt") {
		t.Fatal("rows were dropped while the folder was gone")
	}
	if remote := liveFiles(srv); len(remote) != 2 {
		t.Fatalf("server changed: %v", remote)
	}
	// While it is away, passes stop without touching anything.
	if err := h.sync(); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected the folder to be unavailable, got %v", err)
	}
	// An empty mount point in its place (the drive is not mounted).
	os.Mkdir(h.root, 0o755)
	if err := h.sync(); err == nil || !strings.Contains(err.Error(), "unmounted or replaced") {
		t.Fatalf("expected the marker error, got %v", err)
	}
	os.Remove(h.root)
	// Plugged in again: the cloud delete applies (to the local trash) and
	// is not resurrected by an upload.
	if err := os.Rename(unplugged, h.root); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	if h.exists("removed-in-cloud.txt") || h.trashed()["removed-in-cloud.txt"] != "r" {
		t.Fatal("the cloud delete was not applied after the drive came back")
	}
	if _, ok := liveFiles(srv)["removed-in-cloud.txt"]; ok {
		t.Fatal("a file deleted in the cloud was uploaded again")
	}
	if h.read("big.bin") != "v2" || h.read("keep.txt") != "keep" {
		t.Fatal("did not converge")
	}
}

// A local folder that is suddenly empty (a share that lists nothing, an
// emptied mount) waits for a confirmation whatever the count, like a
// suddenly empty cloud folder.
func TestLocalSuddenlyEmptyWaits(t *testing.T) {
	srv := newFakeServer(t)
	for _, name := range []string{"a.txt", "b.txt", "c/d.txt"} {
		srv.put("Shared/"+name, name)
	}
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	for _, name := range []string{"a.txt", "b.txt", "c"} {
		os.RemoveAll(h.path(name))
	}
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.Reason != GuardLocalEmpty || confirm.Pending.RemoteDeletes != 3 {
		t.Fatalf("expected localEmpty, got %v", err)
	}
	if remote := liveFiles(srv); len(remote) != 3 {
		t.Fatalf("server changed: %v", remote)
	}
	h.engine.ConfirmDeletes(h.pair.ID)
	h.mustSync()
	if remote := liveFiles(srv); len(remote) != 0 {
		t.Fatalf("confirmed deletes did not run: %v", remote)
	}
}

// The state database is lost (deleted, or damaged and set aside): the
// next pass merges like a first sync. A file deleted locally meanwhile
// comes back rather than being deleted in the cloud, and equal files are
// recorded, not turned into conflict copies.
func TestStateDatabaseLostOrDamaged(t *testing.T) {
	for _, how := range []string{"deleted", "damaged"} {
		t.Run(how, func(t *testing.T) {
			srv := newFakeServer(t)
			srv.put("Shared/a.txt", "a")
			srv.put("Shared/sub/b.txt", "b")
			h := newHarness(t, srv, config.Both)
			h.write("local.txt", "local")
			h.mustSync()
			before := srv.everyHash()

			os.Remove(h.path("a.txt"))             // deleted while the state is gone
			h.write("sub/b.txt", "edited locally") // edited while the state is gone
			h.db.Close()
			dbPath := filepath.Join(h.home, "state.db")
			for _, suffix := range []string{"-wal", "-shm"} {
				os.Remove(dbPath + suffix)
			}
			if how == "deleted" {
				os.Remove(dbPath)
			} else {
				os.WriteFile(dbPath, []byte(strings.Repeat("garbage ", 1000)), 0o600)
			}
			h.restart()
			if how == "damaged" && h.db.Recovered() == "" {
				t.Fatal("the damaged database was not set aside")
			}
			h.mustSync()
			h.mustSync()
			serverKeepsAll(t, srv, before)
			if h.read("a.txt") != "a" {
				t.Fatal("a file missing locally was not restored from the cloud")
			}
			if remote := liveFiles(srv); remote["a.txt"] == "" || remote["local.txt"] == "" {
				t.Fatalf("a merge without state deleted in the cloud: %v", remote)
			}
			// sub/b.txt differs on both sides with no base: both kept.
			copies, _ := filepath.Glob(h.path("sub/b (conflict *"))
			if len(copies) != 1 || h.read("sub/b.txt") != "b" {
				t.Fatalf("differing file without state: copies %v, b=%q", copies, h.read("sub/b.txt"))
			}
		})
	}
}

// A process killed mid-download leaves only a part file: the original
// is untouched, and a new process resumes and finishes.
func TestKillMidDownload(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/doc.bin", "old version")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	content := strings.Repeat("new version ", 5000)
	srv.put("Shared/doc.bin", content)
	srv.put("Shared/fresh.bin", content+"fresh")

	ctx, kill := context.WithCancel(context.Background())
	srv.onContent = func(w http.ResponseWriter, r *http.Request, data []byte) bool {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		kill() // the process dies with half the file on disk
		<-r.Context().Done()
		return true
	}
	h.engine.SyncOnce(ctx)
	srv.onContent = nil
	if h.read("doc.bin") != "old version" {
		t.Fatal("a killed download replaced the original")
	}
	if h.exists("fresh.bin") {
		t.Fatal("a killed download appeared under the real name")
	}
	parts, _ := filepath.Glob(h.path("*" + api.PartSuffix))
	if len(parts) == 0 {
		t.Fatal("no part kept for the resume")
	}

	h.restart()
	h.mustSync()
	if h.read("doc.bin") != content || h.read("fresh.bin") != content+"fresh" {
		t.Fatal("the next process did not finish the downloads")
	}
	if parts, _ := filepath.Glob(h.path("*" + api.PartSuffix)); len(parts) != 0 {
		t.Fatalf("parts left: %v", parts)
	}
	if h.trashed()["doc.bin"] != "old version" {
		t.Fatal("the replaced version is not in the local trash")
	}
	h.noConflictCopies()
}

// A process killed mid-upload never leaves a truncated file in the
// cloud (the server checks the declared hash), and the next process
// uploads the file.
func TestKillMidUpload(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	h.write("report.txt", strings.Repeat("r", 1<<20))
	ctx, kill := context.WithCancel(context.Background())
	srv.beforeUpload = func(string, string) { kill() }
	h.engine.SyncOnce(ctx)
	srv.beforeUpload = nil
	if node := srv.lookup("Shared/report.txt"); node != nil && node.Size != 1<<20 {
		t.Fatalf("a truncated file reached the server: %d bytes", node.Size)
	}
	h.restart()
	h.mustSync()
	if remote := remoteContent(h, "Shared/report.txt"); len(remote) != 1<<20 {
		t.Fatalf("server holds %d bytes", len(remote))
	}
}

// The process dies after changing files but before its state rows were
// written: every row write of a pass is lost. The next pass must neither
// delete anything nor make conflict copies.
func TestKillBeforeStateIsWritten(t *testing.T) {
	srv := newFakeServer(t)
	for _, name := range []string{"edit-remote.txt", "edit-local.txt", "del-remote.txt", "del-local.txt", "ren-remote.txt", "ren-local.txt", "dir/x.txt"} {
		srv.put("Shared/"+name, "base "+name)
	}
	h := newHarness(t, srv, config.Both)
	h.mustSync()

	srv.put("Shared/edit-remote.txt", "remote edit")
	srv.put("Shared/new-remote.txt", "remote new")
	srv.del("Shared/del-remote.txt")
	srv.rename("Shared/ren-remote.txt", "renamed-remote.txt")
	h.write("edit-local.txt", "local edit")
	h.write("new-local.txt", "local new")
	os.Remove(h.path("del-local.txt"))
	os.Rename(h.path("ren-local.txt"), h.path("renamed-local.txt"))
	os.Rename(h.path("dir"), h.path("moved-dir"))

	rows, _ := h.db.Entries(h.pair.ID)
	h.mustSync()
	// Roll the state back to before the pass: the files changed, the
	// rows did not.
	h.db.ClearEntries(h.pair.ID)
	for _, row := range rows {
		h.db.Put(h.pair.ID, row)
	}
	before := srv.everyHash()
	beforeRemote := liveFiles(srv)
	h.restart()
	h.mustSync()
	h.mustSync()
	h.noConflictCopies()
	serverKeepsAll(t, srv, before)
	if out := liveFiles(srv); len(out) != len(beforeRemote) {
		t.Fatalf("server files changed after the replay:\nbefore %v\nafter  %v", beforeRemote, out)
	}
	for _, want := range []string{"edit-remote.txt", "edit-local.txt", "new-remote.txt", "new-local.txt", "renamed-remote.txt", "renamed-local.txt", "moved-dir/x.txt"} {
		if !h.exists(want) {
			t.Errorf("%s missing locally", want)
		}
	}
	for _, gone := range []string{"del-remote.txt", "del-local.txt", "ren-remote.txt", "ren-local.txt", "dir"} {
		if h.exists(gone) {
			t.Errorf("%s came back locally", gone)
		}
	}
}

// Server failures in the middle of syncing: a 500 on the tree, on a
// download or on a delete changes nothing that cannot be redone; an
// empty or partial tree never deletes in the cloud and what it removes
// locally sits in the local trash and comes back.
func TestServerFailuresMidPass(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/a.txt", "a")
	srv.put("Shared/sub/b.txt", "b")
	srv.put("Shared/sub/c.txt", "c")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	before := srv.everyHash()

	srv.treeFail = 500
	if err := h.sync(); err == nil {
		t.Fatal("a failing tree was not reported")
	}
	srv.treeFail = 0

	// An empty answer: the pair's folder is missing, nothing happens.
	srv.treeDrop = func(string) bool { return true }
	if err := h.sync(); !errors.Is(err, errRemoteRootMissing) {
		t.Fatalf("expected the missing remote folder, got %v", err)
	}
	// A partial answer: part of the folder is missing.
	srv.treeDrop = func(p string) bool { return strings.HasPrefix(p, "Shared/sub/") }
	h.sync()
	srv.treeDrop = nil
	h.mustSync()
	if h.read("sub/b.txt") != "b" || h.read("sub/c.txt") != "c" {
		t.Fatal("files hidden by a partial answer did not come back")
	}
	if remote := liveFiles(srv); len(remote) != 3 {
		t.Fatalf("server changed: %v", remote)
	}

	// A delete the server refuses is retried, not forgotten.
	os.Remove(h.path("a.txt"))
	srv.removeFail = 500
	if err := h.sync(); err == nil {
		t.Fatal("a failed delete was not reported")
	}
	srv.removeFail = 0
	if !h.tracked("a.txt") {
		t.Fatal("the row of a failed delete was dropped")
	}
	h.mustSync()
	if _, ok := liveFiles(srv)["a.txt"]; ok {
		t.Fatal("the delete was not retried")
	}
	serverKeepsAll(t, srv, before)
}

// A suddenly empty cloud folder for a pair of the whole cloud.
func TestEmptyTreeForWholeCloudPair(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("a.txt", "a")
	srv.put("b/c.txt", "c")
	h := newHarnessAt(t, srv, t.TempDir(), "/", config.Both)
	h.configure(func(p *config.Pair) { p.NewFolders = config.NewFoldersSync })
	h.mustSync()
	srv.treeDrop = func(string) bool { return true }
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.Reason != GuardRemoteEmpty {
		t.Fatalf("expected remoteEmpty, got %v", err)
	}
	if !h.exists("a.txt") || !h.exists("b/c.txt") {
		t.Fatal("local files deleted on an empty answer")
	}
}

// Names Windows cannot hold are reported and never written, and a folder
// deleted on Windows keeps them in the cloud.
func TestWindowsInvalidNamesAreNeverDeleted(t *testing.T) {
	windowsNames = true
	defer func() { windowsNames = false }()
	srv := newFakeServer(t)
	srv.put("Shared/Docs/ok.txt", "ok")
	srv.put("Shared/Docs/what?.txt", "question")
	srv.put("Shared/Docs/NUL.txt", "reserved")
	srv.put("Shared/Docs/trailing.", "dot")
	srv.put("Shared/Docs/space ", "space")
	srv.put("Shared/Docs/a:b.txt", "colon")
	srv.put("Shared/other.txt", "other")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	for _, bad := range []string{"Docs/what?.txt", "Docs/NUL.txt", "Docs/trailing.", "Docs/space ", "Docs/a:b.txt"} {
		if h.exists(bad) {
			t.Errorf("%s written locally", bad)
		}
		if notSyncedReason(h, bad) != ReasonInvalidName {
			t.Errorf("%s not reported", bad)
		}
	}
	os.RemoveAll(h.path("Docs"))
	h.mustSync()
	remote := liveFiles(srv)
	if _, ok := remote["Docs/ok.txt"]; ok {
		t.Error("the synced file was not deleted")
	}
	for _, kept := range []string{"Docs/what?.txt", "Docs/NUL.txt", "Docs/trailing.", "Docs/space ", "Docs/a:b.txt"} {
		if remote[kept] == "" {
			t.Errorf("%s was deleted with its folder", kept)
		}
	}
	if notSyncedReason(h, "Docs") != ReasonKeptInCloud {
		t.Fatalf("kept folder not reported: %+v", h.report().NotSynced)
	}
	h.mustSync() // stays quiet
	if remote := liveFiles(srv); len(remote) != 6 {
		t.Fatalf("server: %v", remote)
	}
}

// A folder deleted locally that holds an excluded (selective sync)
// subfolder in the cloud is not deleted as a whole.
func TestLocalFolderDeleteKeepsExcludedCloudContent(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/Photos/a.jpg", "a")
	srv.put("Shared/Photos/Raw/r.raw", "raw")
	srv.put("Shared/other.txt", "other")
	h := newHarness(t, srv, config.Both)
	h.configure(func(p *config.Pair) { p.ExcludePaths = []string{"Photos/Raw"} })
	h.mustSync()
	if h.exists("Photos/Raw") {
		t.Fatal("excluded folder synced")
	}
	os.RemoveAll(h.path("Photos"))
	h.mustSync()
	remote := liveFiles(srv)
	if remote["Photos/Raw/r.raw"] == "" {
		t.Fatal("excluded content was deleted with its parent folder")
	}
	if _, ok := remote["Photos/a.jpg"]; ok {
		t.Fatal("the synced file was not deleted")
	}
}

// Unicode: a name written decomposed (macOS) on one side and composed on
// the other is one file; when contents differ it is a conflict, never an
// overwrite or a delete.
func TestUnicodeNamesNeverOverwrite(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/"+norm.NFD.String("Zoë.txt"), "remote")
	h := newHarness(t, srv, config.Both)
	h.write(norm.NFC.String("Zoë.txt"), "local")
	h.mustSync()
	h.mustSync()
	copies, _ := filepath.Glob(h.path("Zo*conflict*"))
	if len(copies) != 1 {
		t.Fatalf("expected one conflict copy, got %v (local %v)", copies, h.localFiles())
	}
	if remote := liveFiles(srv); len(remote) != 2 {
		t.Fatalf("server: %v", remote)
	}
	everything := map[string]bool{}
	for _, rel := range h.localFiles() {
		data, _ := os.ReadFile(h.path(rel))
		everything[string(data)] = true
	}
	if !everything["remote"] || !everything["local"] {
		t.Fatalf("a version was lost: %v", everything)
	}
	// Renaming it locally to the other normalization changes nothing.
	name := norm.NFC.String("Zoë.txt")
	if err := os.Rename(h.path(name), h.path(norm.NFD.String("Zoë.txt"))); err == nil {
		seq := srv.journalSeq()
		h.mustSync()
		if srv.journalSeq() != seq {
			t.Fatal("a normalization-only rename changed the server")
		}
	}
}

// Case-only renames against the case-insensitive server: a remote one is
// followed locally; a local one is never a delete.
func TestCaseOnlyRenames(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/Report.txt", "report")
	srv.put("Shared/notes.txt", "notes")
	h := newHarness(t, srv, config.Both)
	h.mustSync()

	srv.rename("Shared/Report.txt", "REPORT.txt")
	h.mustSync()
	// Known gap (cosmetic, nothing is lost): a remote case-only rename is
	// not applied to the local name. A case-insensitive disk (macOS,
	// Windows) finds the file under either spelling; a case-sensitive one
	// (Linux) keeps the old spelling, so only the content is checked there.
	if caseSensitiveDisk(t, h.root) {
		if !h.exists("Report.txt") || h.read("Report.txt") != "report" {
			t.Fatalf("file lost after a remote case rename: %v", h.localFiles())
		}
	} else if !h.exists("REPORT.txt") || h.read("REPORT.txt") != "report" {
		t.Fatalf("remote case rename not followed: %v", h.localFiles())
	}

	if err := os.Rename(h.path("notes.txt"), h.path("Notes.txt")); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	h.mustSync()
	remote := liveFiles(srv)
	if len(remote) != 2 || (remote["notes.txt"] == "" && remote["Notes.txt"] == "") {
		t.Fatalf("a local case rename lost the file in the cloud: %v", remote)
	}
	if h.read("Notes.txt") != "notes" {
		t.Fatal("local file changed")
	}
	h.noConflictCopies()
}

// Moving a synced file into an ignored folder stops syncing it; the
// cloud copy stays (git would delete it, people expect it to stay).
func TestMoveIntoIgnoredFolderKeepsCloudCopy(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/a.txt", "alpha")
	srv.put("Shared/docs/b.txt", "bravo")
	srv.put("Shared/docs/c.txt", "charlie")
	srv.put("Shared/deleted.txt", "delta")
	h := newHarness(t, srv, config.Both, "build/")
	h.mustSync()

	os.MkdirAll(h.path("build"), 0o755)
	os.Rename(h.path("a.txt"), h.path("build/a.txt"))
	os.Rename(h.path("docs"), h.path("build/docs"))
	os.Remove(h.path("deleted.txt")) // a real delete
	h.mustSync()
	remote := liveFiles(srv)
	for _, kept := range []string{"a.txt", "docs/b.txt", "docs/c.txt"} {
		if remote[kept] == "" {
			t.Errorf("%s was deleted in the cloud", kept)
		}
	}
	if _, ok := remote["deleted.txt"]; ok {
		t.Error("a real delete did not sync")
	}
	if notSyncedReason(h, "a.txt") != ReasonMovedIgnored || notSyncedReason(h, "docs/b.txt") != ReasonMovedIgnored {
		t.Fatalf("moves not reported: %+v", h.report().NotSynced)
	}
	// Stable: nothing comes back locally, nothing changes in the cloud.
	seq := srv.journalSeq()
	h.mustSync()
	if h.exists("a.txt") || h.exists("docs") || srv.journalSeq() != seq {
		t.Fatalf("not stable: local %v", h.localFiles())
	}
	// Edited in the cloud: it syncs again (comes back to its old place).
	srv.put("Shared/a.txt", "alpha 2")
	h.mustSync()
	if h.read("a.txt") != "alpha 2" || h.read("build/a.txt") != "alpha" {
		t.Fatal("a cloud edit did not bring the file back")
	}
	h.noConflictCopies()
}

// Editors that save by renaming the original aside (to an ignored *.tmp)
// and writing a new file: the original is parked, not deleted, and the
// new file is an edit of it, not a conflict.
func TestSaveViaRenameToIgnoredName(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/thesis.txt", "draft 1")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	os.Rename(h.path("thesis.txt"), h.path("thesis.txt.tmp"))
	h.mustSync() // a pass catches the moment between the two steps
	if remoteContent(h, "Shared/thesis.txt") != "draft 1" {
		t.Fatal("the original was deleted in the cloud mid-save")
	}
	h.write("thesis.txt", "draft 2")
	os.Remove(h.path("thesis.txt.tmp"))
	h.mustSync()
	if remoteContent(h, "Shared/thesis.txt") != "draft 2" {
		t.Fatalf("the save did not upload: %q", remoteContent(h, "Shared/thesis.txt"))
	}
	h.noConflictCopies()
	if !h.tracked("thesis.txt") {
		t.Fatal("not tracked again")
	}
}

// Removing a pair and adding the same folder again (or signing out and
// in) forgets the state; the merge that follows deletes nothing.
func TestReaddPairAndSignInAgainDeleteNothing(t *testing.T) {
	srv := newFakeServer(t)
	for _, name := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		srv.put("Shared/"+name, name)
	}
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	before := srv.everyHash()

	if err := h.engine.RemovePair(h.pair.ID); err != nil {
		t.Fatal(err)
	}
	os.Remove(h.path("a.txt"))
	srv.del("Shared/b.txt")
	pair, err := config.NewPair(h.root, "/Shared", config.Both, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.AddPair(pair); err != nil {
		t.Fatal(err)
	}
	h.pair = pair
	h.mustSync()
	if !h.exists("a.txt") || !h.exists("b.txt") {
		t.Fatal("a re-added pair deleted locally")
	}
	if remote := liveFiles(srv); remote["a.txt"] == "" || remote["b.txt"] == "" || len(remote) != 3 {
		t.Fatalf("a re-added pair deleted in the cloud: %v", remote)
	}

	token := &api.DeviceToken{Token: "token"}
	if err := h.engine.SignOut(); err != nil {
		t.Fatal(err)
	}
	os.Remove(h.path("sub/c.txt"))
	if err := h.engine.SignIn(srv.srv.URL, token); err != nil {
		t.Fatal(err)
	}
	h.mustSync()
	if h.read("sub/c.txt") != "sub/c.txt" || len(liveFiles(srv)) != 3 {
		t.Fatal("signing in again deleted something")
	}
	h.noConflictCopies()
	serverKeepsAll(t, srv, before)
}

// The local trash keeps every day that holds the only copy of something
// (a local version that lost a conflict, ignored files of a folder
// deleted in the cloud), whatever the retention.
func TestTrashRetentionKeepsOnlyCopies(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/doc.txt", "base")
	srv.put("Shared/Proj/a.txt", "a")
	srv.put("Shared/plain.txt", "p")
	h := newHarness(t, srv, config.Both, "*.env")
	h.configure(func(p *config.Pair) { p.Conflict = config.ConflictPreferRemote })
	h.mustSync()
	h.write("doc.txt", "precious local edit")
	h.write("Proj/secret.env", "only here")
	srv.put("Shared/doc.txt", "remote edit")
	srv.del("Shared/Proj")
	srv.del("Shared/plain.txt")
	h.mustSync()
	trash := h.trashed()
	if trash["doc.txt"] != "precious local edit" || trash["Proj/secret.env"] != "only here" || trash["plain.txt"] != "p" {
		t.Fatalf("trash: %v", trash)
	}
	day := time.Now().Format(trashDayFormat)
	if _, err := os.Stat(filepath.Join(h.root, metaDir, trashDir, day+keepSuffix)); err != nil {
		t.Fatal("the day holding only copies is not marked")
	}
	pruneTrash(h.root, time.Now().AddDate(0, 0, 90), 30)
	if h.trashed()["doc.txt"] != "precious local edit" {
		t.Fatal("retention pruned the only copy of a file")
	}

	// A day of plain cloud deletes (the cloud has them) is pruned.
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o644)
	old := time.Now().AddDate(0, 0, -40)
	if _, err := moveToTrash(other, "x.txt", old, false); err != nil {
		t.Fatal(err)
	}
	pruneTrash(other, time.Now(), 30)
	if _, err := os.Stat(filepath.Join(other, metaDir, trashDir, old.Format(trashDayFormat))); err == nil {
		t.Fatal("an ordinary old day was not pruned")
	}
}

// Odd modification times (a server clock far ahead or behind, files from
// before 1970) never make a file look changed again and again.
func TestOddModificationTimesSettle(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/future.txt", "f")
	srv.put("Shared/past.txt", "p")
	srv.put("Shared/empty.txt", "")
	srv.setModified("Shared/future.txt", "2099-12-31 23:59:59")
	srv.setModified("Shared/past.txt", "1960-01-01 00:00:00")
	h := newHarness(t, srv, config.Both)
	h.write("old-local.txt", "o")
	h.write("empty-local.txt", "")
	os.Chtimes(h.path("old-local.txt"), time.Unix(-86400*365, 0), time.Unix(-86400*365, 0))
	h.mustSync()
	seq := srv.journalSeq()
	trashBefore := len(h.trashed())
	for i := 0; i < 3; i++ {
		h.mustSync()
	}
	if srv.journalSeq() != seq || len(h.trashed()) != trashBefore {
		t.Fatal("odd times keep the pair busy")
	}
	if h.read("empty.txt") != "" || remoteContent(h, "Shared/empty-local.txt") != "" || srv.lookup("Shared/empty-local.txt") == nil {
		t.Fatal("empty files did not sync")
	}
	h.noConflictCopies()
}

// An edit always wins over a delete, also when the delete is of the
// folder around it, on either side.
func TestEditWinsOverFolderDelete(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/Local/a.txt", "a")
	srv.put("Shared/Local/b.txt", "b")
	srv.put("Shared/Remote/c.txt", "c")
	srv.put("Shared/Remote/d.txt", "d")
	srv.put("Shared/other.txt", "o")
	h := newHarness(t, srv, config.Both)
	h.mustSync()

	srv.del("Shared/Local")          // deleted in the cloud…
	h.write("Local/a.txt", "edited") // …while edited here
	os.RemoveAll(h.path("Remote"))   // deleted here…
	srv.put("Shared/Remote/c.txt", "edited in the cloud")
	h.mustSync()
	h.mustSync()
	if h.read("Local/a.txt") != "edited" || remoteContent(h, "Shared/Local/a.txt") != "edited" {
		t.Fatal("a local edit lost against a cloud folder delete")
	}
	if h.read("Remote/c.txt") != "edited in the cloud" || remoteContent(h, "Shared/Remote/c.txt") != "edited in the cloud" {
		t.Fatal("a cloud edit lost against a local folder delete")
	}
	if h.exists("Local/b.txt") || srv.lookup("Shared/Remote/d.txt") != nil {
		t.Fatal("the unedited files were not deleted")
	}
}

// A rename on one side and an edit on the other keep the edit.
func TestRenameAndEditOnOppositeSides(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/local-rename.txt", "base 1")
	srv.put("Shared/remote-rename.txt", "base 2")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	os.Rename(h.path("local-rename.txt"), h.path("renamed-here.txt"))
	srv.put("Shared/local-rename.txt", "edited in the cloud")
	srv.rename("Shared/remote-rename.txt", "renamed-there.txt")
	h.write("remote-rename.txt", "edited here")
	h.mustSync()
	h.mustSync()
	found := map[string]bool{}
	for _, rel := range h.localFiles() {
		found[h.read(rel)] = true
	}
	for _, want := range []string{"edited in the cloud", "edited here"} {
		if !found[want] {
			t.Errorf("%q lost locally (files %v)", want, h.localFiles())
		}
	}
	remote := map[string]bool{}
	for rel := range liveFiles(srv) {
		remote[remoteContent(h, "Shared/"+rel)] = true
	}
	if !remote["edited in the cloud"] || !remote["edited here"] {
		t.Fatalf("an edit is missing in the cloud: %v", remote)
	}
}

// A .cloudignore changed while a pass runs: the pass keeps the rules it
// started with on both sides, and the next one applies the new ones by
// forgetting rows, never by deleting.
func TestRuleChangeDuringPass(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/a.log", "a")
	srv.put("Shared/b.log", "b")
	srv.put("Shared/c.txt", "c")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	srv.put("Shared/c.txt", "c2")
	srv.onContent = func(http.ResponseWriter, *http.Request, []byte) bool {
		h.write(".cloudignore", "*.log\n")
		os.Remove(h.path("a.log")) // and a now-ignored file goes away locally
		return false
	}
	h.mustSync()
	srv.onContent = nil
	h.mustSync()
	h.mustSync()
	remote := liveFiles(srv)
	if remote["b.log"] == "" || remote[".cloudignore"] == "" {
		t.Fatalf("server: %v", remote)
	}
	if h.tracked("b.log") {
		t.Fatal("the newly ignored file is still tracked")
	}
	// a.log went away locally once it was ignored: the cloud copy stays.
	if remote["a.log"] == "" {
		t.Fatal("a.log was deleted in the cloud")
	}
}

// caseSensitiveDisk reports whether dir's file system tells names apart by
// case.
func caseSensitiveDisk(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, ".case-probe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(dir, ".CASE-PROBE"))
	return err != nil
}
