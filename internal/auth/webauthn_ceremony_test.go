package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

type routeAuthenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
	count  uint32
}

func newRouteAuthenticator(t *testing.T) *routeAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credentialID := make([]byte, 32)
	if _, err = rand.Read(credentialID); err != nil {
		t.Fatal(err)
	}
	return &routeAuthenticator{key: key, credID: credentialID}
}

func routeB64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func ceremonyChallenge(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Options struct {
			Challenge string `json:"challenge"`
		} `json:"options"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Options.Challenge == "" {
		t.Fatalf("missing WebAuthn challenge: %s (err=%v)", response.Body.String(), err)
	}
	return payload.Options.Challenge
}

func (a *routeAuthenticator) registration(t *testing.T, rpID, origin, challenge string) json.RawMessage {
	t.Helper()
	rpHash := sha256.Sum256([]byte(rpID))
	point, err := a.key.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		t.Fatal(err)
	}
	x, y := point[1:33], point[33:]
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte{}, rpHash[:]...)
	data = append(data, 0x45) // user present, user verified, attested credential data
	data = binary.BigEndian.AppendUint32(data, 0)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.credID)))
	data = append(data, a.credID...)
	data = append(data, cose...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": challenge, "origin": origin, "crossOrigin": false})
	encoded, _ := json.Marshal(map[string]any{
		"id": routeB64(a.credID), "rawId": routeB64(a.credID), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": routeB64(clientData), "attestationObject": routeB64(attestation), "transports": []string{"internal"}},
		"clientExtensionResults": map[string]any{},
	})
	return encoded
}

func (a *routeAuthenticator) assertion(t *testing.T, rpID, origin, challenge, userHandle string) json.RawMessage {
	t.Helper()
	a.count++
	rpHash := sha256.Sum256([]byte(rpID))
	data := append([]byte{}, rpHash[:]...)
	data = append(data, 0x05) // user present and user verified
	data = binary.BigEndian.AppendUint32(data, a.count)
	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": challenge, "origin": origin, "crossOrigin": false})
	clientHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, data...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	resp := map[string]any{"clientDataJSON": routeB64(clientData), "authenticatorData": routeB64(data), "signature": routeB64(signature)}
	if userHandle != "" {
		resp["userHandle"] = routeB64([]byte(userHandle))
	}
	encoded, _ := json.Marshal(map[string]any{"id": routeB64(a.credID), "rawId": routeB64(a.credID), "type": "public-key", "response": resp, "clientExtensionResults": map[string]any{}})
	return encoded
}

func TestSecurityKeyRegistrationAndBrowserLoginFullCeremony(t *testing.T) {
	const origin = "https://cloud.example.test"
	runtime, handler := testAuthServerWithOptions(t, Options{PublicURL: origin, AllowedOrigins: []string{origin}})
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) == 0 {
		t.Fatalf("password login=%d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	options := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/options", map[string]string{"currentPassword": "G7!mQv#2xL9rT4pZ"}, cookie)
	if options.Code != http.StatusOK || len(options.Result().Cookies()) == 0 {
		t.Fatalf("registration options=%d %s", options.Code, options.Body.String())
	}
	cookie = options.Result().Cookies()[0]
	authenticator := newRouteAuthenticator(t)
	registrationResponse := authenticator.registration(t, "cloud.example.test", origin, ceremonyChallenge(t, options))
	registered := serveJSON(t, handler, http.MethodPost, "/api/account/security-keys/register/verify", map[string]any{"response": registrationResponse, "label": "Virtual passkey"}, cookie)
	if registered.Code != http.StatusOK || !contains(registered.Body.String(), `"label":"Virtual passkey"`) {
		t.Fatalf("registration verification=%d %s", registered.Code, registered.Body.String())
	}
	var registeredCount int
	if err := runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM user_webauthn_credentials WHERE user_id=1`).Scan(&registeredCount); err != nil || registeredCount != 1 {
		t.Fatalf("registered credentials=%d err=%v", registeredCount, err)
	}

	loginOptions := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/options", map[string]string{"username": "cloud-user"}, nil)
	if loginOptions.Code != http.StatusOK || len(loginOptions.Result().Cookies()) == 0 {
		t.Fatalf("browser login options=%d %s", loginOptions.Code, loginOptions.Body.String())
	}
	assertion := authenticator.assertion(t, "cloud.example.test", origin, ceremonyChallenge(t, loginOptions), "1")
	verified := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/verify", map[string]any{"response": assertion}, loginOptions.Result().Cookies()[0])
	if verified.Code != http.StatusOK || !contains(verified.Body.String(), `"success":true`) {
		t.Fatalf("browser login verification=%d %s", verified.Code, verified.Body.String())
	}
	me := serveJSON(t, handler, http.MethodGet, "/api/auth/me", nil, verified.Result().Cookies()[0])
	if me.Code != http.StatusOK || !contains(me.Body.String(), `"username":"cloud-user"`) {
		t.Fatalf("session after WebAuthn login=%d %s", me.Code, me.Body.String())
	}

	// A replayed assertion finds its challenge already spent.
	replay := serveJSON(t, handler, http.MethodPost, "/api/auth/webauthn/login/verify", map[string]any{"response": assertion}, verified.Result().Cookies()[0])
	if replay.Code != http.StatusBadRequest || !contains(replay.Body.String(), `"code":"SECURITY_KEY_CHALLENGE_EXPIRED"`) {
		t.Fatalf("replayed assertion=%d %s", replay.Code, replay.Body.String())
	}

	// A native client signs the same ceremony for the public origin and gets a
	// device token instead of a session.
	deviceOptions := serveJSON(t, handler, http.MethodPost, "/api/devices/webauthn/options", map[string]string{}, nil)
	var started struct {
		ChallengeID string `json:"challengeId"`
		Origin      string `json:"origin"`
	}
	if err := json.Unmarshal(deviceOptions.Body.Bytes(), &started); err != nil || started.Origin != origin {
		t.Fatalf("device options=%d %s (%v)", deviceOptions.Code, deviceOptions.Body.String(), err)
	}
	device := serveJSON(t, handler, http.MethodPost, "/api/devices/webauthn/verify", map[string]any{
		"challengeId": started.ChallengeID, "name": "Lite client", "platform": "lite",
		"response": authenticator.assertion(t, "cloud.example.test", origin, ceremonyChallenge(t, deviceOptions), "1"),
	}, nil)
	if device.Code != http.StatusOK || !contains(device.Body.String(), `"token":"nc_`) {
		t.Fatalf("device security key sign-in=%d %s", device.Code, device.Body.String())
	}
}
