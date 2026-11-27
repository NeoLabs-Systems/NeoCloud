package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type testPrincipalKey struct{}

func newSettingsTest(t *testing.T) (*Handler, *dbx.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "settings.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	_, err = db.W.Exec(`INSERT INTO users(username,password) VALUES('settings-owner','fixture-hash'),('another-owner','fixture-hash')`)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, Now: func() time.Time { return time.UnixMilli(1234) }, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(testPrincipalKey{}).(Principal)
		return p, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}
func requestWithUser(method, body string, userID int64) *http.Request {
	r := httptest.NewRequest(method, "/api/settings/", strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), testPrincipalKey{}, Principal{UserID: userID}))
}

func TestSettingsDefaultsAndAccountScopedAtomicUpdates(t *testing.T) {
	h, db := newSettingsTest(t)
	mux := http.NewServeMux()
	h.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, requestWithUser(http.MethodGet, "", 1))
	if rec.Code != 200 {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var before struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if before.Settings["theme"] != "system" || before.Settings["fullTextSearch"] != true {
		t.Fatalf("defaults=%v", before.Settings)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, requestWithUser(http.MethodPut, `{"theme":"dark","folder_views":{"folder-a":{"sort":"name","dir":"asc","view":"grid"}}}`, 1))
	if rec.Code != 200 {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, requestWithUser(http.MethodPut, `{"folder_views":{"folder-b":{"sort":"size","dir":"desc","view":"list"}}}`, 1))
	if rec.Code != 200 {
		t.Fatalf("merge PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folderRaw string
	if err := db.W.QueryRow(`SELECT value FROM user_settings WHERE user_id=1 AND key='folder_views'`).Scan(&folderRaw); err != nil {
		t.Fatal(err)
	}
	var folders map[string]map[string]any
	if err := json.Unmarshal([]byte(folderRaw), &folders); err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 || folders["folder-a"]["at"] != float64(1234) || folders["folder-b"]["view"] != "list" {
		t.Fatalf("merged folder views=%v", folders)
	}
	if _, err := db.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(2,'theme','"light"')`); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, requestWithUser(http.MethodGet, "", 1))
	if rec.Code != 200 {
		t.Fatalf("GET status=%d", rec.Code)
	}
	var after struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Settings["theme"] != "dark" {
		t.Fatalf("owner theme=%v", after.Settings["theme"])
	}
	var other string
	if err := db.W.QueryRow(`SELECT value FROM user_settings WHERE user_id=2 AND key='theme'`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != `"light"` {
		t.Fatalf("other user's value changed: %s", other)
	}
}

func TestSettingsRejectUnknownOrInvalidKeysWithoutPartialWrites(t *testing.T) {
	h, db := newSettingsTest(t)
	mux := http.NewServeMux()
	h.Routes(mux)
	for _, body := range []string{`{"theme":"dark","notASetting":true}`, `{"theme":"sepia"}`, `[]`, `null`} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, requestWithUser(http.MethodPut, body, 1))
		if rec.Code != 400 {
			t.Errorf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
	var count int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM user_settings WHERE user_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid patch partially wrote %d settings", count)
	}
}

func TestAIModelMetadataRequiresIdentityAndDoesNotExposeProviderAddresses(t *testing.T) {
	h, _ := newSettingsTest(t)
	h.providerBaseURLs = map[string]string{"openai": "https://provider.example/v1"}
	mux := http.NewServeMux()
	h.Routes(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/meta/ai-providers", nil)
	request = request.WithContext(context.WithValue(request.Context(), testPrincipalKey{}, Principal{UserID: 2}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("providers status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "provider.example") {
		t.Fatalf("provider catalog exposed server address: %s", rec.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/settings/meta/models", nil)
	request = request.WithContext(context.WithValue(request.Context(), testPrincipalKey{}, Principal{UserID: 2}))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"models":[]`) {
		t.Fatalf("unconfigured model catalog status=%d body=%s", rec.Code, rec.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/settings/meta/ai-providers", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous provider status=%d", rec.Code)
	}
}

func TestSettingsRequiresSessionOrPhoneDevice(t *testing.T) {
	h, _ := newSettingsTest(t)
	mux := http.NewServeMux()
	h.Routes(mux)
	for _, tc := range []struct {
		name      string
		principal Principal
		status    int
	}{
		{"desktop bearer", Principal{UserID: 1, Device: true, Platform: "macos"}, http.StatusForbidden},
		{"phone bearer", Principal{UserID: 1, Device: true, Platform: "ios"}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/settings", nil).WithContext(context.WithValue(context.Background(), testPrincipalKey{}, tc.principal))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, r)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
