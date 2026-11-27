package admin

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendEmailTestRequiresConfiguredSenderAndAdmin(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	called := false
	h, err := New(Options{
		DB:      db.W,
		EnvFile: mailEnvFile(t, "NEOCLOUD_EMAIL_FROM=ops@example.test", "NEOCLOUD_EMAIL_SMTP_HOST=smtp.example.test"),
		OnSendTestEmail: func(context.Context, string, string) (string, error) {
			called = true
			return "", nil
		},
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.EmailTestRoutes(mux)
	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodPost, "/api/admin/config/email/test", `{}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", denied.Code, denied.Body.String())
	}
	if called {
		t.Fatal("sender called for non-admin")
	}
}

func TestSendEmailTestUsesFallbackAuditsAndSanitizesFailure(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	var recipient string
	failSend := false
	h, err := New(Options{
		DB:      db.W,
		EnvFile: mailEnvFile(t, "NEOCLOUD_EMAIL_FROM=ops@example.test", "NEOCLOUD_EMAIL_SMTP_HOST=smtp.example.test"),
		OnSendTestEmail: func(_ context.Context, to, _ string) (string, error) {
			recipient = to
			if failSend {
				return "", context.DeadlineExceeded
			}
			return to, nil
		},
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.EmailTestRoutes(mux)
	r := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/config/email/test", `{}`)
	if r.Code != http.StatusOK || recipient != "admin@example.test" {
		t.Fatalf("fallback status=%d to=%q body=%s", r.Code, recipient, r.Body.String())
	}
	var okAudit int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='email.test' AND detail_json='{"ok":true}'`).Scan(&okAudit); err != nil || okAudit != 1 {
		t.Fatalf("success audit count=%d err=%v", okAudit, err)
	}
	failSend = true
	failed := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/config/email/test", `{"to":" Test@Example.test "}`)
	if failed.Code != http.StatusUnprocessableEntity || strings.Contains(failed.Body.String(), "SMTP") {
		t.Fatalf("failure response=%d %s", failed.Code, failed.Body.String())
	}
	if recipient != "test@example.test" {
		t.Fatalf("normalized recipient=%q", recipient)
	}
	var failedAudit int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='email.test' AND detail_json='{"ok":false}'`).Scan(&failedAudit); err != nil || failedAudit != 1 {
		t.Fatalf("failure audit count=%d err=%v", failedAudit, err)
	}
}

func TestSendEmailTestValidatesConfigurationAndAddress(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	calls := 0
	h, err := New(Options{
		DB:      db.W,
		EnvFile: mailEnvFile(t, "NEOCLOUD_EMAIL_SMTP_HOST=smtp.example.test"),
		OnSendTestEmail: func(context.Context, string, string) (string, error) {
			calls++
			return "", nil
		},
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.EmailTestRoutes(mux)
	for _, tc := range []struct {
		body string
		want int
	}{{`{"to":"admin@example.test"}`, http.StatusConflict}, {`{"to":"bad-address"}`, http.StatusConflict}} {
		response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/config/email/test", tc.body)
		if response.Code != tc.want {
			t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
		}
	}
	if calls != 0 {
		t.Fatalf("sender called %d times when not configured", calls)
	}
}

func mailEnvFile(t *testing.T, lines ...string) string {
	t.Helper()
	t.Setenv("NEOCLOUD_EMAIL_FROM", "")
	t.Setenv("NEOCLOUD_EMAIL_SMTP_HOST", "")
	path := filepath.Join(t.TempDir(), "server.env")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
