package admin

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestServerConfigStorageAndSignupSettingsPersistAndValidate(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "admin-config.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(id,username,password,is_admin) VALUES(1,'administrator','x',1),(2,'account','x',0)`); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "service.env")
	if err = os.WriteFile(envFile, []byte("# preserved\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var applied StoragePolicy
	h, err := New(Options{DB: db.W, EnvFile: envFile, DefaultQuotaBytes: 10 << 30, AllowSignup: true, MaxUploadBytes: 0, TrashRetentionDays: 30, VersionRetention: 10, OnStorageChanged: func(p StoragePolicy) { applied = p }, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: p.ID}, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	get := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/config/storage", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"defaultQuotaBytes":10737418240`) {
		t.Fatalf("storage get=%d %s", get.Code, get.Body.String())
	}
	put := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/config/storage", `{"defaultQuotaBytes":"2.5GB","maxUploadBytes":"20 MB","trashRetentionDays":0,"versionRetention":3}`)
	if put.Code != 200 {
		t.Fatalf("storage put=%d %s", put.Code, put.Body.String())
	}
	if !strings.Contains(put.Body.String(), `"defaultQuotaBytes":2684354560`) || !strings.Contains(put.Body.String(), `"maxUploadBytes":20971520`) {
		t.Fatalf("normalized storage=%s", put.Body.String())
	}
	if applied != (StoragePolicy{DefaultQuotaBytes: 2684354560, MaxUploadBytes: 20 << 20, TrashRetentionDays: 0, VersionRetention: 3}) {
		t.Fatalf("live storage callback = %+v", applied)
	}
	contents, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "# preserved\n") || !strings.Contains(string(contents), "NEOCLOUD_DEFAULT_QUOTA=2684354560") || !strings.Contains(string(contents), "NEOCLOUD_TRASH_RETENTION_DAYS=0") {
		t.Fatalf("env updates not preserved: %s", contents)
	}
	invalid := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/config/storage", `{"maxUploadBytes":"not-a-size","versionRetention":9}`)
	if invalid.Code != 400 {
		t.Fatalf("invalid storage status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	get = adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/access", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"allowSignup":true`) {
		t.Fatalf("access get=%d %s", get.Code, get.Body.String())
	}
	put = adminRequest(mux, testPrincipal{ID: 1}, http.MethodPut, "/api/admin/access/signup", `{"allowSignup":false}`)
	if put.Code != 200 || !strings.Contains(put.Body.String(), `"allowSignup":false`) {
		t.Fatalf("access put=%d %s", put.Code, put.Body.String())
	}
	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodGet, "/api/admin/config", "")
	if denied.Code != 403 {
		t.Fatalf("non-admin config status=%d", denied.Code)
	}
}
