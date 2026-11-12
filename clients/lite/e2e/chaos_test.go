//go:build e2e

package e2e

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// Chaos tests against a real server: processes that die mid-transfer, a
// server that crashes, drives that go away, lost state, names the two
// sides spell differently. None of it may cost a file.

// restart is the client process starting again: the state database is
// opened from disk and a new engine is made.
func (d *device) restart(t *testing.T) {
	t.Helper()
	d.db.Close()
	db, err := state.Open(d.home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d.db = db
	d.engine = sync.New(d.cfg, db, log.New(testWriter{t, d.name}, "", 0))
}

// throttle caps the device's transfers (KiB/s, 0 is unlimited).
func (d *device) throttle(t *testing.T, up, down int64) {
	t.Helper()
	if err := d.cfg.Update(func(c *config.Config) error {
		c.UploadKiBps, c.DownloadKiBps = up, down
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// syncUntil runs a pass in the background and kills it (cancels it, as
// a dying process would stop) once stop reports true.
func (d *device) syncUntil(t *testing.T, stop func() bool) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.engine.SyncOnce(ctx) }()
	deadline := time.Now().Add(time.Minute)
	for !stop() {
		select {
		case err := <-done:
			t.Fatalf("the pass ended before the kill: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("kill condition never reached")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	return <-done
}

func newChaosServer(t *testing.T) (*server, *api.Client) {
	s := startServer(t)
	rest := api.New(s.url, s.deviceLogin(t, "rest").Token)
	if _, err := rest.EnsureFolderPath(context.Background(), api.RootAlias, remoteFolder); err != nil {
		t.Fatal(err)
	}
	return s, rest
}

func TestKillMidUploadAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "uploader", config.Both, nil)
	d.sync(t)
	big := randomBytes(t, 20<<20) // resumable: three parts
	os.WriteFile(d.path("big.bin"), big, 0o644)
	d.throttle(t, 4096, 0)
	var sessionID string
	d.syncUntil(t, func() bool {
		pending, ok, _ := d.db.PendingUpload(d.pair.ID, "big.bin")
		if !ok {
			return false
		}
		sessionID = pending.ID
		var session struct {
			ReceivedParts []int `json:"receivedParts"`
		}
		return mustDoErr(rest, "GET", "/api/uploads/"+pending.ID, nil, &session) == nil && len(session.ReceivedParts) > 0
	})
	if node, ok := remoteNode(t, rest, "big.bin"); ok && node.Etag != sha(big) {
		t.Fatalf("a truncated file reached the server: %d bytes", node.Size)
	}
	d.throttle(t, 0, 0)
	d.restart(t)
	d.sync(t)
	node, ok := remoteNode(t, rest, "big.bin")
	if !ok || node.Etag != sha(big) || node.Size != int64(len(big)) {
		t.Fatalf("after the resume: %+v %v", node, ok)
	}
	if _, pending, _ := d.db.PendingUpload(d.pair.ID, "big.bin"); pending {
		t.Fatal("the finished upload is still recorded as pending")
	}
	// The session was continued, not started over: it is spent now.
	if err := mustDoErr(rest, "GET", "/api/uploads/"+sessionID, nil, nil); !api.HasStatus(err, 404) {
		t.Fatalf("the interrupted session was not the one completed: %v", err)
	}
	assertConverged(t, s, rest, d)
}

func TestKillMidDownloadAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "downloader", config.Both, nil)
	uploadREST(t, rest, "", "movie.bin", "old version")
	d.sync(t)
	big := randomBytes(t, 12<<20)
	tmp := filepath.Join(t.TempDir(), "movie.bin")
	os.WriteFile(tmp, big, 0o644)
	parent, _ := remoteNode(t, rest, "")
	if _, err := rest.UploadFile(context.Background(), tmp, api.UploadOptions{ParentID: parent.ID, Name: "movie.bin", Conflict: "replace"}); err != nil {
		t.Fatal(err)
	}
	d.throttle(t, 0, 2048)
	part := api.PartPath(d.path("movie.bin"), sha(big))
	d.syncUntil(t, func() bool {
		info, err := os.Stat(part)
		return err == nil && info.Size() > 1<<20
	})
	if d.read(t, "movie.bin") != "old version" {
		t.Fatal("a killed download replaced the original")
	}
	d.throttle(t, 0, 0)
	d.restart(t)
	d.sync(t)
	if got, _ := api.HashFile(d.path("movie.bin")); got != sha(big) {
		t.Fatal("the resumed download is wrong")
	}
	if _, err := os.Stat(part); err == nil {
		t.Fatal("the part file was left behind")
	}
	assertConverged(t, s, rest, d)
}

// The server crashes in the middle of a pass (both directions in
// flight) and comes back.
func TestServerCrashMidPassAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "survivor", config.Both, nil)
	d.write(t, "keep.txt", "keep")
	d.sync(t)
	big := randomBytes(t, 10<<20)
	tmp := filepath.Join(t.TempDir(), "from-cloud.bin")
	os.WriteFile(tmp, big, 0o644)
	parent, _ := remoteNode(t, rest, "")
	if _, err := rest.UploadFile(context.Background(), tmp, api.UploadOptions{ParentID: parent.ID, Name: "from-cloud.bin"}); err != nil {
		t.Fatal(err)
	}
	up := randomBytes(t, 10<<20)
	os.WriteFile(d.path("from-device.bin"), up, 0o644)
	d.throttle(t, 2048, 2048)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.engine.SyncOnce(ctx) }()
	time.Sleep(1500 * time.Millisecond)
	s.stop()
	if err := <-done; err == nil {
		t.Fatal("a pass against a crashed server reported success")
	}
	if d.exists("from-cloud.bin") || d.read(t, "keep.txt") != "keep" {
		t.Fatal("local files changed by a failed pass")
	}
	s.spawn(t)
	d.throttle(t, 0, 0)
	d.sync(t)
	d.sync(t)
	remote := assertConverged(t, s, rest, d)
	if remote["from-device.bin"] != sha(up) || remote["from-cloud.bin"] != sha(big) || remote["keep.txt"] == "" {
		t.Fatalf("server after the crash: %v", remote)
	}
}

// The synced folder's drive goes away and comes back.
func TestDriveDisappearsAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "usb", config.Both, nil)
	for _, name := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		d.write(t, name, "content of "+name)
	}
	d.sync(t)
	before := s.remoteSnapshot(t, rest)
	away := filepath.Join(t.TempDir(), "unplugged")
	if err := os.Rename(d.dir, away); err != nil {
		t.Fatal(err)
	}
	if err := d.engine.SyncOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected the folder to be unavailable, got %v", err)
	}
	os.Mkdir(d.dir, 0o755) // the empty mount point
	if err := d.engine.SyncOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "unmounted or replaced") {
		t.Fatalf("expected the marker error, got %v", err)
	}
	if out := diff(before, s.remoteSnapshot(t, rest)); out != "" {
		t.Fatalf("the server changed while the drive was away:\n%s", out)
	}
	os.Remove(d.dir)
	os.Rename(away, d.dir)
	d.sync(t)
	if out := diff(before, assertConverged(t, s, rest, d)); out != "" {
		t.Fatalf("changed after the drive came back:\n%s", out)
	}
}

// Lost state, a re-added pair and signing in again all merge like a
// first sync: nothing is deleted on either side.
func TestStateLossAndReaddAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "forgetful", config.Both, nil)
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "sub/d.txt"} {
		d.write(t, name, "content of "+name)
	}
	d.sync(t)
	before := s.remoteSnapshot(t, rest)

	// The state database is lost while a file is deleted locally.
	os.Remove(d.path("a.txt"))
	d.db.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(filepath.Join(d.home, "state.db"+suffix))
	}
	d.restart(t)
	d.sync(t)
	if out := diff(before, assertConverged(t, s, rest, d)); out != "" {
		t.Fatalf("lost state changed the server:\n%s", out)
	}

	// The pair is removed and the same folder added again.
	if err := d.engine.RemovePair(d.pair.ID); err != nil {
		t.Fatal(err)
	}
	os.Remove(d.path("b.txt"))
	pair, err := config.NewPair(d.dir, "/"+remoteFolder, config.Both, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.engine.AddPair(pair); err != nil {
		t.Fatal(err)
	}
	d.pair = pair
	d.sync(t)
	if out := diff(before, assertConverged(t, s, rest, d)); out != "" {
		t.Fatalf("re-adding the pair changed the server:\n%s", out)
	}

	// Signing out and in again.
	if err := d.engine.SignOut(); err != nil {
		t.Fatal(err)
	}
	os.Remove(d.path("c.txt"))
	if err := d.engine.SignIn(s.url, s.deviceLogin(t, "forgetful-again")); err != nil {
		t.Fatal(err)
	}
	d.sync(t)
	if out := diff(before, assertConverged(t, s, rest, d)); out != "" {
		t.Fatalf("signing in again changed the server:\n%s", out)
	}
}

// Names the two sides spell differently: case against the
// case-insensitive server, decomposed Unicode from a Mac.
func TestNameSpellingsAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "speller", config.Both, nil)
	d.write(t, "notes.txt", "notes")
	d.write(t, "Report.txt", "report")
	d.sync(t)

	if err := os.Rename(d.path("notes.txt"), d.path("Notes.txt")); err != nil {
		t.Fatal(err)
	}
	d.sync(t)
	d.sync(t)
	found := false
	for rel, etag := range s.remoteSnapshot(t, rest) {
		if strings.EqualFold(rel, "notes.txt") && etag == sha([]byte("notes")) {
			found = true
		}
	}
	if !found || d.read(t, "Notes.txt") != "notes" {
		t.Fatal("a local case-only rename lost the file")
	}

	node, _ := remoteNode(t, rest, "Report.txt")
	if _, err := rest.Move(context.Background(), node.ID, "", "REPORT.txt"); err == nil {
		d.sync(t)
		if d.read(t, "REPORT.txt") != "report" {
			t.Fatal("a remote case-only rename was not followed")
		}
	}

	// A decomposed name in the cloud and the composed one locally.
	uploadREST(t, rest, "", norm.NFD.String("Café.txt"), "same bytes")
	d.write(t, norm.NFC.String("Café.txt"), "same bytes")
	uploadREST(t, rest, "", norm.NFD.String("Zoë.txt"), "cloud version")
	d.write(t, norm.NFC.String("Zoë.txt"), "local version")
	d.sync(t)
	d.sync(t)
	remote := s.remoteSnapshot(t, rest)
	contents := map[string]int{}
	for _, etag := range remote {
		contents[etag]++
	}
	if contents[sha([]byte("same bytes"))] != 1 {
		t.Fatalf("equal files under two spellings were duplicated: %v", remote)
	}
	if contents[sha([]byte("cloud version"))] != 1 || contents[sha([]byte("local version"))] != 1 {
		t.Fatalf("a version was lost: %v", remote)
	}
	if matches, _ := filepath.Glob(d.path("Caf* (conflict*")); len(matches) != 0 {
		t.Fatalf("equal files made a conflict copy: %v", matches)
	}
}

// Moving a synced file into an ignored folder keeps the cloud copy.
func TestMoveIntoIgnoredFolderAgainstRealServer(t *testing.T) {
	s, rest := newChaosServer(t)
	d := newDevice(t, s, "builder", config.Both, []string{"build/"})
	d.write(t, "report.pdf", "the report")
	d.write(t, "src/main.go", "package main")
	d.sync(t)
	before := s.remoteSnapshot(t, rest)

	os.MkdirAll(d.path("build"), 0o755)
	os.Rename(d.path("report.pdf"), d.path("build/report.pdf"))
	d.sync(t)
	d.sync(t)
	if out := diff(before, s.remoteSnapshot(t, rest)); out != "" {
		t.Fatalf("moving into an ignored folder changed the cloud:\n%s", out)
	}
	report := d.engine.Status().PairState[d.pair.ID]
	moved := false
	for _, item := range report.NotSynced {
		moved = moved || item.Path == "report.pdf" && item.Reason == sync.ReasonMovedIgnored
	}
	if !moved || d.exists("report.pdf") {
		t.Fatalf("not reported, or brought back: %+v", report.NotSynced)
	}

	// A plain delete still deletes.
	os.Remove(d.path("src/main.go"))
	d.sync(t)
	if _, ok := remoteNode(t, rest, "src/main.go"); ok {
		t.Fatal("a real delete did not sync")
	}
	var confirm *sync.NeedsConfirmationError
	if err := d.engine.SyncOnce(context.Background()); errors.As(err, &confirm) {
		t.Fatalf("unexpected confirmation: %v", err)
	}
}
