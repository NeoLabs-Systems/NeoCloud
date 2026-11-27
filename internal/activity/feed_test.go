package activity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type principalContextKey struct{}

func fixture(t *testing.T) (*Handler, *dbx.DB, *http.ServeMux) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "activity.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	_, err = db.W.Exec(`INSERT INTO users(username,password,email_verified_at) VALUES
		('viewer','fixture',datetime('now')),('owner','fixture',datetime('now')),('outsider','fixture',datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []struct {
		id, user, parent, typ, name string
	}{
		{"root-viewer", "1", "", "folder", ""}, {"viewer-file", "1", "root-viewer", "file", "mine.txt"},
		{"root-owner", "2", "", "folder", ""}, {"shared-folder", "2", "root-owner", "folder", "Shared"},
		{"shared-file", "2", "shared-folder", "file", "team.txt"}, {"private-file", "2", "root-owner", "file", "private.txt"},
		{"vault-file", "2", "root-owner", "file", "secret.txt"},
	} {
		var parent any
		if node.parent != "" {
			parent = node.parent
		}
		var vault any
		if node.id == "vault-file" {
			vault = "vault-1"
		}
		if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES(?,?,?,?,?,?,?)`, node.id, node.user, parent, node.typ, node.name, node.id, vault); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(2,'shared-folder',1,'read','viewer')`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		owner, actor, kind, node, parent, typ, name, detail string
	}{
		{"1", "1", "upload", "viewer-file", "root-viewer", "file", "mine.txt", "{}"},
		{"2", "2", "upload", "shared-file", "shared-folder", "file", "team.txt", "{}"},
		{"2", "2", "upload", "private-file", "root-owner", "file", "private.txt", "{}"},
		{"2", "2", "share", "shared-folder", "root-owner", "folder", "Shared", `{"granteeId":1}`},
		{"2", "2", "share", "shared-folder", "root-owner", "folder", "Shared", `{"granteeId":3}`},
		{"2", "2", "upload", "vault-file", "root-owner", "file", "secret.txt", "{}"},
	} {
		if _, err = db.W.Exec(`INSERT INTO activity(owner_id,actor_id,kind,node_id,parent_id,node_type,name,detail,at) VALUES(?,?,?,?,?,?,?,?,?)`, row.owner, row.actor, row.kind, row.node, row.parent, row.typ, row.name, row.detail, time.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	h, err := New(Options{DB: db.W, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(Principal)
		return p, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return h, db, mux
}

func request(mux *http.ServeMux, path string, principal *Principal) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if principal != nil {
		r = r.WithContext(context.WithValue(r.Context(), principalContextKey{}, *principal))
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, r)
	return recorder
}

func TestFeedScopesVisibilityAndShareDetails(t *testing.T) {
	_, _, mux := fixture(t)
	viewer := &Principal{UserID: 1}
	for _, tc := range []struct {
		scope string
		want  []string
	}{{"mine", []string{"mine.txt"}}, {"shared", []string{"Shared", "team.txt"}}, {"all", []string{"Shared", "team.txt", "mine.txt"}}} {
		rec := request(mux, "/api/activity?scope="+tc.scope, viewer)
		if rec.Code != http.StatusOK {
			t.Fatalf("scope %s status=%d body=%s", tc.scope, rec.Code, rec.Body.String())
		}
		var body struct {
			Items []item `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(body.Items))
		for _, entry := range body.Items {
			got = append(got, entry.Node.Name)
			if entry.Kind == "share" && entry.Node.Name == "Shared" && (entry.Member == nil || !entry.Member.Self) {
				t.Fatalf("grantee should see their own share event: %#v", entry)
			}
		}
		if len(got) != len(tc.want) {
			t.Fatalf("scope %s got %v, want %v", tc.scope, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("scope %s got %v, want %v", tc.scope, got, tc.want)
			}
		}
	}
}

func TestFeedFocusPaginationAndAuthorization(t *testing.T) {
	_, _, mux := fixture(t)
	viewer := &Principal{UserID: 1}
	if rec := request(mux, "/api/activity?nodeId=shared-folder", viewer); rec.Code != http.StatusOK {
		t.Fatalf("shared focus status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(mux, "/api/activity?nodeId=private-file", viewer); rec.Code != http.StatusNotFound {
		t.Fatalf("private focus status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(mux, "/api/activity?nodeId=vault-file", &Principal{UserID: 2}); rec.Code != http.StatusNotFound {
		t.Fatalf("vault focus status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(mux, "/api/activity?scope=unknown", viewer); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(mux, "/api/activity", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(mux, "/api/activity", &Principal{UserID: 1, Device: true, Platform: "macos"}); rec.Code != http.StatusForbidden {
		t.Fatalf("desktop token status=%d body=%s", rec.Code, rec.Body.String())
	}
}
