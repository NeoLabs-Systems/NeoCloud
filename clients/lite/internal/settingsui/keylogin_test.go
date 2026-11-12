package settingsui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/securitykey"
)

// fakeKey is a Platform that wants the PIN "4711" and signs whatever it is
// asked; the fake server below does not check signatures.
type fakeKey struct{ requests []securitykey.Request }

func (k *fakeKey) Get(_ context.Context, req securitykey.Request, pin string) (*securitykey.Assertion, error) {
	k.requests = append(k.requests, req)
	switch pin {
	case "":
		return nil, securitykey.ErrPINRequired
	case "4711":
		return &securitykey.Assertion{CredentialID: []byte("id"), AuthenticatorData: make([]byte, 37), Signature: []byte("sig")}, nil
	}
	return nil, securitykey.ErrWrongPIN
}

// keyServer answers the device security key routes; twoFactor makes the
// account ask for the authenticator code "123456".
func keyServer(t *testing.T, twoFactor bool) *httptest.Server {
	mux := http.NewServeMux()
	var server *httptest.Server
	token := map[string]any{"token": "nc_key", "user": map[string]any{"id": 7, "username": "ada"}}
	mux.HandleFunc("POST /api/devices/webauthn/options", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"challengeId": "c1",
			"origin":      server.URL,
			"options":     map[string]any{"challenge": "Y2hhbGxlbmdl", "rpId": "127.0.0.1", "userVerification": "preferred"},
		})
	})
	mux.HandleFunc("POST /api/devices/webauthn/verify", func(w http.ResponseWriter, r *http.Request) {
		if twoFactor {
			json.NewEncoder(w).Encode(map[string]any{"requiresTwoFactor": true, "challengeId": "c1"})
			return
		}
		json.NewEncoder(w).Encode(token)
	})
	mux.HandleFunc("POST /api/devices/webauthn/2fa", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "123456" || body["challengeId"] != "c1" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid 2FA code", "code": "INVALID_CODE"})
			return
		}
		json.NewEncoder(w).Encode(token)
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestKeyLoginWithPIN(t *testing.T) {
	s := startServer(t)
	key := &fakeKey{}
	s.keys = func() (securitykey.Platform, error) { return key, nil }
	browser := signedIn(t, s)
	server := keyServer(t, false)

	send := func(pin string) map[string]any {
		body, _ := json.Marshal(map[string]string{"server": server.URL, "username": "ada", "pin": pin})
		code, out := callAPI(t, browser, s, http.MethodPost, "/api/login/key", string(body))
		if code != http.StatusOK {
			t.Fatalf("POST /api/login/key: %d %v", code, out)
		}
		return out
	}
	if out := send(""); out["next"] != "pin" || out["error"] != nil {
		t.Fatalf("without PIN: %v", out)
	}
	if out := send("0000"); out["next"] != "pin" || out["error"] == nil {
		t.Fatalf("wrong PIN: %v", out)
	}
	if out := send("4711"); out["success"] != true {
		t.Fatalf("with PIN: %v", out)
	}
	req := key.requests[len(key.requests)-1]
	if req.Origin != server.URL || req.RPID != "127.0.0.1" || req.Challenge != "Y2hhbGxlbmdl" {
		t.Errorf("ceremony %+v", req)
	}
	cfg := s.cfg.Get()
	if cfg.Server != server.URL || cfg.Token != "nc_key" || cfg.Username != "ada" || cfg.UserID != 7 {
		t.Errorf("stored %q %q %q %d", cfg.Server, cfg.Token, cfg.Username, cfg.UserID)
	}
}

func TestKeyLoginWithTwoFactor(t *testing.T) {
	s := startServer(t)
	s.keys = func() (securitykey.Platform, error) { return &fakeKey{}, nil }
	browser := signedIn(t, s)
	server := keyServer(t, true)

	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/login/key/code", `{"code":"123456"}`); code == http.StatusOK {
		t.Fatal("a code without a pending sign-in was accepted")
	}
	body, _ := json.Marshal(map[string]string{"server": server.URL, "pin": "4711"})
	if _, out := callAPI(t, browser, s, http.MethodPost, "/api/login/key", string(body)); out["next"] != "code" {
		t.Fatalf("key: %v", out)
	}
	if _, out := callAPI(t, browser, s, http.MethodPost, "/api/login/key/code", `{"code":"000000"}`); out["next"] != "code" || out["error"] == nil {
		t.Fatalf("wrong code: %v", out)
	}
	if _, out := callAPI(t, browser, s, http.MethodPost, "/api/login/key/code", `{"code":" 123456 "}`); out["success"] != true {
		t.Fatalf("right code: %v", out)
	}
	if cfg := s.cfg.Get(); cfg.Token != "nc_key" || cfg.Server != server.URL {
		t.Errorf("stored %q %q", cfg.Server, cfg.Token)
	}
}
