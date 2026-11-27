package searchindex

import (
	"bytes"
	"context"
	"database/sql"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr/ocrtest"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// fileIndex is an index over a fresh database with one account, its root
// folder and the given files (id, name, mime, blob).
func fileIndex(t *testing.T, extract Extractor, files ...[4]string) (*Indexer, *sql.DB, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "search.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	result, err := db.W.Exec(`INSERT INTO users(username,password) VALUES('owner','fixture')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,type,name,size,etag) VALUES('root',?,'folder','',0,'root')`, userID); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if _, err = db.W.Exec(`INSERT OR IGNORE INTO blobs(id,user_id,size,sha256,enc_version) VALUES(?,?,10,'hash',1)`, f[3], userID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES(?,?,'root','file',?,10,?,?,?)`, f[0], userID, f[1], f[2], f[3], f[0]); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	read := func(context.Context, int64, Node) ([]byte, error) { return []byte("content"), nil }
	idx, err := New(db.W, keys, read, extract, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return idx, db.W, userID
}

func TestPicturesWaitForRecognition(t *testing.T) {
	ocr.Configure(ocr.Settings{Disabled: true})
	defer ocr.Configure(ocr.Settings{})
	extracted := 0
	extract := func(context.Context, Node, []byte) (string, error) { extracted++; return "words", nil }
	idx, db, userID := fileIndex(t, extract, [4]string{"photo", "scan.JPG", "image/jpeg", "blob-photo"}, [4]string{"note", "a.txt", "text/plain", "blob-note"})
	if extractionKind("scan.jpg", "image/jpeg") != "" || isIndexable("scan.heic", "") {
		t.Fatal("pictures are indexable without recognition")
	}
	if _, err := idx.RunPass(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	var docs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM search_docs WHERE user_id=?`, userID).Scan(&docs); err != nil || docs != 1 || extracted != 1 {
		t.Fatalf("docs = %d, extracted = %d, %v", docs, extracted, err)
	}
	// Once recognition is there, the picture is read; it was not recorded
	// as empty before.
	ocr.Configure(ocr.Settings{})
	if !ocr.Available() {
		t.Skip("tesseract is not installed")
	}
	if extractionKind("scan.jpg", "") != "image" || !isIndexable("x.tiff", "") {
		t.Fatal("pictures are not indexable with recognition")
	}
	idx.idleAt = ""
	if _, err := idx.RunPass(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if extracted != 2 {
		t.Fatalf("extracted = %d", extracted)
	}
}

func TestCopiesShareTheirText(t *testing.T) {
	extracted := 0
	extract := func(context.Context, Node, []byte) (string, error) { extracted++; return "Kaufvertrag Fahrrad", nil }
	idx, _, userID := fileIndex(t, extract,
		[4]string{"a", "Vertrag.txt", "text/plain", "blob-shared"},
		[4]string{"b", "Vertrag (Kopie).txt", "text/plain", "blob-shared"},
		// Read another way, the same content is read again.
		[4]string{"c", "Vertrag.html", "text/html", "blob-shared"},
	)
	if _, err := idx.RunPass(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if extracted != 2 {
		t.Fatalf("extracted = %d, want 2", extracted)
	}
	for _, id := range []string{"a", "b", "c"} {
		status, text, err := idx.ReadIndexedText(context.Background(), userID, Node{ID: id, BlobID: "blob-shared"})
		if err != nil || status != "indexed" || text != "Kaufvertrag Fahrrad" {
			t.Fatalf("%s: %q %q %v", id, status, text, err)
		}
	}
}

func TestScannedPagesNeedRecognition(t *testing.T) {
	ocr.Configure(ocr.Settings{Disabled: true})
	defer ocr.Configure(ocr.Settings{})
	pages := []string{"", "Rechnung Nr. 42 vom 1. Mai"}
	if err := recognizeScannedPages(context.Background(), []byte("%PDF-"), pages); err != nil || pages[0] != "" {
		t.Fatalf("pages = %q, %v", pages, err)
	}
}

func TestExtractPictureLive(t *testing.T) {
	ocr.Configure(ocr.Settings{})
	if !ocr.Available() {
		t.Skip("tesseract is not installed")
	}
	sheet := ocrtest.NewSheet(1400, 500)
	sheet.Text(80, 200, 56, "Invoice number 4711")
	sheet.Text(80, 350, 56, "Total amount due")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, sheet); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractText(context.Background(), Node{Name: "receipt.png", MIME: "image/png"}, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Invoice", "4711", "Total"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text %q lacks %q", text, want)
		}
	}
}
