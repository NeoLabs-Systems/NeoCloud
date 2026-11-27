package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type davFixture struct {
	t      *testing.T
	db     *dbx.DB
	server *httptest.Server
}

const (
	davToken = "nc_alice_app_password"
	davBase  = "/dav/files/alice"
)

func newDAVFixture(t *testing.T) *davFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := dbx.Open(context.Background(), filepath.Join(dir, "dav.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(context.Background(), db, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	f := &davFixture{t: t, db: db}
	for id, name := range []string{"alice", "bob"} {
		wrapped, err := keys.WrapDEK(int64(id+1), bytes.Repeat([]byte{byte(id + 1)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`INSERT INTO users(id,username,password,wrapped_dek) VALUES(?,?,'x',?)`, id+1, name, wrapped)
		f.addToken(id+1, "webdav", "nc_"+name+"_app_password")
	}
	handler := files.New(files.Options{DB: db.W, Keys: keys, BlobDir: filepath.Join(dir, "blobs"), UploadTmpDir: filepath.Join(dir, "tmp"),
		PreviewDir: filepath.Join(dir, "previews"), MaxNodes: 200, MaxCopyNodes: 50, DefaultQuotaBytes: 1 << 30, VersionRetention: 10})
	dav := webdav.NewHandler(webdav.Options{
		Authenticate:  webdav.AppPasswords(db.W),
		Dispatch:      handler.DAVDispatch,
		AllowedOrigin: func(string) bool { return false },
	})
	f.server = httptest.NewServer(dav)
	t.Cleanup(f.server.Close)
	return f
}

func (f *davFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.W.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *davFixture) addToken(userID int, platform, token string) {
	sum := sha256.Sum256([]byte(token))
	f.exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, userID, platform, platform, hex.EncodeToString(sum[:]))
}

func (f *davFixture) integer(query string, args ...any) (value int) {
	f.t.Helper()
	if err := f.db.W.QueryRow(query, args...).Scan(&value); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return value
}

type davReply struct {
	status int
	header http.Header
	body   string
}

func (f *davFixture) send(method, path, body string, headers ...string) davReply {
	f.t.Helper()
	request, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	request.SetBasicAuth("alice", davToken)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return davReply{status: response.StatusCode, header: response.Header, body: string(data)}
}

func (f *davFixture) expect(status int, method, path, body string, headers ...string) davReply {
	f.t.Helper()
	reply := f.send(method, path, body, headers...)
	if reply.status != status {
		f.t.Fatalf("%s %s %v = %d, want %d: %s", method, path, headers, reply.status, status, reply.body)
	}
	return reply
}

func (f *davFixture) read(path string) string {
	f.t.Helper()
	return f.expect(200, "GET", path, "").body
}

func hrefs(xml string) []string {
	result := []string{}
	for _, match := range regexp.MustCompile(`<d:href>([^<]*)</d:href>`).FindAllStringSubmatch(xml, -1) {
		result = append(result, match[1])
	}
	return result
}

func contains(t *testing.T, body string, wanted ...string) {
	t.Helper()
	for _, want := range wanted {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func (f *davFixture) nodeID(name string) (id string) {
	f.t.Helper()
	if err := f.db.W.QueryRow(`SELECT id FROM nodes WHERE user_id=1 AND name=? AND trashed_at IS NULL`, name).Scan(&id); err != nil {
		f.t.Fatalf("node %s: %v", name, err)
	}
	return id
}

func (f *davFixture) versions(name string) int {
	return f.integer(`SELECT COUNT(*) FROM file_versions WHERE node_id=?`, f.nodeID(name))
}

// Every blob's reference count matches its users and used_bytes their sizes.
func (f *davFixture) assertStorageConsistent() {
	f.t.Helper()
	if wrong := f.integer(`SELECT COUNT(*) FROM blobs b WHERE b.ref_count<>(SELECT COUNT(*) FROM nodes WHERE blob_id=b.id)+(SELECT COUNT(*) FROM file_versions WHERE blob_id=b.id)`); wrong != 0 {
		f.t.Fatalf("%d blobs have a wrong reference count", wrong)
	}
	if f.integer(`SELECT used_bytes FROM users WHERE id=1`) != f.integer(`SELECT COALESCE(SUM(size),0) FROM blobs WHERE user_id=1`) {
		f.t.Fatal("used_bytes does not match the account's blobs")
	}
}

func TestWebDAVSignIn(t *testing.T) {
	f := newDAVFixture(t)
	options := f.expect(200, "OPTIONS", davBase+"/", "", "Authorization", "")
	contains(t, options.header.Get("Allow"), "PROPFIND", "LOCK")
	contains(t, options.header.Get("DAV"), "2")
	missing := f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", "")
	if !strings.HasPrefix(missing.header.Get("WWW-Authenticate"), `Basic realm="NeoCloud"`) {
		t.Fatalf("challenge = %q", missing.header.Get("WWW-Authenticate"))
	}
	basic := func(user, password string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	}
	f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", basic("alice", "nc_wrong"))
	f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", basic("bob", davToken))
	f.expect(207, "PROPFIND", davBase+"/", "", "Authorization", basic("ALICE", davToken), "Depth", "0")
	f.addToken(1, "lite", "nc_lite_sync_token")
	f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", basic("alice", "nc_lite_sync_token"))
	f.addToken(1, "webdav", "nc_spare_password")
	f.expect(207, "PROPFIND", davBase+"/", "", "Authorization", basic("alice", "nc_spare_password"), "Depth", "0")
	f.exec(`UPDATE device_tokens SET revoked_at=datetime('now') WHERE name='webdav' AND id=(SELECT MAX(id) FROM device_tokens WHERE platform='webdav')`)
	f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", basic("alice", "nc_spare_password"), "Depth", "0")

	f.expect(404, "PROPFIND", "/dav/files/bob/", "")
	f.expect(403, "PROPFIND", davBase+"/", "", "Origin", "https://evil.example")
	f.exec(`UPDATE users SET disabled_at=datetime('now') WHERE id=1`)
	f.expect(401, "PROPFIND", davBase+"/", "")
}

func TestWebDAVFailedSignInsAreLimitedPerUsername(t *testing.T) {
	f := newDAVFixture(t)
	bad := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:nc_bad"))
	for range 40 {
		f.expect(207, "PROPFIND", davBase+"/", "", "Depth", "0")
		f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", "")
	}
	for range 30 {
		f.expect(401, "PROPFIND", davBase+"/", "", "Authorization", bad)
	}
	limited := f.expect(429, "PROPFIND", davBase+"/", "", "Authorization", bad)
	contains(t, limited.body, "Too many")
	if limited.header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	f.expect(429, "PROPFIND", davBase+"/", "")
	f.expect(207, "PROPFIND", "/dav/files/bob/", "", "Depth", "0", "Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte("bob:nc_bob_app_password")))
}

func TestWebDAVReadAndWrite(t *testing.T) {
	f := newDAVFixture(t)
	content := "Hello WebDAV world"
	sum := sha256.Sum256([]byte(content))
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	created := f.expect(201, "PUT", davBase+"/hello.txt", content, "Content-Type", "application/json")
	if created.header.Get("ETag") != etag || created.header.Get("OC-FileId") == "" {
		t.Fatalf("PUT headers = %v", created.header)
	}
	got := f.expect(200, "GET", davBase+"/hello.txt", "")
	if got.body != content || got.header.Get("ETag") != etag || got.header.Get("Last-Modified") == "" || !strings.Contains(got.header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("GET = %q %v", got.body, got.header)
	}
	if head := f.expect(200, "HEAD", davBase+"/hello.txt", ""); head.header.Get("Content-Length") != "18" {
		t.Fatalf("HEAD length = %q", head.header.Get("Content-Length"))
	}
	part := f.expect(206, "GET", davBase+"/hello.txt", "", "Range", "bytes=6-11")
	if part.body != "WebDAV" || part.header.Get("Content-Range") != "bytes 6-11/18" {
		t.Fatalf("range = %q %q", part.body, part.header.Get("Content-Range"))
	}
	f.expect(416, "GET", davBase+"/hello.txt", "", "Range", "bytes=500-")
	f.expect(304, "GET", davBase+"/hello.txt", "", "If-None-Match", etag)
	f.expect(200, "GET", davBase+"/HELLO.TXT", "")
	f.expect(404, "GET", davBase+"/missing.txt", "")
	if f.integer(`SELECT COUNT(*) FROM changes WHERE user_id=1 AND op='create'`) == 0 || f.integer(`SELECT COUNT(*) FROM activity WHERE kind='upload'`) != 1 {
		t.Fatal("PUT wrote no journal or activity entry")
	}

	f.expect(201, "PUT", davBase+"/notes.md", "first")
	replaced := f.expect(204, "PUT", davBase+"/notes.md", "second", "X-OC-Mtime", "1700000000")
	if replaced.header.Get("X-OC-MTime") != "accepted" || f.versions("notes.md") != 1 {
		t.Fatalf("overwrite: header=%q versions=%d", replaced.header.Get("X-OC-MTime"), f.versions("notes.md"))
	}
	contains(t, f.expect(207, "PROPFIND", davBase+"/notes.md", "", "Depth", "0").body, "<d:getlastmodified>Tue, 14 Nov 2023 22:13:20 GMT</d:getlastmodified>")
	f.expect(412, "PUT", davBase+"/notes.md", "x", "If-None-Match", "*")
	f.expect(412, "PUT", davBase+"/notes.md", "x", "If-Match", `"0000"`)
	f.expect(412, "PUT", davBase+"/new.md", "x", "If-Match", "*")
	f.expect(409, "PUT", davBase+"/no/such/folder.txt", "x")
	f.expect(405, "PUT", davBase+"/", "x")

	name := "Grüße 日本 #1%.txt"
	encoded := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	f.expect(201, "PUT", davBase+"/"+encoded, "unicode")
	decomposed := "Gru%CC%88%C3%9Fe%20%E6%97%A5%E6%9C%AC%20%231%25.txt"
	if f.read(davBase+"/"+decomposed) != "unicode" {
		t.Fatal("decomposed spelling not found")
	}
	contains(t, f.expect(207, "PROPFIND", davBase+"/", "").body, "<d:href>"+davBase+"/"+encoded+"</d:href>")
	f.expect(400, "GET", davBase+"/%E0%A4", "")
	f.assertStorageConsistent()
}

func TestWebDAVProperties(t *testing.T) {
	f := newDAVFixture(t)
	f.expect(201, "PUT", davBase+"/hello.txt", "Hello WebDAV world")
	zero := f.expect(207, "PROPFIND", davBase+"/", "", "Depth", "0")
	if list := hrefs(zero.body); len(list) != 1 || list[0] != davBase+"/" {
		t.Fatalf("depth 0 hrefs = %v", list)
	}
	contains(t, zero.body, "<d:resourcetype><d:collection/></d:resourcetype>", "<d:displayname>alice</d:displayname>")
	contains(t, zero.header.Get("Content-Type"), "application/xml")
	one := f.expect(207, "PROPFIND", davBase+"/", "").body
	contains(t, one, "<d:href>"+davBase+"/hello.txt</d:href>", "<d:getcontentlength>18</d:getcontentlength>", "<d:getcontenttype>text/plain</d:getcontenttype>", "<d:resourcetype/>")
	if !regexp.MustCompile(`<d:getetag>&quot;[0-9a-f]{64}&quot;</d:getetag>`).MatchString(one) {
		t.Fatalf("no file etag in %s", one)
	}
	contains(t, f.expect(403, "PROPFIND", davBase+"/", "", "Depth", "infinity").body, "propfind-finite-depth")
	named := f.expect(207, "PROPFIND", davBase+"/", `<?xml version="1.0"?>
		<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns" xmlns:z="urn:x-test">
		<d:prop><d:quota-used-bytes/><d:quota-available-bytes/><oc:fileid/><oc:favorite/><oc:size/>
		<nc:has-preview/><z:custom/><d:getcontentlength/></d:prop></d:propfind>`, "Depth", "0").body
	contains(t, named, "<d:quota-used-bytes>18</d:quota-used-bytes>", "<oc:favorite>0</oc:favorite>", "<oc:size>18</oc:size>", "<nc:has-preview>false</nc:has-preview>",
		`<x:custom xmlns:x="urn:x-test"/><d:getcontentlength/></d:prop><d:status>HTTP/1.1 404 Not Found`)
	if !regexp.MustCompile(`<d:quota-available-bytes>\d+</d:quota-available-bytes>`).MatchString(named) || !regexp.MustCompile(`<oc:fileid>[0-9a-f-]{36}</oc:fileid>`).MatchString(named) {
		t.Fatalf("quota or fileid missing in %s", named)
	}
	contains(t, f.expect(207, "PROPFIND", davBase+"/hello.txt", `<propfind xmlns="DAV:"><propname/></propfind>`, "Depth", "0").body, "<d:getetag/>")
	f.expect(400, "PROPFIND", davBase+"/", `<d:propfind xmlns:d="DAV:"><d:prop>`)
	f.expect(400, "PROPFIND", davBase+"/", `<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaa">]><d:propfind xmlns:d="DAV:"><d:allprop/></d:propfind>`)
	f.expect(413, "PROPFIND", davBase+"/", strings.Repeat("a", webdav.MaxXMLBodyBytes+1))
	f.expect(404, "PROPFIND", davBase+"/missing/", "")

	patch := func(value string) string {
		return `<?xml version="1.0"?><d:propertyupdate xmlns:d="DAV:" xmlns:z="urn:schemas-microsoft-com:">
			<d:set><d:prop><d:getlastmodified>` + value + `</d:getlastmodified><z:Win32FileAttributes>00000020</z:Win32FileAttributes></d:prop></d:set>
			<d:remove><d:prop><z:Win32CreationTime/></d:prop></d:remove></d:propertyupdate>`
	}
	ok := f.expect(207, "PROPPATCH", davBase+"/hello.txt", patch("Wed, 01 Jan 2020 10:00:00 GMT")).body
	contains(t, ok, "<d:status>HTTP/1.1 200 OK</d:status>", `<x:Win32FileAttributes xmlns:x="urn:schemas-microsoft-com:"/>`)
	if strings.Contains(ok, "HTTP/1.1 4") {
		t.Fatalf("PROPPATCH failures in %s", ok)
	}
	contains(t, f.expect(207, "PROPFIND", davBase+"/hello.txt", "", "Depth", "0").body, "Wed, 01 Jan 2020 10:00:00 GMT")
	f.expect(207, "PROPPATCH", davBase+"/hello.txt", `<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:lastmodified>1600000000</d:lastmodified></d:prop></d:set></d:propertyupdate>`)
	contains(t, f.expect(207, "PROPFIND", davBase+"/hello.txt", "", "Depth", "0").body, "Sun, 13 Sep 2020 12:26:40 GMT")
	contains(t, f.expect(207, "PROPPATCH", davBase+"/hello.txt", patch("not a date")).body, "409 Conflict", "424 Failed Dependency")
	f.expect(404, "PROPPATCH", davBase+"/missing.txt", patch("Wed, 01 Jan 2020 10:00:00 GMT"))
}

func TestWebDAVTree(t *testing.T) {
	f := newDAVFixture(t)
	f.expect(201, "PUT", davBase+"/hello.txt", "Hello WebDAV world")
	f.expect(201, "PUT", davBase+"/notes.md", "notes")
	f.expect(201, "MKCOL", davBase+"/Docs", "")
	f.expect(201, "MKCOL", davBase+"/Docs/Sub", "")
	f.expect(405, "MKCOL", davBase+"/docs", "")
	f.expect(409, "MKCOL", davBase+"/Nope/Sub", "")
	f.expect(415, "MKCOL", davBase+"/WithBody", "<x/>")
	contains(t, f.expect(207, "PROPFIND", davBase+"/Docs/", "").body, davBase+"/Docs/Sub/</d:href>")

	f.expect(201, "PUT", davBase+"/Docs/a.txt", "A")
	f.expect(201, "MOVE", davBase+"/Docs/a.txt", "", "Destination", "http://localhost"+davBase+"/Docs/Sub/b.txt")
	f.expect(404, "GET", davBase+"/Docs/a.txt", "")
	if f.read(davBase+"/Docs/Sub/b.txt") != "A" {
		t.Fatal("moved content differs")
	}
	f.expect(403, "MOVE", davBase+"/Docs/Sub/b.txt", "", "Destination", davBase+"/Docs/Sub/b.txt")
	f.expect(201, "MOVE", davBase+"/Docs/Sub/b.txt", "", "Destination", davBase+"/Docs/Sub/B.txt")
	contains(t, f.expect(207, "PROPFIND", davBase+"/Docs/Sub/", "").body, davBase+"/Docs/Sub/B.txt</d:href>")
	f.expect(409, "MOVE", davBase+"/Docs", "", "Destination", davBase+"/Docs/Sub/Docs")
	f.expect(409, "MOVE", davBase+"/Docs/Sub/B.txt", "", "Destination", davBase+"/Nope/B.txt")
	f.expect(400, "MOVE", davBase+"/Docs/Sub/B.txt", "")
	f.expect(403, "MOVE", davBase+"/Docs/Sub/B.txt", "", "Destination", "/dav/files/bob/x.txt")
	// A folder replacing its own ancestor fails and changes nothing.
	f.expect(404, "MOVE", davBase+"/Docs/Sub", "", "Destination", davBase+"/Docs")
	f.expect(200, "GET", davBase+"/Docs/Sub/B.txt", "")

	// "Save to a temporary file, move it over" keeps the target's history.
	f.expect(201, "PUT", davBase+"/Docs/target.kdbx", "v1")
	target := f.nodeID("target.kdbx")
	f.expect(201, "PUT", davBase+"/Docs/target.kdbx.tmp", "v2", "X-OC-Mtime", "1600000000")
	f.expect(412, "MOVE", davBase+"/Docs/target.kdbx.tmp", "", "Destination", davBase+"/Docs/target.kdbx", "Overwrite", "F")
	f.expect(204, "MOVE", davBase+"/Docs/target.kdbx.tmp", "", "Destination", davBase+"/Docs/target.kdbx")
	if f.read(davBase+"/Docs/target.kdbx") != "v2" || f.nodeID("target.kdbx") != target || f.versions("target.kdbx") != 1 {
		t.Fatal("overwriting by MOVE lost the target's identity or history")
	}
	f.expect(404, "GET", davBase+"/Docs/target.kdbx.tmp", "")
	if f.integer(`SELECT COUNT(*) FROM nodes WHERE id=? AND modified_at='2020-09-13 12:26:40'`, target) != 1 {
		t.Fatal("the moved file's time was not kept")
	}
	f.assertStorageConsistent()

	f.expect(201, "MKCOL", davBase+"/Docs/Folder", "")
	f.expect(201, "PUT", davBase+"/Docs/plain.txt", "P")
	f.expect(204, "MOVE", davBase+"/Docs/plain.txt", "", "Destination", davBase+"/Docs/Folder")
	if f.read(davBase+"/Docs/Folder") != "P" || f.integer(`SELECT COUNT(*) FROM nodes WHERE name='Folder' AND type='folder' AND trashed_at IS NOT NULL`) != 1 {
		t.Fatal("the replaced folder did not go to the trash")
	}
	f.expect(201, "MOVE", davBase+"/Docs/Folder", "", "Destination", "/remote.php/webdav/Docs/plain.txt")

	f.expect(201, "COPY", davBase+"/Docs", "", "Destination", davBase+"/Docs%20Copy")
	if f.read(davBase+"/Docs%20Copy/Sub/B.txt") != "A" || f.read(davBase+"/Docs/Sub/B.txt") != "A" {
		t.Fatal("copied content differs")
	}
	f.expect(412, "COPY", davBase+"/Docs", "", "Destination", davBase+"/Docs%20Copy", "Overwrite", "F")
	f.expect(409, "COPY", davBase+"/Docs", "", "Destination", davBase+"/Docs/Sub/Again")
	f.expect(201, "COPY", davBase+"/Docs", "", "Destination", davBase+"/Shallow", "Depth", "0")
	if list := hrefs(f.expect(207, "PROPFIND", davBase+"/Shallow/", "").body); len(list) != 1 {
		t.Fatalf("shallow copy lists %v", list)
	}
	f.expect(204, "COPY", davBase+"/hello.txt", "", "Destination", davBase+"/notes.md")
	if f.read(davBase+"/notes.md") != "Hello WebDAV world" || f.versions("notes.md") != 1 {
		t.Fatal("COPY over a file did not add a version")
	}
	f.expect(403, "COPY", davBase+"/hello.txt", "", "Destination", davBase+"/hello.txt")
	f.assertStorageConsistent()

	f.expect(201, "PUT", davBase+"/bye.txt", "bye")
	f.expect(204, "DELETE", davBase+"/bye.txt", "")
	f.expect(404, "GET", davBase+"/bye.txt", "")
	f.expect(404, "DELETE", davBase+"/bye.txt", "")
	f.expect(403, "DELETE", davBase+"/", "")
	if f.integer(`SELECT COUNT(*) FROM nodes WHERE name='bye.txt' AND trashed_at IS NOT NULL`) != 1 {
		t.Fatal("DELETE did not trash")
	}

	legacy := f.expect(207, "PROPFIND", "/remote.php/webdav/", "").body
	contains(t, legacy, "<d:href>/remote.php/webdav/hello.txt</d:href>")
	nextcloud := f.expect(207, "PROPFIND", "/remote.php/dav/files/alice/", "", "Depth", "0")
	if list := hrefs(nextcloud.body); len(list) != 1 || list[0] != "/remote.php/dav/files/alice/" {
		t.Fatalf("hrefs = %v", list)
	}
	if known := f.expect(301, "PROPFIND", "/.well-known/webdav", ""); known.header.Get("Location") != "/remote.php/webdav/" {
		t.Fatalf("location = %q", known.header.Get("Location"))
	}
	contains(t, f.expect(405, "POST", davBase+"/hello.txt", "").header.Get("Allow"), "PROPFIND")
}

func TestWebDAVLocks(t *testing.T) {
	f := newDAVFixture(t)
	f.expect(201, "PUT", davBase+"/hello.txt", "hello")
	f.expect(201, "MKCOL", davBase+"/Docs", "")
	info := `<?xml version="1.0"?><d:lockinfo xmlns:d="DAV:"><d:lockscope><d:exclusive/></d:lockscope><d:locktype><d:write/></d:locktype><d:owner><d:href>Finder</d:href></d:owner></d:lockinfo>`
	locked := f.expect(200, "LOCK", davBase+"/hello.txt", info, "Timeout", "Second-600")
	token := locked.header.Get("Lock-Token")
	if !regexp.MustCompile(`^<opaquelocktoken:[0-9a-f-]{36}>$`).MatchString(token) {
		t.Fatalf("lock token = %q", token)
	}
	contains(t, locked.body, "<d:timeout>Second-600</d:timeout>", "<d:owner><d:href>Finder</d:href></d:owner>")
	contains(t, f.expect(423, "PUT", davBase+"/hello.txt", "blocked").body, "lock-token-submitted")
	f.expect(423, "DELETE", davBase+"/hello.txt", "")
	f.expect(423, "LOCK", davBase+"/hello.txt", info)
	f.expect(204, "PUT", davBase+"/hello.txt", "with token", "If", "("+token+")")
	contains(t, f.expect(207, "PROPFIND", davBase+"/hello.txt", "", "Depth", "0").body, strings.Trim(token, "<>"))
	contains(t, f.expect(200, "LOCK", davBase+"/hello.txt", "", "If", "("+token+")", "Timeout", "Second-60").body, "Second-60")
	f.expect(409, "UNLOCK", davBase+"/hello.txt", "", "Lock-Token", "<opaquelocktoken:nope>")
	f.expect(204, "UNLOCK", davBase+"/hello.txt", "", "Lock-Token", token)
	f.expect(204, "PUT", davBase+"/hello.txt", "free again")

	fresh := f.expect(201, "LOCK", davBase+"/Docs/new-doc.docx", info)
	if f.read(davBase+"/Docs/new-doc.docx") != "" {
		t.Fatal("a locked new file is not empty")
	}
	f.expect(204, "UNLOCK", davBase+"/Docs/new-doc.docx", "", "Lock-Token", fresh.header.Get("Lock-Token"))
	folder := f.expect(200, "LOCK", davBase+"/Docs", info, "Depth", "infinity")
	f.expect(423, "PUT", davBase+"/Docs/inside.txt", "x")
	f.expect(423, "MOVE", davBase+"/hello.txt", "", "Destination", davBase+"/Docs/hello.txt")
	f.expect(204, "UNLOCK", davBase+"/Docs", "", "Lock-Token", folder.header.Get("Lock-Token"))
	f.expect(409, "LOCK", davBase+"/Nope/x.txt", info)
}

func TestWebDAVLimitsAndVaults(t *testing.T) {
	f := newDAVFixture(t)
	f.expect(207, "PROPFIND", davBase+"/", "", "Depth", "0")
	f.exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) SELECT 'vault-folder',1,id,'folder','Tresor','e' FROM nodes WHERE user_id=1 AND parent_id IS NULL`)
	f.exec(`INSERT INTO vaults(id,user_id,folder_id,salt,iterations,wrapped_key) VALUES('vault-1',1,'vault-folder','s',600000,'k')`)
	f.exec(`UPDATE nodes SET vault_id='vault-1' WHERE id='vault-folder'`)
	if strings.Contains(f.expect(207, "PROPFIND", davBase+"/", "").body, "Tresor") {
		t.Fatal("vault folder listed")
	}
	f.expect(404, "PROPFIND", davBase+"/Tresor/", "")
	f.expect(409, "PUT", davBase+"/Tresor/leak.txt", "x")
	f.expect(409, "PUT", davBase+"/Tresor", "x")
	f.expect(409, "MKCOL", davBase+"/Tresor", "")
	f.expect(404, "DELETE", davBase+"/Tresor", "")
	f.expect(201, "PUT", davBase+"/plain.txt", "p")
	f.expect(409, "MOVE", davBase+"/plain.txt", "", "Destination", davBase+"/Tresor/plain.txt")

	f.expect(201, "MKCOL", davBase+"/Big", "")
	f.exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<60) INSERT INTO nodes(id,user_id,parent_id,type,name,etag)
		SELECT 'bulk-'||i,1,(SELECT id FROM nodes WHERE name='Big'),'folder','f'||i,'e' FROM n`)
	f.expect(413, "COPY", davBase+"/Big", "", "Destination", davBase+"/Big2")
	f.exec(`UPDATE users SET node_count=200 WHERE id=1`)
	f.expect(507, "MKCOL", davBase+"/viaDav", "")
	f.expect(507, "PUT", davBase+"/node.txt", "x")
	f.exec(`UPDATE users SET node_count=1, quota_bytes=10 WHERE id=1`)
	contains(t, f.expect(507, "PUT", davBase+"/big.bin", strings.Repeat("a", 100)).body, "Not enough storage")
	f.expect(507, "PUT", davBase+"/big2.bin", "a", "X-Expected-Entity-Length", "100")
	f.assertStorageConsistent()
}
