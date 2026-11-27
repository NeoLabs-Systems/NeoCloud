package searchindex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestEncryptedIndexAndContentSearch(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "search.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO users(username,password) VALUES('search-owner','fixture')`); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.W.QueryRow(`SELECT id FROM users WHERE username='search-owner'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version) VALUES('blob-1',?,10,'hash',1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,type,name,size,mime,blob_id,etag) VALUES('root',?,'folder','','0',NULL,NULL,'root-etag')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES('file-1',?,'root','file','note.txt',10,'text/plain','blob-1','file-etag')`, userID); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	idx, err := New(db.W, keys, func(context.Context, int64, Node) ([]byte, error) { return nil, nil }, func(context.Context, Node, []byte) (string, error) { return "", nil }, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	node := Node{ID: "file-1", Name: "note.txt", MIME: "text/plain", BlobID: "blob-1"}
	const content = "The quick brown fox jumps over the quiet stream."
	if err := idx.SaveIndexed(ctx, userID, node, content); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := db.W.QueryRow(`SELECT text_enc FROM search_docs WHERE node_id='file-1'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "quick brown") {
		t.Fatal("plaintext appears in encrypted search document")
	}
	got, err := idx.ContentSearch(ctx, userID, ParseQuery(`"quick brown" fox`), nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].NodeID != "file-1" {
		var docs, fts int
		_ = db.W.QueryRow(`SELECT COUNT(*) FROM search_docs`).Scan(&docs)
		_ = db.W.QueryRow(`SELECT COUNT(*) FROM search_fts`).Scan(&fts)
		var matches int
		_ = db.W.QueryRow(`SELECT COUNT(*) FROM search_fts WHERE search_fts MATCH ?`, MatchExpression(ParseQuery(`"quick brown" fox`))).Scan(&matches)
		t.Fatalf("rows = %#v docs=%d fts=%d matches=%d match=%q", got.Rows, docs, fts, matches, MatchExpression(ParseQuery(`"quick brown" fox`)))
	}
	if !strings.Contains(got.Rows[0].Snippet, "\u0002quick brown\u0003") || !strings.Contains(got.Rows[0].Snippet, "\u0002fox\u0003") {
		t.Fatalf("snippet markers missing: %q", got.Rows[0].Snippet)
	}
	got, err = idx.ContentSearchScoped(ctx, userID, ParseQuery("quick"), nil, ContentScope{Where: "n.id=?", Args: []any{"file-1"}}, 10)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].NodeID != "file-1" {
		t.Fatalf("scoped content search rows=%#v err=%v", got.Rows, err)
	}
	got, err = idx.ContentSearchScoped(ctx, userID, ParseQuery("quick"), nil, ContentScope{
		CTE: "allowed(id) AS (SELECT ?)", CTEArgs: []any{"file-1"}, Where: "n.id IN (SELECT id FROM allowed)",
	}, 10)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].NodeID != "file-1" {
		t.Fatalf("scoped CTE content search rows=%#v err=%v", got.Rows, err)
	}
	if _, err := db.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(?,'fullTextSearch','false')`, userID); err != nil {
		t.Fatal(err)
	}
	got, err = idx.ContentSearch(ctx, userID, ParseQuery("quick"), nil, 10)
	if err != nil || len(got.Rows) != 0 {
		t.Fatalf("opted-out search rows=%#v err=%v", got.Rows, err)
	}
}
