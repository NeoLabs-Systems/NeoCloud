//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/fido/fidotest"
	"github.com/neolabs/neocloud/clients/lite/internal/securitykey"
)

// registerKey does what the web app does under Settings › Security: it
// registers the software key for the server's host as the relying party.
func registerKey(t *testing.T, s *server, key *fidotest.Authenticator) {
	t.Helper()
	var started struct {
		Options struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"options"`
	}
	s.mustJSON(t, "POST", "/api/account/security-keys/register/options", map[string]any{}, &started)
	userID, err := base64.RawURLEncoding.DecodeString(started.Options.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, attestation := key.Enroll(started.Options.RP.ID, userID)
	clientData := `{"type":"webauthn.create","challenge":"` + started.Options.Challenge +
		`","origin":"` + s.url + `","crossOrigin":false}`
	b64 := base64.RawURLEncoding.EncodeToString
	s.mustJSON(t, "POST", "/api/account/security-keys/register/verify", map[string]any{
		"label": "e2e key",
		"response": map[string]any{
			"id":                     b64(id),
			"rawId":                  b64(id),
			"type":                   "public-key",
			"clientExtensionResults": map[string]any{},
			"response": map[string]any{
				"clientDataJSON":    b64([]byte(clientData)),
				"attestationObject": b64(attestation),
				"transports":        []string{"usb"},
			},
		},
	}, nil)
}

// TestSecurityKeySignInAgainstRealServer: the lite client is its own
// WebAuthn client, signs the server's challenge with a key registered in
// the web app and gets a working device token, PIN included.
func TestSecurityKeySignInAgainstRealServer(t *testing.T) {
	s := startServer(t)
	host, _ := url.Parse(s.url)
	bus := fidotest.NewTransport()
	key := &fidotest.Authenticator{PIN: "4711"}
	bus.Plug(key)
	registerKey(t, s, key)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := api.New(s.url, "")
	platform := securitykey.HID(bus)

	// The key has a PIN: the first try asks for it before any touch.
	if _, err := securitykey.SignIn(ctx, client, platform, "", "", "e2e-key"); !errors.Is(err, securitykey.ErrPINRequired) {
		t.Fatalf("without PIN: %v", err)
	}
	if _, err := securitykey.SignIn(ctx, client, platform, "", "0000", "e2e-key"); !errors.Is(err, securitykey.ErrWrongPIN) {
		t.Fatalf("wrong PIN: %v", err)
	}
	for _, username := range []string{"", "neo"} {
		outcome, err := securitykey.SignIn(ctx, client, platform, username, "4711", "e2e-key")
		if err != nil {
			t.Fatalf("sign-in (username %q): %v", username, err)
		}
		if outcome.Token == nil || !strings.HasPrefix(outcome.Token.Token, "nc_") || outcome.Token.User.Username != "neo" {
			t.Fatalf("unexpected outcome: %+v", outcome)
		}
		me, err := api.New(s.url, outcome.Token.Token).Me(ctx)
		if err != nil || me.Device.Name != "e2e-key" || me.Device.Platform != "lite" {
			t.Fatalf("devices/me: %+v %v", me, err)
		}
	}

	// A key the account never registered.
	stranger := fidotest.NewTransport()
	stranger.Plug(&fidotest.Authenticator{})
	if _, err := securitykey.SignIn(ctx, client, securitykey.HID(stranger), "", "", "e2e-key"); !errors.Is(err, securitykey.ErrNotRegistered) {
		t.Fatalf("unregistered key: %v", err)
	}

	// The same server under another name is another relying party.
	other := strings.Replace(s.url, host.Hostname(), "localhost", 1)
	if _, err := securitykey.SignIn(ctx, api.New(other, ""), platform, "", "4711", "e2e-key"); err == nil {
		t.Fatal("signed in under a host the key is not registered for")
	}
}
