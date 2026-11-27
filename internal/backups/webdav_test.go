package backups

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type webDAVFixture struct {
	mu    sync.Mutex
	files map[string][]byte
	folds map[string]bool
}

func newWebDAVFixture() *webDAVFixture {
	return &webDAVFixture{files: map[string][]byte{}, folds: map[string]bool{"/dav/": true}}
}

func (f *webDAVFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Basic dXNlcjpwYXNz" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	key := path.Clean(r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/") && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case "MKCOL":
		if f.folds[key] {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.folds[key] = true
		w.WriteHeader(http.StatusCreated)
	case "PROPFIND":
		if !f.folds[key] && key != "/dav/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		var body strings.Builder
		body.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		writeResponse := func(href string, collection bool) {
			body.WriteString(`<d:response><d:href>`)
			var escaped strings.Builder
			_ = xml.EscapeText(&escaped, []byte(href))
			body.WriteString(escaped.String())
			body.WriteString(`</d:href><d:propstat><d:prop><d:resourcetype>`)
			if collection {
				body.WriteString(`<d:collection/>`)
			}
			body.WriteString(`</d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		}
		writeResponse(key, f.folds[key])
		if r.Header.Get("Depth") != "0" {
			prefix := strings.TrimSuffix(key, "/") + "/"
			for folder := range f.folds {
				if folder == key || !strings.HasPrefix(folder, prefix) {
					continue
				}
				remainder := strings.TrimPrefix(folder, prefix)
				if remainder != "" && !strings.Contains(strings.TrimSuffix(remainder, "/"), "/") {
					writeResponse(folder, true)
				}
			}
			for file := range f.files {
				if !strings.HasPrefix(file, prefix) {
					continue
				}
				remainder := strings.TrimPrefix(file, prefix)
				if remainder != "" && !strings.Contains(remainder, "/") {
					writeResponse(file, false)
				}
			}
		}
		body.WriteString(`</d:multistatus>`)
		_, _ = io.WriteString(w, body.String())
	case http.MethodHead:
		if _, ok := f.files[key]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		f.files[key] = data
		w.WriteHeader(http.StatusCreated)
	case "MOVE":
		destination := r.Header.Get("Destination")
		if destination == "" {
			http.Error(w, "missing destination", 400)
			return
		}
		var target string
		if strings.Contains(destination, "/dav/") {
			target = "/dav/" + strings.SplitN(destination, "/dav/", 2)[1]
		} else {
			target = destination
		}
		data, ok := f.files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.files[path.Clean(target)] = data
		delete(f.files, key)
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		data, ok := f.files[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	case http.MethodDelete:
		delete(f.files, key)
		prefix := strings.TrimSuffix(key, "/") + "/"
		for file := range f.files {
			if strings.HasPrefix(file, prefix) {
				delete(f.files, file)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestWebDAVTargetValidatesURLAndKeys(t *testing.T) {
	for _, raw := range []string{"file:///tmp", "https://user:password@example.test/dav", "https://example.test/dav?x=1", "//example.test/dav"} {
		if _, err := NewWebDAVTarget(WebDAVConfig{URL: raw}); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	target, err := NewWebDAVTargetWithClient(WebDAVConfig{URL: server.URL + "/dav", Username: "user", Password: "pass"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../outside", "a/../outside", "a\\outside", "/outside", "a//b"} {
		if err = validTargetKey(key, false); err == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
	}
	if _, err = target.targetURL("../escape", false); err == nil {
		t.Fatal("accepted traversal key")
	}
}

func TestWebDAVTargetPutGetExistsAndDelete(t *testing.T) {
	fixture := newWebDAVFixture()
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewWebDAVTargetWithClient(WebDAVConfig{URL: server.URL + "/dav", Username: "user", Password: "pass"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = target.Open(ctx); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/source.bin"
	if err = os.WriteFile(source, []byte("ciphertext fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = target.Put(ctx, "blobs/12/a.bin", source); err != nil {
		t.Fatal(err)
	}
	keys, err := target.List(ctx, "blobs")
	if err != nil || len(keys) != 1 || keys[0] != "blobs/12/a.bin" {
		t.Fatalf("list=%v err=%v", keys, err)
	}
	exists, err := target.Exists(ctx, "blobs/12/a.bin")
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	destination := t.TempDir() + "/copy.bin"
	if err = target.Get(ctx, "blobs/12/a.bin", destination); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(destination)
	if string(got) != "ciphertext fixture" {
		t.Fatalf("download=%q", got)
	}
	if err = target.Delete(ctx, "blobs/12/a.bin"); err != nil {
		t.Fatal(err)
	}
	exists, err = target.Exists(ctx, "blobs/12/a.bin")
	if err != nil || exists {
		t.Fatalf("after delete exists=%v err=%v", exists, err)
	}
}

func TestWebDAVTargetRunsEncryptedSnapshotLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := dbx.Open(ctx, filepath.Join(root, "data.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = schema.Migrate(ctx, db, filepath.Join(root, "migration-backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','x'); INSERT INTO backup_targets(id,name,type,config_enc) VALUES(1,'dav','webdav','sealed')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(db, keys, filepath.Join(root, "blobs"), filepath.Join(root, "tmp"), filepath.Join(root, "local-backups"), "test-version")
	if err != nil {
		t.Fatal(err)
	}
	runner.Start(ctx)
	defer runner.Close()
	fixture := newWebDAVFixture()
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewWebDAVTargetWithClient(WebDAVConfig{URL: server.URL + "/dav", Username: "user", Password: "pass"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	runID, err := runner.startTargetRun(1, target, 2)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, db.W, runID)
	var snapshot, status string
	if err = db.W.QueryRow(`SELECT snapshot,status FROM backup_runs WHERE id=?`, runID).Scan(&snapshot, &status); err != nil || status != "ok" {
		t.Fatalf("backup run status=%q err=%v", status, err)
	}
	listed, err := ListTargetSnapshots(ctx, target)
	if err != nil || len(listed) != 1 || listed[0] != snapshot {
		t.Fatalf("snapshots=%v err=%v", listed, err)
	}
	verified := runner.VerifyTargetSnapshot(ctx, target)
	if !verified.OK || verified.Database != "ok" || verified.Snapshot != snapshot {
		t.Fatalf("verification=%+v", verified)
	}
	time.Sleep(2 * time.Millisecond)
	secondID, err := runner.startTargetRun(1, target, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, db.W, secondID)
	remaining, err := ListTargetSnapshots(ctx, target)
	if err != nil || len(remaining) != 1 || remaining[0] == snapshot {
		t.Fatalf("retained snapshots=%v err=%v", remaining, err)
	}
}

func TestSchedulerRunsWebDAVTarget(t *testing.T) {
	db, keys, runner, _ := schedulerFixture(t)
	fixture := newWebDAVFixture()
	server := httptest.NewServer(fixture)
	defer server.Close()
	config, err := json.Marshal(map[string]string{"url": server.URL + "/dav", "username": "user", "password": "pass"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keys.EncryptValue(string(config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(1,'webdav','webdav',?,'daily',?,0,2,1)`, sealed, (time.Now().Hour()+23)%24); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner.Start(ctx)
	defer runner.Close()
	runner.checkSchedules(ctx, time.Now())
	var runID int64
	if err = db.W.QueryRow(`SELECT id FROM backup_runs WHERE target_id=1 ORDER BY id DESC LIMIT 1`).Scan(&runID); err != nil {
		t.Fatalf("scheduled WebDAV run missing: %v", err)
	}
	waitRun(t, db.W, runID)
	target, err := NewWebDAVTargetWithClient(WebDAVConfig{URL: server.URL + "/dav", Username: "user", Password: "pass"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := ListTargetSnapshots(ctx, target)
	if err != nil || len(listed) != 1 {
		t.Fatalf("scheduled WebDAV snapshots=%v err=%v", listed, err)
	}
}
