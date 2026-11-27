package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type principalContextKey struct{}

type testPrincipal struct {
	ID       int64
	Device   bool
	Platform string
}

func testHandler(t *testing.T) (*dbx.DB, *http.ServeMux) {
	db, mux, _, _, _ := testHandlerWithStorage(t)
	return db, mux
}

func testHandlerWithStorage(t *testing.T) (*dbx.DB, *http.ServeMux, string, string, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	blobDir := filepath.Join(root, "blobs")
	previewDir := filepath.Join(root, "previews")
	uploadTmpDir := filepath.Join(root, "uploads")
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "admin.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,email,password) VALUES
		('administrator','admin@example.test','not-a-real-password'),
		('account','account@example.test','not-a-real-password')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{
		DB:                db.W,
		Keys:              keys,
		DefaultQuotaBytes: 10 * 1024 * 1024 * 1024,
		SessionSecret:     "test-session-secret-with-enough-entropy",
		BlobDir:           blobDir,
		PreviewDir:        previewDir,
		UploadTmpDir:      uploadTmpDir,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	h.UserActionRoutes(mux)
	return db, mux, blobDir, previewDir, uploadTmpDir
}

func adminRequest(mux *http.ServeMux, p testPrincipal, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(context.WithValue(context.Background(), principalContextKey{}, p))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestListUsersRequiresCurrentAdminAndDoesNotExposeCredentials(t *testing.T) {
	db, mux := testHandler(t)
	admin := testPrincipal{ID: 1}
	response := adminRequest(mux, admin, http.MethodGet, "/api/admin/users", "")
	if response.Code != http.StatusOK {
		t.Fatalf("admin list=%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Users) != 2 {
		t.Fatalf("users=%s", response.Body.String())
	}
	var adminRow map[string]any
	for _, row := range result.Users {
		if row["username"] == "administrator" {
			adminRow = row
		}
	}
	if adminRow == nil || adminRow["quotaMode"] != "default" || adminRow["quotaBytes"] != float64(10*1024*1024*1024) || adminRow["isAdmin"] != true {
		t.Fatalf("serialized admin=%#v", adminRow)
	}
	for _, secret := range []string{"password", "not-a-real-password", "wrapped_dek", "session_hash"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("user list leaked %q: %s", secret, response.Body.String())
		}
	}
	for _, p := range []testPrincipal{{ID: 2}, {}, {ID: 1, Device: true, Platform: "desktop"}} {
		denied := adminRequest(mux, p, http.MethodGet, "/api/admin/users", "")
		if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "ADMIN_REQUIRED") {
			t.Fatalf("non-admin access=%d %s", denied.Code, denied.Body.String())
		}
	}
	phone := adminRequest(mux, testPrincipal{ID: 1, Device: true, Platform: "ios"}, http.MethodGet, "/api/admin/users?q=account", "")
	if phone.Code != http.StatusOK || !strings.Contains(phone.Body.String(), `"username":"account"`) || strings.Contains(phone.Body.String(), `"username":"administrator"`) {
		t.Fatalf("phone admin search=%d %s", phone.Code, phone.Body.String())
	}
	// Admin rights are read from the database on each request, not cached.
	if _, err := db.W.Exec(`UPDATE users SET is_admin=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	revoked := adminRequest(mux, admin, http.MethodGet, "/api/admin/users", "")
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("revoked admin=%d %s", revoked.Code, revoked.Body.String())
	}
}

func TestQuotaChangeIsValidatedAndAuditedAtomically(t *testing.T) {
	db, mux := testHandler(t)
	admin := testPrincipal{ID: 1}
	response := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/quota", `{"mode":"custom","bytes":"1.5GB"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"quotaMode":"custom"`) || !strings.Contains(response.Body.String(), `"quotaBytes":1610612736`) {
		t.Fatalf("custom quota=%d %s", response.Code, response.Body.String())
	}
	var stored int64
	if err := db.W.QueryRow(`SELECT quota_bytes FROM users WHERE id=2`).Scan(&stored); err != nil || stored != 1610612736 {
		t.Fatalf("stored quota=%d err=%v", stored, err)
	}
	var action string
	var actorID, subjectID int64
	var rawDetail string
	if err := db.W.QueryRow(`SELECT action,actor_user_id,subject_user_id,detail_json FROM access_audit_log ORDER BY id DESC LIMIT 1`).Scan(&action, &actorID, &subjectID, &rawDetail); err != nil {
		t.Fatal(err)
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(rawDetail), &detail); err != nil {
		t.Fatal(err)
	}
	if action != "account.quota" || actorID != 1 || subjectID != 2 || detail["mode"] != "custom" || detail["bytes"] != float64(stored) {
		t.Fatalf("audit action=%q actor=%d subject=%d detail=%s", action, actorID, subjectID, rawDetail)
	}
	for _, body := range []string{
		`{"mode":"custom","bytes":1.5}`,
		`{"mode":"custom","bytes":"not a size"}`,
		`{"mode":"invalid"}`,
	} {
		bad := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/quota", body)
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("invalid quota %s => %d %s", body, bad.Code, bad.Body.String())
		}
	}
	if missing := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/999/quota", `{"mode":"default"}`); missing.Code != http.StatusNotFound {
		t.Fatalf("missing user=%d %s", missing.Code, missing.Body.String())
	}
	reset := adminRequest(mux, admin, http.MethodPut, "/api/admin/users/2/quota", `{"mode":"default"}`)
	if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), `"quotaMode":"default"`) {
		t.Fatalf("reset quota=%d %s", reset.Code, reset.Body.String())
	}
	var nullable any
	if err := db.W.QueryRow(`SELECT quota_bytes FROM users WHERE id=2`).Scan(&nullable); err != nil || nullable != nil {
		t.Fatalf("default quota value=%#v err=%v", nullable, err)
	}
}

func TestParseByteSizeMatchesNodeNumberAndStringRules(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value int64
		valid bool
	}{
		{`1024`, 1024, true},
		{`1024.0`, 1024, true},
		{`"1024"`, 1024, true},
		{`"1.5 GB"`, 1610612736, true},
		{`"1.5"`, 1, true},
		{`"unlimited"`, 0, false},
		{`null`, 0, false},
	} {
		value, valid := parseByteSize(json.RawMessage(tc.raw))
		if value != tc.value || valid != tc.valid {
			t.Errorf("parseByteSize(%s)=(%d,%t), want (%d,%t)", tc.raw, value, valid, tc.value, tc.valid)
		}
	}
}
