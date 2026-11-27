package webdav

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestParsePathAndDestination(t *testing.T) {
	target, err := ParsePath("/remote.php/dav/files/neo/a%20b/e%CC%81.txt")
	if err != nil {
		t.Fatal(err)
	}
	if target.Base != "/remote.php/dav/files/neo" || target.Username != "neo" || len(target.Segments) != 2 || target.Segments[0] != "a b" || target.Segments[1] != "é.txt" {
		t.Fatalf("unexpected target: %#v", target)
	}
	if Href(target.Base, target.Segments, false) != "/remote.php/dav/files/neo/a%20b/%C3%A9.txt" {
		t.Fatal("incorrect canonical href")
	}
	dest, err := ParseDestination("https://cloud.example/dav/files/neo/new%20name")
	if err != nil || dest.Username != "neo" || dest.Segments[0] != "new name" {
		t.Fatalf("destination: %#v, %v", dest, err)
	}
	for _, path := range []string{"/dav/files/neo/%2e%2e/x", "/dav/files/neo/a%2Fb", "/dav/files/neo/%zz"} {
		if _, err := ParsePath(path); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if _, err := ParseDestination("https://evil.example/no-dav"); err == nil {
		t.Fatal("accepted destination outside DAV")
	}
}

func TestBasicCredentials(t *testing.T) {
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte(" Neo:secret:with:colon"))
	credentials, ok := BasicCredentials(header)
	if !ok || credentials.Username != "Neo" || credentials.Password != "secret:with:colon" {
		t.Fatalf("got %#v, %v", credentials, ok)
	}
	if _, ok := BasicCredentials("Bearer token"); ok {
		t.Fatal("accepted non-Basic auth")
	}
}

func TestLockManagerDepthRefreshAndExpiry(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "locks.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := dbx.Migrate(ctx, db.W, schema.Steps()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO users(username,password) VALUES('locks','test')`); err != nil {
		t.Fatal(err)
	}
	manager := NewLockManager(db.W)
	created, ok, err := manager.Create(ctx, 1, Lock{Key: "folder", Href: "/dav/folder/", Depth: "infinity", Scope: "exclusive", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("lock creation failed")
	}
	if unlocked, err := manager.Unlocked(ctx, 1, "folder/child", nil, false); err != nil || unlocked {
		t.Fatal("child was not covered by infinity lock")
	}
	if unlocked, err := manager.Unlocked(ctx, 1, "folder/child", []string{created.Token}, false); err != nil || !unlocked {
		t.Fatal("submitted token did not unlock write")
	}
	if _, ok, err := manager.Create(ctx, 1, Lock{Key: "folder/child", Depth: "0", Scope: "exclusive"}); err != nil || ok {
		t.Fatal("created conflicting exclusive lock")
	}
	refreshed, ok, err := manager.Refresh(ctx, 1, "folder/child", []string{created.Token}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || refreshed.Timeout != 2*time.Minute {
		t.Fatal("lock refresh failed")
	}
	if removed, err := manager.Remove(ctx, 1, "folder/child", created.Token); err != nil || !removed {
		t.Fatal("unlock failed")
	}
	short, ok, err := manager.Create(ctx, 1, Lock{Key: "gone", Depth: "0", Scope: "shared", Timeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("short lock creation failed")
	}
	time.Sleep(3 * time.Millisecond)
	if locks, err := manager.All(ctx, 1); err != nil || len(Covering(locks, "gone")) != 0 {
		t.Fatal("expired lock remains active")
	}
	if removed, err := manager.Remove(ctx, 1, "gone", short.Token); err != nil || removed {
		t.Fatal("removed expired lock")
	}
}

func TestSeparateLockManagersSerializeAndShareLocks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "locks.db")
	db1, err := dbx.Open(ctx, path, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db1.Close() })
	if err := dbx.Migrate(ctx, db1.W, schema.Steps()); err != nil {
		t.Fatal(err)
	}
	if _, err := db1.W.Exec(`INSERT INTO users(username,password) VALUES('locks','test')`); err != nil {
		t.Fatal(err)
	}
	db2, err := dbx.Open(ctx, path, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	managers := []*LockManager{NewLockManager(db1.W), NewLockManager(db2.W)}
	start := make(chan struct{})
	type outcome struct {
		lock Lock
		ok   bool
		err  error
	}
	results := make(chan outcome, len(managers))
	var wg sync.WaitGroup
	for _, manager := range managers {
		wg.Add(1)
		go func(manager *LockManager) {
			defer wg.Done()
			<-start
			lock, ok, err := manager.Create(ctx, 1, Lock{Key: "shared-doc", Href: "/dav/shared-doc", Depth: "0", Scope: "exclusive", Timeout: time.Minute})
			results <- outcome{lock: lock, ok: ok, err: err}
		}(manager)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner Lock
	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent lock create: %v", result.err)
		}
		if result.ok {
			created++
			winner = result.lock
		}
	}
	if created != 1 {
		t.Fatalf("exclusive lock winners=%d, want 1", created)
	}
	visible, err := managers[1].All(ctx, 1)
	if err != nil || len(visible) != 1 || visible[0].Token != winner.Token {
		t.Fatalf("separate manager read locks=%#v, error=%v", visible, err)
	}
	if removed, err := managers[1].Remove(ctx, 1, "shared-doc", winner.Token); err != nil || !removed {
		t.Fatalf("separate manager unlock removed=%v, error=%v", removed, err)
	}
	if visible, err = managers[0].All(ctx, 1); err != nil || len(visible) != 0 {
		t.Fatalf("first manager still sees unlocked row %#v, error=%v", visible, err)
	}
	for range 2 {
		if _, ok, err := managers[0].Create(ctx, 1, Lock{Key: "shared-doc", Href: "/dav/shared-doc", Depth: "0", Scope: "shared", Timeout: time.Minute}); err != nil || !ok {
			t.Fatalf("compatible shared lock create ok=%v, error=%v", ok, err)
		}
	}
	visible, err = managers[1].All(ctx, 1)
	if err != nil || len(visible) != 2 {
		t.Fatalf("separate manager sees %d compatible shared locks, error=%v", len(visible), err)
	}
}

func TestXMLRejectsDirectivesBoundsBodiesAndStaysLinear(t *testing.T) {
	if _, err := ParseXML([]byte(`<!DOCTYPE x [<!ENTITY y "z">]><x>&y;</x>`)); err == nil {
		t.Fatal("accepted XML directive")
	}
	oversized := "<x>" + strings.Repeat("a", MaxXMLBodyBytes) + "</x>"
	chunked := httptest.NewRequest("PROPFIND", "/", strings.NewReader(oversized))
	chunked.ContentLength = -1
	if _, err := ReadBody(chunked); err == nil {
		t.Fatal("accepted oversized XML")
	}
	for _, body := range []string{
		"<a " + strings.Repeat("a", MaxXMLBodyBytes) + ">",
		`<a b="` + strings.Repeat("x", MaxXMLBodyBytes),
		"<a " + strings.Repeat(`b="c" `, MaxXMLBodyBytes/6) + ">",
		strings.Repeat("<a>", MaxXMLBodyBytes/3),
	} {
		started := time.Now()
		if _, err := ParseXML([]byte(body)); err == nil {
			t.Fatal("accepted a broken document")
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("parsing took %s", elapsed)
		}
	}
	root, err := ParseXML([]byte(`<d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>`))
	if err != nil || root.Child(DAV, "prop") == nil {
		t.Fatalf("valid property request rejected: %v", err)
	}
}

func TestHandlerDiscoveryOptionsAuthAndDispatch(t *testing.T) {
	called := false
	h := NewHandler(Options{
		Authenticate: func(_ context.Context, credentials Credentials) (Principal, bool, error) {
			if credentials.Password != "app-pass" {
				return Principal{}, false, nil
			}
			return Principal{UserID: 7, Username: "Neo"}, true, nil
		},
		Dispatch: func(w http.ResponseWriter, _ *http.Request, principal Principal, target *Target) error {
			called = principal.UserID == 7 && target.Segments[0] == "docs"
			w.WriteHeader(http.StatusNoContent)
			return nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/.well-known/webdav", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("discovery returned %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodOptions, "/dav/files/Neo/", nil)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("DAV") != "1, 2" {
		t.Fatalf("OPTIONS returned %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/dav/files/Neo/docs", nil)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthenticated request returned %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/dav/files/Neo/docs", nil)
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("Neo:app-pass")))
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("dispatch returned %d; called=%v", recorder.Code, called)
	}
}
