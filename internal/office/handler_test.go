package office

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

const discoveryDocument = `<?xml version="1.0" encoding="utf-8"?>
<wopi-discovery>
  <net-zone name="external-http">
    <app name="writer">
      <action name="edit" ext="docx" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="edit" ext="odt" default="true" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="view" ext="docx" urlsrc="https://office.example.com/browser/abc/cool.html?view=1&amp;"/>
    </app>
    <app name="calc">
      <action name="edit" ext="XLSX" urlsrc="https://office.example.com/browser/abc/cool.html?"/>
      <action name="edit" ext="__proto__" urlsrc="https://office.example.com/x"/>
      <action name="edit" ext="ods" urlsrc="javascript:alert(1)"/>
      <action name="convert-to" ext="pdf" urlsrc="https://office.example.com/convert"/>
    </app>
    <app name="image"><action name="view" ext="png" urlsrc="https://office.example.com/img?&lt;ui=UI_LLCC&amp;&gt;"/></app>
  </net-zone>
</wopi-discovery>`

func TestParseDiscovery(t *testing.T) {
	actions, err := parseDiscovery([]byte(discoveryDocument))
	if err != nil {
		t.Fatal(err)
	}
	page := "https://office.example.com/browser/abc/cool.html?"
	if actions["docx"].Edit != page || actions["docx"].View != page+"view=1&" || actions["odt"].Edit != page || actions["xlsx"].Edit != page {
		t.Fatalf("actions = %+v", actions)
	}
	if len(actions) != 4 {
		t.Fatalf("unexpected extensions: %+v", actions)
	}
	for _, text := range []string{"<html>nginx</html>", ""} {
		if _, err = parseDiscovery([]byte(text)); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	source := "https://host/files/1"
	encoded := url.QueryEscape(source)
	for urlsrc, want := range map[string]string{
		"https://o/cool.html?":      "https://o/cool.html?WOPISrc=" + encoded,
		"https://o/cool.html?a=1&":  "https://o/cool.html?a=1&WOPISrc=" + encoded,
		"https://o/cool.html?a=1":   "https://o/cool.html?a=1&WOPISrc=" + encoded,
		"https://o/cool.html":       "https://o/cool.html?WOPISrc=" + encoded,
		"https://o/e?<ui=UI_LLCC&>": "https://o/e?WOPISrc=" + encoded,
	} {
		if got := editorPageURL(urlsrc, source); got != want {
			t.Errorf("%s -> %s, want %s", urlsrc, got, want)
		}
	}
}

const (
	alice = int64(1) // the admin
	bob   = int64(2)
)

type fixture struct {
	t       *testing.T
	db      *dbx.DB
	office  *Handler
	mux     *http.ServeMux
	envFile string
	editor  *httptest.Server
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := dbx.Open(context.Background(), filepath.Join(dir, "office.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(context.Background(), db, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, now: time.Now(), envFile: filepath.Join(dir, ".env")}
	for id, name := range map[int64]string{alice: "alice", bob: "bob"} {
		wrapped, err := keys.WrapDEK(id, bytes.Repeat([]byte{byte(id)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`INSERT INTO users(id,username,password,wrapped_dek,is_admin) VALUES(?,?,'x',?,?)`, id, name, wrapped, id == alice)
	}
	if err = os.WriteFile(f.envFile, []byte("# keep this\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.editor = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hosting/discovery":
			_, _ = io.WriteString(w, `<wopi-discovery><net-zone name="external-http"><app name="writer">
				<action name="edit" ext="docx" urlsrc="`+f.editor.URL+`/browser/x/cool.html?"/>
				<action name="view" ext="docx" urlsrc="`+f.editor.URL+`/browser/x/cool.html?view=1&amp;"/>
				</app></net-zone></wopi-discovery>`)
		case "/moved/hosting/discovery":
			http.Redirect(w, r, "/hosting/discovery", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.editor.Close)
	fileHandler := files.New(files.Options{DB: db.W, Keys: keys, BlobDir: filepath.Join(dir, "blobs"), UploadTmpDir: filepath.Join(dir, "tmp"),
		PreviewDir: filepath.Join(dir, "previews"), MaxNodes: 100, DefaultQuotaBytes: 1 << 20, VersionRetention: 10})
	f.office, err = New(Options{
		DB: db.W, Files: fileHandler, EnvFile: f.envFile, SessionSecret: strings.Repeat("s", 48), Now: func() time.Time { return f.now },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			identity, ok := requestauth.IdentityFromContext(r.Context())
			return Principal{UserID: identity.UserID}, ok
		},
		ResolveAdmin: func(r *http.Request) (int64, bool) {
			identity, _ := requestauth.IdentityFromContext(r.Context())
			return identity.UserID, identity.UserID == alice
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mux = http.NewServeMux()
	fileHandler.Routes(f.mux)
	f.office.Routes(f.mux)
	return f
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.W.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

// call serves one request as userID (0 for none, like the editor server).
func (f *fixture) call(status int, userID int64, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if userID > 0 {
		request = request.WithContext(requestauth.WithIdentity(request.Context(), requestauth.RequestIdentity{UserID: userID}))
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	if recorder.Code != status {
		f.t.Fatalf("%s %s = %d, want %d: %s", method, target, recorder.Code, status, recorder.Body.String())
	}
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, recorder.Body.String())
	}
	return body
}

func (f *fixture) upload(userID int64, name, content, parent string) string {
	f.t.Helper()
	target := "/api/upload?name=" + url.QueryEscape(name) + "&parentId=" + parent
	return decodeBody(f.t, f.call(201, userID, "PUT", target, content))["id"].(string)
}

func (f *fixture) enable() {
	f.t.Helper()
	f.call(200, alice, "PUT", "/api/admin/config/office", `{"enabled":true,"url":"`+f.editor.URL+`"}`)
}

type session struct {
	URL      string `json:"editorUrl"`
	Token    string `json:"accessToken"`
	TTL      int64  `json:"accessTokenTtl"`
	CanWrite bool   `json:"canWrite"`
}

func (f *fixture) session(userID int64, nodeID string) session {
	f.t.Helper()
	var result session
	if err := json.Unmarshal(f.call(200, userID, "POST", "/api/files/"+nodeID+"/edit-session", "").Body.Bytes(), &result); err != nil {
		f.t.Fatal(err)
	}
	return result
}

func wopiURL(nodeID, suffix, token string) string {
	return "/wopi/files/" + nodeID + suffix + "?access_token=" + url.QueryEscape(token)
}

func (f *fixture) lock(status int, nodeID, token, operation, value string, headers ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	headers = append([]string{"X-WOPI-Override", operation, "X-WOPI-Lock", value}, headers...)
	return f.call(status, 0, "POST", wopiURL(nodeID, "", token), "", headers...)
}

func (f *fixture) put(status int, nodeID, token, content, lock string, headers ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	headers = append([]string{"X-WOPI-Override", "PUT", "X-WOPI-Lock", lock}, headers...)
	return f.call(status, 0, "POST", wopiURL(nodeID, "/contents", token), content, headers...)
}

func held(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	if values, present := recorder.Header()["X-Wopi-Lock"]; !present || values[0] != want {
		t.Fatalf("X-WOPI-Lock = %v, want %q", values, want)
	}
}

func TestAdminSettings(t *testing.T) {
	f := newFixture(t)
	if body := f.call(200, bob, "GET", "/api/office/config", "").Body.String(); !strings.Contains(body, `"enabled":false`) || !strings.Contains(body, `"extensions":[]`) {
		t.Fatalf("config = %s", body)
	}
	f.call(401, 0, "GET", "/api/office/config", "")
	f.call(401, 0, "POST", "/api/files/x/edit-session", "")
	f.call(403, 0, "GET", "/api/admin/config/office", "")
	f.call(403, bob, "PUT", "/api/admin/config/office", `{"url":"`+f.editor.URL+`","enabled":true}`)
	doc := f.upload(alice, "report.docx", "version one", "root")
	f.call(503, alice, "POST", "/api/files/"+doc+"/edit-session", "")
	f.call(409, alice, "POST", "/api/admin/config/office/check", "")
	if body := f.call(400, alice, "PUT", "/api/admin/config/office", `{"enabled":true}`).Body.String(); !strings.Contains(body, "EDITOR_URL_REQUIRED") {
		t.Fatalf("missing url = %s", body)
	}
	f.call(400, alice, "PUT", "/api/admin/config/office", `{"url":"ftp://x","enabled":true}`)
	f.call(400, alice, "PUT", "/api/admin/config/office", `{"enabled":"yes"}`)

	refused := f.call(422, alice, "PUT", "/api/admin/config/office", `{"url":"http://127.0.0.1:1","enabled":true}`).Body.String()
	if !strings.Contains(refused, "EDITOR_UNREACHABLE") || !strings.Contains(refused, "could not connect") {
		t.Fatalf("unreachable = %s", refused)
	}
	f.call(422, alice, "PUT", "/api/admin/config/office", `{"url":"`+f.editor.URL+`/missing","enabled":true}`)
	if body := f.call(422, alice, "PUT", "/api/admin/config/office", `{"url":"`+f.editor.URL+`/moved","enabled":true}`).Body.String(); !strings.Contains(body, "redirects") {
		t.Fatalf("redirect = %s", body)
	}
	if origins := f.office.EditorOrigins(); len(origins) != 0 {
		t.Fatalf("origins while off = %v", origins)
	}

	saved := f.call(200, alice, "PUT", "/api/admin/config/office", `{"url":"`+f.editor.URL+`/","hostUrl":"https://cloud.test/base/","enabled":true}`).Body.String()
	if !strings.Contains(saved, `"state":"ok"`) || !strings.Contains(saved, `"extensions":["docx"]`) {
		t.Fatalf("saved = %s", saved)
	}
	env, err := os.ReadFile(f.envFile)
	if err != nil || !strings.Contains(string(env), "NEOCLOUD_OFFICE_EDITOR_ENABLED=true") || !strings.Contains(string(env), "NEOCLOUD_OFFICE_HOST_URL=https://cloud.test/base") || !strings.Contains(string(env), "# keep this") {
		t.Fatalf("env = %q, %v", env, err)
	}
	var detail string
	if err = f.db.W.QueryRow(`SELECT detail_json FROM access_audit_log WHERE action='office.config' AND actor_user_id=1`).Scan(&detail); err != nil || detail != `{"enabled":true}` {
		t.Fatalf("audit = %q, %v", detail, err)
	}
	if origins := f.office.EditorOrigins(); len(origins) != 1 || origins[0] != f.editor.URL {
		t.Fatalf("origins = %v", origins)
	}
	if config := f.call(200, bob, "GET", "/api/office/config", "").Body.String(); !strings.Contains(config, `"extensions":["docx"]`) {
		t.Fatalf("config = %s", config)
	}
	f.call(200, alice, "POST", "/api/admin/config/office/check", "")
	open := f.session(alice, doc)
	if !strings.HasSuffix(open.URL, "WOPISrc="+url.QueryEscape("https://cloud.test/base/wopi/files/"+doc)) {
		t.Fatalf("editor URL = %s", open.URL)
	}

	off := decodeBody(t, f.call(200, alice, "PUT", "/api/admin/config/office", `{"enabled":false}`))
	if off["status"].(map[string]any)["state"] != "off" {
		t.Fatalf("off = %v", off)
	}
	f.call(503, alice, "POST", "/api/files/"+doc+"/edit-session", "")
	f.call(401, 0, "GET", wopiURL(doc, "", open.Token), "")
	if origins := f.office.EditorOrigins(); len(origins) != 0 {
		t.Fatalf("origins after switching off = %v", origins)
	}
}

func TestEditingFlow(t *testing.T) {
	f := newFixture(t)
	f.enable()
	doc := f.upload(alice, "report.docx", "version one", "root")
	open := f.session(alice, doc)
	if !strings.HasPrefix(open.URL, f.editor.URL+"/browser/x/cool.html?WOPISrc=") || !open.CanWrite || open.TTL <= f.now.Add(9*time.Hour).UnixMilli() {
		t.Fatalf("session = %+v", open)
	}
	f.call(415, alice, "POST", "/api/files/"+f.upload(alice, "notes.txt", "x", "root")+"/edit-session", "")
	f.call(404, bob, "POST", "/api/files/"+doc+"/edit-session", "")

	info := decodeBody(t, f.call(200, 0, "GET", wopiURL(doc, "", open.Token), ""))
	if info["BaseFileName"] != "report.docx" || info["Size"].(float64) != 11 || info["Version"] != "1" || info["UserCanWrite"] != true ||
		info["SupportsLocks"] != true || info["UserFriendlyName"] != "alice" || info["UserId"] != "1" {
		t.Fatalf("info = %v", info)
	}
	if _, err := time.Parse(time.RFC3339, info["LastModifiedTime"].(string)); err != nil {
		t.Fatal(err)
	}
	content := f.call(200, 0, "GET", "/wopi/files/"+doc+"/contents", "", "Authorization", "Bearer "+open.Token)
	if content.Body.String() != "version one" || content.Header().Get("X-WOPI-ItemVersion") != "1" {
		t.Fatalf("content = %q %v", content.Body.String(), content.Header())
	}

	token := open.Token
	held(t, f.lock(200, doc, token, "GET_LOCK", ""), "")
	held(t, f.lock(409, doc, token, "UNLOCK", "a"), "")
	f.lock(200, doc, token, "LOCK", "lock-a")
	f.lock(200, doc, token, "LOCK", "lock-a")
	held(t, f.lock(409, doc, token, "LOCK", "lock-b"), "lock-a")
	held(t, f.lock(200, doc, token, "GET_LOCK", ""), "lock-a")
	held(t, f.lock(409, doc, token, "REFRESH_LOCK", "lock-b"), "lock-a")
	f.lock(200, doc, token, "REFRESH_LOCK", "lock-a")
	held(t, f.lock(409, doc, token, "LOCK", "lock-c", "X-WOPI-OldLock", "wrong"), "lock-a")
	f.lock(200, doc, token, "LOCK", "lock-c", "X-WOPI-OldLock", "lock-a")
	held(t, f.lock(409, doc, token, "UNLOCK", "lock-a"), "lock-c")
	f.lock(200, doc, token, "UNLOCK", "lock-c")
	f.lock(400, doc, token, "LOCK", "")
	f.lock(501, doc, token, "RENAME_FILE", "x")

	held(t, f.put(409, doc, token, "sneaky", ""), "")
	f.lock(200, doc, token, "LOCK", "edit-1")
	held(t, f.put(409, doc, token, "wrong lock", "other"), "edit-1")
	stale := f.put(409, doc, token, "stale", "edit-1", "X-COOL-WOPI-Timestamp", "2001-01-01T00:00:00.000Z")
	if !strings.Contains(stale.Body.String(), "1010") {
		t.Fatalf("stale = %s", stale.Body.String())
	}
	saved := f.put(200, doc, token, "version two!", "edit-1", "X-COOL-WOPI-Timestamp", info["LastModifiedTime"].(string))
	if saved.Header().Get("X-WOPI-ItemVersion") != "2" || decodeBody(t, saved)["LastModifiedTime"] == "" {
		t.Fatalf("saved = %v %s", saved.Header(), saved.Body.String())
	}
	if got := f.call(200, 0, "GET", wopiURL(doc, "/contents", token), "").Body.String(); got != "version two!" {
		t.Fatalf("content = %q", got)
	}
	var version, versions, author int
	var operation string
	if err := f.db.W.QueryRow(`SELECT version,modified_by,(SELECT COUNT(*) FROM file_versions WHERE node_id=n.id),(SELECT op FROM changes WHERE node_id=n.id ORDER BY seq DESC LIMIT 1) FROM nodes n WHERE id=?`, doc).Scan(&version, &author, &versions, &operation); err != nil {
		t.Fatal(err)
	}
	if version != 2 || versions != 1 || author != 1 || operation != "update" {
		t.Fatalf("version=%d versions=%d author=%d op=%s", version, versions, author, operation)
	}
	if again := f.put(200, doc, token, "version two!", "edit-1"); again.Header().Get("X-WOPI-ItemVersion") != "2" {
		t.Fatalf("unchanged content made version %s", again.Header().Get("X-WOPI-ItemVersion"))
	}
	f.lock(200, doc, token, "UNLOCK", "edit-1")

	// An empty file is a new document and may be written without a lock.
	empty := f.upload(alice, "new.docx", "", "root")
	f.put(200, empty, f.session(alice, empty).Token, "first words", "")
}

func TestTokensAndReadOnly(t *testing.T) {
	f := newFixture(t)
	f.enable()
	doc := f.upload(alice, "report.docx", "kept", "root")
	other := f.upload(alice, "other.docx", "o", "root")
	open := f.session(alice, doc)

	payload, _, _ := strings.Cut(open.Token, ".")
	_, signature, _ := strings.Cut(f.session(alice, other).Token, ".")
	f.now = f.now.Add(-11 * time.Hour)
	expired, _ := f.office.issueToken(alice, doc, true)
	f.now = f.now.Add(11 * time.Hour)
	for _, token := range []string{payload + "." + signature, expired, payload + ".AAAA", "garbage", ""} {
		f.call(401, 0, "GET", wopiURL(doc, "", token), "")
		f.put(401, doc, token, "x", "l")
	}
	f.call(401, 0, "GET", wopiURL(other, "", open.Token), "")

	foreign, _ := f.office.issueToken(bob, doc, true)
	f.call(404, 0, "GET", wopiURL(doc, "", foreign), "")
	f.put(404, doc, foreign, "hijack", "l")

	readOnly, _ := f.office.issueToken(alice, doc, false)
	if decodeBody(t, f.call(200, 0, "GET", wopiURL(doc, "", readOnly), ""))["UserCanWrite"] != false {
		t.Fatal("read-only token may write")
	}
	f.put(403, doc, readOnly, "nope", "l")
	f.lock(403, doc, readOnly, "LOCK", "l")
	if got := f.call(200, 0, "GET", wopiURL(doc, "/contents", readOnly), "").Body.String(); got != "kept" {
		t.Fatalf("content = %q", got)
	}
}

func TestSharesVaultsAndTrash(t *testing.T) {
	f := newFixture(t)
	f.enable()
	folder := decodeBody(t, f.call(201, alice, "POST", "/api/folders", `{"parentId":"root","name":"Team"}`))["id"].(string)
	plan := f.upload(alice, "plan.docx", "plan", folder)
	f.exec(`INSERT INTO user_shares(owner_id,grantee_id,node_id,permission,recipient) VALUES(1,2,?,'write','bob')`, folder)

	writer := f.session(bob, plan)
	if !writer.CanWrite {
		t.Fatal("write share can't edit")
	}
	f.lock(200, plan, writer.Token, "LOCK", "b1")
	f.put(200, plan, writer.Token, "plan by bob", "b1")
	var owner, author int
	if err := f.db.W.QueryRow(`SELECT user_id,modified_by FROM nodes WHERE id=?`, plan).Scan(&owner, &author); err != nil || owner != 1 || author != 2 {
		t.Fatalf("owner=%d author=%d err=%v", owner, author, err)
	}

	f.exec(`UPDATE user_shares SET permission='read' WHERE node_id=?`, folder)
	if f.session(bob, plan).CanWrite {
		t.Fatal("read share can edit")
	}
	if decodeBody(t, f.call(200, 0, "GET", wopiURL(plan, "", writer.Token), ""))["UserCanWrite"] != false {
		t.Fatal("the old write token still writes")
	}
	f.put(403, plan, writer.Token, "late", "b1")

	f.exec(`DELETE FROM user_shares WHERE node_id=?`, folder)
	f.call(404, 0, "GET", wopiURL(plan, "", writer.Token), "")
	f.call(404, 0, "GET", wopiURL(plan, "/contents", writer.Token), "")
	f.put(404, plan, writer.Token, "after revoke", "b1")
	f.call(404, bob, "POST", "/api/files/"+plan+"/edit-session", "")

	sealed := f.upload(alice, "secret.docx", "NCV1 ciphertext", folder)
	f.exec(`INSERT INTO vaults(id,user_id,folder_id,salt,iterations,wrapped_key) VALUES('v',1,?,'s',600000,'k')`, folder)
	f.exec(`UPDATE nodes SET vault_id='v' WHERE id IN (?,?)`, folder, sealed)
	token, _ := f.office.issueToken(alice, sealed, true)
	f.call(404, 0, "GET", wopiURL(sealed, "", token), "")
	f.call(404, alice, "POST", "/api/files/"+sealed+"/edit-session", "")

	gone := f.upload(alice, "gone.docx", "g", "root")
	last := f.session(alice, gone)
	f.call(200, 0, "GET", wopiURL(gone, "", last.Token), "")
	f.call(200, alice, "DELETE", "/api/nodes/"+gone, "")
	f.call(404, 0, "GET", wopiURL(gone, "", last.Token), "")
}

func TestWOPILocksAreAtomic(t *testing.T) {
	f := newFixture(t)
	f.enable()
	doc := f.upload(alice, "race.docx", "x", "root")
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.office.lockFile(context.Background(), doc, "lock-"+strconv.Itoa(i))
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("lock winners = %d", winners)
	}
	value, err := f.office.currentLock(context.Background(), doc)
	if err != nil || value == "" {
		t.Fatalf("lock = %q, %v", value, err)
	}
	f.now = f.now.Add(lockTTL + time.Second)
	if value, err = f.office.currentLock(context.Background(), doc); err != nil || value != "" {
		t.Fatalf("expired lock = %q, %v", value, err)
	}
}
