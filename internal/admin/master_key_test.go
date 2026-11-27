package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

func TestMasterKeyExportRequiresAdminPasswordAndTwoFactorAndAudits(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	keys, err := cryptox.NewKeyring(strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	const password = "Correct-Violet-Quartz-4827"
	hash, err := passhash.Hash(password, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`UPDATE users SET password=? WHERE id=1`, hash); err != nil {
		t.Fatal(err)
	}
	identity, err := auth.New(auth.Options{DB: db.W, Keys: keys, SessionSecret: strings.Repeat("s", 64)})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	encryptedSecret, err := keys.EncryptValue(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO user_two_factor(user_id,secret,enabled) VALUES(1,?,1)`, encryptedSecret); err != nil {
		t.Fatal(err)
	}
	const masterKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h, err := New(Options{
		DB: db.W, MasterKeyHex: masterKey,
		Reauthenticate: identity.ConfirmIdentity,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	wrongPassword := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/keys/master", `{"password":"wrong","code":""}`)
	if wrongPassword.Code != http.StatusUnauthorized || !strings.Contains(wrongPassword.Body.String(), "WRONG_PASSWORD") || strings.Contains(wrongPassword.Body.String(), masterKey) {
		t.Fatalf("wrong password=%d %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	missingCode := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/keys/master", `{"password":"`+password+`"}`)
	if missingCode.Code != http.StatusUnauthorized || !strings.Contains(missingCode.Body.String(), "TWO_FACTOR_REQUIRED") || strings.Contains(missingCode.Body.String(), masterKey) {
		t.Fatalf("missing factor=%d %s", missingCode.Code, missingCode.Body.String())
	}
	badCode := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/keys/master", `{"password":"`+password+`","code":"ABC"}`)
	if badCode.Code != http.StatusUnauthorized || !strings.Contains(badCode.Body.String(), "INVALID_CODE") || strings.Contains(badCode.Body.String(), masterKey) {
		t.Fatalf("bad factor=%d %s", badCode.Code, badCode.Body.String())
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	success := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/keys/master", `{"password":"`+password+`","code":"`+code+`"}`)
	if success.Code != http.StatusOK || !strings.Contains(success.Body.String(), `"masterKey":"`+masterKey+`"`) {
		t.Fatalf("export=%d %s", success.Code, success.Body.String())
	}
	var count int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM access_audit_log WHERE action='keys.master_export' AND actor_user_id=1 AND detail_json='{}'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("export audit count=%d err=%v", count, err)
	}
	if strings.Contains(success.Body.String(), `"detail"`) {
		t.Fatalf("unexpected export response=%s", success.Body.String())
	}
}

func TestMasterKeyExportDoesNotReturnKeyIfAuditFails(t *testing.T) {
	db, _, _, _, _ := testHandlerWithStorage(t)
	if _, err := db.W.Exec(`CREATE TRIGGER reject_master_key_audit BEFORE INSERT ON access_audit_log WHEN NEW.action='keys.master_export' BEGIN SELECT RAISE(ABORT,'audit blocked'); END`); err != nil {
		t.Fatal(err)
	}
	const masterKey = "never-return-without-audit"
	h, err := New(Options{
		DB: db.W, MasterKeyHex: masterKey,
		Reauthenticate: func(context.Context, int64, string, string) error { return nil },
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodPost, "/api/admin/keys/master", `{}`)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), masterKey) {
		t.Fatalf("failed audit response=%d %s", response.Code, response.Body.String())
	}
}
