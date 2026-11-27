package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceCodeAuthorizationAndOneTimeExchange(t *testing.T) {
	runtime, handler := testAuthServerWithOptions(t, Options{PublicURL: "https://cloud.example"})
	if _, err := runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/devices/authorize", strings.NewReader(`{"name":"My Desktop","platform":"linux"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	started := httptest.NewRecorder()
	handler.ServeHTTP(started, request)
	if started.Code != http.StatusOK {
		t.Fatalf("authorize = %d: %s", started.Code, started.Body.String())
	}
	var authorization struct {
		DeviceCode      string `json:"deviceCode"`
		UserCode        string `json:"userCode"`
		VerificationURL string `json:"verificationUrl"`
		ExpiresIn       int    `json:"expiresIn"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &authorization); err != nil {
		t.Fatal(err)
	}
	if len(authorization.DeviceCode) != 43 || len(authorization.UserCode) != 9 || authorization.VerificationURL != "https://cloud.example/app/device?code="+authorization.UserCode || authorization.ExpiresIn != 600 || authorization.Interval != 3 {
		t.Fatalf("authorization = %+v", authorization)
	}
	var storedHash, status string
	if err := runtime.DB.W.QueryRow(`SELECT device_code_hash,status FROM device_authorizations WHERE user_code=?`, authorization.UserCode).Scan(&storedHash, &status); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(authorization.DeviceCode))
	if storedHash != hex.EncodeToString(hash[:]) || storedHash == authorization.DeviceCode || status != "pending" {
		t.Fatalf("stored device authorization hash=%q status=%q", storedHash, status)
	}
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	described := serveJSON(t, handler, http.MethodGet, "/api/account/devices/authorize/"+strings.ToLower(authorization.UserCode), nil, cookie)
	if described.Code != http.StatusOK || !strings.Contains(described.Body.String(), `"name":"My Desktop"`) {
		t.Fatalf("describe = %d: %s", described.Code, described.Body.String())
	}
	pending := serveJSON(t, handler, http.MethodPost, "/api/devices/token", map[string]string{"deviceCode": authorization.DeviceCode}, nil)
	if pending.Code != http.StatusPreconditionRequired || !strings.Contains(pending.Body.String(), "AUTHORIZATION_PENDING") {
		t.Fatalf("pending token = %d: %s", pending.Code, pending.Body.String())
	}
	approved := serveJSON(t, handler, http.MethodPost, "/api/account/devices/authorize/"+authorization.UserCode, map[string]bool{"approve": true}, cookie)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"success":true`) {
		t.Fatalf("approve = %d: %s", approved.Code, approved.Body.String())
	}
	exchangeResponses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/api/devices/token", strings.NewReader(`{"deviceCode":"`+authorization.DeviceCode+`"}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			exchangeResponses <- response
		}()
	}
	var exchanged *httptest.ResponseRecorder
	for range 2 {
		result := <-exchangeResponses
		if result.Code == http.StatusOK {
			if exchanged != nil {
				t.Fatal("concurrent exchange issued more than one device token")
			}
			exchanged = result
		} else if result.Code != http.StatusGone || !strings.Contains(result.Body.String(), "EXPIRED_TOKEN") {
			t.Fatalf("concurrent exchange = %d: %s", result.Code, result.Body.String())
		}
	}
	if exchanged == nil {
		t.Fatal("concurrent exchange did not issue a token")
	}
	var result struct {
		Token  string `json:"token"`
		Device struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Platform string `json:"platform"`
		} `json:"device"`
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(exchanged.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Token) != 46 || !strings.HasPrefix(result.Token, "nc_") || result.Device.ID == 0 || result.Device.Name != "My Desktop" || result.Device.Platform != "linux" || result.User.ID != 1 || result.User.Username != "cloud-user" {
		t.Fatalf("exchange payload = %+v", result)
	}
	tokenHash := sha256.Sum256([]byte(result.Token))
	var persisted string
	if err := runtime.DB.W.QueryRow(`SELECT token_hash FROM device_tokens WHERE id=?`, result.Device.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != hex.EncodeToString(tokenHash[:]) || persisted == result.Token {
		t.Fatalf("device token was not stored by hash: %q", persisted)
	}
	var issued int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM device_tokens WHERE user_id=1`).Scan(&issued); err != nil || issued != 1 {
		t.Fatalf("issued device count = %d, error = %v", issued, err)
	}
}

func TestDeviceCodeAuthorizationRejectsDesktopTokenApprovalAndExpiredCode(t *testing.T) {
	runtime, handler := testAuthServer(t)
	created := serveJSON(t, handler, http.MethodPost, "/api/devices/authorize", map[string]string{"name": "Desktop", "platform": ""}, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("authorize = %d: %s", created.Code, created.Body.String())
	}
	var authorization struct {
		DeviceCode string `json:"deviceCode"`
		UserCode   string `json:"userCode"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &authorization); err != nil {
		t.Fatal(err)
	}
	desktopToken := "nc_" + strings.Repeat("a", 43)
	desktopHash := sha256.Sum256([]byte(desktopToken))
	if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,'Desktop','linux',?)`, hex.EncodeToString(desktopHash[:])); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/account/devices/authorize/"+authorization.UserCode, nil)
	request.Header.Set("Authorization", "Bearer "+desktopToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	denied := response
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "PHONE_REQUIRED") {
		t.Fatalf("desktop token approval = %d: %s", denied.Code, denied.Body.String())
	}
	if _, err := runtime.DB.W.Exec(`UPDATE device_authorizations SET expires_at=datetime('now','-1 second') WHERE user_code=?`, authorization.UserCode); err != nil {
		t.Fatal(err)
	}
	expired := serveJSON(t, handler, http.MethodPost, "/api/devices/token", map[string]string{"deviceCode": authorization.DeviceCode}, nil)
	if expired.Code != http.StatusGone || !strings.Contains(expired.Body.String(), "EXPIRED_TOKEN") {
		t.Fatalf("expired token = %d: %s", expired.Code, expired.Body.String())
	}
}
