package files

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

type zipFixture struct {
	db      *dbx.DB
	handler *Handler
	keys    *cryptox.Keyring
	blobDir string
	dek     []byte
}

func newZIPFixture(t *testing.T) *zipFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(dir, "files.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Migrate(ctx, db, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	dek := bytes.Repeat([]byte{0x35}, 32)
	wrapped, err := keys.WrapDEK(1, dek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password,wrapped_dek) VALUES(1,'zip-owner','x',?)`, wrapped); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(2,'zip-member','x')`); err != nil {
		t.Fatal(err)
	}
	blobDir := filepath.Join(dir, "blobs")
	if err = os.MkdirAll(blobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	h := New(Options{DB: db.W, Keys: keys, BlobDir: blobDir})
	return &zipFixture{db: db, handler: h, keys: keys, blobDir: blobDir, dek: dek}
}

func (f *zipFixture) addRoot(t *testing.T, id string, userID int64) {
	t.Helper()
	if _, err := f.db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','',?)`, id, userID, "root-etag-"+id); err != nil {
		t.Fatal(err)
	}
}

func (f *zipFixture) addFolder(t *testing.T, id string, parent *string, userID int64, name string, vaultID *string) {
	t.Helper()
	var parentValue any
	if parent != nil {
		parentValue = *parent
	}
	var vaultValue any
	if vaultID != nil {
		vaultValue = *vaultID
	}
	if _, err := f.db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES(?,?,?,'folder',?,?,?)`, id, userID, parentValue, name, "etag-"+id, vaultValue); err != nil {
		t.Fatal(err)
	}
}

func (f *zipFixture) addFile(t *testing.T, id, parent string, userID int64, name string, content []byte) {
	t.Helper()
	if userID != 1 {
		t.Fatal("fixture only has the owner data key")
	}
	hash := sha256.Sum256(content)
	blobID := id
	path := blobPath(f.blobDir, userID, blobID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.EncryptBlob(out, bytes.NewReader(content), f.dek, blobID); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err = out.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,?,?,1,1)`, blobID, userID, len(content), hex.EncodeToString(hash[:]), 1); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES(?,?,?,'file',?,?,?, ?,?)`, id, userID, parent, name, len(content), "text/plain", blobID, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
}

func (f *zipFixture) server(t *testing.T, userID int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	store, err := sessionx.NewStore(context.Background(), f.db.W)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := sessionx.NewManager(store, sessionx.Options{Name: "neocloud.sid", Secrets: []string{"zip-test-session-secret-with-enough-entropy"}, SameSite: http.SameSiteLaxMode, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionx.FromContext(r.Context()).Set("userId", userID)
		mux.ServeHTTP(w, r)
	}))
	server := httptest.NewServer(wrapped)
	t.Cleanup(server.Close)
	return server
}

func TestDownloadZIPStreamsFilesAndFolders(t *testing.T) {
	f := newZIPFixture(t)
	root := "00000000-0000-4000-8000-000000000001"
	folder := "00000000-0000-4000-8000-000000000002"
	nested := "00000000-0000-4000-8000-000000000003"
	rootFile := "00000000-0000-4000-8000-000000000004"
	f.addRoot(t, root, 1)
	f.addFolder(t, folder, &root, 1, "Folder", nil)
	f.addFile(t, nested, folder, 1, "nested.txt", []byte("nested content"))
	f.addFile(t, rootFile, root, 1, "readme.txt", []byte("root content"))
	server := f.server(t, 1)
	response, err := server.Client().Get(server.URL + "/api/download/zip?ids=" + folder + "," + rootFile)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/zip" || !strings.Contains(response.Header.Get("Content-Disposition"), "NeoCloud.zip") {
		t.Fatalf("ZIP response status=%d headers=%v error=%v", response.StatusCode, response.Header, err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Folder/": "", "Folder/nested.txt": "nested content", "readme.txt": "root content"}
	for _, entry := range archive.File {
		content, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		value, readErr := io.ReadAll(content)
		content.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, exists := want[entry.Name]; !exists {
			t.Errorf("unexpected ZIP entry %q", entry.Name)
			continue
		}
		if string(value) != want[entry.Name] {
			t.Errorf("entry %q content=%q want=%q", entry.Name, value, want[entry.Name])
		}
		delete(want, entry.Name)
	}
	if len(want) != 0 {
		t.Fatalf("ZIP is missing entries: %#v", want)
	}
	f.handler.maxNodes = 1
	oversized, err := server.Client().Get(server.URL + "/api/download/zip?ids=" + folder)
	if err != nil {
		t.Fatal(err)
	}
	oversizedBody, _ := io.ReadAll(oversized.Body)
	oversized.Body.Close()
	if oversized.StatusCode != http.StatusBadRequest || !strings.Contains(string(oversizedBody), `"code":"TOO_MANY_ITEMS"`) {
		t.Fatalf("ZIP entry bound status=%d body=%s", oversized.StatusCode, oversizedBody)
	}
}

func TestDownloadZIPRejectsAnonymousEmptyTooManyAndMixedOwners(t *testing.T) {
	f := newZIPFixture(t)
	root := "10000000-0000-4000-8000-000000000001"
	shared := "10000000-0000-4000-8000-000000000002"
	ownerFile := "10000000-0000-4000-8000-000000000003"
	ownRoot := "20000000-0000-4000-8000-000000000001"
	f.addRoot(t, root, 1)
	f.addFolder(t, shared, &root, 1, "Shared", nil)
	f.addFile(t, ownerFile, shared, 1, "shared.txt", []byte("owner content"))
	f.addRoot(t, ownRoot, 2)
	vaultID := "30000000-0000-4000-8000-000000000001"
	vaultFolder := "30000000-0000-4000-8000-000000000002"
	f.addFolder(t, vaultFolder, &shared, 1, "Private vault", &vaultID)
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,?,?, 'read','zip-member')`, shared, 2); err != nil {
		t.Fatal(err)
	}
	// A grantee's root and the owner's shared file resolve to different
	// owners, so mixing them is rejected before any archive bytes are sent.
	server := f.server(t, 2)
	client := server.Client()
	unauthMux := http.NewServeMux()
	f.handler.Routes(unauthMux)
	unauth := httptest.NewRecorder()
	unauthMux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/download/zip?ids="+ownerFile, nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", unauth.Code, unauth.Body.String())
	}
	tooMany := make([]string, maxZIPSelection+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("missing-%d", i)
	}
	for _, tc := range []struct {
		query, code string
	}{
		{"", "NOTHING_SELECTED"},
		{"ids=" + strings.Join(tooMany, ","), "TOO_MANY_ITEMS"},
		{"ids=" + ownRoot + "," + ownerFile, "ZIP_MIXED_OWNERS"},
	} {
		resp, requestErr := client.Get(server.URL + "/api/download/zip?" + tc.query)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"`+tc.code+`"`) {
			t.Errorf("query %q status=%d body=%s want code %s", tc.query, resp.StatusCode, body, tc.code)
		}
	}
	sharedResponse, err := client.Get(server.URL + "/api/download/zip?ids=" + shared)
	if err != nil {
		t.Fatal(err)
	}
	sharedBody, err := io.ReadAll(sharedResponse.Body)
	sharedResponse.Body.Close()
	if err != nil || sharedResponse.StatusCode != http.StatusOK {
		t.Fatalf("shared ZIP status=%d error=%v", sharedResponse.StatusCode, err)
	}
	sharedArchive, err := zip.NewReader(bytes.NewReader(sharedBody), int64(len(sharedBody)))
	if err != nil {
		t.Fatal(err)
	}
	sharedNames := make([]string, 0, len(sharedArchive.File))
	for _, entry := range sharedArchive.File {
		sharedNames = append(sharedNames, entry.Name)
	}
	if strings.Join(sharedNames, ",") != "Shared/,Shared/shared.txt" {
		t.Fatalf("shared ZIP entries=%v; vault content must remain hidden", sharedNames)
	}
}
