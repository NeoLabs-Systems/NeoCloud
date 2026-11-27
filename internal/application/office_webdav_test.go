package application_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

func TestDocumentEditorWiring(t *testing.T) {
	editor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<wopi-discovery><action name="edit" ext="docx" urlsrc="https://frame.example.test/cool.html?"/></wopi-discovery>`)
	}))
	defer editor.Close()
	server := apptest.Start(t, nil)
	admin := server.Register(t, "alice")
	settings := map[string]any{"url": editor.URL, "enabled": true}

	// Admin settings take a browser or the phone app, never a desktop token.
	admin.Device(t, "macos").Expect(t, 403, "PUT", "/api/admin/config/office", settings)
	admin.Device(t, "webdav").Expect(t, 403, "PUT", "/api/admin/config/office", settings)
	admin.Device(t, "ios").Expect(t, 200, "PUT", "/api/admin/config/office", settings)

	policy := server.Visitor(t).Expect(t, 200, "GET", "/api/health", nil).Header.Get("Content-Security-Policy")
	for _, directive := range []string{"frame-src 'self' blob: " + editor.URL + " https://frame.example.test", "form-action 'self' " + editor.URL} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("policy %q is missing %q", policy, directive)
		}
	}

	doc := admin.Upload(t, "root", "report.docx", "one")
	session := admin.Call(t, 200, "POST", "/api/files/"+doc+"/edit-session", nil)
	// The editor server sends its own Origin and no cookie.
	wopi := "/wopi/files/" + doc
	query := "?access_token=" + session["accessToken"].(string)
	visitor := server.Visitor(t)
	for _, call := range [][]string{{wopi + query, "LOCK"}, {wopi + "/contents" + query, "PUT"}} {
		reply := visitor.Do(t, "POST", call[0], []byte("two"), "X-WOPI-Override", call[1], "X-WOPI-Lock", "l1", "Content-Type", "application/json")
		if reply.Status != 200 {
			t.Fatalf("%s = %d: %s", call[1], reply.Status, reply.Body)
		}
	}
	if got := visitor.Do(t, "GET", wopi+"/contents"+query, nil); string(got.Body) != "two" {
		t.Fatalf("content = %q", got.Body)
	}

	// A browser session is not a WebDAV sign-in, and WebDAV answers OPTIONS itself.
	if reply := admin.Do(t, "PROPFIND", "/remote.php/webdav/", nil, "Depth", "0"); reply.Status != 401 {
		t.Fatalf("PROPFIND with a session = %d", reply.Status)
	}
	if reply := visitor.Do(t, "OPTIONS", "/remote.php/webdav/", nil); reply.Status != 200 || reply.Header.Get("DAV") != "1, 2" {
		t.Fatalf("OPTIONS = %d %v", reply.Status, reply.Header)
	}
}
