package imports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

const (
	owner    = int64(1)
	stranger = int64(2)
)

type fixture struct {
	t       *testing.T
	handler *Handler
	db      *dbx.DB
	files   *files.Handler
	mux     *http.ServeMux
	mu      sync.Mutex
	events  []string
}

func newFixture(t *testing.T, options Options) *fixture {
	t.Helper()
	ctx, base := context.Background(), t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(base, "test.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(base, "snapshots")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','x'),(2,'stranger','x')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db}
	f.files = files.New(files.Options{
		DB: db.W, Keys: keys, BlobDir: filepath.Join(base, "blobs"), UploadTmpDir: filepath.Join(base, "tmp"),
		PreviewDir: filepath.Join(base, "previews"), MaxUploadBytes: 1 << 30, DefaultQuotaBytes: 1 << 30, MaxNodes: 1000000, MaxCopyNodes: 100,
	})
	options.DB, options.Keys, options.Files = db.W, keys, f.files
	options.ResolvePrincipal = func(r *http.Request) (Principal, bool) {
		var p Principal
		_, err := fmt.Sscan(r.Header.Get("X-User"), &p.UserID)
		p.Platform = r.Header.Get("X-Platform")
		p.Device = p.Platform != ""
		return p, err == nil
	}
	options.Notify = func(userID int64, event string, data map[string]any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events = append(f.events, fmt.Sprintf("%d %s %v", userID, event, data["id"]))
	}
	if f.handler, err = New(options); err != nil {
		t.Fatal(err)
	}
	f.handler.retryDelay = time.Millisecond
	f.mux = http.NewServeMux()
	f.handler.Routes(f.mux)
	return f
}

func (f *fixture) start() {
	f.handler.StartWorker(context.Background())
	f.t.Cleanup(f.handler.Close)
}

func (f *fixture) call(user int64, method, target string, body any, header ...string) (int, map[string]any) {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, reader)
	if user != 0 {
		request.Header.Set("X-User", fmt.Sprint(user))
	}
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	var out map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &out)
	return recorder.Code, out
}

func (f *fixture) expect(status int, user int64, method, target string, body any) map[string]any {
	f.t.Helper()
	code, out := f.call(user, method, target, body)
	if code != status {
		f.t.Fatalf("%s %s = %d, want %d: %v", method, target, code, status, out)
	}
	return out
}

func (f *fixture) waitFor(user int64, id string, statuses ...string) map[string]any {
	f.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		job := f.expect(200, user, "GET", "/api/imports/"+id, nil)
		if slices.Contains(statuses, job["status"].(string)) {
			return job
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("import stayed %v", job)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (f *fixture) run(user int64, body map[string]any) map[string]any {
	f.t.Helper()
	return f.waitFor(user, f.expect(201, user, "POST", "/api/imports", body)["id"].(string), done, failed)
}

func (f *fixture) child(user int64, parent, name string) files.Node {
	f.t.Helper()
	node, found, err := f.files.ChildByName(context.Background(), user, parent, name)
	if err != nil || !found {
		f.t.Fatalf("missing %q: %v", name, err)
	}
	return node
}

func (f *fixture) content(user int64, nodeID string) string {
	f.t.Helper()
	_, stream, err := f.files.ExternalCopySource(context.Background(), user, nodeID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) storedPassword(id string) any {
	var value any
	if err := f.db.W.QueryRow(`SELECT password_enc FROM import_jobs WHERE id=?`, id).Scan(&value); err != nil {
		f.t.Fatal(err)
	}
	return value
}

type remoteFile struct {
	content, mtime string
	favorite       bool
}

// fakeNextcloud answers PROPFIND (Depth 0/1) and GET like Nextcloud does.
type fakeNextcloud struct {
	*httptest.Server
	mu        sync.Mutex
	passwords map[string]string
	files     map[string]map[string]remoteFile
	folders   map[string]map[string]remoteFile
	gets      map[string]int
	behaviour map[string]string
	hold      chan struct{}
}

func newFakeNextcloud(t *testing.T) *fakeNextcloud {
	n := &fakeNextcloud{
		passwords: map[string]string{}, files: map[string]map[string]remoteFile{}, folders: map[string]map[string]remoteFile{},
		gets: map[string]int{}, behaviour: map[string]string{}, hold: make(chan struct{}),
	}
	n.Server = httptest.NewServer(n)
	t.Cleanup(n.Close)
	return n
}

func (n *fakeNextcloud) account(name string, content map[string]remoteFile) {
	n.passwords[name], n.files[name] = name+"-app-pass", content
}

func (n *fakeNextcloud) connection(name string) map[string]any {
	return map[string]any{"source": "nextcloud", "url": n.URL, "username": name, "password": name + "-app-pass"}
}

func (n *fakeNextcloud) fetched(name string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.gets[name]
}

func (n *fakeNextcloud) behave(name, mode string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.behaviour[name] = mode
}

func (n *fakeNextcloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rest, moved := strings.CutPrefix(r.URL.EscapedPath(), "/old/"); moved {
		http.Redirect(w, r, "/"+rest, http.StatusMovedPermanently)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/remote.php/dav/files/")
	if !ok {
		w.WriteHeader(404)
		return
	}
	user, key, _ := strings.Cut(rest, "/")
	key = strings.Trim(key, "/")
	if given, password, ok := r.BasicAuth(); !ok || given != user || password != n.passwords[user] || password == "" {
		w.WriteHeader(401)
		return
	}
	type node struct {
		remoteFile
		folder bool
	}
	tree := map[string]node{"": {remoteFile{mtime: "2026-01-01 00:00:00"}, true}}
	for path := range n.files[user] {
		parts := strings.Split(path, "/")
		for depth := 1; depth < len(parts); depth++ {
			tree[strings.Join(parts[:depth], "/")] = node{remoteFile{mtime: "2026-01-01 00:00:00"}, true}
		}
	}
	for path, folder := range n.folders[user] {
		tree[path] = node{folder, true}
	}
	for path, file := range n.files[user] {
		tree[path] = node{file, false}
	}
	entry, found := tree[key]
	if !found {
		w.WriteHeader(404)
		return
	}
	if r.Method == "GET" {
		n.mu.Lock()
		n.gets[key]++
		mode, attempt := n.behaviour[key], n.gets[key]
		n.mu.Unlock()
		switch {
		case mode == "forbidden":
			w.WriteHeader(403)
			return
		case mode == "flaky" && attempt == 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
		if mode == "hold" {
			_, _ = io.WriteString(w, entry.content[:len(entry.content)/2])
			w.(http.Flusher).Flush()
			select {
			case <-n.hold:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, entry.content[len(entry.content)/2:])
			return
		}
		_, _ = io.WriteString(w, entry.content)
		return
	}
	depth := r.Header.Get("Depth")
	if r.Method != "PROPFIND" || depth != "0" && depth != "1" {
		w.WriteHeader(403)
		return
	}
	var out strings.Builder
	out.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">`)
	describe := func(path string, entry node) {
		segments := []string{url.PathEscape(user)}
		size := len(entry.content)
		if path != "" {
			for _, part := range strings.Split(path, "/") {
				segments = append(segments, url.PathEscape(part))
			}
		}
		href := "/remote.php/dav/files/" + strings.Join(segments, "/")
		props := `<d:resourcetype/><d:getcontentlength>` + strconv.Itoa(size) + `</d:getcontentlength>`
		if entry.folder {
			href += "/"
			size = 0
			for name, file := range n.files[user] {
				if path == "" || strings.HasPrefix(name, path+"/") {
					size += len(file.content)
				}
			}
			props = `<d:resourcetype><d:collection/></d:resourcetype><oc:size>` + strconv.Itoa(size) + `</oc:size>`
		}
		modified, _ := time.Parse(time.DateTime, entry.mtime)
		favorite := "0"
		if entry.favorite {
			favorite = "1"
		}
		fmt.Fprintf(&out, `<d:response><d:href>%s</d:href><d:propstat><d:prop>%s<d:getlastmodified>%s</d:getlastmodified><oc:favorite>%s</oc:favorite></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><d:quota-available-bytes/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>`,
			html.EscapeString(href), props, modified.UTC().Format(http.TimeFormat), favorite)
	}
	describe(key, entry)
	if depth == "1" && entry.folder {
		for path, child := range tree {
			parent := path[:max(strings.LastIndex(path, "/"), 0)]
			if path != "" && path != key && parent == key {
				describe(path, child)
			}
		}
	}
	out.WriteString(`</d:multistatus>`)
	w.WriteHeader(207)
	_, _ = io.WriteString(w, out.String())
}

var aliceFiles = map[string]remoteFile{
	"readme.txt":              {"hello from nextcloud", "2025-12-24 18:30:00", false},
	"Fotos/Übersicht ü.txt":   {"umlauts", "2024-02-29 08:15:00", true},
	"Fotos/2026/日本/写真 #1.jpg": {"jpeg bytes", "2026-01-02 03:04:05", false},
	"Документы/отчёт 2026.md": {"# отчёт", "2023-06-01 10:00:00", false},
	"Документы/a&b + c%.txt":  {"escaping", "2023-06-02 10:00:00", false},
	// Decomposed (NFD) on the server, as macOS clients upload it.
	"Café 🎉.txt": {"party", "2022-01-01 00:00:00", false},
}

func carolFiles() map[string]remoteFile {
	out := map[string]remoteFile{}
	for _, name := range []string{"a", "b", "c", "d"} {
		out[name+".bin"] = remoteFile{strings.Repeat(name, 4000), "2026-05-01 00:00:00", false}
	}
	return out
}

func TestTestingAConnection(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("alice", aliceFiles)
	nc.folders["alice"] = map[string]remoteFile{"Leer": {mtime: "2021-07-07 07:07:07"}}
	body := nc.connection("alice")
	body["password"] = "wrong"
	got := f.expect(200, owner, "POST", "/api/imports/test", body)
	if got["ok"] != false || got["code"] != "REMOTE_AUTH_FAILED" || !strings.Contains(got["error"].(string), "user name or app password") {
		t.Fatal(got)
	}
	total := 0
	for _, file := range aliceFiles {
		total += len(file.content)
	}
	got = f.expect(200, owner, "POST", "/api/imports/test", nc.connection("alice"))
	if got["ok"] != true || got["files"] != float64(5) || got["bytes"] != float64(total) {
		t.Fatal(got, total)
	}
	// A moved install: redirects are followed with the credentials kept.
	moved := nc.connection("alice")
	moved["url"] = nc.URL + "/old/index.php/apps/files"
	if got = f.expect(200, owner, "POST", "/api/imports/test", moved); got["ok"] != true {
		t.Fatal(got)
	}
	for field, value := range map[string]any{"url": "ftp://example.com", "sourcePath": "/a/../b", "source": "dropbox", "username": " ", "password": 5} {
		invalidBody := nc.connection("alice")
		invalidBody[field] = value
		if got = f.expect(400, owner, "POST", "/api/imports/test", invalidBody); got["code"] == nil {
			t.Fatal(field, got)
		}
	}
}

func TestImportKeepsTreeDatesAndFavoritesAndForgetsThePassword(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("alice", aliceFiles)
	nc.folders["alice"] = map[string]remoteFile{
		"Fotos": {mtime: "2024-03-01 09:00:00", favorite: true},
		"Leer":  {mtime: "2021-07-07 07:07:07"},
	}
	f.start()
	target, err := f.files.EnsureFolderPath(context.Background(), owner, "root", "From Nextcloud")
	if err != nil {
		t.Fatal(err)
	}
	body := nc.connection("alice")
	body["targetFolderId"] = target.ID
	created := f.expect(201, owner, "POST", "/api/imports", body)
	if created["targetPath"] != "/From Nextcloud" || created["password"] != nil {
		t.Fatal(created)
	}
	job := f.waitFor(owner, created["id"].(string), done, failed)
	if job["status"] != done || job["totalFiles"] != float64(6) || job["doneFiles"] != float64(6) || job["skippedFiles"] != float64(0) ||
		job["failedFiles"] != float64(0) || job["doneBytes"] != job["totalBytes"] || job["finishedAt"] == nil || job["startedAt"] == nil {
		t.Fatal(job)
	}
	if f.storedPassword(job["id"].(string)) != nil {
		t.Fatal("password kept")
	}
	readme := f.child(owner, target.ID, "readme.txt")
	if readme.ModifiedAt != "2025-12-24 18:30:00" || f.content(owner, readme.ID) != "hello from nextcloud" {
		t.Fatal(readme)
	}
	empty := f.child(owner, target.ID, "Leer")
	fotos := f.child(owner, target.ID, "Fotos")
	if empty.Type != "folder" || empty.ModifiedAt != "2021-07-07 07:07:07" || fotos.ModifiedAt != "2024-03-01 09:00:00" || !fotos.Favorite {
		t.Fatal(empty, fotos)
	}
	overview := f.child(owner, fotos.ID, "Übersicht ü.txt")
	if !overview.Favorite || overview.ModifiedAt != "2024-02-29 08:15:00" {
		t.Fatal(overview)
	}
	japan := f.child(owner, f.child(owner, fotos.ID, "2026").ID, "日本")
	if f.content(owner, f.child(owner, japan.ID, "写真 #1.jpg").ID) != "jpeg bytes" {
		t.Fatal("photo")
	}
	docs := f.child(owner, target.ID, "Документы")
	if f.child(owner, docs.ID, "a&b + c%.txt").Size != 8 || f.child(owner, target.ID, "Café 🎉.txt").Size != 5 {
		t.Fatal("names")
	}
	f.mu.Lock()
	events := slices.Clone(f.events)
	f.mu.Unlock()
	if !slices.Contains(events, "1 imports:changed "+job["id"].(string)) {
		t.Fatal(events)
	}

	// Running it again skips everything and downloads nothing.
	fetched := nc.fetched("readme.txt")
	again := f.run(owner, body)
	if again["status"] != done || again["doneFiles"] != float64(6) || again["skippedFiles"] != float64(6) || nc.fetched("readme.txt") != fetched {
		t.Fatal(again)
	}
	var versions int
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM file_versions`).Scan(&versions); err != nil || versions != 0 {
		t.Fatal(versions, err)
	}
	// A changed file becomes a new version of the same node.
	nc.files["alice"] = map[string]remoteFile{"readme.txt": {"changed", "2026-02-02 02:02:02", false}}
	f.run(owner, body)
	changed := f.child(owner, target.ID, "readme.txt")
	if changed.ID != readme.ID || f.content(owner, changed.ID) != "changed" || changed.ModifiedAt != "2026-02-02 02:02:02" {
		t.Fatal(changed)
	}
}

func TestOneBadFileDoesNotStopTheJob(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("bob", map[string]remoteFile{
		"good.txt":    {"fine", "2026-01-01 00:00:00", false},
		"blocked.exe": {"quarantined", "2026-01-01 00:00:00", false},
		"flaky.txt":   {"second try", "2026-01-01 00:00:00", false},
	})
	nc.behave("blocked.exe", "forbidden")
	nc.behave("flaky.txt", "flaky")
	f.start()
	job := f.run(owner, nc.connection("bob"))
	if job["status"] != done || job["doneFiles"] != float64(2) || job["failedFiles"] != float64(1) || job["targetPath"] != "/Nextcloud" {
		t.Fatal(job)
	}
	if got := fmt.Sprint(job["errors"]); got != "[map[message:The server denied access. path:/blocked.exe]]" {
		t.Fatal(got)
	}
	if nc.fetched("flaky.txt") != 2 {
		t.Fatal(nc.fetched("flaky.txt"))
	}
}

func TestImportThatCannotFitTheQuotaFailsUpFront(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("dave", map[string]remoteFile{"big.bin": {strings.Repeat("x", 5000), "2026-01-01 00:00:00", false}})
	if _, err := f.db.W.Exec(`UPDATE users SET quota_bytes=1000 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.start()
	job := f.run(owner, nc.connection("dave"))
	message := job["errors"].([]any)[0].(map[string]any)["message"]
	if job["status"] != failed || message != "Not enough storage: the import needs 4.9 KB, 1000 B are left." || nc.fetched("big.bin") != 0 || f.storedPassword(job["id"].(string)) != nil {
		t.Fatal(job)
	}
}

func TestCancelPauseResumeAndDelete(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("carol", carolFiles())
	nc.behave("a.bin", "hold")
	nc.behave("b.bin", "hold")
	f.start()
	id := f.expect(201, owner, "POST", "/api/imports", nc.connection("carol"))["id"].(string)
	waitUntil(t, func() bool { return nc.fetched("a.bin") >= 1 && nc.fetched("b.bin") >= 1 })
	f.expect(409, owner, "DELETE", "/api/imports/"+id, nil)
	f.expect(409, owner, "POST", "/api/imports/"+id+"/resume", nil)
	if got := f.expect(200, owner, "POST", "/api/imports/"+id+"/cancel", nil); got["status"] != cancelled {
		t.Fatal(got)
	}
	if f.storedPassword(id) != nil {
		t.Fatal("password kept")
	}
	f.expect(409, owner, "POST", "/api/imports/"+id+"/cancel", nil)
	time.Sleep(100 * time.Millisecond)
	job := f.waitFor(owner, id, cancelled)
	if job["doneFiles"] != float64(0) || job["failedFiles"] != float64(0) || nc.fetched("c.bin") != 0 {
		t.Fatal(job)
	}
	if got := f.expect(400, owner, "POST", "/api/imports/"+id+"/resume", nil); got["code"] != "PASSWORD_REQUIRED" {
		t.Fatal(got)
	}
	f.expect(200, owner, "DELETE", "/api/imports/"+id, nil)
	f.expect(404, owner, "GET", "/api/imports/"+id, nil)

	// Pause keeps the password and resume finishes the job.
	nc.behave("b.bin", "")
	started := nc.fetched("a.bin")
	id = f.expect(201, owner, "POST", "/api/imports", nc.connection("carol"))["id"].(string)
	waitUntil(t, func() bool { return nc.fetched("a.bin") > started })
	if got := f.expect(200, owner, "POST", "/api/imports/"+id+"/pause", nil); got["status"] != paused || f.storedPassword(id) == nil {
		t.Fatal(got)
	}
	f.expect(409, owner, "POST", "/api/imports/"+id+"/pause", nil)
	nc.behave("a.bin", "")
	f.expect(200, owner, "POST", "/api/imports/"+id+"/resume", nil)
	job = f.waitFor(owner, id, done, failed)
	if job["status"] != done || job["doneFiles"] != float64(4) || f.storedPassword(id) != nil {
		t.Fatal(job)
	}
}

func waitUntil(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInterruptedJobResumesAfterARestart(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("carol", carolFiles())
	nc.behave("c.bin", "hold")
	f.start()
	id := f.expect(201, owner, "POST", "/api/imports", nc.connection("carol"))["id"].(string)
	waitUntil(t, func() bool {
		return f.expect(200, owner, "GET", "/api/imports/"+id, nil)["doneFiles"] == float64(3) && nc.fetched("c.bin") > 0
	})
	f.handler.Close()
	var status string
	if err := f.db.W.QueryRow(`SELECT status FROM import_jobs WHERE id=?`, id).Scan(&status); err != nil || status != running {
		t.Fatal(status, err)
	}
	nc.behave("c.bin", "")
	f.handler.StartWorker(context.Background())
	job := f.waitFor(owner, id, done, failed)
	if job["status"] != done || job["doneFiles"] != float64(4) || nc.fetched("c.bin") != 2 {
		t.Fatal(job, nc.fetched("c.bin"))
	}
	for _, name := range []string{"a.bin", "b.bin", "d.bin"} {
		if nc.fetched(name) != 1 {
			t.Fatal(name, nc.fetched(name))
		}
	}
	file := f.child(owner, job["targetFolderId"].(string), "c.bin")
	if f.content(owner, file.ID) != strings.Repeat("c", 4000) {
		t.Fatal("content")
	}
}

func TestImportsAreScopedToTheAccount(t *testing.T) {
	f := newFixture(t, Options{})
	target, err := f.files.EnsureFolderPath(context.Background(), owner, "root", "Mine")
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"source": "webdav", "url": "https://dav.example.com", "username": "x", "password": "p4ss-never-shown", "targetFolderId": target.ID}
	created := f.expect(201, owner, "POST", "/api/imports", body)
	id := created["id"].(string)
	if stored := f.storedPassword(id).(string); strings.Contains(stored, "p4ss") || strings.Contains(fmt.Sprint(created), "p4ss") {
		t.Fatal("password exposed")
	}
	f.expect(404, stranger, "POST", "/api/imports", body)
	routes := []struct{ method, path string }{
		{"GET", ""}, {"POST", "/pause"}, {"POST", "/resume"}, {"POST", "/cancel"}, {"DELETE", ""},
	}
	for _, route := range routes {
		f.expect(404, stranger, route.method, "/api/imports/"+id+route.path, nil)
		f.expect(401, 0, route.method, "/api/imports/"+id+route.path, nil)
		if code, got := f.call(owner, route.method, "/api/imports/"+id+route.path, nil, "X-Platform", "desktop"); code != 403 || got["code"] != "PHONE_REQUIRED" {
			t.Fatal(code, got)
		}
	}
	f.expect(401, 0, "POST", "/api/imports/test", body)
	if got := f.expect(200, stranger, "GET", "/api/imports", nil); len(got["items"].([]any)) != 0 {
		t.Fatal(got)
	}
	if got := f.expect(200, owner, "GET", "/api/imports/"+id, nil); got["status"] != queued {
		t.Fatal(got)
	}
	// Importing into a vault is refused.
	if _, err = f.db.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE nodes SET vault_id='fake' WHERE id=?`, target.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.expect(409, owner, "POST", "/api/imports", body); got["code"] != "VAULT_BOUNDARY" {
		t.Fatal(got)
	}
	// A remote folder that meets a vault creates nothing inside it.
	var before, after int
	_ = f.db.W.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&before)
	stopped, err := f.files.EnsureFolderPath(context.Background(), owner, "root", "Mine/inside/deeper")
	_ = f.db.W.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&after)
	if err != nil || stopped.VaultID == nil || before != after {
		t.Fatal(stopped, err, before, after)
	}
	// Each test signs in somewhere else: it is rate limited.
	var code int
	for range 21 {
		code, _ = f.call(stranger, "POST", "/api/imports/test", map[string]any{"source": "webdav", "url": "not a url"})
	}
	if code != 429 {
		t.Fatal(code)
	}
}

func TestPrivateNetworksNeedAnAdminOrTheOptIn(t *testing.T) {
	nc := newFakeNextcloud(t)
	nc.account("alice", aliceFiles)
	f := newFixture(t, Options{IsAdmin: func(_ context.Context, id int64) (bool, error) { return id == stranger, nil }})
	for _, address := range []string{nc.URL, "http://localhost:1", "http://[::1]:1", "http://[::ffff:192.168.0.1]", "http://169.254.169.254", "http://100.64.0.1"} {
		body := nc.connection("alice")
		body["url"] = address
		if got := f.expect(200, owner, "POST", "/api/imports/test", body); got["code"] != "IMPORT_HOST_BLOCKED" || got["ok"] != false {
			t.Fatal(address, got)
		}
	}
	if got := f.expect(200, stranger, "POST", "/api/imports/test", nc.connection("alice")); got["ok"] != true {
		t.Fatal(got)
	}
	f.start()
	job := f.run(owner, nc.connection("alice"))
	if job["status"] != failed || !strings.Contains(fmt.Sprint(job["errors"]), "private network") || f.storedPassword(job["id"].(string)) != nil {
		t.Fatal(job)
	}
}

// A name that resolves to a public address once and to loopback for the next
// connection must be refused when dialled.
func TestRebindingNameIsRefusedAtConnectTime(t *testing.T) {
	seen := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { seen++ }))
	defer server.Close()
	f := newFixture(t, Options{Resolve: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, nil
	}})
	port := server.URL[strings.LastIndex(server.URL, ":"):]
	got := f.expect(200, owner, "POST", "/api/imports/test", map[string]any{"source": "webdav", "url": "http://rebind.test" + port, "username": "u", "password": "p"})
	if got["code"] != "IMPORT_HOST_BLOCKED" || seen != 0 {
		t.Fatal(got, seen)
	}
}

func TestCredentialsFollowARedirectOnlyToTheSameOrigin(t *testing.T) {
	var authorization []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		w.WriteHeader(401)
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/dav/", http.StatusFound)
	}))
	defer origin.Close()
	f := newFixture(t, Options{AllowPrivate: true})
	f.expect(200, owner, "POST", "/api/imports/test", map[string]any{"source": "webdav", "url": origin.URL, "username": "u", "password": "p"})
	if len(authorization) == 0 || strings.Join(authorization, "") != "" {
		t.Fatal(authorization)
	}
}

func TestListingsAreCapped(t *testing.T) {
	huge := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(207)
		if !huge {
			_, _ = io.WriteString(w, `<d:multistatus xmlns:d="DAV:">`+strings.Repeat("<d:response>", 90000))
			return
		}
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for range 20 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	f := newFixture(t, Options{AllowPrivate: true})
	body := map[string]any{"source": "webdav", "url": server.URL, "username": "u", "password": "p"}
	if got := f.expect(200, owner, "POST", "/api/imports/test", body); got["code"] != "REMOTE_ERROR" || !strings.Contains(got["error"].(string), "too large") {
		t.Fatal(got)
	}
	huge = false
	if got := f.expect(200, owner, "POST", "/api/imports/test", body); got["code"] != "REMOTE_NOT_WEBDAV" {
		t.Fatal(got)
	}
}

// The remote server decides how big a tree a scan sees; one that never stops
// must fail the job.
func TestScanLimits(t *testing.T) {
	var mu sync.Mutex
	childName, wide, listings := "a", false, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		listings++
		self := strings.TrimRight(r.URL.EscapedPath(), "/") + "/"
		folder := func(href string) string {
			return `<d:response><d:href>` + href + `</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`
		}
		var children strings.Builder
		if r.Header.Get("Depth") == "1" {
			switch {
			case !wide:
				children.WriteString(folder(self + childName + "/"))
			case self == "/dav/":
				for i := range 30 {
					children.WriteString(folder(fmt.Sprintf("%sf%d/", self, i)))
				}
			default:
				for i := range 9000 {
					fmt.Fprintf(&children, `<d:response><d:href>%s%d</d:href></d:response>`, self, i)
				}
			}
		}
		w.WriteHeader(207)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`+folder(self)+children.String()+`</d:multistatus>`)
	}))
	defer server.Close()
	f := newFixture(t, Options{AllowPrivate: true})
	f.start()
	runImport := func(want string) string {
		job := f.run(owner, map[string]any{"source": "webdav", "url": server.URL + "/dav/", "username": "u", "password": "p"})
		problems := job["errors"].([]any)
		if job["status"] != failed || !strings.Contains(problems[len(problems)-1].(map[string]any)["message"].(string), want) {
			t.Fatal(job)
		}
		return job["id"].(string)
	}
	items := func(id string) (n int) {
		_ = f.db.W.QueryRow(`SELECT COUNT(*) FROM import_items WHERE job_id=?`, id).Scan(&n)
		return n
	}
	if id := runImport("deeper than 64 folders"); listings > 70 || items(id) > 64 {
		t.Fatal(listings, items(id))
	}
	mu.Lock()
	childName, listings = strings.Repeat("n", 1000), 0
	mu.Unlock()
	if runImport("longer than 4096 characters"); listings > 8 {
		t.Fatal(listings)
	}
	// A quarter of a million rows: too slow under the race detector.
	if raceEnabled {
		return
	}
	mu.Lock()
	wide = true
	mu.Unlock()
	if id := runImport("more than 250000"); items(id) > 250000 {
		t.Fatal(items(id))
	}
}

type fullDisk struct{ Files }

func (fullDisk) ImportCopy(context.Context, int64, string, string, io.Reader, int64, string) (files.Node, error) {
	return files.Node{}, fmt.Errorf("write blob: %w", syscall.ENOSPC)
}

func TestFullDiskPausesTheJobAndKeepsThePassword(t *testing.T) {
	nc, f := newFakeNextcloud(t), newFixture(t, Options{AllowPrivate: true})
	nc.account("carol", carolFiles())
	f.handler.files = fullDisk{f.files}
	f.start()
	id := f.expect(201, owner, "POST", "/api/imports", nc.connection("carol"))["id"].(string)
	job := f.waitFor(owner, id, paused, done, failed)
	if job["status"] != paused || job["failedFiles"] != float64(0) || f.storedPassword(id) == nil || !strings.Contains(fmt.Sprint(job["errors"]), "out of space") {
		t.Fatal(job)
	}
	if !asDiskFull(errors.Join(syscall.ENOSPC)) || asDiskFull(errors.New("other")) {
		t.Fatal("classification")
	}
}

func TestConnectionNormalisation(t *testing.T) {
	for given, want := range map[string]string{
		"https://cloud.example.com/index.php/apps/files?dir=/#x": "https://cloud.example.com",
		"https://example.com/nc/remote.php/dav/files/me/":        "https://example.com/nc",
		" https://cloud.example.com/ ":                           "https://cloud.example.com",
	} {
		if got, err := normalizeURL(given, nextcloud); err != nil || got != want {
			t.Fatal(given, got, err)
		}
	}
	if got, err := normalizeURL("https://dav.example.com/share", plainWebDAV); err != nil || got != "https://dav.example.com/share/" {
		t.Fatal(got, err)
	}
	if got, err := normalizeSourcePath("/Photos//2026/"); err != nil || got != "/Photos/2026" {
		t.Fatal(got, err)
	}
}

func TestProgressEventsAreThrottled(t *testing.T) {
	f := newFixture(t, Options{})
	f.handler.Announce(owner, "job", false)
	f.handler.Announce(owner, "job", false)
	f.handler.Announce(owner, "job", true)
	if len(f.events) != 2 {
		t.Fatal(f.events)
	}
}
