package securitykey

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/fido/fidotest"
)

func TestClientDataJSON(t *testing.T) {
	got := string(ClientDataJSON("dGVzdC1jaGFsbGVuZ2U", "https://cloud.example.com"))
	want := `{"type":"webauthn.get","challenge":"dGVzdC1jaGFsbGVuZ2U","origin":"https://cloud.example.com","crossOrigin":false}`
	if got != want {
		t.Errorf("clientDataJSON\n got %s\nwant %s", got, want)
	}
	// Nothing is HTML-escaped, as in a browser.
	if got := string(ClientDataJSON("a", "https://x&y<z>")); got != `{"type":"webauthn.get","challenge":"a","origin":"https://x&y<z>","crossOrigin":false}` {
		t.Errorf("escaped: %s", got)
	}
}

func TestOrigin(t *testing.T) {
	cases := map[string]string{
		"https://Cloud.Example.com/":         "https://cloud.example.com",
		"https://cloud.example.com:443/app":  "https://cloud.example.com",
		"http://192.168.1.5:3000":            "http://192.168.1.5:3000",
		"http://localhost:80":                "http://localhost",
		"https://[::1]:8443":                 "https://[::1]:8443",
		"https://cloud.example.com:8443/x?y": "https://cloud.example.com:8443",
	}
	for in, want := range cases {
		if got, err := Origin(in); err != nil || got != want {
			t.Errorf("Origin(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Origin("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
}

func TestCheckRPID(t *testing.T) {
	ok := [][2]string{
		{"https://cloud.example.com", "cloud.example.com"},
		{"https://cloud.example.com", "example.com"},
		{"http://localhost:3000", "localhost"},
	}
	for _, c := range ok {
		if err := CheckRPID(c[0], c[1]); err != nil {
			t.Errorf("CheckRPID(%q, %q): %v", c[0], c[1], err)
		}
	}
	bad := [][2]string{
		{"https://cloud.example.com", "evil.com"},
		{"https://cloud.example.com", "com"},
		{"https://notexample.com", "example.com"},
		{"https://cloud.example.com", ""},
	}
	for _, c := range bad {
		if err := CheckRPID(c[0], c[1]); err == nil {
			t.Errorf("CheckRPID(%q, %q) accepted", c[0], c[1])
		}
	}
}

const origin = "https://cloud.example.com"

func request(allow ...[]byte) Request {
	return Request{Origin: origin, RPID: "cloud.example.com", Challenge: "Y2hhbGxlbmdl", AllowCredentials: allow, UserVerification: "preferred", Timeout: 5 * time.Second}
}

// verify checks the assertion as a server would: the signature over
// authenticatorData || SHA-256(clientDataJSON) with the credential's key.
func verify(t *testing.T, key *fidotest.Authenticator, got *Assertion) byte {
	t.Helper()
	public := key.PublicKey(got.CredentialID)
	if public == nil {
		t.Fatal("unknown credential")
	}
	hash := sha256.Sum256(got.ClientDataJSON)
	digest := sha256.Sum256(append(append([]byte(nil), got.AuthenticatorData...), hash[:]...))
	if !ecdsa.VerifyASN1(public, digest[:], got.Signature) {
		t.Fatal("signature does not verify")
	}
	rpHash := sha256.Sum256([]byte("cloud.example.com"))
	if !bytes.Equal(got.AuthenticatorData[:32], rpHash[:]) {
		t.Fatal("rpIdHash is not the relying party's")
	}
	if !bytes.Equal(got.ClientDataJSON, ClientDataJSON("Y2hhbGxlbmdl", origin)) {
		t.Fatalf("clientDataJSON %s", got.ClientDataJSON)
	}
	return got.AuthenticatorData[32]
}

func TestHIDDiscoverable(t *testing.T) {
	ctx := context.Background()
	bus := fidotest.NewTransport()
	key := &fidotest.Authenticator{}
	bus.Plug(key)
	older, _ := key.Enroll("cloud.example.com", []byte("1"))
	newer, _ := key.Enroll("cloud.example.com", []byte("2"))
	key.Enroll("other.example.com", []byte("3"))

	got, err := HID(bus).Get(ctx, request(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.CredentialID, newer) || string(got.UserHandle) != "2" {
		t.Errorf("picked %x (%q), want the newest credential", got.CredentialID, got.UserHandle)
	}
	if flags := verify(t, key, got); flags&0x01 == 0 || flags&0x04 != 0 {
		t.Errorf("flags %02x: want UP without UV", flags)
	}

	// With an allow list (a username was typed) that credential signs.
	got, err = HID(bus).Get(ctx, request(older), "")
	if err != nil || !bytes.Equal(got.CredentialID, older) {
		t.Fatalf("allow list: %v", err)
	}
	verify(t, key, got)

	if _, err := HID(bus).Get(ctx, request([]byte("unknown")), ""); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("unknown credential: %v", err)
	}
	evil := request()
	evil.RPID = "evil.com"
	if _, err := HID(bus).Get(ctx, evil, ""); err == nil || key.Touches() != 2 {
		t.Errorf("foreign relying party: %v (touches %d)", err, key.Touches())
	}
}

func TestHIDPIN(t *testing.T) {
	ctx := context.Background()
	bus := fidotest.NewTransport()
	key := &fidotest.Authenticator{PIN: "123456"}
	bus.Plug(key)
	id, _ := key.Enroll("cloud.example.com", []byte("1"))

	if _, err := HID(bus).Get(ctx, request(id), ""); !errors.Is(err, ErrPINRequired) {
		t.Errorf("no PIN: %v", err)
	}
	if _, err := HID(bus).Get(ctx, request(id), "000000"); !errors.Is(err, ErrWrongPIN) {
		t.Errorf("wrong PIN: %v", err)
	}
	got, err := HID(bus).Get(ctx, request(id), "123456")
	if err != nil {
		t.Fatal(err)
	}
	if flags := verify(t, key, got); flags&0x05 != 0x05 {
		t.Errorf("flags %02x: want UP and UV", flags)
	}
	// Discouraged: a touch only, no PIN asked.
	discouraged := request(id)
	discouraged.UserVerification = "discouraged"
	if got, err = HID(bus).Get(ctx, discouraged, ""); err != nil || verify(t, key, got)&0x04 != 0 {
		t.Errorf("discouraged: %v", err)
	}
}

func TestHIDBuiltInUV(t *testing.T) {
	bus := fidotest.NewTransport()
	key := &fidotest.Authenticator{PIN: "123456", BuiltInUV: true}
	bus.Plug(key)
	key.Enroll("cloud.example.com", []byte("1"))
	got, err := HID(bus).Get(context.Background(), request(), "")
	if err != nil {
		t.Fatal(err)
	}
	if flags := verify(t, key, got); flags&0x04 == 0 {
		t.Errorf("flags %02x: want UV from the sensor", flags)
	}
}

func TestHIDErrors(t *testing.T) {
	keyWait = 300 * time.Millisecond
	t.Cleanup(func() { keyWait = 20 * time.Second })
	ctx := context.Background()
	bus := fidotest.NewTransport()
	if _, err := HID(bus).Get(ctx, request(), ""); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	lazy := &fidotest.Authenticator{NoTouch: true}
	lazy.Enroll("cloud.example.com", []byte("1"))
	id := bus.Plug(lazy)
	if _, err := HID(bus).Get(ctx, request(), ""); !errors.Is(err, ErrTimeout) {
		t.Errorf("no touch: %v", err)
	}
	bus.Plug(&fidotest.Authenticator{})
	if _, err := HID(bus).Get(ctx, request(), ""); !errors.Is(err, ErrMultipleKeys) {
		t.Errorf("two keys: %v", err)
	}
	bus.Unplug(id)
	if _, err := HID(bus).Get(ctx, request(), ""); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("a key without credentials: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := HID(fidotest.NewTransport()).Get(cancelled, request(), ""); !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled: %v", err)
	}
}
