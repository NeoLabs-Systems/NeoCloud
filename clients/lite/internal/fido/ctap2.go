package fido

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// CTAP2 commands (CTAP 2.1 §6).
const (
	ctapMakeCredential = 0x01
	ctapGetAssertion   = 0x02
	ctapGetInfo        = 0x04
	ctapClientPIN      = 0x06

	pinGetKeyAgreement = 0x02
	pinGetPinToken     = 0x05

	pinProtocolOne = 1
	coseES256      = -7
	hmacSecret     = "hmac-secret"

	// authenticator data flags (WebAuthn §6.1)
	flagAttested   = 0x40
	flagExtensions = 0x80
)

// StatusError is a CTAP2 status code other than CTAP2_OK.
type StatusError byte

// Status codes the lock and the sign-in handle (CTAP 2.1 §8.2).
const (
	ErrOperationDenied   StatusError = 0x27
	ErrUnsupportedOption StatusError = 0x2b
	ErrInvalidOption     StatusError = 0x2c
	ErrKeepaliveCancel   StatusError = 0x2d
	ErrNoCredentials     StatusError = 0x2e
	ErrActionTimeout     StatusError = 0x2f
	ErrPINInvalid        StatusError = 0x31
	ErrPINBlocked        StatusError = 0x32
	ErrPINAuthBlocked    StatusError = 0x34
	ErrPINRequired       StatusError = 0x36
	ErrUPRequired        StatusError = 0x3b
)

func (e StatusError) Error() string {
	switch e {
	case ErrOperationDenied:
		return "the security key refused the request"
	case ErrKeepaliveCancel:
		return "cancelled"
	case ErrNoCredentials:
		return "the security key does not hold this credential"
	case ErrActionTimeout:
		return "the security key was not touched in time"
	case ErrPINInvalid:
		return "wrong PIN"
	case ErrPINBlocked:
		return "the security key's PIN is blocked; reset the key"
	case ErrPINAuthBlocked:
		return "too many wrong PINs; unplug the key and plug it in again"
	case ErrPINRequired:
		return "the security key requires its PIN"
	}
	return fmt.Sprintf("security key error 0x%02x", byte(e))
}

// NeedsTouch reports whether err means the key refused a silent
// (up=false) assertion and wants a touch instead.
func NeedsTouch(err error) bool {
	var status StatusError
	if !errors.As(err, &status) {
		return false
	}
	return status == ErrUnsupportedOption || status == ErrInvalidOption || status == ErrUPRequired
}

var (
	encMode, _ = cbor.CTAP2EncOptions().EncMode()
	decMode, _ = cbor.DecOptions{}.DecMode()
)

// coseKey is an EC2 P-256 public key for ECDH-ES+HKDF-256 (COSE alg -25),
// the key-agreement key of the PIN/UV auth protocol.
type coseKey struct {
	Kty int    `cbor:"1,keyasint"`
	Alg int    `cbor:"3,keyasint"`
	Crv int    `cbor:"-1,keyasint"`
	X   []byte `cbor:"-2,keyasint"`
	Y   []byte `cbor:"-3,keyasint"`
}

type rpEntity struct {
	ID   string `cbor:"id"`
	Name string `cbor:"name,omitempty"`
}

type userEntity struct {
	ID          []byte `cbor:"id"`
	Name        string `cbor:"name,omitempty"`
	DisplayName string `cbor:"displayName,omitempty"`
}

type credentialParameter struct {
	Alg  int    `cbor:"alg"`
	Type string `cbor:"type"`
}

type credentialDescriptor struct {
	ID   []byte `cbor:"id"`
	Type string `cbor:"type"`
}

type makeCredentialRequest struct {
	ClientDataHash    []byte                `cbor:"1,keyasint"`
	RP                rpEntity              `cbor:"2,keyasint"`
	User              userEntity            `cbor:"3,keyasint"`
	PubKeyCredParams  []credentialParameter `cbor:"4,keyasint"`
	Extensions        map[string]bool       `cbor:"6,keyasint,omitempty"`
	PinUvAuthParam    []byte                `cbor:"8,keyasint,omitempty"`
	PinUvAuthProtocol uint                  `cbor:"9,keyasint,omitempty"`
}

type makeCredentialResponse struct {
	Fmt      string `cbor:"1,keyasint"`
	AuthData []byte `cbor:"2,keyasint"`
}

type hmacSecretInput struct {
	KeyAgreement coseKey `cbor:"1,keyasint"`
	SaltEnc      []byte  `cbor:"2,keyasint"`
	SaltAuth     []byte  `cbor:"3,keyasint"`
}

type assertionExtensions struct {
	HMACSecret hmacSecretInput `cbor:"hmac-secret"`
}

type getAssertionRequest struct {
	RPID           string                 `cbor:"1,keyasint"`
	ClientDataHash []byte                 `cbor:"2,keyasint"`
	AllowList      []credentialDescriptor `cbor:"3,keyasint"`
	Extensions     assertionExtensions    `cbor:"4,keyasint"`
	Options        map[string]bool        `cbor:"5,keyasint,omitempty"`
}

type getAssertionResponse struct {
	Credential credentialDescriptor `cbor:"1,keyasint"`
	AuthData   []byte               `cbor:"2,keyasint"`
}

type clientPINRequest struct {
	Protocol     uint     `cbor:"1,keyasint"`
	SubCommand   uint     `cbor:"2,keyasint"`
	KeyAgreement *coseKey `cbor:"3,keyasint,omitempty"`
	PinHashEnc   []byte   `cbor:"6,keyasint,omitempty"`
}

type clientPINResponse struct {
	KeyAgreement *coseKey `cbor:"1,keyasint"`
	PinToken     []byte   `cbor:"2,keyasint"`
}

// Info is the part of authenticatorGetInfo the lock needs.
type Info struct {
	Versions     []string        `cbor:"1,keyasint"`
	Extensions   []string        `cbor:"2,keyasint"`
	Options      map[string]bool `cbor:"4,keyasint"`
	PinProtocols []uint          `cbor:"6,keyasint"`
}

// HasExtension reports whether the key lists the extension.
func (i Info) HasExtension(name string) bool {
	for _, ext := range i.Extensions {
		if ext == name {
			return true
		}
	}
	return false
}

// NeedsPIN reports whether makeCredential needs a PIN token: a PIN is set
// and the key does not allow creating credentials without it.
func (i Info) NeedsPIN() bool {
	return i.Options["clientPin"] && !i.Options["makeCredUvNotRqd"]
}

// authData is the parsed authenticator data (WebAuthn §6.1).
type authData struct {
	flags        byte
	credentialID []byte
	extensions   map[string]cbor.RawMessage
}

func parseAuthData(raw []byte) (authData, error) {
	var out authData
	if len(raw) < 37 { // rpIdHash, flags, signCount
		return out, errors.New("fido: authenticator data too short")
	}
	out.flags = raw[32]
	rest := raw[37:]
	if out.flags&flagAttested != 0 {
		if len(rest) < 18 { // aaguid, credentialIdLength
			return out, errors.New("fido: attested credential data too short")
		}
		size := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if len(rest) < size {
			return out, errors.New("fido: credential id truncated")
		}
		out.credentialID, rest = rest[:size], rest[size:]
		var publicKey cbor.RawMessage
		var err error
		if rest, err = decMode.UnmarshalFirst(rest, &publicKey); err != nil {
			return out, fmt.Errorf("fido: credential public key: %w", err)
		}
	}
	if out.flags&flagExtensions != 0 {
		var err error
		if rest, err = decMode.UnmarshalFirst(rest, &out.extensions); err != nil {
			return out, fmt.Errorf("fido: extensions: %w", err)
		}
	}
	if len(rest) != 0 {
		return out, errors.New("fido: trailing bytes after authenticator data")
	}
	return out, nil
}

// Authenticator is an opened security key with a CTAPHID channel.
type Authenticator struct {
	dev  Device
	conn *conn
	// OnTouch, when set, is called once per request when the key starts
	// waiting for a touch (a keepalive with status UPNEEDED).
	OnTouch func()
}

// Open opens the device and allocates a channel.
func Open(ctx context.Context, transport Transport, info DeviceInfo) (*Authenticator, error) {
	dev, err := transport.Open(info)
	if err != nil {
		return nil, err
	}
	c, err := newConn(ctx, dev)
	if err != nil {
		dev.Close()
		return nil, err
	}
	return &Authenticator{dev: dev, conn: c}, nil
}

// Close releases the device.
func (a *Authenticator) Close() error { return a.dev.Close() }

func (a *Authenticator) call(ctx context.Context, command byte, request, response any) error {
	var body []byte
	if request != nil {
		var err error
		if body, err = encMode.Marshal(request); err != nil {
			return err
		}
	}
	prompted := false
	a.conn.onKeepalive = func(status byte) {
		if status == keepaliveUPNeeded && !prompted && a.OnTouch != nil {
			prompted = true
			a.OnTouch()
		}
	}
	reply, err := a.conn.cbor(ctx, command, body)
	if err != nil {
		return err
	}
	if response == nil {
		return nil
	}
	return decMode.Unmarshal(reply, response)
}

// Info runs authenticatorGetInfo.
func (a *Authenticator) Info(ctx context.Context) (Info, error) {
	var info Info
	err := a.call(ctx, ctapGetInfo, nil, &info)
	return info, err
}

// keyAgreement runs clientPIN getKeyAgreement and agrees on a shared
// secret with a fresh platform key.
func (a *Authenticator) keyAgreement(ctx context.Context) (coseKey, []byte, error) {
	var resp clientPINResponse
	err := a.call(ctx, ctapClientPIN, clientPINRequest{Protocol: pinProtocolOne, SubCommand: pinGetKeyAgreement}, &resp)
	if err != nil {
		return coseKey{}, nil, err
	}
	if resp.KeyAgreement == nil {
		return coseKey{}, nil, errors.New("fido: no key agreement key in the response")
	}
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return coseKey{}, nil, err
	}
	return agree(priv, *resp.KeyAgreement)
}

// PINToken runs clientPIN getPinToken (PIN protocol 1, §6.5.5.7.1).
func (a *Authenticator) PINToken(ctx context.Context, pin string) ([]byte, error) {
	if len(pin) < 4 || len(pin) > 63 {
		return nil, errors.New("a security key PIN has 4 to 63 characters")
	}
	platform, shared, err := a.keyAgreement(ctx)
	if err != nil {
		return nil, err
	}
	pinHash := sha256.Sum256([]byte(pin))
	var resp clientPINResponse
	err = a.call(ctx, ctapClientPIN, clientPINRequest{
		Protocol:     pinProtocolOne,
		SubCommand:   pinGetPinToken,
		KeyAgreement: &platform,
		PinHashEnc:   encrypt(shared, pinHash[:16]),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return decrypt(shared, resp.PinToken)
}

// Credential describes the credential MakeCredential creates.
type Credential struct {
	RPID     string
	RPName   string
	UserID   []byte
	UserName string
}

// MakeCredential creates a non-resident ES256 credential with the
// hmac-secret extension and returns its id. pinToken is nil when the key
// needs no PIN. The key waits for a touch.
func (a *Authenticator) MakeCredential(ctx context.Context, cred Credential, pinToken []byte) ([]byte, error) {
	clientDataHash, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	req := makeCredentialRequest{
		ClientDataHash:   clientDataHash,
		RP:               rpEntity{ID: cred.RPID, Name: cred.RPName},
		User:             userEntity{ID: cred.UserID, Name: cred.UserName, DisplayName: cred.UserName},
		PubKeyCredParams: []credentialParameter{{Alg: coseES256, Type: "public-key"}},
		Extensions:       map[string]bool{hmacSecret: true},
	}
	if pinToken != nil {
		req.PinUvAuthParam = authenticate(pinToken, clientDataHash)
		req.PinUvAuthProtocol = pinProtocolOne
	}
	var resp makeCredentialResponse
	if err := a.call(ctx, ctapMakeCredential, req, &resp); err != nil {
		return nil, err
	}
	data, err := parseAuthData(resp.AuthData)
	if err != nil {
		return nil, err
	}
	if len(data.credentialID) == 0 {
		return nil, errors.New("fido: the key returned no credential")
	}
	var enabled bool
	if raw, ok := data.extensions[hmacSecret]; !ok || decMode.Unmarshal(raw, &enabled) != nil || !enabled {
		return nil, errors.New("the security key did not enable hmac-secret for the credential")
	}
	return data.credentialID, nil
}

// HMACSecret runs getAssertion with the hmac-secret extension and returns
// HMAC-SHA-256(CredRandom, salt) for the credential (§12.5). With
// presence false the key is asked not to wait for a touch (up=false);
// keys that refuse return an error for which NeedsTouch is true. No PIN
// is sent, so the key uses its CredRandom without UV, and the output is
// the same with or without a touch.
func (a *Authenticator) HMACSecret(ctx context.Context, rpID string, credentialID, salt []byte, presence bool) ([]byte, error) {
	if len(salt) != 32 {
		return nil, errors.New("fido: the hmac-secret salt must be 32 bytes")
	}
	platform, shared, err := a.keyAgreement(ctx)
	if err != nil {
		return nil, err
	}
	clientDataHash, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	saltEnc := encrypt(shared, salt)
	req := getAssertionRequest{
		RPID:           rpID,
		ClientDataHash: clientDataHash,
		AllowList:      []credentialDescriptor{{ID: credentialID, Type: "public-key"}},
		Extensions: assertionExtensions{HMACSecret: hmacSecretInput{
			KeyAgreement: platform,
			SaltEnc:      saltEnc,
			SaltAuth:     authenticate(shared, saltEnc),
		}},
	}
	if !presence {
		req.Options = map[string]bool{"up": false}
	}
	var resp getAssertionResponse
	if err := a.call(ctx, ctapGetAssertion, req, &resp); err != nil {
		return nil, err
	}
	data, err := parseAuthData(resp.AuthData)
	if err != nil {
		return nil, err
	}
	var output []byte
	if raw, ok := data.extensions[hmacSecret]; !ok || decMode.Unmarshal(raw, &output) != nil {
		return nil, errors.New("fido: the key returned no hmac-secret output")
	}
	plain, err := decrypt(shared, output)
	if err != nil {
		return nil, err
	}
	if len(plain) != 32 {
		return nil, errors.New("fido: unexpected hmac-secret output length")
	}
	return plain, nil
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	return buf, err
}
