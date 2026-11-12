//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

const remoteFolder = "Shared"

// device is one installation of the lite client with its own config,
// state database and local folder.
type device struct {
	name   string
	dir    string // the synced folder
	cfg    *config.Store
	db     *state.DB
	engine *sync.Engine
	pair   config.Pair
}

func newDevice(t *testing.T, s *server, name, direction string, ignore []string) *device {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pair, err := config.NewPair(dir, "/"+remoteFolder, direction, ignore, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) error {
		c.DeviceName = name
		return c.AddPair(pair)
	}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(testWriter{t, name}, "", 0)
	d := &device{name: name, dir: dir, cfg: cfg, db: db, engine: sync.New(cfg, db, logger), pair: pair}
	if err := d.engine.SignIn(s.url, s.deviceLogin(t, name)); err != nil {
		t.Fatal(err)
	}
	return d
}

type testWriter struct {
	t    *testing.T
	name string
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("[%s] %s", w.name, strings.TrimSpace(string(p)))
	return len(p), nil
}

func (d *device) sync(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := d.engine.SyncOnce(ctx); err != nil {
		t.Fatalf("%s sync: %v", d.name, err)
	}
}

func (d *device) path(rel string) string { return filepath.Join(d.dir, filepath.FromSlash(rel)) }

func (d *device) write(t *testing.T, rel, content string) {
	t.Helper()
	full := d.path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (d *device) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(d.path(rel))
	if err != nil {
		t.Fatalf("%s: %v", d.name, err)
	}
	return string(data)
}

func (d *device) exists(rel string) bool {
	_, err := os.Lstat(d.path(rel))
	return err == nil
}

// snapshot maps every non-ignored path to its content hash ("/" for folders).
func localSnapshot(t *testing.T, dir string, ignore *sync.Matcher) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
		if err != nil || full == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, full)
		rel = filepath.ToSlash(rel)
		if ignore.Match(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			out[rel] = "/"
			return nil
		}
		hash, err := api.HashFile(full)
		if err != nil {
			t.Fatal(err)
		}
		out[rel] = hash
		return nil
	})
	return out
}

func (s *server) remoteSnapshot(t *testing.T, client *api.Client) map[string]string {
	t.Helper()
	tree, err := client.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	prefix := remoteFolder + "/"
	for _, node := range tree.Nodes {
		if !strings.HasPrefix(node.Path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(node.Path, prefix)
		if node.IsFolder() {
			out[rel] = "/"
		} else {
			out[rel] = node.Etag
		}
	}
	return out
}

func remoteNode(t *testing.T, client *api.Client, rel string) (api.Node, bool) {
	t.Helper()
	tree, err := client.Tree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	full := remoteFolder
	if rel != "" {
		full += "/" + rel
	}
	for _, node := range tree.Nodes {
		if node.Path == full {
			return node, true
		}
	}
	return api.Node{}, false
}

func diff(a, b map[string]string) string {
	var lines []string
	for k, v := range a {
		if b[k] != v {
			lines = append(lines, "  "+k+": "+short(v)+" vs "+short(b[k]))
		}
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			lines = append(lines, "  "+k+": (missing) vs "+short(v))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func short(v string) string {
	if len(v) > 8 {
		return v[:8]
	}
	if v == "" {
		return "(missing)"
	}
	return v
}

// assertConverged checks every device folder equals the server folder.
func assertConverged(t *testing.T, s *server, rest *api.Client, devices ...*device) map[string]string {
	t.Helper()
	remote := s.remoteSnapshot(t, rest)
	for _, d := range devices {
		local := localSnapshot(t, d.dir, sync.MatcherFor(d.pair))
		if out := diff(local, remote); out != "" {
			t.Fatalf("%s differs from the server (local vs remote):\n%s", d.name, out)
		}
	}
	return remote
}

// uploadREST stands in for another client (the web app) changing files.
func uploadREST(t *testing.T, rest *api.Client, parentRel, name, content string) api.Node {
	t.Helper()
	parent, ok := remoteNode(t, rest, parentRel)
	if !ok {
		t.Fatalf("remote folder %q missing", parentRel)
	}
	tmp := filepath.Join(t.TempDir(), name)
	os.WriteFile(tmp, []byte(content), 0o644)
	node, err := rest.UploadFile(context.Background(), tmp, api.UploadOptions{ParentID: parent.ID, Name: name, Conflict: "replace"})
	if err != nil {
		t.Fatalf("REST upload %s: %v", name, err)
	}
	return *node
}

func randomBytes(t *testing.T, n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestLiteClientAgainstRealServer(t *testing.T) {
	s := startServer(t)
	restToken := s.deviceLogin(t, "rest")
	rest := api.New(s.url, restToken.Token)
	ctx := context.Background()

	// The server folder already has content before any device syncs.
	if _, err := rest.EnsureFolderPath(ctx, api.RootAlias, remoteFolder); err != nil {
		t.Fatal(err)
	}
	uploadREST(t, rest, "", "remote-first.txt", "was on the server first")

	a := newDevice(t, s, "device-a", config.Both, []string{"*.log"})
	b := newDevice(t, s, "device-b", config.Both, nil)

	t.Run("initial sync of both sides", func(t *testing.T) {
		a.write(t, "hello.txt", "hello from A")
		a.write(t, "docs/a.txt", "doc a")
		a.write(t, "docs/deep/b.txt", "doc b")
		a.write(t, "same.txt", "identical on both devices")
		a.write(t, "conflict.txt", "base")
		os.MkdirAll(a.path("empty"), 0o755)
		a.write(t, "debug.log", "ignored by A's pattern")
		a.write(t, ".DS_Store", "ignored by default")
		a.write(t, "docs/~$lock.docx", "ignored by default")
		b.write(t, "same.txt", "identical on both devices")

		a.sync(t)
		b.sync(t)
		a.sync(t)
		remote := assertConverged(t, s, rest, a, b)
		for _, ignored := range []string{"debug.log", ".DS_Store", "docs/~$lock.docx"} {
			if _, ok := remote[ignored]; ok {
				t.Errorf("ignored %s reached the server", ignored)
			}
		}
		if b.read(t, "remote-first.txt") != "was on the server first" {
			t.Error("pre-existing remote file not downloaded")
		}
		if matches, _ := filepath.Glob(b.path("same (conflict*")); len(matches) > 0 {
			t.Errorf("identical files on both sides made a conflict copy: %v", matches)
		}
	})

	t.Run("edits, renames and moves on one device", func(t *testing.T) {
		before, _ := remoteNode(t, rest, "docs/a.txt")
		b.write(t, "hello.txt", "hello edited on B")
		os.Rename(b.path("docs/a.txt"), b.path("docs/a-renamed.txt"))
		os.Rename(b.path("docs/deep/b.txt"), b.path("moved-b.txt"))
		b.write(t, "newdir/inner/file.txt", "nested new")
		b.sync(t)
		a.sync(t)
		assertConverged(t, s, rest, a, b)
		after, ok := remoteNode(t, rest, "docs/a-renamed.txt")
		if !ok || after.ID != before.ID {
			t.Errorf("local rename was not sent as a move (id %s → %s)", before.ID, after.ID)
		}
		if a.read(t, "hello.txt") != "hello edited on B" {
			t.Error("edit did not reach A")
		}
	})

	t.Run("changes made through the REST API", func(t *testing.T) {
		uploadREST(t, rest, "", "api.txt", "made by the web app")
		hello, _ := remoteNode(t, rest, "hello.txt")
		if _, err := rest.Move(ctx, hello.ID, "", "hello-renamed.txt"); err != nil {
			t.Fatal(err)
		}
		docs, _ := remoteNode(t, rest, "docs")
		if _, err := rest.Move(ctx, docs.ID, "", "papers"); err != nil {
			t.Fatal(err)
		}
		same, _ := remoteNode(t, rest, "same.txt")
		if err := rest.Delete(ctx, same.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := rest.CreateFolder(ctx, mustNode(t, rest, "").ID, "from-web"); err != nil {
			t.Fatal(err)
		}
		a.sync(t)
		b.sync(t)
		assertConverged(t, s, rest, a, b)
		for _, d := range []*device{a, b} {
			if !d.exists("papers/a-renamed.txt") || d.exists("docs") || d.exists("same.txt") || !d.exists("from-web") {
				t.Errorf("%s did not follow the remote rename/delete", d.name)
			}
			if d.read(t, "hello-renamed.txt") != "hello edited on B" {
				t.Errorf("%s: renamed file content wrong", d.name)
			}
		}
	})

	t.Run("large file uses the resumable upload", func(t *testing.T) {
		big := randomBytes(t, 9<<20+12345)
		os.WriteFile(a.path("big.bin"), big, 0o644)
		a.sync(t)
		b.sync(t)
		assertConverged(t, s, rest, a, b)
		if node, _ := remoteNode(t, rest, "big.bin"); node.Etag != sha(big) || node.Size != int64(len(big)) {
			t.Errorf("big.bin on the server: size %d etag %s", node.Size, node.Etag)
		}
	})

	t.Run("an interrupted large upload resumes", func(t *testing.T) {
		data := randomBytes(t, 17<<20)
		os.WriteFile(a.path("resume.bin"), data, 0o644)
		// Start the upload by hand and send only the first part, as if the
		// client had been killed midway; then record it like the engine does.
		root := mustNode(t, rest, "")
		var session struct {
			ID       string `json:"id"`
			PartSize int64  `json:"partSize"`
		}
		devClient, _ := a.engine.Client()
		mustDo(t, devClient, "POST", "/api/uploads", map[string]any{
			"parentId": root.ID, "name": "resume.bin", "size": len(data), "sha256": sha(data),
		}, &session)
		putPart(t, devClient, session.ID, 0, data[:session.PartSize])
		a.db.SetPendingUpload(a.pair.ID, "resume.bin", state.Upload{ID: session.ID, SHA256: sha(data), Size: int64(len(data))})

		a.sync(t)
		if node, _ := remoteNode(t, rest, "resume.bin"); node.Etag != sha(data) {
			t.Fatalf("resumed upload has etag %s", node.Etag)
		}
		if err := mustDoErr(devClient, "GET", "/api/uploads/"+session.ID, nil, nil); !api.HasStatus(err, 404) {
			t.Errorf("the recorded session was not the one completed (still open: %v)", err)
		}
		if _, pending, _ := a.db.PendingUpload(a.pair.ID, "resume.bin"); pending {
			t.Error("pending upload not cleared")
		}
		b.sync(t)
		assertConverged(t, s, rest, a, b)
	})

	t.Run("conflict on both sides keeps both versions", func(t *testing.T) {
		a.write(t, "conflict.txt", "A's version")
		b.write(t, "conflict.txt", "B's version!")
		a.sync(t)
		b.sync(t)
		a.sync(t)
		assertConverged(t, s, rest, a, b)
		for _, d := range []*device{a, b} {
			if got := d.read(t, "conflict.txt"); got != "A's version" {
				t.Errorf("%s conflict.txt = %q", d.name, got)
			}
			copies, _ := filepath.Glob(d.path("conflict (conflict device-b *).txt"))
			if len(copies) != 1 {
				t.Fatalf("%s: conflict copies %v", d.name, copies)
			}
			data, _ := os.ReadFile(copies[0])
			if string(data) != "B's version!" {
				t.Errorf("%s: conflict copy holds %q", d.name, data)
			}
		}
	})

	t.Run("deletes propagate", func(t *testing.T) {
		os.RemoveAll(a.path("newdir"))
		os.Remove(a.path("moved-b.txt"))
		a.sync(t)
		b.sync(t)
		remote := assertConverged(t, s, rest, a, b)
		if _, ok := remote["newdir"]; ok || b.exists("newdir") || b.exists("moved-b.txt") {
			t.Error("deleted folder or file survived")
		}
	})

	t.Run("an edit wins over a delete", func(t *testing.T) {
		// Local delete + remote edit → downloaded again.
		os.Remove(a.path("api.txt"))
		uploadREST(t, rest, "", "api.txt", "edited on the web meanwhile")
		// Remote delete + local edit → uploaded again.
		renamed, _ := remoteNode(t, rest, "papers/a-renamed.txt")
		if err := rest.Delete(ctx, renamed.ID); err != nil {
			t.Fatal(err)
		}
		b.write(t, "papers/a-renamed.txt", "B kept working on it")
		a.sync(t)
		b.sync(t)
		a.sync(t)
		assertConverged(t, s, rest, a, b)
		if a.read(t, "api.txt") != "edited on the web meanwhile" {
			t.Error("remote edit lost to a local delete")
		}
		if a.read(t, "papers/a-renamed.txt") != "B kept working on it" {
			t.Error("local edit lost to a remote delete")
		}
	})

	t.Run("download-only pair via the CLI", func(t *testing.T) {
		bin := buildCLI(t)
		home, dir := t.TempDir(), t.TempDir()
		cliLogin(t, s, bin, home)
		runCLI(t, bin, home, "pair", "add", dir, "/"+remoteFolder, "--direction", "down")
		runCLI(t, bin, home, "sync", "--once")
		c := &device{name: "cli", dir: dir, pair: config.Pair{}}
		assertConverged(t, s, rest, c)

		// Local changes stay local.
		c.write(t, "local-only.txt", "never uploaded")
		c.write(t, "hello-renamed.txt", "local scribble")
		os.Remove(c.path("remote-first.txt"))
		runCLI(t, bin, home, "sync", "--once")
		remote := s.remoteSnapshot(t, rest)
		if _, ok := remote["local-only.txt"]; ok {
			t.Error("download-only pair uploaded a file")
		}
		if node, _ := remoteNode(t, rest, "hello-renamed.txt"); node.Etag != sha([]byte("hello edited on B")) {
			t.Error("download-only pair overwrote a remote file")
		}
		if c.read(t, "remote-first.txt") != "was on the server first" {
			t.Error("locally deleted file was not restored from the server")
		}
		// Remote changes still arrive.
		uploadREST(t, rest, "", "later.txt", "arrives later")
		gone, _ := remoteNode(t, rest, "big.bin")
		rest.Delete(ctx, gone.ID)
		runCLI(t, bin, home, "sync", "--once")
		if c.read(t, "later.txt") != "arrives later" || c.exists("big.bin") {
			t.Error("download-only pair missed remote changes")
		}
		status := runCLI(t, bin, home, "status")
		if !strings.Contains(status, "Account: neo") || !strings.Contains(status, "down") {
			t.Errorf("status output:\n%s", status)
		}
		a.sync(t)
		b.sync(t)
	})

	t.Run("run loop reacts to local and remote changes", func(t *testing.T) {
		b.cfg.Update(func(c *config.Config) error { c.PollSeconds = 1; return nil })
		runCtx, stop := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- b.engine.Run(runCtx) }()
		defer func() {
			stop()
			<-done
		}()
		time.Sleep(500 * time.Millisecond) // let the first run and watches start

		b.write(t, "watched/new.txt", "seen by the watcher")
		waitFor(t, "watched file on the server", func() bool {
			_, ok := remoteNode(t, rest, "watched/new.txt")
			return ok
		})
		uploadREST(t, rest, "", "pushed.txt", "seen by the journal poll")
		waitFor(t, "remote file on device B", func() bool { return b.exists("pushed.txt") })
	})

	t.Run("everything converges", func(t *testing.T) {
		a.sync(t)
		b.sync(t)
		remote := assertConverged(t, s, rest, a, b)
		t.Logf("server folder holds %d entries", len(remote))
	})
}

func mustNode(t *testing.T, rest *api.Client, rel string) api.Node {
	t.Helper()
	node, ok := remoteNode(t, rest, rel)
	if !ok {
		t.Fatalf("remote %q missing", rel)
	}
	return node
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "neocloud-lite")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/neocloud-lite")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runCLI(t *testing.T, bin, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "NEOCLOUD_LITE_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("neocloud-lite %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// cliLogin runs `login` and approves the code it prints, like a user would.
func cliLogin(t *testing.T, s *server, bin, home string) {
	t.Helper()
	cmd := exec.Command(bin, "login", "--server", s.url, "--no-browser")
	cmd.Env = append(os.Environ(), "NEOCLOUD_LITE_HOME="+home)
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	codePattern := regexp.MustCompile(`Code: ([A-Z0-9]{4}-[A-Z0-9]{4})`)
	scanner := bufio.NewScanner(stdout)
	var output strings.Builder
	approved := false
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line + "\n")
		if m := codePattern.FindStringSubmatch(line); m != nil && !approved {
			s.approve(t, m[1])
			approved = true
		}
	}
	io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil || !approved || !strings.Contains(output.String(), "Signed in as neo") {
		t.Fatalf("CLI login: %v\n%s%s", err, output.String(), stderr.String())
	}
}

// TestDataSafetyAgainstRealServer: an emptied local folder never empties
// the server, and a folder without its marker is not synced at all.
func TestDataSafetyAgainstRealServer(t *testing.T) {
	s := startServer(t)
	rest := api.New(s.url, s.deviceLogin(t, "rest").Token)
	if _, err := rest.EnsureFolderPath(context.Background(), api.RootAlias, remoteFolder); err != nil {
		t.Fatal(err)
	}
	d := newDevice(t, s, "guarded", config.Both, nil)
	for i := 0; i < 30; i++ {
		d.write(t, fmt.Sprintf("docs/file-%02d.txt", i), fmt.Sprintf("content %d", i))
	}
	d.sync(t)
	before := s.remoteSnapshot(t, rest)
	if len(before) != 31 {
		t.Fatalf("server holds %d entries", len(before))
	}

	t.Run("emptying the local folder waits for a confirmation", func(t *testing.T) {
		os.RemoveAll(d.path("docs"))
		err := d.engine.SyncOnce(context.Background())
		var confirm *sync.NeedsConfirmationError
		if !errors.As(err, &confirm) || confirm.Pending.RemoteDeletes != 30 {
			t.Fatalf("expected a confirmation request, got %v", err)
		}
		if out := diff(before, s.remoteSnapshot(t, rest)); out != "" {
			t.Fatalf("the server changed:\n%s", out)
		}
		report := d.engine.Status().PairState[d.pair.ID]
		if report.NeedsConfirmation == nil || report.NeedsConfirmation.RemoteDeletes != 30 {
			t.Fatalf("status: %+v", report)
		}
		if _, err := d.engine.ConfirmDeletes(d.pair.ID); err != nil {
			t.Fatal(err)
		}
		d.sync(t)
		if remote := s.remoteSnapshot(t, rest); len(remote) != 0 {
			t.Fatalf("confirmed deletes did not run: %v", remote)
		}
	})

	t.Run("a missing root marker stops the pair", func(t *testing.T) {
		d.write(t, "precious.txt", "keep me")
		d.sync(t)
		os.RemoveAll(d.path(".neocloud"))
		os.Remove(d.path("precious.txt"))
		err := d.engine.SyncOnce(context.Background())
		if err == nil || !strings.Contains(err.Error(), "folder looks unmounted or replaced") {
			t.Fatalf("expected the marker error, got %v", err)
		}
		if _, ok := remoteNode(t, rest, "precious.txt"); !ok {
			t.Fatal("precious.txt was deleted on the server")
		}
	})
}
