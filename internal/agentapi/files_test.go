package agentapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/oauth"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func fixture(t *testing.T) (*Handler, *dbx.DB, string) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(base, "neo.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(base, "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,password) VALUES('agent-owner','hash')`); err != nil {
		t.Fatal(err)
	}
	keyring, err := cryptox.NewKeyring(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	blobDir := filepath.Join(base, "blobs")
	if err = os.MkdirAll(blobDir, 0700); err != nil {
		t.Fatal(err)
	}
	store := files.New(files.Options{
		DB: db.W, Keys: keyring, BlobDir: blobDir, UploadTmpDir: filepath.Join(base, "tmp"),
		PreviewDir: filepath.Join(base, "previews"), DefaultQuotaBytes: 1 << 30, MaxNodes: 1000, MaxUploadBytes: maxContentBytes, VersionRetention: 10,
	})
	h, err := New(Options{
		DB: db.W, Keys: keyring, BlobDir: blobDir, Files: store,
		DefaultQuotaBytes: 1 << 30, MaxNodes: 1000, MaxUploadBytes: maxContentBytes, VersionRetention: 10,
		Authenticate: func(_ context.Context, token string) (oauth.AccessPrincipal, error) {
			switch token {
			case "read-token":
				return oauth.AccessPrincipal{UserID: 1, ClientID: "fixture", Scopes: []string{"files:read"}}, nil
			case "write-only-token":
				return oauth.AccessPrincipal{UserID: 1, ClientID: "fixture", Scopes: []string{"files:write"}}, nil
			default:
				return oauth.AccessPrincipal{}, oauth.ErrInvalidAccessToken
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dek, err := keyring.GetUserDEK(ctx, db.W, 1)
	if err != nil {
		t.Fatal(err)
	}
	const rootID = "12345678-1234-1234-1234-123456789abc"
	const folderID = "22345678-1234-1234-1234-123456789abc"
	const textID = "32345678-1234-1234-1234-123456789abc"
	const binaryID = "42345678-1234-1234-1234-123456789abc"
	const largeID = "52345678-1234-1234-1234-123456789abc"
	const vaultID = "62345678-1234-1234-1234-123456789abc"
	const nestedVaultID = "72345678-1234-1234-1234-123456789abc"
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,1,NULL,'folder','','root')`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,1,?,'folder','Docs','docs')`, folderID, rootID); err != nil {
		t.Fatal(err)
	}
	putBlob := func(id, name, mime string, parent string, plain []byte) {
		t.Helper()
		var encrypted bytes.Buffer
		if _, e := cryptox.EncryptBlob(&encrypted, bytes.NewReader(plain), dek, id); e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(plain)
		if _, e := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,?,?,1,1)`, id, len(plain), hex.EncodeToString(sum[:])); e != nil {
			t.Fatal(e)
		}
		dir := filepath.Join(blobDir, "1", id[:2])
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, id), encrypted.Bytes(), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES(?,1,?,'file',?,?,?,?,?)`, id, parent, name, len(plain), mime, id, "etag-"+id[:1]); e != nil {
			t.Fatal(e)
		}
	}
	putBlob(textID, "Lease.md", "text/markdown", folderID, []byte("hello agent"))
	putBlob(binaryID, "archive.bin", "application/octet-stream", rootID, []byte{0, 0xff, 0x41})
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag) VALUES(?,1,?,'file','large.bin',?, 'application/octet-stream','large-etag')`, largeID, rootID, maxContentBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES(?,1,?,'folder','Secret','vault','vault-record')`, vaultID, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,1,?,'file','nested-secret.txt','nested-secret')`, nestedVaultID, vaultID); err != nil {
		t.Fatal(err)
	}
	return h, db, rootID
}

func serve(h *Handler, method, target, auth string) *httptest.ResponseRecorder {
	return serveBody(h, method, target, auth, nil)
}

func serveBody(h *Handler, method, target, auth string, body []byte) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Routes(mux)
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	return response
}

func TestAgentFileWritesRequireWriteScope(t *testing.T) {
	h, _, _ := fixture(t)
	writes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/files"},
		{http.MethodPut, "/api/v1/files/32345678-1234-1234-1234-123456789abc/content"},
		{http.MethodPatch, "/api/v1/files/32345678-1234-1234-1234-123456789abc"},
		{http.MethodDelete, "/api/v1/files/32345678-1234-1234-1234-123456789abc"},
		{http.MethodPost, "/api/v1/folders"},
	}
	for _, route := range writes {
		got := serveBody(h, route.method, route.path, "Bearer read-token", []byte(`{}`))
		if got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "INSUFFICIENT_SCOPE") {
			t.Errorf("%s %s with read scope = %d %s", route.method, route.path, got.Code, got.Body.String())
		}
	}
}

func TestAgentFileCreateUpdateMoveTrashAndVaultProtection(t *testing.T) {
	h, db, _ := fixture(t)
	created := serveBody(h, http.MethodPost, "/api/v1/files", "Bearer write-only-token", []byte(`{"path":"/Drafts/new.txt","content":"v1"}`))
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), `"revision"`) {
		t.Fatalf("create file = %d %s", created.Code, created.Body.String())
	}
	var item node
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil || item.ID == "" || item.Path != "/Drafts/new.txt" {
		t.Fatalf("created item = %#v, %v", item, err)
	}
	gotMeta := serve(h, http.MethodGet, "/api/v1/files/"+item.ID, "Bearer read-token")
	if gotMeta.Code != http.StatusOK || json.Unmarshal(gotMeta.Body.Bytes(), &item) != nil || item.Revision == "" {
		t.Fatalf("get created item = %d %s", gotMeta.Code, gotMeta.Body.String())
	}
	conflict := serveBody(h, http.MethodPost, "/api/v1/files", "Bearer write-only-token", []byte(`{"path":"/Drafts/new.txt","content":"v2"}`))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("create conflict = %d %s", conflict.Code, conflict.Body.String())
	}
	updated := serveBody(h, http.MethodPut, "/api/v1/files/"+item.ID+"/content", "Bearer write-only-token", []byte(`{"content":"v2","baseRevision":"`+item.Revision+`"}`))
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"revision"`) {
		t.Fatalf("update = %d %s", updated.Code, updated.Body.String())
	}
	var after node
	if err := json.Unmarshal(updated.Body.Bytes(), &after); err != nil || after.Revision == item.Revision {
		t.Fatalf("updated revision = %#v, %v", after, err)
	}
	readback := serve(h, http.MethodGet, "/api/v1/files/"+item.ID+"/content", "Bearer read-token")
	var contentPayload map[string]any
	if readback.Code != http.StatusOK || json.Unmarshal(readback.Body.Bytes(), &contentPayload) != nil || contentPayload["content"] != "v2" {
		t.Fatalf("encrypted write readback = %d %s", readback.Code, readback.Body.String())
	}
	stale := serveBody(h, http.MethodPut, "/api/v1/files/"+item.ID+"/content", "Bearer write-only-token", []byte(`{"content":"v3","baseRevision":"`+item.Revision+`"}`))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update = %d %s", stale.Code, stale.Body.String())
	}
	moved := serveBody(h, http.MethodPatch, "/api/v1/files/"+item.ID, "Bearer write-only-token", []byte(`{"name":"renamed.txt","parentPath":"/Docs"}`))
	if moved.Code != http.StatusOK || !strings.Contains(moved.Body.String(), `"path":"/Docs/renamed.txt"`) || strings.Contains(moved.Body.String(), `"revision"`) {
		t.Fatalf("move/rename = %d %s", moved.Code, moved.Body.String())
	}
	trash := serve(h, http.MethodDelete, "/api/v1/files/"+item.ID, "Bearer write-only-token")
	if trash.Code != http.StatusOK || !strings.Contains(trash.Body.String(), `"trashed":true`) {
		t.Fatalf("trash = %d %s", trash.Code, trash.Body.String())
	}
	missing := serve(h, http.MethodGet, "/api/v1/files/"+item.ID, "Bearer read-token")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("trashed item visible = %d %s", missing.Code, missing.Body.String())
	}
	for _, path := range []string{"/Secret/new.txt", "/Docs/../Secret/new.txt"} {
		response := serveBody(h, http.MethodPost, "/api/v1/files", "Bearer write-only-token", []byte(`{"path":"`+path+`","content":"x"}`))
		if response.Code == http.StatusCreated {
			t.Fatalf("created file through protected path %q", path)
		}
	}
	var count int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE name='new.txt' OR name='renamed.txt'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected written file count %d: %v", count, err)
	}
}

func TestAgentFolderCreationIsIdempotentAndProtectsVault(t *testing.T) {
	h, _, _ := fixture(t)
	first := serveBody(h, http.MethodPost, "/api/v1/folders", "Bearer write-only-token", []byte(`{"path":"/Projects/One"}`))
	second := serveBody(h, http.MethodPost, "/api/v1/folders", "Bearer write-only-token", []byte(`{"path":"/Projects/One"}`))
	if first.Code != http.StatusCreated || second.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"path":"/Projects/One"`) {
		t.Fatalf("folder create/idempotence = %d %s; %d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	protected := serveBody(h, http.MethodPost, "/api/v1/folders", "Bearer write-only-token", []byte(`{"path":"/Secret/Nested"}`))
	if protected.Code != http.StatusNotFound {
		t.Fatalf("vault folder create = %d %s", protected.Code, protected.Body.String())
	}
}

func TestAgentFileReadsRequireBearerAndScope(t *testing.T) {
	h, _, _ := fixture(t)
	missing := serve(h, http.MethodGet, "/api/v1/files", "")
	if missing.Code != http.StatusUnauthorized || missing.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("missing bearer = %d %s", missing.Code, missing.Body.String())
	}
	invalid := serve(h, http.MethodGet, "/api/v1/files", "Bearer bad-token")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer = %d %s", invalid.Code, invalid.Body.String())
	}
	insufficient := serve(h, http.MethodGet, "/api/v1/files", "Bearer write-only-token")
	if insufficient.Code != http.StatusForbidden || !strings.Contains(insufficient.Body.String(), "INSUFFICIENT_SCOPE") || insufficient.Header().Get("RateLimit-Limit") != "3000" {
		t.Fatalf("insufficient scope = %d %s", insufficient.Code, insufficient.Body.String())
	}
	authorized := serve(h, http.MethodGet, "/api/v1/files", "Bearer read-token")
	if authorized.Header().Get("RateLimit-Limit") != "3000" || authorized.Header().Get("RateLimit-Remaining") != "2998" {
		t.Fatalf("per-user rate limit headers = %#v", authorized.Header())
	}
}

func TestAgentListSearchMetadataAndVaultHiding(t *testing.T) {
	h, _, _ := fixture(t)
	list := serve(h, http.MethodGet, "/api/v1/files?path=%2FDocs", "Bearer read-token")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"path":"/Docs"`) || !strings.Contains(list.Body.String(), `"name":"Lease.md"`) || strings.Contains(list.Body.String(), `"Secret"`) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	rootList := serve(h, http.MethodGet, "/api/v1/files", "Bearer read-token")
	if rootList.Code != http.StatusOK || strings.Contains(rootList.Body.String(), `"name":"Secret"`) || strings.Contains(rootList.Body.String(), `nested-secret.txt`) {
		t.Fatalf("root list exposes vault entries: %d %s", rootList.Code, rootList.Body.String())
	}
	search := serve(h, http.MethodGet, "/api/v1/files/search?q=lease", "Bearer read-token")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"path":"/Docs/Lease.md"`) {
		t.Fatalf("search = %d %s", search.Code, search.Body.String())
	}
	metadata := serve(h, http.MethodGet, "/api/v1/files/32345678-1234-1234-1234-123456789abc", "Bearer read-token")
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"revision":"etag-3"`) || !strings.Contains(metadata.Body.String(), `"mimeType":"text/markdown"`) {
		t.Fatalf("metadata = %d %s", metadata.Code, metadata.Body.String())
	}
	vault := serve(h, http.MethodGet, "/api/v1/files/62345678-1234-1234-1234-123456789abc", "Bearer read-token")
	if vault.Code != http.StatusNotFound {
		t.Fatalf("vault metadata = %d %s", vault.Code, vault.Body.String())
	}
	nestedVault := serve(h, http.MethodGet, "/api/v1/files/72345678-1234-1234-1234-123456789abc", "Bearer read-token")
	if nestedVault.Code != http.StatusNotFound {
		t.Fatalf("nested vault metadata = %d %s", nestedVault.Code, nestedVault.Body.String())
	}
	badPath := serve(h, http.MethodGet, "/api/v1/files?path=%2F..%2FSecret", "Bearer read-token")
	if badPath.Code != http.StatusBadRequest {
		t.Fatalf("dot path = %d %s", badPath.Code, badPath.Body.String())
	}
}

func TestAgentContentUsesUTF8OrBase64AndEnforcesFiveMiB(t *testing.T) {
	h, _, _ := fixture(t)
	text := serve(h, http.MethodGet, "/api/v1/files/32345678-1234-1234-1234-123456789abc/content", "Bearer read-token")
	var textPayload map[string]any
	if text.Code != http.StatusOK || json.Unmarshal(text.Body.Bytes(), &textPayload) != nil || textPayload["encoding"] != "utf8" || textPayload["content"] != "hello agent" {
		t.Fatalf("text content = %d %s", text.Code, text.Body.String())
	}
	binary := serve(h, http.MethodGet, "/api/v1/files/42345678-1234-1234-1234-123456789abc/content", "Bearer read-token")
	var binaryPayload map[string]any
	if binary.Code != http.StatusOK || json.Unmarshal(binary.Body.Bytes(), &binaryPayload) != nil || binaryPayload["encoding"] != "base64" {
		t.Fatalf("binary content = %d %s", binary.Code, binary.Body.String())
	}
	decoded, err := base64.StdEncoding.DecodeString(binaryPayload["content"].(string))
	if err != nil || !bytes.Equal(decoded, []byte{0, 0xff, 0x41}) {
		t.Fatalf("base64 payload = %x %v", decoded, err)
	}
	large := serve(h, http.MethodGet, "/api/v1/files/52345678-1234-1234-1234-123456789abc/content", "Bearer read-token")
	if large.Code != http.StatusRequestEntityTooLarge || !strings.Contains(large.Body.String(), "FILE_TOO_LARGE") {
		t.Fatalf("large content = %d %s", large.Code, large.Body.String())
	}
}
