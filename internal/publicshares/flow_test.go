package publicshares_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

type world struct {
	server                                           *apptest.Server
	alice, bob                                       *apptest.Client
	shared, sub, outside, inSub, inShared, secretDoc string
}

func setup(t *testing.T, env map[string]string) world {
	t.Helper()
	w := world{server: apptest.Start(t, env)}
	w.alice, w.bob = w.server.Register(t, "alice"), w.server.Register(t, "bob")
	w.shared = w.alice.Folder(t, "root", "Shared")
	w.sub = w.alice.Folder(t, w.shared, "Sub")
	w.inShared = w.alice.Upload(t, w.shared, "readme.txt", "shared readme")
	w.inSub = w.alice.Upload(t, w.sub, "a.txt", "alpha content")
	w.outside = w.alice.Folder(t, "root", "Outside")
	w.secretDoc = w.alice.Upload(t, w.outside, "secret.txt", "not for the link")
	return w
}

type link struct {
	id    string
	token string
	base  string
	data  map[string]any
}

func (w world) share(t *testing.T, owner *apptest.Client, body map[string]any) link {
	t.Helper()
	data := owner.Call(t, 201, "POST", "/api/shares", body)
	token := data["token"].(string)
	return link{id: strconv.Itoa(int(data["id"].(float64))), token: token, base: "/api/public/shares/" + token, data: data}
}

func code(t *testing.T, c *apptest.Client, status int, method, path string, body any) string {
	t.Helper()
	value, _ := c.Call(t, status, method, path, body)["code"].(string)
	return value
}

func put(t *testing.T, c *apptest.Client, l link, name, folderID, content string, headers ...string) apptest.Response {
	t.Helper()
	query := url.Values{"name": {name}}
	if folderID != "" {
		query.Set("folderId", folderID)
	}
	return c.Do(t, "PUT", l.base+"/upload?"+query.Encode(), []byte(content), append([]string{"Content-Type", "application/octet-stream"}, headers...)...)
}

func status(t *testing.T, r apptest.Response, want int, wantCode string) apptest.Response {
	t.Helper()
	if r.Status != want || wantCode != "" && !strings.Contains(string(r.Body), `"`+wantCode+`"`) {
		t.Fatalf("status=%d want %d %s: %s", r.Status, want, wantCode, r.Body)
	}
	return r
}

func zipNames(t *testing.T, data []byte) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

func TestVisitorsBrowseOnlyTheSharedSubtree(t *testing.T) {
	w := setup(t, nil)
	share := w.share(t, w.alice, map[string]any{"nodeId": w.shared})
	if len(share.token) < 30 || share.data["url"] != w.server.HTTP.URL+"/s/"+share.token || share.data["nodeType"] != "folder" || share.data["hasPassword"] != false {
		t.Fatalf("created %v", share.data)
	}
	visitor := w.server.Visitor(t)
	top := visitor.Call(t, 200, "GET", share.base, nil)
	meta := top["share"].(map[string]any)
	if meta["name"] != "Shared" || meta["sharedBy"] != "alice" || meta["mode"] != "view" || meta["host"] != strings.TrimPrefix(w.server.HTTP.URL, "http://") {
		t.Fatalf("meta %v", meta)
	}
	if apptest.Names(top["path"], "name") != "Shared" || apptest.Names(top["items"], "name") != "Sub,readme.txt" {
		t.Fatalf("listing %v", top)
	}
	if _, leaked := top["items"].([]any)[0].(map[string]any)["parentId"]; leaked {
		t.Fatal("items name their parent")
	}
	inner := visitor.Call(t, 200, "GET", share.base+"?folderId="+w.sub, nil)
	if apptest.Names(inner["path"], "name") != "Shared,Sub" || apptest.Names(inner["items"], "name") != "a.txt" {
		t.Fatalf("inner %v", inner)
	}
	content := visitor.Expect(t, 200, "GET", share.base+"/files/"+w.inSub+"/content", nil)
	if string(content.Body) != "alpha content" || !strings.Contains(content.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("content %q %v", content.Body, content.Header)
	}
	partial := status(t, visitor.Do(t, "GET", share.base+"/files/"+w.inSub+"/content", nil, "Range", "bytes=6-"), 206, "")
	if string(partial.Body) != "content" {
		t.Fatalf("range %q", partial.Body)
	}

	bobFile := w.bob.Upload(t, "root", "bob.txt", "bob")
	var aliceRoot string
	if err := w.server.DB.QueryRow(`SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, w.alice.ID).Scan(&aliceRoot); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{w.outside, aliceRoot, bobFile, "root", "not-a-uuid"} {
		visitor.Expect(t, 404, "GET", share.base+"?folderId="+id, nil)
		visitor.Expect(t, 404, "GET", share.base+"/zip?folderId="+id, nil)
	}
	for _, id := range []string{w.secretDoc, bobFile} {
		visitor.Expect(t, 404, "GET", share.base+"/files/"+id+"/content", nil)
	}
	visitor.Expect(t, 400, "GET", share.base+"/files/"+w.sub+"/content", nil)

	all := visitor.Expect(t, 200, "GET", share.base+"/zip", nil)
	if all.Header.Get("Content-Type") != "application/zip" || zipNames(t, all.Body) != "Shared/ Shared/Sub/ Shared/Sub/a.txt Shared/readme.txt" {
		t.Fatalf("zip %s", zipNames(t, all.Body))
	}
	if names := zipNames(t, visitor.Expect(t, 200, "GET", share.base+"/zip?folderId="+w.sub, nil).Body); names != "Sub/ Sub/a.txt" {
		t.Fatalf("sub zip %s", names)
	}
	// ZIPs count, inline views don't.
	if listed := w.alice.Call(t, 200, "GET", "/api/shares", nil)["shares"].([]any)[0].(map[string]any); listed["downloadCount"] != 2.0 {
		t.Fatalf("count %v", listed["downloadCount"])
	}

	// Trashed nodes disappear from the link.
	w.alice.Expect(t, 200, "DELETE", "/api/nodes/"+w.sub, nil)
	visitor.Expect(t, 404, "GET", share.base+"?folderId="+w.sub, nil)
	visitor.Expect(t, 404, "GET", share.base+"/files/"+w.inSub+"/content", nil)
	if names := apptest.Names(visitor.Call(t, 200, "GET", share.base, nil)["items"], "name"); names != "readme.txt" {
		t.Fatalf("after trash %s", names)
	}
	w.alice.Expect(t, 200, "POST", "/api/trash/"+w.sub+"/restore", nil)
	w.alice.Expect(t, 200, "DELETE", "/api/nodes/"+w.shared, nil)
	visitor.Expect(t, 404, "GET", share.base, nil)
	visitor.Expect(t, 404, "GET", share.base+"/zip", nil)
	w.alice.Expect(t, 200, "POST", "/api/trash/"+w.shared+"/restore", nil)
	visitor.Expect(t, 200, "GET", share.base, nil)

	// A file link serves just that file.
	file := w.share(t, w.alice, map[string]any{"nodeId": w.inShared})
	one := visitor.Call(t, 200, "GET", file.base, nil)
	if one["share"].(map[string]any)["type"] != "file" || one["node"].(map[string]any)["name"] != "readme.txt" || len(one["items"].([]any)) != 0 {
		t.Fatalf("file link %v", one)
	}
	downloaded := visitor.Expect(t, 200, "GET", file.base+"/files/"+w.inShared+"/content?download=1", nil)
	if string(downloaded.Body) != "shared readme" || !strings.HasPrefix(downloaded.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download %q %v", downloaded.Body, downloaded.Header)
	}
	visitor.Expect(t, 404, "GET", file.base+"/files/"+w.inSub+"/content", nil)
	visitor.Expect(t, 404, "GET", file.base+"?folderId="+w.shared, nil)

	// The public page itself.
	page := status(t, visitor.Do(t, "GET", "/s/some-token", nil, "Accept", "text/html"), 200, "")
	if !strings.HasPrefix(page.Header.Get("Content-Type"), "text/html") || page.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(string(page.Body), "/js/share.js") {
		t.Fatalf("share page %v", page.Header)
	}
	for _, asset := range []string{"/js/share.js", "/css/styles.css", "/css/fonts.css", "/fonts/inter-latin.woff2"} {
		visitor.Expect(t, 200, "GET", asset, nil)
	}
}

func TestPasswordLinksNeedTheUnlockCookie(t *testing.T) {
	w := setup(t, nil)
	share := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "password": "open sesame"})
	other := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "password": "open sesame"})
	visitor := w.server.Visitor(t)
	if got := code(t, visitor, 401, "GET", share.base, nil); got != "SHARE_PASSWORD_REQUIRED" {
		t.Fatal(got)
	}
	visitor.Expect(t, 401, "GET", share.base+"/files/"+w.inShared+"/content", nil)
	visitor.Expect(t, 401, "GET", share.base+"/zip", nil)
	if got := code(t, visitor, 401, "POST", share.base+"/unlock", map[string]any{"password": "nope"}); got != "SHARE_PASSWORD_INVALID" {
		t.Fatal(got)
	}
	unlocked := visitor.Expect(t, 200, "POST", share.base+"/unlock", map[string]any{"password": "open sesame"})
	cookie := unlocked.Header.Get("Set-Cookie")
	for _, part := range []string{"Path=" + share.base, "HttpOnly", "Max-Age=3600", "SameSite=Strict"} {
		if !strings.Contains(cookie, part) {
			t.Fatalf("cookie %q lacks %s", cookie, part)
		}
	}
	visitor.Expect(t, 200, "GET", share.base, nil)
	visitor.Expect(t, 200, "GET", share.base+"/files/"+w.inShared+"/content", nil)
	visitor.Expect(t, 401, "GET", other.base, nil)
	value := strings.TrimPrefix(strings.Split(cookie, ";")[0], "nc_share_unlock=")
	expires, signature, _ := strings.Cut(value, ".")
	later, _ := strconv.ParseInt(expires, 10, 64)
	anonymous := w.server.Visitor(t)
	for _, forged := range []string{expires + ".forged", fmt.Sprintf("%d.%s", later+1_000_000_000, signature)} {
		status(t, anonymous.Do(t, "GET", share.base, nil, "Cookie", "nc_share_unlock="+forged), 401, "")
	}
	status(t, anonymous.Do(t, "GET", other.base, nil, "Cookie", "nc_share_unlock="+value), 401, "")
	status(t, anonymous.Do(t, "GET", share.base, nil, "Cookie", "nc_share_unlock="+value), 200, "")

	// A new or removed password signs visitors out; the URL stays.
	changed := w.alice.Call(t, 200, "PATCH", "/api/shares/"+share.id, map[string]any{"password": "another one"})
	if changed["token"] != share.token || changed["hasPassword"] != true {
		t.Fatalf("changed %v", changed)
	}
	visitor.Expect(t, 401, "GET", share.base, nil)
	opened := w.alice.Call(t, 200, "PATCH", "/api/shares/"+share.id, map[string]any{"password": nil, "expiresAt": nil, "note": nil})
	if opened["hasPassword"] != false || opened["expiresAt"] != nil || opened["note"] != nil {
		t.Fatalf("opened %v", opened)
	}
	visitor.Expect(t, 200, "GET", share.base, nil)

	// Expiry.
	expiring := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "expiresAt": time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")})
	visitor.Expect(t, 200, "GET", expiring.base, nil)
	if _, err := w.server.DB.Exec(`UPDATE shares SET expires_at=datetime('now','-1 minute') WHERE token=?`, expiring.token); err != nil {
		t.Fatal(err)
	}
	if got := code(t, visitor, 410, "GET", expiring.base, nil); got != "SHARE_EXPIRED" {
		t.Fatal(got)
	}
	visitor.Expect(t, 410, "GET", expiring.base+"/files/"+w.inShared+"/content", nil)
	visitor.Expect(t, 410, "POST", expiring.base+"/unlock", map[string]any{})
	listed := w.alice.Call(t, 200, "GET", "/api/shares", nil)["shares"].([]any)[0].(map[string]any)
	if listed["expired"] != true {
		t.Fatalf("listed %v", listed)
	}
	w.alice.Expect(t, 200, "DELETE", "/api/shares/"+expiring.id, nil)
	if got := code(t, visitor, 410, "GET", expiring.base, nil); got != "SHARE_REVOKED" {
		t.Fatal(got)
	}
	w.alice.Expect(t, 404, "PATCH", "/api/shares/"+expiring.id, map[string]any{"note": "x"})
}

func TestOwnerSideValidationAndIsolation(t *testing.T) {
	w := setup(t, nil)
	for body, want := range map[*map[string]any]string{
		{"nodeId": w.shared, "mode": "everything"}:                  "INVALID_MODE",
		{"nodeId": w.inShared, "mode": "upload_only"}:               "NOT_A_FOLDER",
		{"nodeId": w.inShared, "mode": "view_upload"}:               "NOT_A_FOLDER",
		{"nodeId": w.shared, "password": "seven77"}:                 "PASSWORD_TOO_SHORT",
		{"nodeId": w.shared, "note": strings.Repeat("x", 501)}:      "NOTE_TOO_LONG",
		{"nodeId": w.shared, "maxDownloads": 0}:                     "INVALID_MAX_DOWNLOADS",
		{"nodeId": w.shared, "maxDownloads": 2.5}:                   "INVALID_MAX_DOWNLOADS",
		{"nodeId": w.shared, "maxUploadBytes": -5}:                  "INVALID_SIZE",
		{"nodeId": w.shared, "expiresAt": "2001-01-01"}:             "INVALID_EXPIRY",
		{"nodeId": w.shared, "password": strings.Repeat("a", 201)}:  "PASSWORD_TOO_LONG",
		{"nodeId": w.shared, "note": []any{"x"}}:                    "INVALID_NOTE",
		{"nodeId": w.shared, "expiresAt": "next week, I think"}:     "INVALID_EXPIRY",
		{"nodeId": w.shared, "maxDownloads": 1000001, "note": "ok"}: "INVALID_MAX_DOWNLOADS",
	} {
		if got := code(t, w.alice, 400, "POST", "/api/shares", *body); got != want {
			t.Errorf("%v = %s, want %s", *body, got, want)
		}
	}
	legacy := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "allowUpload": true, "note": "  Photos from Saturday  ", "password": ""})
	if legacy.data["mode"] != "view_upload" || legacy.data["allowUpload"] != true || legacy.data["note"] != "Photos from Saturday" || legacy.data["hasPassword"] != false || legacy.data["maxDownloads"] != nil || legacy.data["exhausted"] != false {
		t.Fatalf("legacy %v", legacy.data)
	}
	file := w.share(t, w.alice, map[string]any{"nodeId": w.inShared})
	if got := code(t, w.alice, 400, "PATCH", "/api/shares/"+file.id, map[string]any{"mode": "upload_only"}); got != "NOT_A_FOLDER" {
		t.Fatal(got)
	}
	w.alice.Expect(t, 400, "PATCH", "/api/shares/abc", map[string]any{})

	// Another account's ids behave as missing, and desktop and phone tokens
	// manage links like a browser.
	for _, id := range []string{w.shared, w.inShared, "missing"} {
		w.bob.Expect(t, 404, "POST", "/api/shares", map[string]any{"nodeId": id})
		if got := w.bob.Call(t, 200, "GET", "/api/shares?nodeId="+id, nil)["shares"].([]any); len(got) != 0 {
			t.Fatalf("bob lists %v", got)
		}
	}
	w.bob.Expect(t, 404, "PATCH", "/api/shares/"+legacy.id, map[string]any{"password": "take over now"})
	w.bob.Expect(t, 404, "DELETE", "/api/shares/"+legacy.id, nil)
	if got := w.bob.Call(t, 200, "GET", "/api/shares", nil)["shares"].([]any); len(got) != 0 {
		t.Fatalf("bob lists %v", got)
	}
	var revoked, note any
	if err := w.server.DB.QueryRow(`SELECT revoked_at,note FROM shares WHERE token=?`, legacy.token).Scan(&revoked, &note); err != nil || revoked != nil || note != "Photos from Saturday" {
		t.Fatalf("changed by another account: %v %v %v", revoked, note, err)
	}
	anonymous := w.server.Visitor(t)
	anonymous.Expect(t, 401, "GET", "/api/shares", nil)
	anonymous.Expect(t, 401, "POST", "/api/shares", map[string]any{"nodeId": w.shared})
	anonymous.Expect(t, 401, "PATCH", "/api/shares/"+legacy.id, map[string]any{})
	anonymous.Expect(t, 401, "DELETE", "/api/shares/"+legacy.id, nil)
	w.alice.Device(t, "linux").Expect(t, 200, "GET", "/api/shares", nil)
	w.alice.Device(t, "webdav").Expect(t, 401, "GET", "/api/shares", nil)

	// Bob's own link never reaches Alice's nodes through its node id slots.
	public := w.bob.Folder(t, "root", "Public")
	bobShare := w.share(t, w.bob, map[string]any{"nodeId": public, "allowUpload": true})
	anonymous.Expect(t, 404, "GET", bobShare.base+"?folderId="+w.shared, nil)
	anonymous.Expect(t, 404, "GET", bobShare.base+"/files/"+w.inShared+"/content", nil)
	anonymous.Expect(t, 404, "GET", bobShare.base+"/zip?folderId="+w.shared, nil)
	status(t, put(t, anonymous, bobShare, "x.txt", w.shared, "x"), 404, "NOT_FOUND")

	edited := w.alice.Call(t, 200, "PATCH", "/api/shares/"+legacy.id, map[string]any{"mode": "view_upload", "note": "Hello", "expiresAt": time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano), "maxDownloads": 5, "maxUploadBytes": 1024})
	if edited["note"] != "Hello" || edited["maxDownloads"] != 5.0 || edited["maxUploadBytes"] != 1024.0 || edited["expiresAt"] == nil {
		t.Fatalf("edited %v", edited)
	}
	forNode := w.alice.Call(t, 200, "GET", "/api/shares?nodeId="+w.shared, nil)["shares"].([]any)
	if len(forNode) != 1 || forNode[0].(map[string]any)["nodeId"] != w.shared {
		t.Fatalf("for node %v", forNode)
	}
}

func TestUploadsIntoLinks(t *testing.T) {
	w := setup(t, nil)
	visitor := w.server.Visitor(t)
	closed := w.share(t, w.alice, map[string]any{"nodeId": w.shared})
	status(t, put(t, visitor, closed, "drop.txt", "", "x"), 403, "UPLOAD_NOT_ALLOWED")

	open := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "allowUpload": true})
	first := status(t, put(t, visitor, open, "readme.txt", "", "from a visitor", "Content-Type", "application/json"), 201, "").JSON(t)
	if first["name"] != "readme (1).txt" || first["mime"] != "text/plain" {
		t.Fatalf("first %v", first)
	}
	if original := w.alice.Expect(t, 200, "GET", "/api/files/"+w.inShared+"/content", nil); string(original.Body) != "shared readme" {
		t.Fatalf("original %q", original.Body)
	}
	nested := status(t, put(t, visitor, open, "into-sub.txt", w.sub, "nested"), 201, "").JSON(t)
	items := w.alice.Call(t, 200, "GET", "/api/folders/"+w.sub+"/children", nil)["items"]
	if !strings.Contains(apptest.Names(items, "id"), nested["id"].(string)) {
		t.Fatal("not in the sub folder")
	}
	content := w.alice.Expect(t, 200, "GET", "/api/files/"+nested["id"].(string)+"/content", nil)
	if string(content.Body) != "nested" {
		t.Fatalf("stored %q", content.Body)
	}
	status(t, put(t, visitor, open, "escape.txt", w.outside, "x"), 404, "")
	status(t, put(t, visitor, open, "../x", "", "x"), 400, "NAME_INVALID")
	// The sync journal and the activity log see visitors' files.
	changes := w.alice.Expect(t, 200, "GET", "/api/sync/changes?cursor=0", nil)
	if !strings.Contains(string(changes.Body), nested["id"].(string)) {
		t.Fatal("upload missing from the sync journal")
	}
	activity := w.alice.Expect(t, 200, "GET", "/api/activity?nodeId="+nested["id"].(string), nil)
	if !strings.Contains(string(activity.Body), `"kind":"upload"`) {
		t.Fatalf("activity %s", activity.Body)
	}
	// A resent upload with its checksum is the file already there.
	digest := "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb" // sha256("a")
	sent := status(t, put(t, visitor, open, "once.txt", "", "a", "X-Content-SHA256", digest), 201, "").JSON(t)
	again := status(t, put(t, visitor, open, "once.txt", "", "a", "X-Content-SHA256", digest), 201, "").JSON(t)
	if sent["id"] != again["id"] {
		t.Fatalf("resent made a second file: %v %v", sent, again)
	}

	// Against the owner's quota.
	inbox := w.bob.Folder(t, "root", "Inbox")
	bobShare := w.share(t, w.bob, map[string]any{"nodeId": inbox, "allowUpload": true})
	if _, err := w.server.DB.Exec(`UPDATE users SET quota_bytes=1024 WHERE id=?`, w.bob.ID); err != nil {
		t.Fatal(err)
	}
	status(t, put(t, visitor, bobShare, "big.bin", "", string(apptest.Random(4096))), 413, "QUOTA_EXCEEDED")
	status(t, put(t, visitor, bobShare, "small.bin", "", string(apptest.Random(100))), 201, "")

	// Upload-only: takes files, shows and serves nothing.
	only := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "mode": "upload_only", "maxUploadBytes": 100})
	meta := visitor.Call(t, 200, "GET", only.base, nil)
	if meta["share"].(map[string]any)["uploadBytesLeft"] != 100.0 || len(meta["items"].([]any)) != 0 || apptest.Names(meta["path"], "name") != "Shared" {
		t.Fatalf("inbox %v", meta)
	}
	for _, path := range []string{"?folderId=" + w.sub, "/files/" + w.inShared + "/content", "/files/" + w.inShared + "/content?download=1", "/zip"} {
		if got := code(t, visitor, 403, "GET", only.base+path, nil); got != "SHARE_UPLOAD_ONLY" {
			t.Fatalf("%s %s", path, got)
		}
	}
	dropped := status(t, put(t, visitor, only, "readme.txt", "", "dropped in"), 201, "")
	if string(dropped.Body) != `{"name":"readme.txt","size":10}` {
		t.Fatalf("answer reveals %s", dropped.Body)
	}
	status(t, put(t, visitor, only, "x.txt", w.sub, "x"), 403, "SHARE_UPLOAD_ONLY")
	if left := visitor.Call(t, 200, "GET", only.base, nil)["share"].(map[string]any)["uploadBytesLeft"]; left != 90.0 {
		t.Fatalf("left %v", left)
	}

	capped := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "mode": "view_upload", "maxUploadBytes": 10})
	status(t, put(t, visitor, capped, "eight.bin", "", "12345678"), 201, "")
	status(t, put(t, visitor, capped, "five.bin", "", "12345"), 413, "SHARE_UPLOAD_LIMIT")
	status(t, put(t, visitor, capped, "two.bin", "", "12"), 201, "")
	status(t, put(t, visitor, capped, "one.bin", "", "1"), 413, "SHARE_UPLOAD_LIMIT")

	// Vaults are never shared, not even inside a shared folder.
	vault := w.alice.Vault(t, w.shared, "Locked", "")
	folderID := vault["folderId"].(string)
	cipher := w.alice.Upload(t, folderID, "Y2lwaGVy", "NCV1 ciphertext")
	for _, id := range []string{folderID, cipher} {
		if got := code(t, w.alice, 409, "POST", "/api/shares", map[string]any{"nodeId": id}); got != "VAULT_NOT_SHAREABLE" {
			t.Fatal(got)
		}
	}
	if names := apptest.Names(visitor.Call(t, 200, "GET", closed.base, nil)["items"], "name"); strings.Contains(names, "Locked") {
		t.Fatalf("vault listed: %s", names)
	}
	visitor.Expect(t, 404, "GET", closed.base+"?folderId="+folderID, nil)
	visitor.Expect(t, 404, "GET", closed.base+"/zip?folderId="+folderID, nil)
	visitor.Expect(t, 404, "GET", closed.base+"/files/"+cipher+"/content", nil)
	if names := zipNames(t, visitor.Expect(t, 200, "GET", closed.base+"/zip", nil).Body); strings.Contains(names, "Locked") || strings.Contains(names, "Y2lwaGVy") {
		t.Fatalf("vault in zip: %s", names)
	}
}

// A raw request whose body is sent by hand, so a test can hold it half-sent.
func rawUpload(t *testing.T, w world, path, headers string) net.Conn {
	t.Helper()
	host := strings.TrimPrefix(w.server.HTTP.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err = fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\nContent-Type: application/octet-stream\r\n%s\r\n\r\n", path, host, headers); err != nil {
		t.Fatal(err)
	}
	return conn
}

func answer(t *testing.T, conn net.Conn) string {
	t.Helper()
	data, err := io.ReadAll(conn)
	if err != nil && len(data) == 0 {
		t.Fatal(err)
	}
	return string(data)
}

func TestParallelUploadsCannotPassTheCap(t *testing.T) {
	w := setup(t, nil)
	share := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "mode": "view_upload", "maxUploadBytes": 1000})
	uploaded := func() (n int) {
		if err := w.server.DB.QueryRow(`SELECT uploaded_bytes FROM shares WHERE token=?`, share.token).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	visitor := w.server.Visitor(t)
	first := rawUpload(t, w, share.base+"/upload?name=first.bin", "Content-Length: 900")
	_, _ = first.Write([]byte{1})
	for i := 0; i < 200 && uploaded() != 900; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if uploaded() != 900 {
		t.Fatalf("reserved %d", uploaded())
	}
	status(t, put(t, visitor, share, "second.bin", "", strings.Repeat("2", 900)), 413, "SHARE_UPLOAD_LIMIT")
	_, _ = first.Write(bytes.Repeat([]byte{1}, 899))
	if got := answer(t, first); !strings.Contains(got, "201 Created") || uploaded() != 900 {
		t.Fatalf("first %q uploaded=%d", got, uploaded())
	}
	chunked := rawUpload(t, w, share.base+"/upload?name=chunked.bin", "Transfer-Encoding: chunked")
	_, _ = chunked.Write([]byte("a\r\n0123456789\r\n0\r\n\r\n"))
	if got := answer(t, chunked); !strings.Contains(got, "411") || !strings.Contains(got, "LENGTH_REQUIRED") {
		t.Fatalf("chunked %q", got)
	}
	status(t, put(t, visitor, share, "broken.bin", "", strings.Repeat("4", 50), "X-Content-SHA256", strings.Repeat("0", 64)), 422, "CHECKSUM_MISMATCH")
	if uploaded() != 900 {
		t.Fatalf("reservation kept: %d", uploaded())
	}
	// A visitor who gives up mid-way gets the reservation back too.
	gone := rawUpload(t, w, share.base+"/upload?name=gone.bin", "Content-Length: 80")
	_, _ = gone.Write([]byte("partial"))
	for i := 0; i < 200 && uploaded() != 980; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	_ = gone.Close()
	for i := 0; i < 200 && uploaded() != 900; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	status(t, put(t, visitor, share, "last.bin", "", strings.Repeat("5", 100)), 201, "")
	if uploaded() != 1000 {
		t.Fatalf("uploaded %d", uploaded())
	}
}

func TestDownloadLimits(t *testing.T) {
	w := setup(t, nil)
	share := w.share(t, w.alice, map[string]any{"nodeId": w.inShared, "maxDownloads": 2})
	file := share.base + "/files/" + w.inShared + "/content"
	anonymous := func() *apptest.Client { return w.server.Visitor(t) }
	anonymous().Expect(t, 200, "GET", file, nil)
	anonymous().Expect(t, 200, "GET", file, nil)
	status(t, anonymous().Do(t, http.MethodHead, file+"?download=1", nil), 200, "")
	count := func() (n int) {
		if err := w.server.DB.QueryRow(`SELECT download_count FROM shares WHERE token=?`, share.token).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 0 {
		t.Fatalf("inline views and HEAD count: %d", count())
	}
	anonymous().Expect(t, 200, "GET", file+"?download=1", nil)
	visitor := anonymous()
	status(t, visitor.Do(t, "GET", file+"?download=1", nil, "Range", "bytes=0-3"), 206, "")
	rest := status(t, visitor.Do(t, "GET", file+"?download=1", nil, "Range", "bytes=4-"), 206, "")
	if string(rest.Body) != "ed readme" || count() != 2 {
		t.Fatalf("resume %q count=%d", rest.Body, count())
	}
	for _, ranged := range []string{"bytes=-1000000", "bytes=00-", "bytes=0-"} {
		status(t, visitor.Do(t, "GET", file+"?download=1", nil, "Range", ranged), 410, "SHARE_EXHAUSTED")
	}
	status(t, anonymous().Do(t, "GET", file+"?download=1", nil, "Range", "bytes=1-"), 410, "")
	for _, path := range []string{file + "?download=1", file, share.base} {
		if got := code(t, anonymous(), 410, "GET", path, nil); got != "SHARE_EXHAUSTED" {
			t.Fatal(got)
		}
	}
	listed := w.alice.Call(t, 200, "GET", "/api/shares", nil)["shares"].([]any)[0].(map[string]any)
	if listed["exhausted"] != true || listed["downloadCount"] != 2.0 {
		t.Fatalf("listed %v", listed)
	}
	if got := w.alice.Call(t, 200, "GET", "/api/shares?nodeId="+w.inShared, nil)["shares"].([]any); len(got) != 0 {
		t.Fatalf("used up link offered: %v", got)
	}
	folder := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "maxDownloads": 1})
	anonymous().Expect(t, 200, "GET", folder.base+"/zip", nil)
	if got := code(t, anonymous(), 410, "GET", folder.base+"/zip", nil); got != "SHARE_EXHAUSTED" {
		t.Fatal(got)
	}
	raised := w.alice.Call(t, 200, "PATCH", "/api/shares/"+share.id, map[string]any{"maxDownloads": nil})
	if raised["exhausted"] != false {
		t.Fatalf("raised %v", raised)
	}
	anonymous().Expect(t, 200, "GET", share.base, nil)
}

func TestWrongPasswordsSlowThenLockALink(t *testing.T) {
	w := setup(t, nil)
	share := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "password": "correct horse"})
	visitor := w.server.Visitor(t)
	guess := func(password string, want int) apptest.Response {
		return visitor.Expect(t, want, "POST", share.base+"/unlock", map[string]any{"password": password})
	}
	failures := func() (n int) {
		if err := w.server.DB.QueryRow(`SELECT COUNT(*) FROM share_unlock_failures`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	age := func(by time.Duration) {
		if _, err := w.server.DB.Exec(`UPDATE share_unlock_failures SET failed_at_ms=failed_at_ms-?`, by.Milliseconds()); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		guess(fmt.Sprint("wrong ", i), 401)
	}
	// Counted from now, however long the password checks took.
	if _, err := w.server.DB.Exec(`UPDATE share_unlock_failures SET failed_at_ms=?`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	slowed := guess("wrong 5", 429)
	body := slowed.JSON(t)
	retry := int(body["retryAfter"].(float64))
	if body["code"] != "SHARE_LOCKED" || retry < 1 || retry > 2 || slowed.Header.Get("Retry-After") != strconv.Itoa(retry) || failures() != 5 {
		t.Fatalf("slowed %v %v failures=%d", body, slowed.Header, failures())
	}
	age(time.Minute)
	guess("wrong 6", 401)
	for i := 0; i < 4; i++ {
		if _, err := w.server.DB.Exec(`INSERT INTO share_unlock_failures(share_id,failed_at_ms) VALUES(?,?)`, share.id, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if locked := guess("correct horse", 429).JSON(t); locked["retryAfter"].(float64) <= 25*60 {
		t.Fatalf("locked %v", locked)
	}
	other := w.share(t, w.alice, map[string]any{"nodeId": w.sub, "password": "correct horse"})
	visitor.Expect(t, 200, "POST", other.base+"/unlock", map[string]any{"password": "correct horse"})
	age(31 * time.Minute)
	guess("correct horse", 200)
	if failures() != 0 {
		t.Fatalf("failures %d", failures())
	}
	// The address itself is limited too (20 tries in 15 minutes).
	limited := false
	for i := 0; i < 20 && !limited; i++ {
		limited = visitor.Do(t, "POST", other.base+"/unlock", []byte(`{"password":"correct horse"}`), "Content-Type", "application/json").Status == 429
	}
	if !limited {
		t.Fatal("unlock is not rate limited per address")
	}
}

func TestShareUploadsRespectTheAccountNodeLimit(t *testing.T) {
	w := setup(t, map[string]string{"NEOCLOUD_MAX_NODES": "8"})
	share := w.share(t, w.alice, map[string]any{"nodeId": w.shared, "allowUpload": true})
	visitor := w.server.Visitor(t)
	// The setup made seven of alice's eight nodes, the root included.
	status(t, put(t, visitor, share, "eighth.txt", "", "x"), 201, "")
	status(t, put(t, visitor, share, "ninth.txt", "", "x"), 507, "NODE_LIMIT")
	if got := code(t, w.alice, 507, "POST", "/api/vaults", func() map[string]any { b := apptest.KeyParams(); b["name"] = "V"; return b }()); got != "NODE_LIMIT" {
		t.Fatal(got)
	}
}
