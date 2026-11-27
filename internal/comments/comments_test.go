package comments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type contextKey struct{}

func fixture(t *testing.T) (*dbx.DB, *http.ServeMux, *cryptox.Keyring) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "comments.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,display_name,password) VALUES('viewer','Viewer','x'),('owner','Owner','x'),('outsider','Outsider','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('root1',2,NULL,'folder','','r'),('shared',2,'root1','folder','Shared','s'),('child',2,'shared','file','Report','c'),('private',2,'root1','file','Private','p'),('root3',3,NULL,'folder','','r3'),('vault',2,'root1','folder','Vault','v')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE nodes SET vault_id='vault' WHERE id='vault'; INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(2,'shared',1,'read','viewer')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, Keys: keys, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		uid, ok := r.Context().Value(contextKey{}).(int64)
		return Principal{UserID: uid}, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, mux, keys
}
func request(m *http.ServeMux, uid int64, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(context.WithValue(context.Background(), contextKey{}, uid))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}
func TestSharedCommentsAreEncryptedAndRespectAuthorOwnerPermissions(t *testing.T) {
	db, m, keys := fixture(t)
	created := request(m, 1, "POST", "/api/files/child/comments", `{"body":"hello\nshared"}`)
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var c comment
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Body != "hello\nshared" || !c.Mine || !c.CanDelete {
		t.Fatalf("comment=%#v", c)
	}
	var stored string
	if err := db.W.QueryRow(`SELECT body FROM node_comments WHERE id=?`, c.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "hello") || !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("body was not sealed: %q", stored)
	}
	if _, err := keys.DecryptValue(stored); err != nil {
		t.Fatal(err)
	}
	listed := request(m, 2, "GET", "/api/files/child/comments", "")
	if listed.Code != 200 {
		t.Fatalf("list=%d %s", listed.Code, listed.Body.String())
	}
	var page struct {
		Comments []comment `json:"comments"`
		Next     any       `json:"nextCursor"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 1 || page.Comments[0].CanDelete != true || page.Comments[0].Mine {
		t.Fatalf("owner view=%s", listed.Body.String())
	}
	edit := request(m, 2, "PATCH", "/api/files/child/comments/"+itoa(c.ID), `{"body":"owner edit"}`)
	if edit.Code != 403 {
		t.Fatalf("foreign edit=%d %s", edit.Code, edit.Body.String())
	}
	deleted := request(m, 2, "DELETE", "/api/files/child/comments/"+itoa(c.ID), "")
	if deleted.Code != 200 {
		t.Fatalf("owner delete=%d %s", deleted.Code, deleted.Body.String())
	}
}
func TestCommentsHideUnsharedVaultAndRootAndNotifyOnlyLiveWatchers(t *testing.T) {
	_, m, _ := fixture(t)
	for _, id := range []string{"private", "vault", "root1"} {
		w := request(m, 1, "GET", "/api/files/"+id+"/comments", "")
		if w.Code != 404 {
			t.Fatalf("%s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
	var notified []int64
	db, _, keys := fixture(t)
	h, err := New(Options{DB: db.W, Keys: keys, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		uid, ok := r.Context().Value(contextKey{}).(int64)
		return Principal{UserID: uid}, ok
	}, Notify: func(uid int64, event string, payload map[string]any) {
		if event != "comments:changed" || payload["nodeId"] != "child" {
			t.Errorf("notification %d %s %#v", uid, event, payload)
		}
		notified = append(notified, uid)
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	w := request(mux, 1, "POST", "/api/files/child/comments", `{"body":"shared"}`)
	if w.Code != 201 {
		t.Fatalf("add=%d %s", w.Code, w.Body.String())
	}
	if len(notified) != 2 || notified[0] != 2 || notified[1] != 1 {
		t.Fatalf("watchers=%v", notified)
	}
}
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
