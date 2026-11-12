// Package securitykey signs in with a NeoCloud passkey or security key
// from the desktop app. The app is its own WebAuthn client: the keys are
// registered for the server's host (the relying party), so a page on
// 127.0.0.1 could not use them. It builds clientDataJSON with the server's
// origin exactly as a browser would and has the key sign its hash.
//
// macOS and Linux talk CTAP2 to the key over HID (internal/fido) and ask
// for the PIN in the app window. Windows lets only administrators open
// FIDO devices, so there the OS WebAuthn API (webauthn.dll) runs the
// ceremony and shows its own dialog, PIN prompt included.
package securitykey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/fido"
)

// Errors in the words the app window shows.
var (
	ErrNoKey         = errors.New("no security key found; plug in your key and try again")
	ErrMultipleKeys  = errors.New("more than one security key is plugged in; keep only the one to use")
	ErrNotRegistered = errors.New("this security key is not registered on this account; add it in NeoCloud under Settings › Security first")
	ErrCancelled     = errors.New("cancelled")
	ErrTimeout       = errors.New("the security key was not touched in time")
	ErrPINRequired   = errors.New("this security key has a PIN; enter it to sign in")
	ErrWrongPIN      = errors.New("wrong PIN; try again")
	ErrNoUV          = errors.New("this server requires a security key with a PIN or fingerprint")
)

// Request is a sign-in ceremony, from the server's options.
type Request struct {
	// Origin is the server's origin (https://cloud.example.com); RPID
	// must be its host or a parent domain of it.
	Origin string
	RPID   string
	// Challenge is the server's challenge, base64url without padding.
	Challenge string
	// AllowCredentials are credential ids; empty lets the key pick one
	// of its discoverable credentials.
	AllowCredentials [][]byte
	// UserVerification is required, preferred or discouraged.
	UserVerification string
	Timeout          time.Duration
}

// Assertion is what the server's verify endpoint needs.
type Assertion struct {
	CredentialID      []byte
	ClientDataJSON    []byte
	AuthenticatorData []byte
	Signature         []byte
	UserHandle        []byte
}

// Platform runs the ceremony with a key on this computer.
type Platform interface {
	// Get asks the key for an assertion. pin is used when the key
	// verifies users with a PIN; without one Get returns ErrPINRequired.
	Get(ctx context.Context, req Request, pin string) (*Assertion, error)
}

// clientData is CollectedClientData (WebAuthn §5.8.1) in the member order
// browsers serialize it.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

// ClientDataJSON is the JSON a browser builds for navigator.credentials.get:
// {"type":"webauthn.get","challenge":…,"origin":…,"crossOrigin":false}.
func ClientDataJSON(challenge, origin string) []byte {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(clientData{Type: "webauthn.get", Challenge: challenge, Origin: origin})
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// Origin is a server address's origin: scheme://host[:port], lower case,
// without default ports.
func Origin(server string) (string, error) {
	parsed, err := url.Parse(server)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("the server address must start with https:// (or http://)")
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return parsed.Scheme + "://" + host, nil
}

// CheckRPID applies the browser's rule (WebAuthn §5.1.4.1 step 8): the
// relying party must be the origin's host or a domain it belongs to, so a
// server cannot have the key sign for another site.
func CheckRPID(origin, rpID string) error {
	parsed, err := url.Parse(origin)
	if err != nil || rpID == "" {
		return errors.New("the server sent no valid security key options")
	}
	host := strings.ToLower(parsed.Hostname())
	rpID = strings.ToLower(rpID)
	if host == rpID || (strings.HasSuffix(host, "."+rpID) && strings.Contains(rpID, ".")) {
		return nil
	}
	return errors.New("this server's security keys belong to " + rpID + "; enter the server address with that name")
}

func (r Request) clientData() ([]byte, []byte, error) {
	if err := CheckRPID(r.Origin, r.RPID); err != nil {
		return nil, nil, err
	}
	data := ClientDataJSON(r.Challenge, r.Origin)
	hash := sha256.Sum256(data)
	return data, hash[:], nil
}

// HID is the platform on a raw FIDO HID transport.
func HID(transport fido.Transport) Platform { return hidPlatform{transport: transport} }

// keyWait is how long Get waits for a key to be plugged in.
var keyWait = 20 * time.Second

type hidPlatform struct{ transport fido.Transport }

func (h hidPlatform) Get(ctx context.Context, req Request, pin string) (*Assertion, error) {
	data, hash, err := req.clientData()
	if err != nil {
		return nil, err
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	info, err := h.waitForKey(ctx)
	if err != nil {
		return nil, plainError(err)
	}
	key, err := fido.Open(ctx, h.transport, info)
	if err != nil {
		return nil, plainError(err)
	}
	defer key.Close()
	keyInfo, err := key.Info(ctx)
	if err != nil {
		return nil, plainError(err)
	}
	assertion := fido.AssertionRequest{RPID: req.RPID, ClientDataHash: hash, AllowList: req.AllowCredentials}
	// As a browser does for "preferred": verify with the key's own
	// sensor, else its PIN, else settle for a touch.
	if req.UserVerification != "discouraged" {
		switch {
		case keyInfo.HasBuiltInUV():
			assertion.UV = true
		case keyInfo.HasPIN():
			if pin == "" {
				return nil, ErrPINRequired
			}
			if assertion.PINToken, err = key.PINToken(ctx, pin); err != nil {
				return nil, plainError(err)
			}
		case req.UserVerification == "required":
			return nil, ErrNoUV
		}
	}
	answers, err := key.GetAssertion(ctx, assertion)
	if err != nil {
		return nil, plainError(err)
	}
	// Several accounts on one key: the newest credential, as the key
	// lists it first. Typing the username narrows it to that account.
	first := answers[0]
	return &Assertion{
		CredentialID:      first.CredentialID,
		ClientDataJSON:    data,
		AuthenticatorData: first.AuthenticatorData,
		Signature:         first.Signature,
		UserHandle:        first.UserHandle,
	}, nil
}

// waitForKey returns the one plugged-in key, waiting up to keyWait for one.
func (h hidPlatform) waitForKey(ctx context.Context) (fido.DeviceInfo, error) {
	deadline := time.Now().Add(keyWait)
	for {
		devices, err := h.transport.Devices()
		if err != nil {
			return fido.DeviceInfo{}, err
		}
		switch {
		case len(devices) == 1:
			return devices[0], nil
		case len(devices) > 1:
			return fido.DeviceInfo{}, ErrMultipleKeys
		case time.Now().After(deadline):
			return fido.DeviceInfo{}, ErrNoKey
		}
		select {
		case <-ctx.Done():
			return fido.DeviceInfo{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// plainError turns key and context errors into the package's errors.
func plainError(err error) error {
	switch {
	case errors.Is(err, fido.ErrNoCredentials):
		return ErrNotRegistered
	case errors.Is(err, fido.ErrActionTimeout), errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout
	case errors.Is(err, fido.ErrKeepaliveCancel), errors.Is(err, fido.ErrOperationDenied), errors.Is(err, context.Canceled):
		return ErrCancelled
	case errors.Is(err, fido.ErrPINInvalid):
		return ErrWrongPIN
	case errors.Is(err, fido.ErrPINRequired):
		return ErrPINRequired
	}
	return err
}
