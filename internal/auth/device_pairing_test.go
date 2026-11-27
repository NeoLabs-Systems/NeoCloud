package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDevicePairingLifecycleAndSingleUse(t *testing.T) {
	runtime, handler := testAuthServerWithOptions(t, Options{PublicOrigin: func(r *http.Request) string { return "https://pair.example" }})
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE username='cloud-user'`); err != nil {
		t.Fatal(err)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	created := serveJSON(t, handler, http.MethodPost, "/api/account/devices/pairing", map[string]string{"name": "My Phone", "platform": "ios"}, cookie)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var payload struct {
		Code   string `json:"code"`
		QR     string `json:"qrPayload"`
		Server string `json:"server"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code == "" || payload.Server != "https://pair.example" || payload.QR == "" {
		t.Fatalf("unexpected create response: %s", created.Body.String())
	}
	hash := sha256.Sum256([]byte(payload.Code))
	var stored string
	if err := runtime.DB.W.QueryRow(`SELECT code_hash FROM device_pairings`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hex.EncodeToString(hash[:]) || stored == payload.Code {
		t.Fatalf("pairing code was not stored as SHA256: %q", stored)
	}
	var rawMatches int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM device_pairings WHERE code_hash=?`, payload.Code).Scan(&rawMatches); err != nil || rawMatches != 0 {
		t.Fatalf("raw code stored: %d, %v", rawMatches, err)
	}

	var wg sync.WaitGroup
	responses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := serveJSON(t, handler, http.MethodPost, "/api/devices/pair", map[string]string{"code": payload.Code}, nil)
			responses <- response.Code
		}()
	}
	wg.Wait()
	close(responses)
	successes, rejected := 0, 0
	for code := range responses {
		if code == http.StatusOK {
			successes++
		} else if code == http.StatusGone {
			rejected++
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("concurrent redeem statuses: successes=%d rejected=%d", successes, rejected)
	}
	status := serveJSON(t, handler, http.MethodGet, "/api/account/devices/pairing/"+payload.Code, nil, cookie)
	if status.Code != http.StatusOK || !contains(status.Body.String(), `"status":"used"`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
}

func TestDevicePairingExpiryAndOpenLimit(t *testing.T) {
	runtime, handler := testAuthServer(t)
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE username='cloud-user'`); err != nil {
		t.Fatal(err)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	cookie := login.Result().Cookies()[0]
	var code string
	for i := 0; i < 5; i++ {
		response := serveJSON(t, handler, http.MethodPost, "/api/account/devices/pairing", map[string]string{}, cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("create %d: %d %s", i, response.Code, response.Body.String())
		}
		var result struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		code = result.Code
	}
	limited := serveJSON(t, handler, http.MethodPost, "/api/account/devices/pairing", map[string]string{}, cookie)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth create status %d: %s", limited.Code, limited.Body.String())
	}
	if _, err := runtime.DB.W.Exec(`UPDATE device_pairings SET expires_at=datetime('now','-1 second') WHERE code_hash=?`, func() string { sum := sha256.Sum256([]byte(code)); return hex.EncodeToString(sum[:]) }()); err != nil {
		t.Fatal(err)
	}
	status := serveJSON(t, handler, http.MethodGet, "/api/account/devices/pairing/"+code, nil, cookie)
	if status.Code != http.StatusOK || !contains(status.Body.String(), `"status":"expired"`) {
		t.Fatalf("expired status: %d %s", status.Code, status.Body.String())
	}
}

func TestDevicePairingPolicy(t *testing.T) {
	runtime, handler := testAuthServer(t)
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE username='cloud-user'`); err != nil {
		t.Fatal(err)
	}
	unauthenticated := serveJSON(t, handler, http.MethodPost, "/api/account/devices/pairing", map[string]string{}, nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", unauthenticated.Code)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	device := serveJSON(t, handler, http.MethodPost, "/api/account/devices/pairing", map[string]string{}, login.Result().Cookies()[0])
	if device.Code != http.StatusOK {
		t.Fatalf("browser should be allowed: %d %s", device.Code, device.Body.String())
	}
	for _, platform := range []string{"macos", "ios"} {
		token := "nc_pairing-policy-" + platform
		hash := sha256.Sum256([]byte(token))
		if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) SELECT id,?,?,? FROM users WHERE username='cloud-user'`, platform, platform, hex.EncodeToString(hash[:])); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/account/devices/pairing", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if platform == "macos" && response.Code != http.StatusForbidden {
			t.Fatalf("desktop device should be rejected from account pairing route: %d %s", response.Code, response.Body.String())
		}
		if platform == "ios" && response.Code != http.StatusOK {
			t.Fatalf("phone device should be allowed: %d %s", response.Code, response.Body.String())
		}
	}
}

func contains(value, part string) bool { return strings.Contains(value, part) }
