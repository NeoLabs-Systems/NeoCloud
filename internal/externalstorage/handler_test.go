package externalstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"golang.org/x/net/webdav"
)

const (
	owner    = int64(1)
	stranger = int64(2)
	admin    = int64(3)
)

type fixture struct {
	t       *testing.T
	handler *Handler
	db      *dbx.DB
	files   *files.Handler
	mux     *http.ServeMux
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
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password,is_admin) VALUES(1,'owner','x',0),(2,'stranger','x',0),(3,'admin','x',1)`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	options.DB, options.Keys = db.W, keys
	options.ResolvePrincipal = func(r *http.Request) (Principal, bool) {
		var p Principal
		_, err := fmt.Sscan(r.Header.Get("X-User"), &p.UserID)
		p.Platform = r.Header.Get("X-Platform")
		p.Device = p.Platform != ""
		return p, err == nil
	}
	options.IsAdmin = func(_ context.Context, id int64) (bool, error) { return id == admin, nil }
	handler, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	tree := files.New(files.Options{
		DB: db.W, Keys: keys, BlobDir: filepath.Join(base, "blobs"), UploadTmpDir: filepath.Join(base, "tmp"),
		PreviewDir: filepath.Join(base, "previews"), MaxUploadBytes: 1 << 30, DefaultQuotaBytes: 1 << 30, MaxNodes: 1000, MaxCopyNodes: 100,
	})
	handler.SetFiles(tree)
	mux := http.NewServeMux()
	handler.Routes(mux)
	return &fixture{t: t, handler: handler, db: db, files: tree, mux: mux}
}

func (f *fixture) call(user int64, method, target string, body any, header ...string) *httptest.ResponseRecorder {
	var reader io.Reader
	switch typed := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(typed)
	default:
		encoded, err := json.Marshal(typed)
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
	return recorder
}

func (f *fixture) expect(status int, recorder *httptest.ResponseRecorder) map[string]any {
	f.t.Helper()
	if recorder.Code != status {
		f.t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &out)
	return out
}

func names(listing map[string]any) []string {
	var out []string
	for _, raw := range listing["items"].([]any) {
		item := raw.(map[string]any)
		out = append(out, item["type"].(string)+":"+item["name"].(string))
	}
	return out
}

// davServer is a WebDAV share with Basic auth that records what it was sent.
type davServer struct {
	*httptest.Server
	fs       webdav.FileSystem
	mu       sync.Mutex
	redirect string
	lengths  map[string]string
}

func newDAV(t *testing.T) *davServer {
	t.Helper()
	d := &davServer{fs: webdav.NewMemFS(), lengths: map[string]string{}}
	share := &webdav.Handler{Prefix: "/dav", FileSystem: d.fs, LockSystem: webdav.NewMemLS()}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		redirect := d.redirect
		d.lengths[r.Method+" "+r.URL.Path] = r.Header.Get("Content-Length")
		d.mu.Unlock()
		if redirect != "" {
			http.Redirect(w, r, redirect, http.StatusFound)
			return
		}
		if user, password, ok := r.BasicAuth(); !ok || user != "dav" || password != "dav-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		share.ServeHTTP(w, r)
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *davServer) put(t *testing.T, name, content string) {
	t.Helper()
	file, err := d.fs.OpenFile(context.Background(), name, 0x42|0x200, 0o600) // O_RDWR|O_CREATE|O_TRUNC
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(content))
	_ = file.Close()
}

func (d *davServer) get(name string) (string, bool) {
	file, err := d.fs.OpenFile(context.Background(), name, 0, 0)
	if err != nil {
		return "", false
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	return string(data), true
}

func (d *davServer) url() string { return d.URL + "/dav" }

func (d *davServer) body(name string) map[string]any {
	return map[string]any{"name": name, "provider": "webdav", "config": map[string]any{"url": d.url(), "username": "dav", "password": "dav-pass"}}
}

func (f *fixture) connectDAV(user int64, d *davServer, name string) string {
	f.t.Helper()
	return f.expect(201, f.call(user, "POST", "/api/external-storage", d.body(name)))["id"].(string)
}

func TestConnectingTestsTheCredentialsFirst(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	dav.put(t, "/note.txt", "x")
	_ = dav.fs.Mkdir(context.Background(), "/docs", 0o700)
	bad := dav.body("NAS")
	bad["config"].(map[string]any)["password"] = "wrong"
	if got := f.expect(422, f.call(owner, "POST", "/api/external-storage", bad)); got["code"] != "EXTERNAL_TEST_FAILED" {
		t.Fatal(got)
	}
	if got := f.expect(200, f.call(owner, "GET", "/api/external-storage", nil)); len(got["items"].([]any)) != 0 {
		t.Fatal(got)
	}
	if got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", dav.body("NAS"))); got["ok"] != true || got["items"] != float64(2) {
		t.Fatal(got)
	}
	if got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", bad)); got["ok"] != false || got["code"] != "EXTERNAL_AUTH_FAILED" {
		t.Fatal(got)
	}
}

func TestWebDAVBrowseReadWriteAndCopy(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	dav.put(t, "/note.txt", "0123456789")
	_ = dav.fs.Mkdir(context.Background(), "/docs", 0o700)
	dav.put(t, "/docs/a.md", "# a")
	base := "/api/external-storage/" + f.connectDAV(owner, dav, "NAS")

	root := f.expect(200, f.call(owner, "GET", base+"/list", nil))
	if got := names(root); !slices.Equal(got, []string{"folder:docs", "file:note.txt"}) {
		t.Fatal(got)
	}
	part := f.call(owner, "GET", base+"/file?path=note.txt", nil, "Range", "bytes=2-4")
	if part.Code != 206 || part.Body.String() != "234" || part.Header().Get("Content-Range") != "bytes 2-4/10" {
		t.Fatal(part.Code, part.Body, part.Header())
	}
	if got := f.call(owner, "GET", base+"/file?path=note.txt", nil, "Range", "bytes=50-"); got.Code != 416 || got.Header().Get("Content-Range") != "bytes */10" {
		t.Fatal(got.Code, got.Header())
	}
	f.expect(201, f.call(owner, "PUT", base+"/file?path=docs/new.txt", []byte("dav upload")))
	if content, _ := dav.get("/docs/new.txt"); content != "dav upload" || dav.lengths["PUT /dav/docs/new.txt"] != "10" {
		t.Fatal(content, dav.lengths)
	}
	f.expect(201, f.call(owner, "PUT", base+"/file?path=docs/empty.txt", []byte{}))
	f.expect(409, f.call(owner, "PUT", base+"/file?path=docs/new.txt", []byte("again")))
	if got := f.expect(201, f.call(owner, "PUT", base+"/file?path=docs/new.txt&conflict=rename", []byte("again"))); got["name"] != "new (1).txt" || got["path"] != "docs/new (1).txt" {
		t.Fatal(got)
	}
	f.expect(201, f.call(owner, "POST", base+"/folder", map[string]any{"path": "docs/sub"}))
	f.expect(409, f.call(owner, "POST", base+"/folder", map[string]any{"path": "docs/sub"}))
	f.expect(200, f.call(owner, "POST", base+"/move", map[string]any{"from": "docs/new.txt", "to": "docs/sub/renamed.txt"}))
	f.expect(409, f.call(owner, "POST", base+"/move", map[string]any{"from": "docs/a.md", "to": "note.txt"}))
	f.expect(200, f.call(owner, "DELETE", base+"/entry?path=docs/sub", nil))
	if _, found := dav.get("/docs/sub/renamed.txt"); found {
		t.Fatal("folder not removed")
	}

	// Copy in and out: the file stays at the provider and never enters the tree by itself.
	rootFolder, err := f.files.OwnFolder(context.Background(), owner, "root")
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.files.ImportCopy(context.Background(), owner, rootFolder.ID, "report.txt", strings.NewReader("quarterly numbers"), 17, "rename")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docs/report.txt", "docs/report (1).txt"} {
		if got := f.expect(201, f.call(owner, "POST", base+"/copy-in", map[string]any{"nodeId": report.ID, "folder": "docs"})); got["path"] != want {
			t.Fatal(got)
		}
	}
	if content, _ := dav.get("/docs/report.txt"); content != "quarterly numbers" {
		t.Fatal(content)
	}
	out := f.expect(201, f.call(owner, "POST", base+"/copy-out", map[string]any{"path": "note.txt", "parentId": "root"}))
	if out["name"] != "note.txt" || out["size"] != float64(10) || out["modifiedAt"] == nil {
		t.Fatal(out)
	}
	f.expect(400, f.call(owner, "POST", base+"/copy-out", map[string]any{"path": "docs", "parentId": "root"}))
	// Another account's file or folder is missing, in both directions.
	otherBase := "/api/external-storage/" + f.connectDAV(stranger, dav, "Mine")
	f.expect(404, f.call(stranger, "POST", otherBase+"/copy-in", map[string]any{"nodeId": report.ID, "folder": ""}))
	f.expect(404, f.call(stranger, "POST", otherBase+"/copy-out", map[string]any{"path": "note.txt", "parentId": rootFolder.ID}))
	// Vault files and vault folders stay out.
	if _, err = f.db.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE nodes SET vault_id='fake-vault' WHERE id=?`, report.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.expect(409, f.call(owner, "POST", base+"/copy-in", map[string]any{"nodeId": report.ID, "folder": ""})); got["code"] != "VAULT_BOUNDARY" {
		t.Fatal(got)
	}
	if _, found := dav.get("/report.txt"); found {
		t.Fatal("vault file left NeoCloud")
	}

	// A read-only mount refuses every change but still reads.
	f.expect(200, f.call(owner, "PATCH", base, map[string]any{"readOnly": true}))
	for _, got := range []*httptest.ResponseRecorder{
		f.call(owner, "PUT", base+"/file?path=x.txt", []byte("x")),
		f.call(owner, "POST", base+"/folder", map[string]any{"path": "x"}),
		f.call(owner, "POST", base+"/move", map[string]any{"from": "note.txt", "to": "y.txt"}),
		f.call(owner, "DELETE", base+"/entry?path=note.txt", nil),
		f.call(owner, "POST", base+"/copy-in", map[string]any{"nodeId": out["id"], "folder": ""}),
	} {
		if body := f.expect(403, got); body["code"] != "EXTERNAL_READ_ONLY" {
			t.Fatal(body)
		}
	}
	f.expect(200, f.call(owner, "GET", base+"/list", nil))
	var audited int
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action IN ('external_storage.connect','external_storage.update','external_storage.move','external_storage.delete') AND subject_user_id=1`).Scan(&audited); err != nil || audited != 4 {
		t.Fatal(audited, err)
	}
}

func TestSecretsNamesAndTheConnectionCap(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	id := f.connectDAV(owner, dav, "NAS")
	base := "/api/external-storage/" + id
	listed := f.call(owner, "GET", "/api/external-storage", nil)
	if strings.Contains(listed.Body.String(), "dav-pass") || !strings.Contains(listed.Body.String(), `"passwordSet":true`) || !strings.Contains(listed.Body.String(), `"createdAt":"`) {
		t.Fatal(listed.Body)
	}
	var sealed string
	if err := f.db.W.QueryRow(`SELECT config_enc FROM external_mounts WHERE id=?`, id).Scan(&sealed); err != nil || strings.Contains(sealed, "dav") {
		t.Fatal(sealed, err)
	}
	config := map[string]any{"url": dav.url(), "username": "dav", "password": ""}
	edited := f.call(owner, "PATCH", base, map[string]any{"name": "Renamed NAS", "config": config})
	if body := f.expect(200, edited); body["name"] != "Renamed NAS" || strings.Contains(edited.Body.String(), "dav-pass") {
		t.Fatal(body)
	}
	if got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", map[string]any{"id": id, "config": config})); got["ok"] != true {
		t.Fatal(got)
	}
	f.expect(200, f.call(owner, "GET", base+"/list", nil))
	f.expect(422, f.call(owner, "PATCH", base, map[string]any{"config": map[string]any{"url": dav.url(), "username": "someone-else"}}))
	// A row moved to another account cannot be opened.
	_, _ = f.db.W.Exec(`UPDATE external_mounts SET user_id=2 WHERE id=?`, id)
	f.expect(500, f.call(stranger, "GET", base+"/list", nil))
	_, _ = f.db.W.Exec(`UPDATE external_mounts SET user_id=1 WHERE id=?`, id)

	f.expect(409, f.call(owner, "POST", "/api/external-storage", dav.body("renamed nas")))
	f.expect(400, f.call(owner, "POST", "/api/external-storage", dav.body("a/b")))
	for i := range 9 {
		f.connectDAV(owner, dav, fmt.Sprintf("extra %d", i))
	}
	if got := f.expect(409, f.call(owner, "POST", "/api/external-storage", dav.body("one too many"))); got["code"] != "EXTERNAL_MOUNT_LIMIT" {
		t.Fatal(got)
	}
	f.expect(200, f.call(owner, "DELETE", base, nil))
	f.expect(404, f.call(owner, "DELETE", base, nil))
}

func TestAccessControl(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	dav.put(t, "/inside.txt", "inside")
	id := f.connectDAV(owner, dav, "NAS")
	base := "/api/external-storage/" + id
	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", base + "/list", nil},
		{"GET", base + "/file?path=inside.txt", nil},
		{"PUT", base + "/file?path=x.txt", []byte("x")},
		{"POST", base + "/folder", map[string]any{"path": "x"}},
		{"POST", base + "/move", map[string]any{"from": "inside.txt", "to": "y.txt"}},
		{"DELETE", base + "/entry?path=inside.txt", nil},
		{"PATCH", base, map[string]any{"name": "mine now"}},
		{"DELETE", base, nil},
		{"POST", base + "/copy-out", map[string]any{"path": "inside.txt", "parentId": "root"}},
		{"POST", "/api/external-storage/test", map[string]any{"id": id, "config": map[string]any{"url": dav.url(), "username": "u"}}},
	}
	for _, route := range routes {
		f.expect(404, f.call(stranger, route.method, route.path, route.body))
		f.expect(401, f.call(0, route.method, route.path, route.body))
		if got := f.expect(403, f.call(owner, route.method, route.path, route.body, "X-Platform", "desktop")); got["code"] != "PHONE_REQUIRED" {
			t.Fatal(got)
		}
	}
	if got := f.expect(200, f.call(stranger, "GET", "/api/external-storage", nil)); len(got["items"].([]any)) != 0 {
		t.Fatal(got)
	}
	f.expect(200, f.call(owner, "GET", "/api/external-storage", nil, "X-Platform", "ios"))
	if _, found := dav.get("/inside.txt"); !found {
		t.Fatal("file touched")
	}
}

func TestPathsThatClimbAreRefused(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	dav.put(t, "/inside.txt", "inside")
	base := "/api/external-storage/" + f.connectDAV(owner, dav, "NAS")
	if got, err := normalizePath("/a//b/"); err != nil || got != "a/b" {
		t.Fatal(got, err)
	}
	for _, bad := range []any{"..", "../x", "a/../..", "a/./b", `a\..\b`, "a\x00b", []any{"a"}, strings.Repeat("x", 3000)} {
		if _, err := normalizePath(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	for _, bad := range []string{"../outside.txt", "a/../../outside.txt", ".."} {
		query := "?path=" + url.QueryEscape(bad)
		for _, got := range []*httptest.ResponseRecorder{
			f.call(owner, "GET", base+"/list"+query, nil),
			f.call(owner, "GET", base+"/file"+query, nil),
			f.call(owner, "PUT", base+"/file"+query, []byte("x")),
			f.call(owner, "POST", base+"/folder", map[string]any{"path": bad}),
			f.call(owner, "POST", base+"/move", map[string]any{"from": "inside.txt", "to": bad}),
			f.call(owner, "DELETE", base+"/entry"+query, nil),
		} {
			if body := f.expect(400, got); body["code"] != "INVALID_PATH" {
				t.Fatal(bad, body)
			}
		}
	}
	f.expect(400, f.call(owner, "GET", base+"/list?path=a&path=b", nil))
	f.expect(400, f.call(owner, "DELETE", base+"/entry?path=", nil))
	f.expect(400, f.call(owner, "POST", base+"/move", map[string]any{"from": "inside.txt", "to": "inside.txt/x"}))
}

func TestPrivateAndMetadataAddressesAreRefused(t *testing.T) {
	dav := newDAV(t)
	f := newFixture(t, Options{})
	blocked := []string{
		dav.url(), "http://127.0.0.1:1/", "http://localhost/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://10.0.0.5/",
		"http://192.168.1.2/", "http://172.16.0.1/", "http://169.254.169.254/latest/meta-data/", "http://0.0.0.0/", "http://100.100.100.200/",
	}
	test := func(user int64, provider string, config map[string]any) map[string]any {
		return f.expect(200, f.call(user, "POST", "/api/external-storage/test", map[string]any{"name": "x", "provider": provider, "config": config}))
	}
	for _, address := range blocked {
		if got := test(owner, "webdav", map[string]any{"url": address, "username": "u", "password": "p"}); got["code"] != "EXTERNAL_HOST_BLOCKED" {
			t.Fatal(address, got)
		}
	}
	if got := test(owner, "s3", map[string]any{"endpoint": "http://127.0.0.1:9", "bucket": "bkt", "accessKeyId": "a", "secretAccessKey": "s"}); got["code"] != "EXTERNAL_HOST_BLOCKED" {
		t.Fatal(got)
	}
	if got := test(owner, "sftp", map[string]any{"host": "10.1.2.3", "username": "u", "password": "p"}); got["code"] != "EXTERNAL_HOST_BLOCKED" {
		t.Fatal(got)
	}
	f.expect(422, f.call(owner, "POST", "/api/external-storage", dav.body("NAS")))
	// Admins may reach the home network, but never metadata addresses.
	id := f.connectDAV(admin, dav, "LAN")
	for _, address := range []string{"http://169.254.169.254/", "http://[::ffff:169.254.169.254]/", "http://[fd00:ec2::254]/"} {
		if got := test(admin, "webdav", map[string]any{"url": address, "username": "u", "password": "p"}); got["code"] != "EXTERNAL_HOST_BLOCKED" {
			t.Fatal(address, got)
		}
	}
	// A stored connection is checked again when used.
	if _, err := f.db.W.Exec(`UPDATE users SET is_admin=0`); err != nil {
		t.Fatal(err)
	}
	f.handler.isAdmin = func(context.Context, int64) (bool, error) { return false, nil }
	if got := f.expect(403, f.call(admin, "GET", "/api/external-storage/"+id+"/list", nil)); got["code"] != "EXTERNAL_HOST_BLOCKED" {
		t.Fatal(got)
	}
}

// A name that answers a public address at check time and a private one when
// dialled must not get through: the guard resolves for the connection itself.
func TestRebindingNameIsRefusedAtConnectTime(t *testing.T) {
	var mu sync.Mutex
	lookups := 0
	f := newFixture(t, Options{Resolve: func(context.Context, string) ([]netip.Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}})
	got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", map[string]any{"provider": "webdav", "config": map[string]any{"url": "http://rebind.test:1/", "username": "u", "password": "p"}}))
	if got["code"] != "EXTERNAL_HOST_BLOCKED" || lookups < 2 {
		t.Fatal(got, lookups)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true}), newDAV(t)
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer target.Close()
	dav.redirect = target.URL + "/stolen/"
	got := f.expect(200, f.call(owner, "POST", "/api/external-storage/test", dav.body("x")))
	if got["ok"] != false || got["code"] != "EXTERNAL_REDIRECT" || hits != 0 {
		t.Fatal(got, hits)
	}
}

func TestUploadLimitAndSandboxedDownloads(t *testing.T) {
	f, dav := newFixture(t, Options{AllowPrivate: true, MaxUploadBytes: 5}), newDAV(t)
	base := "/api/external-storage/" + f.connectDAV(owner, dav, "NAS")
	if got := f.expect(413, f.call(owner, "PUT", base+"/file?path=big.bin", make([]byte, 100))); got["code"] != "FILE_TOO_LARGE" {
		t.Fatal(got)
	}
	// Without a declared size the limit still holds.
	server := httptest.NewServer(f.mux)
	defer server.Close()
	request, _ := http.NewRequest("PUT", server.URL+base+"/file?path=chunked.bin", struct{ io.Reader }{bytes.NewReader(make([]byte, 100))})
	request.Header.Set("X-User", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 413 {
		t.Fatal(response.StatusCode)
	}
	if _, found := dav.get("/big.bin"); found {
		t.Fatal("oversized upload kept")
	}
	dav.put(t, "/page.html", "<script>alert(1)</script>")
	dav.put(t, "/pic.svg", "<svg/>")
	dav.put(t, "/pic.png", "png")
	for name, disposition := range map[string]string{"page.html": "attachment", "pic.svg": "attachment", "pic.png": "inline"} {
		got := f.call(owner, "GET", base+"/file?path="+name, nil)
		if got.Code != 200 || !strings.HasPrefix(got.Header().Get("Content-Disposition"), disposition) ||
			!strings.HasPrefix(got.Header().Get("Content-Security-Policy"), "sandbox") || got.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(name, got.Code, got.Header())
		}
	}
	if got := f.call(owner, "GET", base+"/file?path=pic.png&download=1", nil); !strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(got.Header())
	}
	if got := f.call(owner, "HEAD", base+"/file?path=pic.png", nil); got.Code != 200 || got.Body.Len() != 0 || got.Header().Get("Content-Length") != "3" {
		t.Fatal(got.Code, got.Header())
	}
}

func TestConnectionAttemptsAreRateLimited(t *testing.T) {
	f := newFixture(t, Options{})
	var last *httptest.ResponseRecorder
	for range 31 {
		last = f.call(owner, "POST", "/api/external-storage/test", map[string]any{"provider": "nope"})
	}
	if got := f.expect(429, last); got["code"] != "RATE_LIMITED" {
		t.Fatal(got)
	}
}
