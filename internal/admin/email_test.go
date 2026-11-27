package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminEmailConfigIsWriteOnlyAndLiveUpdates(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	envFile := filepath.Join(t.TempDir(), "server.env")
	if err := os.WriteFile(envFile, []byte("# keep this\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key := range map[string]bool{"NEOCLOUD_EMAIL_FROM": true, "NEOCLOUD_EMAIL_SMTP_HOST": true, "NEOCLOUD_EMAIL_SMTP_PASS": true, "NEOCLOUD_EMAIL_SMTP_PORT": true, "NEOCLOUD_EMAIL_BRAND_NAME": true, "NEOCLOUD_EMAIL_SMTP_SECURE": true, "NEOCLOUD_EMAIL_TOKEN_TTL_HOURS": true} {
		t.Setenv(key, "")
	}
	if err := os.WriteFile(envFile, []byte("# keep this\nNEOCLOUD_EMAIL_FROM=old@example.test\nNEOCLOUD_EMAIL_SMTP_HOST=smtp.example.test\nNEOCLOUD_EMAIL_SMTP_PASS=existing-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	applied := 0
	h, err := New(Options{DB: db.W, EnvFile: envFile,
		OnEmailChanged: func() { applied++ },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.ServerConfigRoutes(mux)
	admin := testPrincipal{ID: 1}

	get := adminRequest(mux, admin, http.MethodGet, "/api/admin/config/email", "")
	if get.Code != http.StatusOK {
		t.Fatalf("get=%d %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "existing-secret") || !strings.Contains(get.Body.String(), `"smtpPasswordConfigured":true`) {
		t.Fatalf("password was exposed or configured indicator missing: %s", get.Body.String())
	}

	put := adminRequest(mux, admin, http.MethodPut, "/api/admin/config/email", `{"from":"Neo Ops <ops@example.test>","smtpHost":"smtp2.example.test","smtpPort":465,"smtpPassword":"","brandName":"Neo # Mail","smtpSecure":true,"tokenTtlHours":48}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	if !strings.Contains(put.Body.String(), `"smtpPasswordConfigured":true`) || strings.Contains(put.Body.String(), "existing-secret") {
		t.Fatalf("response password handling: %s", put.Body.String())
	}
	if applied != 1 || !strings.Contains(put.Body.String(), `"brandName":"Neo # Mail"`) || !strings.Contains(put.Body.String(), `"smtpPort":465`) {
		t.Fatalf("applied=%d response=%s", applied, put.Body.String())
	}
	// The TLS defaults follow the port, as the sender resolves them.
	if !strings.Contains(put.Body.String(), `"smtpRequireTls":false`) {
		t.Fatalf("require TLS default on an implicit TLS port: %s", put.Body.String())
	}
	contents, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "# keep this\n") || !strings.Contains(string(contents), `NEOCLOUD_EMAIL_BRAND_NAME="Neo # Mail"`) || !strings.Contains(string(contents), "NEOCLOUD_EMAIL_SMTP_PORT=465") {
		t.Fatalf("env file not safely persisted: %s", contents)
	}

	var auditDetail string
	if err = db.W.QueryRow(`SELECT detail_json FROM access_audit_log WHERE action='email.config' ORDER BY id DESC LIMIT 1`).Scan(&auditDetail); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(auditDetail, "existing-secret") || strings.Contains(auditDetail, "Neo Ops") {
		t.Fatalf("audit contains sensitive config: %s", auditDetail)
	}
	var parsed map[string]any
	if err = json.Unmarshal([]byte(auditDetail), &parsed); err != nil || parsed["configured"] != true {
		t.Fatalf("audit detail=%s err=%v", auditDetail, err)
	}

	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodGet, "/api/admin/config/email", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin get=%d %s", denied.Code, denied.Body.String())
	}
}

func TestAdminEmailConfigValidatesAndCanClearPassword(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	envFile := filepath.Join(t.TempDir(), "server.env")
	t.Setenv("NEOCLOUD_EMAIL_SMTP_PASS", "")
	if err := os.WriteFile(envFile, []byte("NEOCLOUD_EMAIL_SMTP_PASS=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, EnvFile: envFile, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: p.ID}, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.ServerConfigRoutes(mux)
	admin := testPrincipal{ID: 1}
	invalid := adminRequest(mux, admin, http.MethodPut, "/api/admin/config/email", `{"smtpPort":70000}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid port status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	if _, err = os.Stat(envFile); err != nil {
		t.Fatal(err)
	}
	cleared := adminRequest(mux, admin, http.MethodPut, "/api/admin/config/email", `{"clearSmtpPassword":true}`)
	if cleared.Code != http.StatusOK || strings.Contains(cleared.Body.String(), `"smtpPasswordConfigured":true`) {
		t.Fatalf("clear response=%d %s", cleared.Code, cleared.Body.String())
	}
	contents, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "=secret") || !strings.Contains(string(contents), `NEOCLOUD_EMAIL_SMTP_PASS=""`) {
		t.Fatalf("password clear not persisted: %s", contents)
	}
}
