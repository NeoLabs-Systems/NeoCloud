package runtimeapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

func TestOpenCreatesCompatibleRuntimeAndKeepsSecretsAcrossRestart(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	var firstVersion int
	if err := runtime.DB.W.QueryRow(`PRAGMA user_version`).Scan(&firstVersion); err != nil {
		t.Fatal(err)
	}
	firstKey, err := runtime.Keys.DeriveKey("neocloud:backup")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var secondVersion int
	if err := runtime.DB.W.QueryRow(`PRAGMA user_version`).Scan(&secondVersion); err != nil {
		t.Fatal(err)
	}
	secondKey, err := runtime.Keys.DeriveKey("neocloud:backup")
	if err != nil {
		t.Fatal(err)
	}
	if firstVersion != schema.Version || secondVersion != firstVersion || string(firstKey) != string(secondKey) {
		t.Fatalf("runtime restart changed schema or key: versions %d -> %d", firstVersion, secondVersion)
	}
	info, err := os.Stat(filepath.Join(home, ".env"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode %v, error %v", info, err)
	}
}

func TestEnvironmentAdminGrantsAreAdditiveAuditedAndDoNotCreateMissingAccounts(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err = runtime.DB.W.Exec(`INSERT INTO users(username,email,password,is_admin) VALUES('first','first@example.test','hash',1),('OpsUser','ops@example.test','hash',0)`); err != nil {
		t.Fatal(err)
	}
	if err = applyEnvironmentAdminGrants(context.Background(), runtime.DB.W, []string{" opsuser ", "OPSUSER", "future-admin"}); err != nil {
		t.Fatal(err)
	}
	var isAdmin int
	if err = runtime.DB.W.QueryRow(`SELECT is_admin FROM users WHERE username='OpsUser'`).Scan(&isAdmin); err != nil || isAdmin != 1 {
		t.Fatalf("configured account admin=%d, err=%v", isAdmin, err)
	}
	var action, detail string
	var count int
	if err = runtime.DB.W.QueryRow(`SELECT action,detail_json,COUNT(*) OVER() FROM access_audit_log WHERE subject_user_id=(SELECT id FROM users WHERE username='OpsUser')`).Scan(&action, &detail, &count); err != nil {
		t.Fatal(err)
	}
	if action != "admin.grant" || detail != `{"source":"env"}` || count != 1 {
		t.Fatalf("grant audit = %q %q count=%d", action, detail, count)
	}
	if err = applyEnvironmentAdminGrants(context.Background(), runtime.DB.W, []string{"OpsUser", "future-admin"}); err != nil {
		t.Fatal(err)
	}
	if err = runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM users WHERE lower(username)='future-admin'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("missing configured user count=%d, err=%v", count, err)
	}
	if err = runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='admin.grant' AND subject_user_id=(SELECT id FROM users WHERE username='OpsUser')`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeat bootstrap audit count=%d, err=%v", count, err)
	}
}

func TestOpenAppliesEnvironmentAdminGrantToExistingAccount(t *testing.T) {
	home := t.TempDir()
	adminName := "ops-user"
	getenv := func(key string) string {
		switch key {
		case "NEOCLOUD_HOME":
			return home
		case "NEOCLOUD_ADMIN_USERS":
			return adminName
		default:
			return ""
		}
	}
	runtime, err := Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.DB.W.Exec(`INSERT INTO users(username,email,password,is_admin) VALUES(?,?,?,0)`, adminName, "ops@example.test", "hash"); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var isAdmin int
	if err = runtime.DB.W.QueryRow(`SELECT is_admin FROM users WHERE username=?`, adminName).Scan(&isAdmin); err != nil || isAdmin != 1 {
		t.Fatalf("startup admin=%d, err=%v", isAdmin, err)
	}
}

func TestSessionCookieAndStoredSessionSurviveRuntimeRestart(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	set := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionx.FromContext(r.Context()).Set("userId", int64(42))
		w.WriteHeader(http.StatusNoContent)
	})
	first := httptest.NewRecorder()
	runtime.SessionMiddleware(set).ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/", nil))
	cookie := first.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != "neocloud.sid" {
		t.Fatalf("session response cookies = %#v", cookie)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := sessionx.FromContext(r.Context()).String("userId"); got != "42" {
			t.Errorf("restored userId = %q, want 42", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie[0])
	second := httptest.NewRecorder()
	runtime.SessionMiddleware(read).ServeHTTP(second, request)
	if second.Code != http.StatusNoContent {
		t.Fatalf("restored session status = %d", second.Code)
	}
}

func TestOpenDoesNotReplaceMissingKeyForExistingAccount(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users (username, password) VALUES ('existing-account', 'password-hash')`); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(home, ".env")
	contents, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	var kept []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "NEOCLOUD_MASTER_KEY=") && line != "" {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(envFile, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), getenv, home); err == nil || !strings.Contains(err.Error(), "already holds encrypted data") {
		t.Fatalf("startup without the original key returned error %v", err)
	}
	contents, err = os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "NEOCLOUD_MASTER_KEY=") {
		t.Fatal("startup wrote a replacement master key")
	}
}
