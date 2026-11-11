package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// harness is one lite client installation against a fakeServer, syncing
// the server folder "Shared" with a temporary local folder.
type harness struct {
	t      *testing.T
	srv    *fakeServer
	root   string
	db     *state.DB
	engine *Engine
	pair   config.Pair
}

func newHarness(t *testing.T, srv *fakeServer, direction string, ignore ...string) *harness {
	t.Helper()
	home, root := t.TempDir(), t.TempDir()
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pair, err := config.NewPair(root, "/Shared", direction, ignore)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.Server, c.Token, c.DeviceName = srv.srv.URL, "token", "dev"
		return c.AddPair(pair)
	}); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, srv: srv, root: root, db: db, engine: New(cfg, db, log.New(io.Discard, "", 0)), pair: pair}
}

func (h *harness) sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.engine.SyncOnce(ctx)
}

func (h *harness) mustSync() {
	h.t.Helper()
	if err := h.sync(); err != nil {
		h.t.Fatalf("sync: %v", err)
	}
}

func (h *harness) path(rel string) string { return filepath.Join(h.root, filepath.FromSlash(rel)) }

func (h *harness) write(rel, content string) {
	h.t.Helper()
	full := h.path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) read(rel string) string {
	h.t.Helper()
	data, err := os.ReadFile(h.path(rel))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *harness) exists(rel string) bool {
	_, err := os.Lstat(h.path(rel))
	return err == nil
}

// trashed lists the files in the local trash, relative to their day folder.
func (h *harness) trashed() map[string]string {
	out := map[string]string{}
	base := filepath.Join(h.root, metaDir, trashDir)
	filepath.WalkDir(base, func(full string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, full)
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		data, _ := os.ReadFile(full)
		out[parts[1]] = string(data)
		return nil
	})
	return out
}

func TestGuardThresholds(t *testing.T) {
	cases := []struct {
		n, tracked int
		want       bool
	}{
		{19, 19, false},   // below the minimum, even if it is everything
		{20, 39, true},    // more than half
		{20, 40, false},   // exactly half
		{501, 5000, true}, // above 500
		{500, 5000, false},
	}
	for _, c := range cases {
		if got := guardTrips(c.n, c.tracked); got != c.want {
			t.Errorf("guardTrips(%d, %d) = %v", c.n, c.tracked, got)
		}
	}
	if got := evaluateGuard(0, 3, 5, true); got == nil || got.Reason != GuardRemoteEmpty {
		t.Errorf("remote empty: %+v", got)
	}
	if got := evaluateGuard(0, 0, 5, true); got != nil {
		t.Errorf("remote empty without local deletes: %+v", got)
	}
	if got := evaluateGuard(25, 0, 30, false); got == nil || got.Reason != GuardMassDelete || got.RemoteDeletes != 25 {
		t.Errorf("mass delete: %+v", got)
	}
	approved := DeleteConfirmation{RemoteDeletes: 25, Reason: GuardMassDelete, Digest: "d1"}
	if !approved.covers(approved) {
		t.Error("an approval must cover the plan it was given for")
	}
	for _, other := range []DeleteConfirmation{
		{RemoteDeletes: 24, Reason: GuardMassDelete, Digest: "d1"},
		{RemoteDeletes: 25, Reason: GuardRemoteEmpty, Digest: "d1"},
		{RemoteDeletes: 25, Reason: GuardMassDelete, Digest: "d2"},
	} {
		if approved.covers(other) {
			t.Errorf("covers %+v", other)
		}
	}
	if (DeleteConfirmation{RemoteDeletes: 25, Reason: GuardMassDelete}).covers(DeleteConfirmation{RemoteDeletes: 25, Reason: GuardMassDelete}) {
		t.Error("an approval without a digest (older versions) covers nothing")
	}
	one := []step{{"a.txt", DeleteLocal}, {"b.txt", DeleteRemote}, {"c.txt", Upload}}
	two := []step{{"b.txt", DeleteRemote}, {"a.txt", DeleteLocal}}
	if deletionDigest(one) != deletionDigest(two) {
		t.Error("digest depends on order or non-deletions")
	}
	if deletionDigest(one) == deletionDigest([]step{{"a.txt", DeleteRemote}, {"b.txt", DeleteRemote}}) {
		t.Error("digest ignores the side")
	}
}

func TestMassDeleteGuardWaitsForConfirmation(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	for i := 0; i < 30; i++ {
		h.write(fmt.Sprintf("docs/f%02d.txt", i), fmt.Sprintf("file %d", i))
	}
	h.write("keep.txt", "keep")
	h.mustSync()
	if len(srv.files("Shared")) != 32 {
		t.Fatalf("server holds %d entries", len(srv.files("Shared")))
	}

	// Emptying the folder (an unmounted drive, a bad script) …
	os.RemoveAll(h.path("docs"))
	os.Remove(h.path("keep.txt"))
	h.write("new.txt", "made meanwhile")
	err := h.sync()
	var confirm *NeedsConfirmationError
	if !errors.As(err, &confirm) || confirm.Pending.RemoteDeletes != 31 || confirm.Pending.Reason != GuardMassDelete {
		t.Fatalf("expected a confirmation request, got %v", err)
	}
	// … deletes nothing, but everything else still syncs.
	remote := srv.files("Shared")
	if len(remote) != 33 || remote["new.txt"] == "" {
		t.Fatalf("server changed: %d entries", len(remote))
	}
	if st := h.engine.Status(); st.State != StateConfirm || st.PairState[h.pair.ID].NeedsConfirmation == nil {
		t.Fatalf("status %+v", st)
	}
	// Still waiting on the next run.
	if err := h.sync(); !errors.As(err, &confirm) {
		t.Fatalf("second run: %v", err)
	}

	pending, err := h.engine.ConfirmDeletes(h.pair.ID)
	if err != nil || pending.RemoteDeletes != 31 {
		t.Fatalf("confirm: %+v %v", pending, err)
	}
	h.mustSync()
	remote = srv.files("Shared")
	if len(remote) != 1 || remote["new.txt"] == "" {
		t.Fatalf("after confirming the server holds %v", remote)
	}
	if _, err := h.engine.ConfirmDeletes(h.pair.ID); err == nil {
		t.Error("nothing should be left to confirm")
	}
}

func TestConfirmationNeverCoversMoreDeletes(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	for i := 0; i < 40; i++ {
		h.write(fmt.Sprintf("f%02d.txt", i), fmt.Sprintf("file %d", i))
	}
	h.mustSync()
	for i := 0; i < 25; i++ {
		os.Remove(h.path(fmt.Sprintf("f%02d.txt", i)))
	}
	if err := h.sync(); err == nil {
		t.Fatal("expected a confirmation request")
	}
	h.engine.ConfirmDeletes(h.pair.ID)
	for i := 25; i < 40; i++ {
		os.Remove(h.path(fmt.Sprintf("f%02d.txt", i)))
	}
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.RemoteDeletes != 40 {
		t.Fatalf("a bigger plan must ask again, got %v", err)
	}
	if len(srv.files("Shared")) != 40 {
		t.Fatal("deleted beyond the confirmation")
	}
}

func TestRemoteSuddenlyEmptyIsNotPropagated(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/a.txt", "a")
	srv.put("Shared/b.txt", "b")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	srv.del("Shared/a.txt")
	srv.del("Shared/b.txt")
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.Reason != GuardRemoteEmpty || confirm.Pending.LocalDeletes != 2 {
		t.Fatalf("expected remoteEmpty, got %v", err)
	}
	if !h.exists("a.txt") || !h.exists("b.txt") {
		t.Fatal("local files deleted")
	}
	h.engine.ConfirmDeletes(h.pair.ID)
	h.mustSync()
	if h.exists("a.txt") || h.trashed()["a.txt"] != "a" {
		t.Fatal("confirmed delete did not go to the local trash")
	}
}

func TestReplacedRemoteFolderIsMergedWithoutDeletes(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/a.txt", "a")
	h := newHarness(t, srv, config.Both)
	h.write("local.txt", "local")
	h.mustSync()
	srv.del("Shared")
	srv.put("Shared/other.txt", "other")
	h.mustSync()
	if !h.exists("a.txt") || !h.exists("local.txt") || h.read("other.txt") != "other" {
		t.Fatal("local files were touched")
	}
	remote := srv.files("Shared")
	if remote["a.txt"] == "" || remote["local.txt"] == "" {
		t.Fatalf("local files were not merged into the new folder: %v", remote)
	}
}

func TestRootMarker(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	h.write("a.txt", "a")
	h.mustSync()
	data, err := os.ReadFile(h.path(".neocloud/pair-id"))
	if err != nil || strings.TrimSpace(string(data)) != h.pair.ID {
		t.Fatalf("marker: %q %v", data, err)
	}
	if _, ok := srv.files("Shared")[".neocloud"]; ok {
		t.Fatal(".neocloud reached the server")
	}

	// An empty folder in place of the real one (unmounted drive).
	os.RemoveAll(h.path(".neocloud"))
	os.Remove(h.path("a.txt"))
	err = h.sync()
	if err == nil || !strings.Contains(err.Error(), "folder looks unmounted or replaced") {
		t.Fatalf("expected the marker error, got %v", err)
	}
	if srv.files("Shared")["a.txt"] == "" {
		t.Fatal("a.txt deleted on the server")
	}
	// A marker of another pair counts as missing too.
	os.MkdirAll(h.path(".neocloud"), 0o755)
	os.WriteFile(h.path(".neocloud/pair-id"), []byte("someone-else\n"), 0o644)
	if err := h.sync(); err == nil {
		t.Fatal("foreign marker accepted")
	}
}

func TestRemoteDrivenDeletesAndOverwritesGoToLocalTrash(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/dir/gone.txt", "old content")
	srv.put("Shared/edit.txt", "v1")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	srv.del("Shared/dir")
	srv.put("Shared/edit.txt", "v2")
	h.mustSync()
	if h.exists("dir") || h.read("edit.txt") != "v2" {
		t.Fatal("remote changes not applied")
	}
	trash := h.trashed()
	if trash["dir/gone.txt"] != "old content" || trash["edit.txt"] != "v1" {
		t.Fatalf("trash holds %v", trash)
	}
	// Old days are pruned.
	old := filepath.Join(h.root, metaDir, trashDir, time.Now().AddDate(0, 0, -31).Format(trashDayFormat))
	recent := filepath.Join(h.root, metaDir, trashDir, time.Now().AddDate(0, 0, -29).Format(trashDayFormat))
	os.MkdirAll(old, 0o755)
	os.MkdirAll(recent, 0o755)
	pruneTrash(h.root, time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Error("31-day-old trash kept")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("29-day-old trash pruned")
	}
}

func TestMoveToTrashKeepsBothOnNameClash(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for i, content := range []string{"one", "two"} {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte(content), 0o644)
		if _, err := moveToTrash(root, "a.txt", now); err != nil {
			t.Fatal(i, err)
		}
	}
	day := filepath.Join(root, metaDir, trashDir, now.Format(trashDayFormat))
	first, _ := os.ReadFile(filepath.Join(day, "a.txt"))
	second, _ := os.ReadFile(filepath.Join(day, "a 2.txt"))
	if string(first) != "one" || string(second) != "two" {
		t.Fatalf("trash: %q %q", first, second)
	}
}

func TestBaseEtagMismatchBecomesConflict(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/doc.txt", "base")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	h.write("doc.txt", "local edit")
	// Someone else saves between our tree read and our upload.
	raced := false
	srv.beforeUpload = func(_, name string) {
		if name == "doc.txt" && !raced {
			raced = true
			srv.put("Shared/doc.txt", "remote edit")
		}
	}
	h.mustSync()
	if got := h.read("doc.txt"); got != "remote edit" {
		t.Fatalf("doc.txt = %q", got)
	}
	matches, _ := filepath.Glob(h.path("doc (conflict dev *).txt"))
	if len(matches) != 1 {
		t.Fatalf("conflict copies %v", matches)
	}
	if data, _ := os.ReadFile(matches[0]); string(data) != "local edit" {
		t.Fatalf("conflict copy holds %q", data)
	}
}

func TestUnicodeNormalization(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	nfd := norm.NFD.String("Café.txt")
	srv.put("Shared/"+norm.NFC.String("Résumé.txt"), "remote")
	h := newHarness(t, srv, config.Both)
	h.write(nfd, "decomposed on disk")
	// The remote name in NFD on disk is the same file, not a new one.
	h.write(norm.NFD.String("Résumé.txt"), "remote")
	h.mustSync()
	h.mustSync()
	remote := srv.files("Shared")
	if remote[norm.NFC.String("Café.txt")] == "" {
		t.Fatalf("NFD name not uploaded in NFC: %v", remote)
	}
	if len(remote) != 2 {
		t.Fatalf("duplicates on the server: %v", remote)
	}
	entries, _ := os.ReadDir(h.root)
	names := 0
	for _, entry := range entries {
		if entry.Name() != metaDir {
			names++
		}
	}
	if names != 2 {
		t.Fatalf("local folder holds %d names", names)
	}
}

func TestRemoteCaseCollisionIsReported(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/Report.txt", "upper")
	srv.putRaw("Shared", "report.txt", "lower")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	report := h.engine.Status().PairState[h.pair.ID]
	if len(report.NotSynced) != 1 || report.NotSynced[0].Path != "report.txt" || report.NotSynced[0].Reason != ReasonNameClash {
		t.Fatalf("not synced: %+v", report.NotSynced)
	}
	if !strings.Contains(report.NotSynced[0].Detail, "name clashes with Report.txt") {
		t.Errorf("detail %q", report.NotSynced[0].Detail)
	}
	if h.read("Report.txt") != "upper" {
		t.Fatal("the kept name was not synced")
	}
}

func TestClashes(t *testing.T) {
	set := func(keys ...string) map[string]bool {
		out := map[string]bool{}
		for _, key := range keys {
			out[key] = true
		}
		return out
	}
	got := clashes(set("A.txt", "a.txt", "new.TXT"), set("a.txt", "New.txt"), set("a.txt"), map[string][]string{"x": {"x", "x"}})
	want := map[string]string{
		"A.txt":   "name clashes with a.txt",
		"new.TXT": "name clashes with New.txt on the server",
		"New.txt": "name clashes with new.TXT on this computer",
	}
	if len(got) != len(want)+1 {
		t.Fatalf("clashes: %+v", got)
	}
	for _, item := range got {
		if w, ok := want[item.Path]; ok && !strings.HasSuffix(item.Detail, w) {
			t.Errorf("%s: %q", item.Path, item.Detail)
		}
	}
}

func TestWindowsNames(t *testing.T) {
	for name, bad := range map[string]bool{
		"CON": true, "con.txt": true, "Nul.tar.gz": true, "COM1": true, "lpt9.log": true, "COM0": false,
		"trailing.": true, "space ": true, "a:b": true, "q?": true, "console.txt": false, "normal.txt": false,
	} {
		if got := windowsNameProblem(name) != ""; got != bad {
			t.Errorf("%q: %v", name, got)
		}
	}
	if !windowsPathTooLong(strings.Repeat("a", 260)) || windowsPathTooLong(strings.Repeat("a", 259)) {
		t.Error("MAX_PATH")
	}

	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/CON.txt", "reserved")
	srv.put("Shared/ok.txt", "fine")
	srv.put("Shared/"+strings.Repeat("d", 120)+"/"+strings.Repeat("f", 150)+".txt", "long")
	windowsNames = true
	defer func() { windowsNames = false }()
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	if h.exists("CON.txt") || !h.exists("ok.txt") {
		t.Fatal("reserved name downloaded")
	}
	reasons := map[string]bool{}
	for _, item := range h.engine.Status().PairState[h.pair.ID].NotSynced {
		reasons[item.Reason] = true
	}
	if !reasons[ReasonInvalidName] || !reasons[ReasonPathTooLong] {
		t.Fatalf("reasons %v", reasons)
	}
}

func TestSymlinksAreReported(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	h.write("real.txt", "x")
	if err := os.Symlink(h.path("real.txt"), h.path("link.txt")); err != nil {
		t.Skip("no symlinks here")
	}
	h.mustSync()
	report := h.engine.Status().PairState[h.pair.ID]
	if len(report.NotSynced) != 1 || report.NotSynced[0].Reason != ReasonSymlink {
		t.Fatalf("not synced: %+v", report.NotSynced)
	}
	if _, ok := srv.files("Shared")["link.txt"]; ok {
		t.Fatal("symlink uploaded")
	}
	// A tracked file that becomes a link is not deleted on the server.
	os.Remove(h.path("real.txt"))
	os.Symlink("/etc/hosts", h.path("real.txt"))
	h.mustSync()
	if srv.files("Shared")["real.txt"] == "" {
		t.Fatal("file deleted on the server after turning into a link")
	}
}

func TestPairLock(t *testing.T) {
	dir := t.TempDir()
	release, err := lockPair(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockPair(dir, "p1"); err == nil {
		t.Fatal("second lock on the same pair")
	}
	other, err := lockPair(dir, "p2")
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	again, err := lockPair(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestRemoteChangesOutsideThePairAreSkipped(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/a.txt", "a")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	client, _ := h.engine.Client()
	ctx := context.Background()
	srv.put("Elsewhere/x.txt", "not ours")
	if changed, err := h.engine.remoteChanged(ctx, client, h.pair.ID); err != nil || changed {
		t.Fatalf("change outside the pair: %v %v", changed, err)
	}
	before, _ := h.db.Cursor(h.pair.ID)
	if before != srv.seq {
		t.Errorf("cursor not moved past foreign changes: %d vs %d", before, srv.seq)
	}
	srv.put("Shared/sub/new.txt", "ours")
	if changed, _ := h.engine.remoteChanged(ctx, client, h.pair.ID); !changed {
		t.Fatal("change inside the pair missed")
	}
	h.mustSync()
	srv.del("Shared/a.txt")
	if changed, _ := h.engine.remoteChanged(ctx, client, h.pair.ID); !changed {
		t.Fatal("trash inside the pair missed")
	}
}

func TestDownloadResumesFromPart(t *testing.T) {
	content := []byte(strings.Repeat("0123456789", 1000))
	etag := sha(content)
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Header().Set("ETag", `"`+etag+`"`)
		if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
			var start int
			fmt.Sscanf(spec, "%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[start:])
			return
		}
		w.Write(content)
	}))
	defer srv.Close()
	client := api.New(srv.URL, "t")
	dest := filepath.Join(t.TempDir(), "file.bin")

	// A part from an interrupted attempt is resumed, not restarted.
	os.WriteFile(api.PartPath(dest, etag), content[:4000], 0o644)
	os.WriteFile(dest+".ncpart", []byte("stale"), 0o644)
	if err := client.DownloadFile(context.Background(), "x", dest, etag, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Fatal("resumed content differs")
	}
	if len(ranges) != 1 || ranges[0] != "bytes=4000-" {
		t.Fatalf("requests: %q", ranges)
	}
	if _, err := os.Stat(dest + ".ncpart"); err == nil {
		t.Error("stale part left behind")
	}

	// A damaged prefix fails the hash check and is thrown away; the next
	// attempt starts over.
	ranges = nil
	os.Remove(dest)
	os.WriteFile(api.PartPath(dest, etag), []byte("garbage!!"), 0o644)
	if err := client.DownloadFile(context.Background(), "x", dest, etag, nil); err == nil {
		t.Fatal("damaged part accepted")
	}
	if _, err := os.Stat(api.PartPath(dest, etag)); err == nil {
		t.Fatal("damaged part kept")
	}
	if err := client.DownloadFile(context.Background(), "x", dest, etag, nil); err != nil {
		t.Fatal(err)
	}
	if ranges[len(ranges)-1] != "" {
		t.Errorf("fresh attempt sent a range: %q", ranges)
	}

	// A transient failure keeps the part.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		w.Write(content[:3000])
		panic(http.ErrAbortHandler)
	}))
	defer broken.Close()
	os.Remove(dest)
	if err := api.New(broken.URL, "t").DownloadFile(context.Background(), "x", dest, etag, nil); err == nil {
		t.Fatal("truncated download accepted")
	}
	if info, err := os.Stat(api.PartPath(dest, etag)); err != nil || info.Size() != 3000 {
		t.Fatalf("part after a dropped connection: %v %v", info, err)
	}
}

// A folder deleted on the server goes to the local trash as a whole,
// ignored files included: they are never uploaded, so the local copy is
// the only one. Only OS junk may be removed for good.
func TestRemoteFolderDeleteKeepsIgnoredFilesInTrash(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	srv.put("Shared/Proj/a.txt", "x")
	srv.put("Shared/Proj/sub/b.txt", "y")
	srv.put("Shared/Junk/c.txt", "z")
	srv.put("Shared/stays.txt", "s")
	h := newHarness(t, srv, config.Both, "*.env", "cache/")
	h.mustSync()
	h.write("Proj/secret.env", "API_KEY=dummy")
	h.write("Proj/~$doc.docx", "lock")
	h.write("Proj/sub/cache/blob", "cached")
	h.write("Proj/.DS_Store", "finder")
	h.write("Junk/Thumbs.db", "thumbs")
	srv.del("Shared/Proj")
	srv.del("Shared/Junk")
	h.mustSync()

	if h.exists("Proj") || h.exists("Junk") {
		t.Fatal("remotely deleted folders still exist")
	}
	trash := h.trashed()
	for rel, content := range map[string]string{
		"Proj/a.txt":          "x",
		"Proj/sub/b.txt":      "y",
		"Proj/secret.env":     "API_KEY=dummy",
		"Proj/~$doc.docx":     "lock",
		"Proj/sub/cache/blob": "cached",
		"Junk/c.txt":          "z",
	} {
		if trash[rel] != content {
			t.Errorf("trash[%s] = %q, want %q (trash %v)", rel, trash[rel], content, trash)
		}
	}
	if _, ok := trash["Junk/Thumbs.db"]; !ok {
		t.Error("a folder with real content keeps its junk too (moved, not removed)")
	}
	if entries, _ := h.db.Entries(h.pair.ID); len(entries) != 1 {
		t.Errorf("rows left: %v", entries)
	}
}

func TestRemoteFolderDeleteRemovesOnlyEmptyFoldersAndJunk(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared/Empty/inner")
	srv.mkdir("Shared/Kept")
	srv.put("Shared/stays.txt", "s")
	h := newHarness(t, srv, config.Down, "*.env")
	h.mustSync()
	h.write("Empty/.DS_Store", "finder")
	h.write("Empty/inner/desktop.ini", "explorer")
	h.write("Kept/mine.txt", "made locally, never uploaded")
	srv.del("Shared/Empty")
	srv.del("Shared/Kept")
	h.mustSync()
	if h.exists("Empty") {
		t.Fatal("folder holding only junk kept")
	}
	if len(h.trashed()) != 0 {
		t.Fatalf("junk-only folders need no trash: %v", h.trashed())
	}
	if h.read("Kept/mine.txt") != "made locally, never uploaded" {
		t.Fatal("untracked local content lost")
	}
}

// An approval is spent by the first pass that applies it, even when that
// pass fails elsewhere, and only covers the paths it was shown for.
func TestDeleteApprovalIsSpentOnce(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	for i := 0; i < 40; i++ {
		srv.put(fmt.Sprintf("Shared/f%02d.txt", i), fmt.Sprintf("file %d", i))
	}
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	wipe := func() {
		for i := 0; i < 30; i++ {
			srv.del(fmt.Sprintf("Shared/f%02d.txt", i))
		}
	}
	wipe()
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.LocalDeletes != 30 || confirm.Pending.Digest == "" {
		t.Fatalf("expected a confirmation request, got %v", err)
	}
	if _, err := h.engine.ConfirmDeletes(h.pair.ID); err != nil {
		t.Fatal(err)
	}
	// A file that fails on every pass.
	srv.failContent = "broken.txt"
	srv.put("Shared/broken.txt", "never arrives")
	if err := h.sync(); err == nil || errors.As(err, &confirm) {
		t.Fatalf("expected the download error, got %v", err)
	}
	if len(h.trashed()) != 30 {
		t.Fatalf("approved deletions not applied: %d in trash", len(h.trashed()))
	}
	if stored, _ := h.db.Meta(h.pair.ID, metaConfirm); stored != "" {
		t.Fatalf("approval kept after use: %s", stored)
	}

	// The same files come back and go again: a new wave asks again.
	for i := 0; i < 30; i++ {
		srv.put(fmt.Sprintf("Shared/f%02d.txt", i), fmt.Sprintf("file %d", i))
	}
	h.sync()
	if !h.exists("f00.txt") {
		t.Fatal("files not downloaded again")
	}
	wipe()
	h.sync()
	if !h.exists("f00.txt") {
		t.Fatal("a second wave of deletions ran on a spent approval")
	}
	if h.engine.Status().PairState[h.pair.ID].NeedsConfirmation == nil {
		t.Fatal("the second wave did not ask for a confirmation")
	}
}

// An approval given for one set of paths does not cover another set of
// the same size.
func TestDeleteApprovalIsBoundToPaths(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	for i := 0; i < 40; i++ {
		srv.put(fmt.Sprintf("Shared/f%02d.txt", i), fmt.Sprintf("file %d", i))
	}
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	for i := 0; i < 25; i++ {
		srv.del(fmt.Sprintf("Shared/f%02d.txt", i))
	}
	if err := h.sync(); err == nil {
		t.Fatal("expected a confirmation request")
	}
	h.engine.ConfirmDeletes(h.pair.ID)
	// Before the next pass, one held file comes back and another goes.
	srv.put("Shared/f00.txt", "file 0")
	srv.del("Shared/f30.txt")
	var confirm *NeedsConfirmationError
	if err := h.sync(); !errors.As(err, &confirm) || confirm.Pending.LocalDeletes != 25 {
		t.Fatalf("a different set must ask again, got %v", err)
	}
	if !h.exists("f30.txt") || !h.exists("f01.txt") {
		t.Fatal("deleted on an approval for other paths")
	}
}
