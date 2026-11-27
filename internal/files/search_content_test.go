package files

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

func TestContentSearchReturnsScopedEncryptedIndexMatches(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(base, "search.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = schema.Migrate(ctx, db, filepath.Join(base, "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','fixture');
		INSERT INTO blobs(id,user_id,size,sha256,enc_version) VALUES('content-blob',1,28,'hash',1);
		INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES
		('root',1,NULL,'folder','','0',NULL,NULL,'root'),
		('content-file',1,'root','file','memo.txt',28,'text/plain','content-blob','file');`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	indexer, err := searchindex.New(db.W, keys, func(context.Context, int64, searchindex.Node) ([]byte, error) {
		return nil, nil
	}, func(context.Context, searchindex.Node, []byte) (string, error) { return "", nil }, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.SaveIndexed(ctx, 1, searchindex.Node{ID: "content-file", Name: "memo.txt", MIME: "text/plain", Size: 28, BlobID: "content-blob"}, "Needle appears in this memo."); err != nil {
		t.Fatal(err)
	}
	h := New(Options{DB: db.W, Keys: keys})
	h.SetSearchIndexer(indexer)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test-login", func(w http.ResponseWriter, r *http.Request) {
		sessionx.FromContext(r.Context()).Set("userId", int64(1))
		w.WriteHeader(http.StatusNoContent)
	})
	h.Routes(mux)
	store, err := sessionx.NewStore(ctx, db.W)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := sessionx.NewManager(store, sessionx.Options{Name: "test.sid", Secrets: []string{"test-secret"}, Rolling: true, MaxAge: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(sessions.Middleware(mux))
	defer server.Close()
	login, err := server.Client().Get(server.URL + "/test-login")
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent || len(login.Cookies()) == 0 {
		t.Fatalf("test login = %d, cookies=%v", login.StatusCode, login.Cookies())
	}
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/search?q=needle&content=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(login.Cookies()[0])
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Items []struct {
			ID      string `json:"id"`
			Match   string `json:"match"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(payload.Items) != 1 || payload.Items[0].ID != "content-file" || payload.Items[0].Match != "content" || !strings.Contains(strings.ToLower(payload.Items[0].Snippet), "needle") {
		t.Fatalf("content search = %d %#v", response.StatusCode, payload.Items)
	}
}
