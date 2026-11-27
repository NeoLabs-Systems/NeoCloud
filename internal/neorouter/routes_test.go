package neorouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

const (
	adminID  = 1
	memberID = 2
)

type principalKey struct{}

// fakeRouter approves a sign-in on the second poll and lists two models.
func fakeRouter(t *testing.T) (*httptest.Server, func() []map[string]string) {
	t.Helper()
	var requests []map[string]string
	polls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(status int, body string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
		switch r.URL.Path {
		case "/oauth/device/code":
			_ = r.ParseForm()
			requests = append(requests, map[string]string{"client_id": r.Form.Get("client_id"), "client_name": r.Form.Get("client_name"), "scope": r.Form.Get("scope")})
			code := "device-" + r.Form.Get("client_name") + strings.Repeat("x", len(requests))
			polls[code] = 0
			origin := "http://" + r.Host
			reply(200, `{"device_code":"`+code+`","user_code":"ABCD-EFGH","verification_uri_complete":"`+origin+`/app/connect?code=ABCD-EFGH","interval":1,"expires_in":600}`)
		case "/oauth/token":
			_ = r.ParseForm()
			code := r.Form.Get("device_code")
			polls[code]++
			if polls[code] < 2 {
				reply(400, `{"error":"authorization_pending"}`)
				return
			}
			reply(200, `{"access_token":"nr-key","token_type":"Bearer"}`)
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer nr-key" {
				reply(401, `{"error":{"message":"Invalid key"}}`)
				return
			}
			data := map[string]string{"chat": `{"id":"auto"},{"id":"router-chat"}`, "embedding": `{"id":"router-embed"}`}[r.URL.Query().Get("type")]
			reply(200, `{"data":[`+data+`],"neorouter":{"not_allowed_for_key":{"paid":2}}}`)
		default:
			reply(404, `{}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []map[string]string { return requests }
}

type fixture struct {
	t         *testing.T
	db        *dbx.DB
	keys      *cryptox.Keyring
	envFile   string
	connector *RuntimeConnector
	mux       *http.ServeMux
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "router.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,password) VALUES('router-admin','x'),('router-member','x')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, keys: keys, envFile: filepath.Join(t.TempDir(), ".env")}
	if err := os.WriteFile(f.envFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.connector, err = NewRuntimeConnector(RuntimeOptions{DB: db.W, Keys: keys, EnvFile: f.envFile, Hostname: func() (string, error) { return "box", nil }})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalKey{}).(Principal)
			return p, ok
		},
		IsAdmin:   func(_ context.Context, id int64) (bool, error) { return id == adminID, nil },
		Connector: f.connector,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mux = http.NewServeMux()
	handler.Routes(f.mux)
	return f
}

func (f *fixture) call(p Principal, method, path, body string, status int) map[string]any {
	f.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if p.UserID != 0 {
		request = request.WithContext(context.WithValue(request.Context(), principalKey{}, p))
	}
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	if recorder.Code != status {
		f.t.Fatalf("%s %s status=%d want %d body=%s", method, path, recorder.Code, status, recorder.Body)
	}
	var result map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &result)
	return result
}

func (f *fixture) waitForSignIn(p Principal, id string) map[string]any {
	f.t.Helper()
	for range 60 {
		result := f.call(p, "GET", "/api/neorouter/connect/"+id, "", 200)
		if result["status"] != "pending" {
			return result
		}
		time.Sleep(100 * time.Millisecond)
	}
	f.t.Fatal("sign-in did not finish")
	return nil
}

func TestDeviceSignInConnectsServerAndAccount(t *testing.T) {
	router, deviceRequests := fakeRouter(t)
	f := newFixture(t)
	admin, member := Principal{UserID: adminID}, Principal{UserID: memberID}
	address := `{"scope":"server","address":"` + router.URL + `/app/"}`

	if body := f.call(member, "POST", "/api/neorouter/connect", address, 403); body["code"] != "ADMIN_REQUIRED" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(admin, "POST", "/api/neorouter/connect", `{"scope":"everyone"}`, 400); body["code"] != "INVALID_SCOPE" {
		t.Fatalf("code=%v", body["code"])
	}
	if body := f.call(admin, "POST", "/api/neorouter/connect", `{"scope":"server","address":""}`, 400); body["code"] != "INVALID_ADDRESS" {
		t.Fatalf("code=%v", body["code"])
	}
	started := f.call(admin, "POST", "/api/neorouter/connect", address, 200)
	if started["userCode"] != "ABCD-EFGH" || !strings.HasPrefix(started["url"].(string), router.URL) {
		t.Fatalf("started=%v", started)
	}
	if sent := deviceRequests()[0]; sent["client_id"] != "neocloud" || sent["client_name"] != "NeoCloud (box)" || sent["scope"] != "chat embedding transcription image" {
		t.Fatalf("device request=%v", sent)
	}
	id := started["id"].(string)
	f.call(member, "GET", "/api/neorouter/connect/"+id, "", 404)
	if result := f.waitForSignIn(admin, id); result["status"] != "completed" || result["error"] != nil {
		t.Fatalf("result=%v", result)
	}
	f.call(admin, "GET", "/api/neorouter/connect/"+id, "", 404) // handed out once

	view := f.call(admin, "GET", "/api/neorouter", "", 200)
	server := view["server"].(map[string]any)
	offer := server["offer"].(map[string]any)
	roles := offer["roles"].(map[string]any)
	if server["address"] != router.URL || server["reachable"] != true || roles["chat"] != 1.0 || roles["embeddings"] != 1.0 || roles["images"] != 0.0 || offer["notAllowed"].(map[string]any)["chat"] != 2.0 {
		t.Fatalf("server view=%v", server)
	}
	seen := f.call(member, "GET", "/api/neorouter", "", 200)
	if got := seen["server"].(map[string]any); len(got) != 1 || got["connected"] != true || seen["user"].(map[string]any)["connected"] != false {
		t.Fatalf("member view=%v", seen)
	}
	env, err := os.ReadFile(f.envFile)
	if err != nil || !strings.Contains(string(env), "NEOROUTER_API_KEY=nr-key") {
		t.Fatalf("env=%q error=%v", env, err)
	}
	// A restart reads the connection back from the env file's values.
	restarted, err := NewRuntimeConnector(RuntimeOptions{DB: f.db.W, Keys: f.keys, Env: map[string]string{"NEOROUTER_BASE_URL": router.URL + "/v1", "NEOROUTER_API_KEY": "nr-key", "NEOROUTER_ROLES": "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if connection := restarted.ServerConnection(); !connection.Connected() || connection.Handles("chat") || !connection.Trusted {
		t.Fatalf("restarted connection=%+v", connection)
	}

	if got := f.call(admin, "PUT", "/api/neorouter/server/roles", `{"roles":["chat","bogus","chat"]}`, 200)["roles"].([]any); len(got) != 1 || got[0] != "chat" {
		t.Fatalf("roles=%v", got)
	}
	f.call(admin, "PUT", "/api/neorouter/server/roles", `{"roles":"chat"}`, 400)
	f.call(member, "PUT", "/api/neorouter/server/roles", `{"roles":[]}`, 403)

	// A person may use the server's router, no other private address.
	blocked := f.call(member, "POST", "/api/neorouter/connect", `{"scope":"user","address":"http://192.168.1.50:3333"}`, 400)
	if blocked["code"] != "URL_NOT_ALLOWED" {
		t.Fatalf("code=%v", blocked["code"])
	}
	sealed, err := f.keys.EncryptValue(`{"openai":"sk-mine"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(?,'ai_provider_api_keys',?)`, memberID, sealed); err != nil {
		t.Fatal(err)
	}
	own := f.call(member, "POST", "/api/neorouter/connect", `{"scope":"user","address":"`+router.URL+`"}`, 200)
	if result := f.waitForSignIn(member, own["id"].(string)); result["status"] != "completed" {
		t.Fatalf("result=%v", result)
	}
	connection, err := f.connector.Connection(context.Background(), memberID)
	if err != nil || connection.Scope != "user" || connection.APIKey != "nr-key" || len(connection.Roles) != 4 {
		t.Fatalf("connection=%+v error=%v", connection, err)
	}
	var stored string
	if err := f.db.W.QueryRow(`SELECT value FROM user_settings WHERE user_id=? AND key='ai_provider_api_keys'`, memberID).Scan(&stored); err != nil || strings.Contains(stored, "nr-key") {
		t.Fatalf("stored=%q error=%v", stored, err)
	}
	f.call(member, "PUT", "/api/neorouter/user/roles", `{"roles":[]}`, 200)
	if connection, _ = f.connector.Connection(context.Background(), memberID); connection.Scope != "user" || len(connection.Roles) != 0 {
		t.Fatalf("connection without roles=%+v", connection)
	}

	f.call(member, "DELETE", "/api/neorouter/user", "", 200)
	if err := f.db.W.QueryRow(`SELECT value FROM user_settings WHERE user_id=? AND key='ai_provider_api_keys'`, memberID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if plain, err := f.keys.DecryptValue(stored); err != nil || plain != `{"openai":"sk-mine"}` {
		t.Fatalf("other keys after disconnect=%q error=%v", plain, err)
	}
	f.call(member, "DELETE", "/api/neorouter/server", "", 403)
	f.call(admin, "DELETE", "/api/neorouter/server", "", 200)
	if f.connector.ServerConnection().Connected() {
		t.Fatal("server connection survived disconnect")
	}
}

func TestRoutesRequireSessionOrPhone(t *testing.T) {
	f := newFixture(t)
	f.call(Principal{}, "GET", "/api/neorouter", "", 401)
	f.call(Principal{}, "DELETE", "/api/neorouter/user", "", 401)
	desktop := Principal{UserID: memberID, Device: true, Platform: "lite"}
	if body := f.call(desktop, "GET", "/api/neorouter", "", 403); body["code"] != "PHONE_REQUIRED" {
		t.Fatalf("code=%v", body["code"])
	}
	f.call(Principal{UserID: memberID, Device: true, Platform: "android"}, "GET", "/api/neorouter", "", 200)
}
