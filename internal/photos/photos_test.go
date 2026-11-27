package photos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func testHandler(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:photos-tests?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY,wrapped_dek TEXT);
	INSERT INTO users(id) VALUES(1),(2);
	CREATE TABLE nodes(id TEXT PRIMARY KEY,user_id INTEGER,parent_id TEXT,type TEXT,name TEXT,size INTEGER,mime TEXT,etag TEXT,trashed_at TEXT,vault_id TEXT,blob_id TEXT,modified_at TEXT);
	CREATE TABLE file_client_dates(node_id TEXT PRIMARY KEY,blob_id TEXT,created_at TEXT,modified_at TEXT);
	CREATE TABLE changes(seq INTEGER PRIMARY KEY AUTOINCREMENT,user_id INTEGER,node_id TEXT,op TEXT);
	CREATE TABLE photo_index_state(key TEXT PRIMARY KEY,value TEXT);
	CREATE TABLE photo_items(node_id TEXT PRIMARY KEY,user_id INTEGER,blob_id TEXT,kind TEXT,taken_at TEXT,taken_at_source TEXT,date_state INTEGER,width INTEGER,height INTEGER,duration_seconds REAL,probed INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE favorites(user_id INTEGER,node_id TEXT,PRIMARY KEY(user_id,node_id));
	INSERT INTO nodes VALUES('a',1,'root','file','a.jpg',12,'image/jpeg','etag-a',NULL,NULL,'ba','2025-03-02 10:00:00'),('b',1,'root','file','b.mp4',20,'video/mp4','etag-b',NULL,NULL,'bb','2025-03-01 10:00:00'),('c',1,'root','file','secret.jpg',2,'image/jpeg','etag-c',NULL,'v','bc','2025-03-03 10:00:00'),('other',2,'root','file','other.jpg',4,'image/jpeg','etag-o',NULL,NULL,'bo','2025-03-04 10:00:00');
	INSERT INTO photo_items(node_id,user_id,blob_id,kind,taken_at,taken_at_source,date_state,width,height,duration_seconds,probed) VALUES('a',1,'ba','photo','2025-03-02T10:00:00','exif',1,640,480,NULL,1),('b',1,'bb','video','2025-03-01T10:00:00',NULL,0,NULL,NULL,3.5,1),('c',1,'bc','photo','2025-03-03T10:00:00','client',1,1,1,NULL,1),('other',2,'bo','photo','2025-03-04T10:00:00','client',1,1,1,NULL,1);
	CREATE TABLE image_hashes(node_id TEXT PRIMARY KEY,user_id INTEGER,etag TEXT,dhash TEXT);
	INSERT INTO favorites VALUES(1,'a');`)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db, ResolvePrincipal: func(*http.Request) (Principal, bool) { return Principal{UserID: 1}, true }})
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}

type fixedPhotoKeys []byte

func (k fixedPhotoKeys) GetUserDEK(context.Context, *sql.DB, int64) ([]byte, error) {
	return append([]byte(nil), k...), nil
}

func TestHashPassDecryptsAndCachesCurrentETag(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	plainImage := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			plainImage.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 9), B: uint8((x + y) * 3), A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, plainImage); err != nil {
		t.Fatal(err)
	}
	userKey := bytes.Repeat([]byte{0x42}, 32)
	h.keys = fixedPhotoKeys(userKey)
	h.blobDir = t.TempDir()
	blobID := "12345678-1234-4234-8234-123456789abc"
	nodeID := "12345678-1234-4234-8234-123456789abd"
	blobDir := filepath.Join(h.blobDir, "1", blobID[:2])
	if err := os.MkdirAll(blobDir, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(blobDir, blobID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.EncryptBlob(file, bytes.NewReader(source.Bytes()), userKey, blobID); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag,blob_id,modified_at) VALUES(?,1,'root','file','new.png',?,'image/png','etag-1',?,'2026-01-01')`, nodeID, source.Len(), blobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO photo_items(node_id,user_id,blob_id,kind,taken_at,probed) VALUES(?,1,?,'photo','2026-01-01T00:00:00',1)`, nodeID, blobID); err != nil {
		t.Fatal(err)
	}
	count, err := h.HashPass(context.Background(), 100)
	if err != nil || count < 1 {
		t.Fatalf("hash pass count=%d err=%v", count, err)
	}
	var candidates int
	if err = db.QueryRow(`SELECT COUNT(*) FROM photo_items p JOIN nodes n ON n.id=p.node_id AND n.user_id=p.user_id AND n.blob_id=p.blob_id WHERE p.node_id=?`, nodeID).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	var got string
	if err = db.QueryRow(`SELECT dhash FROM image_hashes WHERE node_id=? AND etag='etag-1'`, nodeID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := imaging.DHash(plainImage)
	if got != want {
		t.Fatalf("hash=%s want=%s", got, want)
	}
	if _, err = db.Exec(`UPDATE nodes SET etag='etag-2' WHERE id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.HashPass(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	var cachedETag string
	if err = db.QueryRow(`SELECT etag,dhash FROM image_hashes WHERE node_id=?`, nodeID).Scan(&cachedETag, &got); err != nil {
		t.Fatal(err)
	}
	if cachedETag != "etag-2" || got != want {
		t.Fatalf("updated cache etag=%s hash=%s want=%s", cachedETag, got, want)
	}
}

func TestDHashAndHashPassCacheFailureByETag(t *testing.T) {
	flat := image.NewGray(image.Rect(0, 0, 20, 20))
	for i := range flat.Pix {
		flat.Pix[i] = 90
	}
	if got := imaging.DHash(flat); got != "0000000000000000" {
		t.Fatalf("flat image hash=%s", got)
	}
	gradient := image.NewGray(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			gradient.SetGray(x, y, color.Gray{Y: uint8(255 - x*10)})
		}
	}
	if got := imaging.DHash(gradient); got != "ffffffffffffffff" {
		t.Fatalf("decreasing gradient hash=%s", got)
	}
	h, db := testHandler(t)
	defer db.Close()
	count, err := h.HashPass(context.Background(), 1)
	if err != nil || count != 1 {
		t.Fatalf("hash pass=%d err=%v", count, err)
	}
	var etag string
	var hash sql.NullString
	if err = db.QueryRow(`SELECT etag,dhash FROM image_hashes WHERE node_id='a'`).Scan(&etag, &hash); err != nil || etag != "etag-a" || hash.Valid {
		t.Fatalf("cached failed photo hash etag=%q hash=%v err=%v", etag, hash, err)
	}
	count, err = h.HashPass(context.Background(), 1)
	if err != nil || count != 1 {
		t.Fatalf("second hash pass=%d err=%v", count, err)
	}
	count, err = h.HashPass(context.Background(), 1)
	if err != nil || count != 0 {
		t.Fatalf("third hash pass=%d err=%v", count, err)
	}
}

func TestCatchUpAddsMediaAndExcludesHiddenSubtrees(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag,trashed_at,vault_id,blob_id,modified_at) VALUES
		('new',1,'root','file','IMG_20260312_101010.jpg',100,'image/jpeg','e',NULL,NULL,'new-blob','2025-01-01 00:00:00'),
		('trash-folder',1,'root','folder','Trash',0,NULL,'t','2025-01-01',NULL,NULL,'2025-01-01 00:00:00'),
		('trash-child',1,'trash-folder','file','hidden.jpg',100,'image/jpeg','h',NULL,NULL,'hidden-blob','2025-01-01 00:00:00'),
		('vault-folder',1,'root','folder','Vault',0,NULL,'v',NULL,'vault',NULL,'2025-01-01 00:00:00'),
		('vault-child',1,'vault-folder','file','hidden-vault.jpg',100,'image/jpeg','vh',NULL,NULL,'hidden-vault-blob','2025-01-01 00:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var blob, source string
	var state int
	if err = db.QueryRow(`SELECT blob_id,taken_at_source,date_state FROM photo_items WHERE node_id='new'`).Scan(&blob, &source, &state); err != nil {
		t.Fatal(err)
	}
	if blob != "new-blob" || source != "filename" || state != 2 {
		t.Fatalf("new index row=%q %q %d", blob, source, state)
	}
	for _, id := range []string{"trash-child", "vault-child"} {
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM photo_items WHERE node_id=?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("indexed hidden item %s", id)
		}
	}
}

func TestCatchUpAppliesRenameAndTrashChanges(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE nodes SET mime='text/plain' WHERE id='a'; INSERT INTO changes(user_id,node_id,op) VALUES(1,'a','rename')`); err != nil {
		t.Fatal(err)
	}
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM photo_items WHERE node_id='a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("renamed non-media node remains in the photo index")
	}
	if _, err := db.Exec(`UPDATE nodes SET trashed_at='2026-01-01' WHERE id='b'; INSERT INTO changes(user_id,node_id,op) VALUES(1,'b','trash')`); err != nil {
		t.Fatal(err)
	}
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM photo_items WHERE node_id='b'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("trashed item remains in the photo index")
	}
}

func TestCatchUpAppliesMoveIntoAndOutOfVault(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE nodes SET vault_id='vault' WHERE id='a'; INSERT INTO changes(user_id,node_id,op) VALUES(1,'a','move')`); err != nil {
		t.Fatal(err)
	}
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM photo_items WHERE node_id='a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("item moved into a vault remains in the photo index")
	}
	if _, err := db.Exec(`UPDATE nodes SET vault_id=NULL WHERE id='a'; INSERT INTO changes(user_id,node_id,op) VALUES(1,'a','move')`); err != nil {
		t.Fatal(err)
	}
	if err := h.CatchUp(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM photo_items WHERE node_id='a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("item moved out of a vault was not rediscovered")
	}
}

func TestRunPassDecryptsAndProbesPNGMetadata(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	keyring, err := cryptox.NewKeyring(strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	dek, err := keyring.GetUserDEK(context.Background(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 37, 19))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var plain bytes.Buffer
	if err = png.Encode(&plain, img); err != nil {
		t.Fatal(err)
	}
	blobID := uuid.NewString()
	root := t.TempDir()
	blobPath := filepath.Join(root, "1", blobID[:2], blobID)
	if err = os.MkdirAll(filepath.Dir(blobPath), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(blobPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.EncryptBlob(file, bytes.NewReader(plain.Bytes()), dek, blobID); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO nodes VALUES('png',1,'root','file','IMG_20260312_101010.png',?,'image/png','etag-png',NULL,NULL,?,'2025-01-01 00:00:00')`, plain.Len(), blobID); err != nil {
		t.Fatal(err)
	}
	h.keys = keyring
	h.blobDir = root
	done, err := h.RunPass(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if done < 1 {
		t.Fatalf("probe count=%d", done)
	}
	var width, height, probed, state int
	var source string
	var taken string
	if err = db.QueryRow(`SELECT width,height,probed,date_state,taken_at_source,taken_at FROM photo_items WHERE node_id='png'`).Scan(&width, &height, &probed, &state, &source, &taken); err != nil {
		t.Fatal(err)
	}
	if width != 37 || height != 19 || probed != 1 || state != 1 || source != "filename" || taken != "2026-03-12T10:10:10" {
		t.Fatalf("probe=%d×%d probed=%d state=%d source=%q taken=%q", width, height, probed, state, source, taken)
	}
}

func TestStartWorkerStopsAndJoins(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	stop := h.StartWorker(ctx)
	cancel()
	finished := make(chan struct{})
	go func() { stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("photo worker did not stop")
	}
}

func TestTimelinePagingSummaryAndVaultExclusion(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	mux := http.NewServeMux()
	h.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/photos?limit=1", nil))
	if w.Code != 200 {
		t.Fatalf("first page = %d %s", w.Code, w.Body.String())
	}
	var first struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"nextCursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0]["id"] != "a" || first.Items[0]["favorite"] != true || first.Items[0]["dateSource"] != "exif" || first.Next == nil {
		t.Fatalf("first page = %#v", first)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/photos?cursor="+*first.Next, nil))
	var second struct {
		Items []map[string]any `json:"items"`
		Next  any              `json:"nextCursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0]["id"] != "b" || second.Items[0]["provisional"] != false || second.Items[0]["dateUnknown"] != true || second.Next != nil {
		t.Fatalf("second page = %#v status=%d", second, w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/photos/summary", nil))
	var summary struct {
		Total   int64            `json:"total"`
		Pending int64            `json:"pending"`
		Unknown int64            `json:"unknown"`
		Days    []map[string]any `json:"days"`
		Months  []map[string]any `json:"months"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || summary.Total != 2 || summary.Unknown != 1 || summary.Pending != 0 || len(summary.Days) != 1 || len(summary.Months) != 1 {
		t.Fatalf("summary=%#v status=%d body=%s", summary, w.Code, w.Body.String())
	}
}

func TestTimelineRejectsInvalidParameters(t *testing.T) {
	h, db := testHandler(t)
	defer db.Close()
	mux := http.NewServeMux()
	h.Routes(mux)
	for _, path := range []string{"/api/photos?limit=0", "/api/photos?limit=1.2", "/api/photos?day=bad", "/api/photos?cursor=a"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}
