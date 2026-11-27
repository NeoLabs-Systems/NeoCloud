package publicshares

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/files"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"golang.org/x/crypto/bcrypt"
)

func testServer(t *testing.T) (*dbx.DB, *http.ServeMux, *cryptox.Keyring, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "public-shares.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password) VALUES(1,'owner','hash'),(2,'other','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag) VALUES
		('root',1,NULL,'folder','','0',NULL,'root-etag'),
		('shared',1,'root','folder','Shared',0,NULL,'shared-etag'),
		('nested',1,'shared','folder','Nested',0,NULL,'nested-etag'),
		('visible',1,'nested','file','Report.txt',12,'text/plain','visible-etag'),
		('private',1,'root','file','Private.txt',9,'text/plain','private-etag'),
		('vault',1,'shared','folder','Vault',0,NULL,'vault-etag'),
		('other-root',2,NULL,'folder','','0',NULL,'other-root-etag'),
		('other-shared',2,'other-root','folder','Disabled owner share',0,NULL,'other-shared-etag')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE nodes SET vault_id='vault-key' WHERE id='vault'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO shares(id,user_id,node_id,token,mode) VALUES(1,1,'shared','read-token','view'),(2,1,'shared','upload-token','upload_only'),(3,1,'shared','revoked-token','view'),(4,1,'shared','expired-token','view'),(5,2,'other-shared','disabled-token','view'),(6,1,'vault','vault-token','view'),(7,1,'shared','write-token','view_upload'),(8,1,'shared','limited-token','view_upload'); UPDATE shares SET revoked_at=datetime('now') WHERE id=3; UPDATE shares SET expires_at='2000-01-01 00:00:00' WHERE id=4; UPDATE shares SET max_upload_bytes=100 WHERE id=8;`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE users SET disabled_at=datetime('now') WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(hex.EncodeToString(bytesOf(0x42, 32)))
	if err != nil {
		t.Fatal(err)
	}
	blobDir := filepath.Join(root, "blobs")
	store := files.New(files.Options{DB: db.W, Keys: keys, BlobDir: blobDir, UploadTmpDir: filepath.Join(root, "upload-tmp"), DefaultQuotaBytes: 5 * 1024 * 1024 * 1024, MaxUploadBytes: 1024 * 1024, MaxNodes: 1000})
	h, err := New(Options{DB: db.W, Keys: keys, Files: store, SessionSecret: strings.Repeat("session-secret-", 4), PublicURL: "https://cloud.example", SecureCookies: true, Now: func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, mux, keys, blobDir
}

func bytesOf(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func doRequest(mux *http.ServeMux, method, target string, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var cookies []*http.Cookie
	if cookie != nil {
		cookies = append(cookies, cookie)
	}
	return doRequestWith(mux, method, target, body, cookies, nil)
}

func doRequestWith(mux *http.ServeMux, method, target string, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "cloud.example"
	r.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func installEncryptedFile(t *testing.T, db *dbx.DB, keys *cryptox.Keyring, blobDir, content string) {
	t.Helper()
	const blobID = "0123456789abcdef0123456789abcdef0123"
	dek, err := keys.GetUserDEK(context.Background(), db.W, 1)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(blobDir, "1", blobID[:2])
	if err = os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, blobID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.EncryptBlob(file, strings.NewReader(content), dek, blobID); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	if _, err = db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,?,?,1,1)`, blobID, len(content), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE nodes SET blob_id=?,size=?,etag='download-etag',mime='text/plain' WHERE id='visible'`, blobID, len(content)); err != nil {
		t.Fatal(err)
	}
}

func doUpload(mux *http.ServeMux, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Length"] = strconv.Itoa(len(body))
	return doRequestWith(mux, http.MethodPut, target, body, nil, headers)
}

func TestPublicShareUploadsRespectModesAndSubtree(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	if w := doUpload(mux, "/api/public/shares/read-token/upload?name=Blocked.txt", "data", nil); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"UPLOAD_NOT_ALLOWED"`) {
		t.Fatalf("view upload=%d %s", w.Code, w.Body.String())
	}
	if w := doUpload(mux, "/api/public/shares/upload-token/upload?folderId=nested&name=Blocked.txt", "data", nil); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"SHARE_UPLOAD_ONLY"`) {
		t.Fatalf("upload-only folder override=%d %s", w.Code, w.Body.String())
	}
	if w := doUpload(mux, "/api/public/shares/write-token/upload?folderId=private&name=Blocked.txt", "data", nil); w.Code != http.StatusNotFound {
		t.Fatalf("outside subtree upload=%d %s", w.Code, w.Body.String())
	}
	created := doUpload(mux, "/api/public/shares/write-token/upload?folderId=nested&name=Report.txt", "new contents", nil)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"name":"Report (1).txt"`) || !strings.Contains(created.Body.String(), `"mime":"text/plain"`) {
		t.Fatalf("upload mode response=%d %s", created.Code, created.Body.String())
	}
	var parent, blobID string
	var storedSize int64
	if err := db.W.QueryRow(`SELECT parent_id,blob_id,size FROM nodes WHERE user_id=1 AND name='Report (1).txt'`).Scan(&parent, &blobID, &storedSize); err != nil {
		t.Fatal(err)
	}
	if parent != "nested" || storedSize != int64(len("new contents")) {
		t.Fatalf("uploaded node parent=%q size=%d", parent, storedSize)
	}
	dek, err := keys.GetUserDEK(context.Background(), db.W, 1)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(blobDir, "1", blobID[:2], blobID))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if _, err = cryptox.DecryptBlobRange(file, storedSize, 0, storedSize-1, dek, blobID, &plain); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if plain.String() != "new contents" {
		t.Fatalf("decrypted upload=%q", plain.String())
	}
	only := doUpload(mux, "/api/public/shares/upload-token/upload?name=Private%20upload.txt", "secret", nil)
	if only.Code != http.StatusCreated || !strings.Contains(only.Body.String(), `"name":"Private upload.txt"`) || !strings.Contains(only.Body.String(), `"size":6`) || strings.Contains(only.Body.String(), `"id"`) {
		t.Fatalf("upload-only response=%d %s", only.Code, only.Body.String())
	}
	if err = db.W.QueryRow(`SELECT parent_id FROM nodes WHERE user_id=1 AND name='Private upload.txt'`).Scan(&parent); err != nil || parent != "shared" {
		t.Fatalf("upload-only parent=%q err=%v", parent, err)
	}
}

func TestPublicShareUploadReservationRollsBackOnChecksumAndQuotaFailure(t *testing.T) {
	db, mux, _, _ := testServer(t)
	checksum := doUpload(mux, "/api/public/shares/limited-token/upload?name=damaged.txt", "data", map[string]string{"X-Content-SHA256": strings.Repeat("0", 64)})
	if checksum.Code != http.StatusUnprocessableEntity || !strings.Contains(checksum.Body.String(), `"code":"CHECKSUM_MISMATCH"`) {
		t.Fatalf("checksum failure=%d %s", checksum.Code, checksum.Body.String())
	}
	var uploaded int64
	if err := db.W.QueryRow(`SELECT uploaded_bytes FROM shares WHERE id=8`).Scan(&uploaded); err != nil || uploaded != 0 {
		t.Fatalf("reservation after checksum failure=%d err=%v", uploaded, err)
	}
	if _, err := db.W.Exec(`UPDATE users SET quota_bytes=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	quota := doUpload(mux, "/api/public/shares/limited-token/upload?name=too-large.txt", "data", nil)
	if quota.Code != http.StatusRequestEntityTooLarge || !strings.Contains(quota.Body.String(), `"code":"QUOTA_EXCEEDED"`) {
		t.Fatalf("quota failure=%d %s", quota.Code, quota.Body.String())
	}
	if err := db.W.QueryRow(`SELECT uploaded_bytes FROM shares WHERE id=8`).Scan(&uploaded); err != nil || uploaded != 0 {
		t.Fatalf("reservation after quota failure=%d err=%v", uploaded, err)
	}
}

func TestPublicShareUploadReservationsAreAtomic(t *testing.T) {
	db, mux, _, _ := testServer(t)
	if _, err := db.W.Exec(`UPDATE shares SET max_upload_bytes=5,uploaded_bytes=0 WHERE id=8`); err != nil {
		t.Fatal(err)
	}
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			name := []string{"first.txt", "second.txt"}[index]
			responses[index] = doUpload(mux, "/api/public/shares/limited-token/upload?name="+name, "four", nil)
		}(i)
	}
	wg.Wait()
	successes, limited := 0, 0
	for _, response := range responses {
		if response.Code == http.StatusCreated {
			successes++
		} else if response.Code == http.StatusRequestEntityTooLarge && strings.Contains(response.Body.String(), `"code":"SHARE_UPLOAD_LIMIT"`) {
			limited++
		} else {
			t.Fatalf("concurrent upload=%d %s", response.Code, response.Body.String())
		}
	}
	if successes != 1 || limited != 1 {
		t.Fatalf("successes=%d limited=%d", successes, limited)
	}
	var uploaded int64
	if err := db.W.QueryRow(`SELECT uploaded_bytes FROM shares WHERE id=8`).Scan(&uploaded); err != nil || uploaded != 4 {
		t.Fatalf("uploaded bytes=%d err=%v", uploaded, err)
	}
}

func TestPublicShareBrowseIsBoundedAndFiltersVaults(t *testing.T) {
	_, mux, _, _ := testServer(t)
	w := doRequest(mux, http.MethodGet, "/api/public/shares/read-token", "", nil)
	if w.Code != 200 {
		t.Fatalf("browse=%d %s", w.Code, w.Body.String())
	}
	var result struct {
		Share map[string]any      `json:"share"`
		Node  map[string]any      `json:"node"`
		Path  []map[string]string `json:"path"`
		Items []map[string]any    `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Share["sharedBy"] != "owner" || result.Share["host"] != "cloud.example" || result.Node["id"] != "shared" || len(result.Path) != 1 || result.Path[0]["id"] != "shared" {
		t.Fatalf("share response=%s", w.Body.String())
	}
	if len(result.Items) != 1 || result.Items[0]["id"] != "nested" {
		t.Fatalf("vault leaked or subtree listing wrong: %s", w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token?folderId=nested", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"path":[{"id":"shared","name":"Shared"},{"id":"nested","name":"Nested"}]`) || !strings.Contains(w.Body.String(), `"id":"visible"`) {
		t.Fatalf("nested browse=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token?folderId=private", "", nil)
	if w.Code != 404 {
		t.Fatalf("outside subtree status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPublicShareStateAndUploadOnlyBoundaries(t *testing.T) {
	_, mux, _, _ := testServer(t)
	for token, state := range map[string]struct {
		status int
		code   string
	}{
		"missing": {404, "NOT_FOUND"}, "revoked-token": {410, "SHARE_REVOKED"}, "expired-token": {410, "SHARE_EXPIRED"}, "disabled-token": {404, "NOT_FOUND"}, "vault-token": {404, "NOT_FOUND"},
	} {
		w := doRequest(mux, http.MethodGet, "/api/public/shares/"+token, "", nil)
		if w.Code != state.status || !strings.Contains(w.Body.String(), `"code":"`+state.code+`"`) {
			t.Fatalf("%s=%d %s", token, w.Code, w.Body.String())
		}
	}
	w := doRequest(mux, http.MethodGet, "/api/public/shares/upload-token", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("upload-only view=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/upload-token?folderId=nested", "", nil)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"SHARE_UPLOAD_ONLY"`) {
		t.Fatalf("upload-only traversal=%d %s", w.Code, w.Body.String())
	}
}

func TestPasswordShareUnlockIssuesPathScopedHttpOnlyProof(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "abcdefghij")
	hash, err := bcrypt.GenerateFromPassword([]byte("strong share password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE shares SET password_hash=? WHERE token='read-token'`, string(hash)); err != nil {
		t.Fatal(err)
	}
	w := doRequest(mux, http.MethodGet, "/api/public/shares/read-token", "", nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "SHARE_PASSWORD_REQUIRED") {
		t.Fatalf("locked browse=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodPost, "/api/public/shares/read-token/unlock", `{"password":"wrong"}`, nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "SHARE_PASSWORD_INVALID") {
		t.Fatalf("wrong password=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodPost, "/api/public/shares/read-token/unlock", `{"password":"strong share password"}`, nil)
	if w.Code != 200 {
		t.Fatalf("unlock=%d %s", w.Code, w.Body.String())
	}
	var proof *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == unlockCookie {
			proof = c
		}
	}
	if proof == nil || !proof.HttpOnly || !proof.Secure || proof.SameSite != http.SameSiteStrictMode || proof.Path != "/api/public/shares/read-token" {
		t.Fatalf("unlock cookie=%#v", proof)
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token", "", proof)
	if w.Code != 200 {
		t.Fatalf("unlocked browse=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "SHARE_PASSWORD_REQUIRED") {
		t.Fatalf("password-gated content=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", proof)
	if w.Code != http.StatusOK || w.Body.String() != "abcdefghij" {
		t.Fatalf("unlocked content=%d %q", w.Code, w.Body.String())
	}
}

func TestPublicFileDownloadCountsOnceAndAllowsOnlySignedRangeResume(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "abcdefghij")
	if _, err := db.W.Exec(`UPDATE shares SET max_downloads=1 WHERE token='read-token'`); err != nil {
		t.Fatal(err)
	}
	url := "/api/public/shares/read-token/files/visible/content?download=1"
	w := doRequest(mux, http.MethodGet, url, "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "abcdefghij" {
		t.Fatalf("first download=%d %q", w.Code, w.Body.String())
	}
	var count int
	if err := db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("download count=%d err=%v", count, err)
	}
	var continuation *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == continueCookie {
			continuation = cookie
		}
	}
	if continuation == nil || !continuation.HttpOnly || continuation.Path != "/api/public/shares/read-token/files/visible" || continuation.SameSite != http.SameSiteStrictMode {
		t.Fatalf("continuation cookie=%#v", continuation)
	}
	w = doRequestWith(mux, http.MethodGet, url, "", []*http.Cookie{continuation}, map[string]string{"Range": "bytes=2-5"})
	if w.Code != http.StatusPartialContent || w.Body.String() != "cdef" || w.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range resume=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	if err := db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("resumed count=%d err=%v", count, err)
	}
	w = doRequestWith(mux, http.MethodGet, url, "", nil, map[string]string{"Range": "bytes=0-1"})
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "SHARE_EXHAUSTED") {
		t.Fatalf("new exhausted download=%d %s", w.Code, w.Body.String())
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token/files/private/content?download=1", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("outside share node=%d %s", w.Code, w.Body.String())
	}
}

func TestPublicFileHeadAndInlineRangesDoNotCountDownloads(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "abcdefghij")
	base := "/api/public/shares/read-token/files/visible/content?download=1"
	w := doRequestWith(mux, http.MethodHead, base, "", nil, nil)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "10" {
		t.Fatalf("HEAD=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	var count int
	if err := db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("HEAD download count=%d err=%v", count, err)
	}
	w = doRequestWith(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", nil, map[string]string{"Range": "bytes=3-6"})
	if w.Code != http.StatusPartialContent || w.Body.String() != "defg" || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("inline range=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	if err := db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inline count=%d err=%v", count, err)
	}
	w = doRequestWith(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", nil, map[string]string{"If-None-Match": `W/"download-etag"`})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("conditional content=%d %q", w.Code, w.Body.String())
	}
}

func TestPublicFileHeadersMatchNodeContentDispositionAndMIMEFallback(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "safe text content")
	if _, err := db.W.Exec(`UPDATE nodes SET name='report.pdf',mime=NULL WHERE id='visible'`); err != nil {
		t.Fatal(err)
	}
	w := doRequest(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("null MIME fallback=%d headers=%v", w.Code, w.Header())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("ordinary file disposition=%q", w.Header().Get("Content-Disposition"))
	}
	if w.Header().Get("Cache-Control") != "private, no-cache" || w.Header().Get("ETag") != `"download-etag"` || w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("content security/cache headers=%v", w.Header())
	}
	if _, err := db.W.Exec(`UPDATE nodes SET name='report.html',mime='text/html' WHERE id='visible'`); err != nil {
		t.Fatal(err)
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token/files/visible/content", "", nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("active content disposition=%d headers=%v", w.Code, w.Header())
	}
}

func TestPublicZipStreamsSelectedFolderSkipsVaultsAndCountsOnce(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "zip file content")
	if _, err := db.W.Exec(`UPDATE shares SET max_downloads=1 WHERE token='read-token'`); err != nil {
		t.Fatal(err)
	}
	url := "/api/public/shares/read-token/zip?folderId=shared"
	w := doRequestWith(mux, http.MethodGet, url, "", nil, map[string]string{"Range": "bytes=0-1"})
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Content-Range") != "" {
		t.Fatalf("zip status=%d headers=%v body=%d bytes", w.Code, w.Header(), w.Body.Len())
	}
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("zip archive: %v", err)
	}
	if len(archive.File) != 3 || archive.File[0].Name != "Shared/" || archive.File[1].Name != "Shared/Nested/" || archive.File[2].Name != "Shared/Nested/Report.txt" {
		names := make([]string, 0, len(archive.File))
		for _, f := range archive.File {
			names = append(names, f.Name)
		}
		t.Fatalf("archive entries=%v", names)
	}
	contents, err := archive.File[2].Open()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(contents)
	contents.Close()
	if err != nil || string(payload) != "zip file content" {
		t.Fatalf("archived content=%q err=%v", payload, err)
	}
	for _, entry := range archive.File {
		if strings.Contains(entry.Name, "Vault") {
			t.Fatalf("vault entry leaked into archive: %q", entry.Name)
		}
	}
	var count int
	if err = db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ZIP count=%d err=%v", count, err)
	}
	w = doRequest(mux, http.MethodGet, url, "", nil)
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "SHARE_EXHAUSTED") {
		t.Fatalf("second ZIP=%d %s", w.Code, w.Body.String())
	}
	if _, err = db.W.Exec(`UPDATE shares SET download_count=0 WHERE token='read-token'`); err != nil {
		t.Fatal(err)
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/read-token/zip?folderId=private", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("outside folder ZIP=%d %s", w.Code, w.Body.String())
	}
}

func TestPublicZipHeadDoesNotCountAndUploadOnlyCannotZip(t *testing.T) {
	db, mux, keys, blobDir := testServer(t)
	installEncryptedFile(t, db, keys, blobDir, "head content")
	if _, err := db.W.Exec(`UPDATE shares SET max_downloads=1 WHERE token='read-token'`); err != nil {
		t.Fatal(err)
	}
	w := doRequest(mux, http.MethodHead, "/api/public/shares/read-token/zip?folderId=shared", "", nil)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("ZIP HEAD=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	var count int
	if err := db.W.QueryRow(`SELECT download_count FROM shares WHERE token='read-token'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ZIP HEAD count=%d err=%v", count, err)
	}
	w = doRequest(mux, http.MethodGet, "/api/public/shares/upload-token/zip", "", nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "SHARE_UPLOAD_ONLY") {
		t.Fatalf("upload-only ZIP=%d %s", w.Code, w.Body.String())
	}
}

func TestOwnerManagedShareLifecycleAndAccountIsolation(t *testing.T) {
	db, mux, _, _ := testServer(t)
	var ownerToken, otherToken string
	for _, user := range []int64{1, 3} {
		if user == 3 {
			if _, err := db.W.Exec(`INSERT INTO users(id,username,password) VALUES(3,'enabled-other','hash')`); err != nil {
				t.Fatal(err)
			}
		}
		token := "nc_" + strings.Repeat("s", 43) + strconv.FormatInt(user, 10)
		hash := sha256.Sum256([]byte(token))
		if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, user, "Share manager test", "linux", hex.EncodeToString(hash[:])); err != nil {
			t.Fatal(err)
		}
		if user == 1 {
			// Keep this raw token only for the test requests below.
			ownerToken = token
		} else {
			otherToken = token
		}
	}
	ownerShareHandler := requestauth.IdentityMiddleware(db.W, mux)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "cloud.example"
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		ownerShareHandler.ServeHTTP(w, r)
		return w
	}

	unauthenticated := request(http.MethodGet, "/api/shares", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d: %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	created := request(http.MethodPost, "/api/shares", `{"nodeId":"nested","mode":"view_upload","expiresAt":"2027-01-01T00:00:00Z","password":"strong share password","maxDownloads":7,"maxUploadBytes":4096,"note":"  from owner  "}`, ownerToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var share map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &share); err != nil {
		t.Fatal(err)
	}
	if share["url"] != "https://cloud.example/s/"+share["token"].(string) || share["nodeId"] != "nested" || share["nodeName"] != "Nested" || share["mode"] != "view_upload" || share["allowUpload"] != true || share["hasPassword"] != true || share["note"] != "from owner" || share["maxDownloads"] != float64(7) || share["maxUploadBytes"] != float64(4096) {
		t.Fatalf("created share = %#v", share)
	}
	shareID := int64(share["id"].(float64))
	var storedHash, mode string
	if err := db.W.QueryRow(`SELECT password_hash,mode FROM shares WHERE id=? AND user_id=1`, shareID).Scan(&storedHash, &mode); err != nil {
		t.Fatal(err)
	}
	if storedHash == "strong share password" || bcrypt.CompareHashAndPassword([]byte(storedHash), []byte("strong share password")) != nil || mode != "view_upload" {
		t.Fatalf("stored password/mode = %q/%q", storedHash, mode)
	}

	listed := request(http.MethodGet, "/api/shares?nodeId=nested", "", ownerToken)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"token":"`+share["token"].(string)+`"`) {
		t.Fatalf("list by node = %d: %s", listed.Code, listed.Body.String())
	}
	otherList := request(http.MethodGet, "/api/shares?nodeId=nested", "", otherToken)
	if otherList.Code != http.StatusOK || !strings.Contains(otherList.Body.String(), `"shares":[]`) {
		t.Fatalf("other account list = %d: %s", otherList.Code, otherList.Body.String())
	}
	otherRootAlias := request(http.MethodPost, "/api/shares", `{"nodeId":"root"}`, otherToken)
	if otherRootAlias.Code != http.StatusBadRequest || !strings.Contains(otherRootAlias.Body.String(), `"code":"ROOT_IMMUTABLE"`) {
		t.Fatalf("root alias with generated root id = %d: %s", otherRootAlias.Code, otherRootAlias.Body.String())
	}

	updated := request(http.MethodPatch, "/api/shares/"+strconv.FormatInt(shareID, 10), `{"mode":"upload_only","allowUpload":false,"expiresAt":null,"password":null,"maxDownloads":null,"maxUploadBytes":null,"note":null}`, ownerToken)
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", updated.Code, updated.Body.String())
	}
	var afterUpdate map[string]any
	if err := json.Unmarshal(updated.Body.Bytes(), &afterUpdate); err != nil {
		t.Fatal(err)
	}
	if afterUpdate["mode"] != "upload_only" || afterUpdate["allowUpload"] != true || afterUpdate["expiresAt"] != nil || afterUpdate["hasPassword"] != false || afterUpdate["maxDownloads"] != nil || afterUpdate["maxUploadBytes"] != nil || afterUpdate["note"] != nil {
		t.Fatalf("updated share = %#v", afterUpdate)
	}
	var nullPassword sql.NullString
	if err := db.W.QueryRow(`SELECT password_hash FROM shares WHERE id=?`, shareID).Scan(&nullPassword); err != nil || nullPassword.Valid {
		t.Fatalf("cleared password = %#v, err=%v", nullPassword, err)
	}

	otherPatch := request(http.MethodPatch, "/api/shares/"+strconv.FormatInt(shareID, 10), `{"note":"hijack"}`, otherToken)
	if otherPatch.Code != http.StatusNotFound {
		t.Fatalf("cross-account patch = %d: %s", otherPatch.Code, otherPatch.Body.String())
	}
	otherDelete := request(http.MethodDelete, "/api/shares/"+strconv.FormatInt(shareID, 10), "", otherToken)
	if otherDelete.Code != http.StatusNotFound {
		t.Fatalf("cross-account revoke = %d: %s", otherDelete.Code, otherDelete.Body.String())
	}
	revoked := request(http.MethodDelete, "/api/shares/"+strconv.FormatInt(shareID, 10), "", ownerToken)
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), `"success":true`) {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body.String())
	}
}

func TestOwnerManagedShareValidation(t *testing.T) {
	db, mux, _, _ := testServer(t)
	token := "nc_" + strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(token))
	if _, err := db.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,'share validation','linux',?)`, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	secured := requestauth.IdentityMiddleware(db.W, mux)
	for _, test := range []struct {
		body string
		code string
		want int
	}{
		{`{"nodeId":"root"}`, "ROOT_IMMUTABLE", http.StatusBadRequest},
		{`{"nodeId":"vault"}`, "VAULT_NOT_SHAREABLE", http.StatusConflict},
		{`{"nodeId":"visible","mode":"upload_only"}`, "NOT_A_FOLDER", http.StatusBadRequest},
		{`{"nodeId":"nested","mode":"other"}`, "INVALID_MODE", http.StatusBadRequest},
		{`{"nodeId":"nested","maxDownloads":0}`, "INVALID_MAX_DOWNLOADS", http.StatusBadRequest},
		{`{"nodeId":"nested","maxUploadBytes":9007199254740992}`, "INVALID_SIZE", http.StatusBadRequest},
		{`{"nodeId":"nested","password":"short"}`, "PASSWORD_TOO_SHORT", http.StatusBadRequest},
		{`{"nodeId":"missing"}`, "NOT_FOUND", http.StatusNotFound},
		{`{}`, "NOT_FOUND", http.StatusNotFound},
		{`{"nodeId":"nested","password":true}`, "PASSWORD_TOO_SHORT", http.StatusBadRequest},
		{`{"nodeId":"nested","expiresAt":"not a date"}`, "INVALID_EXPIRY", http.StatusBadRequest},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/shares", strings.NewReader(test.body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		secured.ServeHTTP(w, r)
		if w.Code != test.want || !strings.Contains(w.Body.String(), `"code":"`+test.code+`"`) {
			t.Errorf("body %s: status=%d want=%d response=%s", test.body, w.Code, test.want, w.Body.String())
		}
	}
}

func TestManagedSharePasswordUsesNodeStringCoercion(t *testing.T) {
	for name, test := range map[string]struct {
		value any
		want  string
	}{
		"number":             {float64(12345678), "12345678"},
		"object":             {map[string]any{"a": 1}, "[object Object]"},
		"array":              {[]any{"abcd", "efgh"}, "abcd,efgh"},
		"unicode code units": {"😀😀😀😀", "😀😀😀😀"},
		"past 72 bytes":      {strings.Repeat("long", 40), strings.Repeat("long", 18)},
	} {
		value, err := passwordHash(test.value)
		if err != nil {
			t.Fatal(name, err)
		}
		hash, ok := value.(string)
		if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte(test.want)) != nil {
			t.Fatalf("%s: hash did not match Node-coerced password %q", name, test.want)
		}
	}
	for _, unset := range []any{nil, ""} {
		if value, err := passwordHash(unset); err != nil || value != nil {
			t.Fatalf("empty should mean no password: value=%v error=%v", value, err)
		}
	}
	if _, err := passwordHash(float64(1e21)); err == nil {
		t.Fatal("a large number password follows JavaScript's short string form")
	}
}
