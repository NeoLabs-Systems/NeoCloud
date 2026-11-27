package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

func TestSelfDeleteReauthenticatesConfirmsUsernameAndCleansAccount(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}
	runtime, err := runtimeapp.Open(context.Background(), getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	password := "G7!mQv#2xL9rT4pZ"
	hash, err := passhash.Hash(password, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.DB.W.Exec(`INSERT INTO users(username,email,password) VALUES('first-admin','admin@example.test',?); INSERT INTO users(username,email,password,is_admin) VALUES('delete-me','delete@example.test',?,0)`, hash, hash); err != nil {
		t.Fatal(err)
	}
	options := Options{DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret,
		BlobDir: runtime.Config.Paths.BlobDir, PreviewDir: runtime.Config.Paths.PreviewDir, UploadTmpDir: runtime.Config.Paths.UploadTmpDir}
	h, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	server := runtime.SessionMiddleware(IdentityMiddleware(runtime.DB.W, mux))
	login := serveJSON(t, server, http.MethodPost, "/api/auth/login", map[string]string{"username": "delete-me", "password": password}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login=%d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	blobPath := filepath.Join(runtime.Config.Paths.BlobDir, "2")
	if err := os.MkdirAll(blobPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobPath, "fixture"), []byte("account data"), 0600); err != nil {
		t.Fatal(err)
	}
	wrongPassword := serveJSON(t, server, http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": "incorrect", "confirmUsername": "delete-me",
	}, cookie)
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password=%d %s", wrongPassword.Code, wrongPassword.Body.String())
	}

	badConfirmation := serveJSON(t, server, http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": password, "confirmUsername": "wrong",
	}, cookie)
	if badConfirmation.Code != http.StatusBadRequest {
		t.Fatalf("bad confirmation=%d %s", badConfirmation.Code, badConfirmation.Body.String())
	}
	var stillThere int
	if err = runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM users WHERE username='delete-me'`).Scan(&stillThere); err != nil || stillThere != 1 {
		t.Fatalf("account after rejected confirmation count=%d err=%v", stillThere, err)
	}
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	encryptedSecret, err := runtime.Keys.EncryptValue(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.DB.W.Exec(`INSERT INTO user_two_factor(user_id,secret,enabled) VALUES(2,?,1)`, encryptedSecret); err != nil {
		t.Fatal(err)
	}
	missingCode := serveJSON(t, server, http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": password, "confirmUsername": "delete-me",
	}, cookie)
	if missingCode.Code != http.StatusUnauthorized || !strings.Contains(missingCode.Body.String(), "TWO_FACTOR_REQUIRED") {
		t.Fatalf("missing factor=%d %s", missingCode.Code, missingCode.Body.String())
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	deleted := serveJSON(t, server, http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": password, "code": code, "confirmUsername": " DELETE-ME ",
	}, cookie)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"success":true`) {
		t.Fatalf("delete=%d %s", deleted.Code, deleted.Body.String())
	}
	if err = runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM users WHERE username='delete-me'`).Scan(&stillThere); err != nil || stillThere != 0 {
		t.Fatalf("deleted account count=%d err=%v", stillThere, err)
	}
	if _, err = os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("account blob directory still exists, stat err=%v", err)
	}
	var actorID, subjectID any
	var detail string
	if err = runtime.DB.W.QueryRow(`SELECT actor_user_id,subject_user_id,detail_json FROM access_audit_log WHERE action='account.delete' ORDER BY id DESC LIMIT 1`).Scan(&actorID, &subjectID, &detail); err != nil {
		t.Fatal(err)
	}
	if actorID != nil || subjectID != nil || detail != `{"userId":2,"self":true}` {
		t.Fatalf("self-delete audit actor=%v subject=%v detail=%s", actorID, subjectID, detail)
	}
	if cookieHeader := deleted.Header().Get("Set-Cookie"); !strings.Contains(cookieHeader, "Max-Age=0") && !strings.Contains(cookieHeader, "Expires=") {
		t.Fatalf("session cookie was not cleared: %q", cookieHeader)
	}
	var payload map[string]any
	if err = json.Unmarshal(deleted.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	adminLogin := serveJSON(t, server, http.MethodPost, "/api/auth/login", map[string]string{"username": "first-admin", "password": password}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login=%d %s", adminLogin.Code, adminLogin.Body.String())
	}
	lastAdmin := serveJSON(t, server, http.MethodPost, "/api/account/delete", map[string]string{
		"currentPassword": password, "confirmUsername": "first-admin",
	}, adminLogin.Result().Cookies()[0])
	if lastAdmin.Code != http.StatusConflict || !strings.Contains(lastAdmin.Body.String(), "LAST_ADMIN") {
		t.Fatalf("last admin deletion=%d %s", lastAdmin.Code, lastAdmin.Body.String())
	}
}
