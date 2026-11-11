package fido_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/fido"
	"github.com/neolabs/neocloud/clients/lite/internal/fido/fidotest"
)

const rpID = "neocloud-lite.local"

func open(t *testing.T, bus *fidotest.Transport, id string) *fido.Authenticator {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key, err := fido.Open(ctx, bus, fido.DeviceInfo{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { key.Close() })
	return key
}

func TestHMACSecretFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bus := fidotest.NewTransport()
	authenticator := &fidotest.Authenticator{}
	key := open(t, bus, bus.Plug(authenticator))
	touched := 0
	key.OnTouch = func() { touched++ }

	info, err := key.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasExtension("hmac-secret") || info.NeedsPIN() {
		t.Fatalf("info %+v", info)
	}
	credentialID, err := key.MakeCredential(ctx, fido.Credential{RPID: rpID, RPName: "NeoCloud Lite", UserID: []byte{1}, UserName: "neo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if touched != 1 || authenticator.Touches() != 1 {
		t.Errorf("makeCredential: OnTouch %d, touches %d; want 1, 1", touched, authenticator.Touches())
	}
	salt := bytes.Repeat([]byte{7}, 32)
	silent, err := key.HMACSecret(ctx, rpID, credentialID, salt, false)
	if err != nil {
		t.Fatal(err)
	}
	if authenticator.Touches() != 1 {
		t.Error("a silent assertion waited for a touch")
	}
	touchedOutput, err := key.HMACSecret(ctx, rpID, credentialID, salt, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(silent, touchedOutput) || len(silent) != 32 {
		t.Error("the output depends on user presence; it must not")
	}
	other, err := key.HMACSecret(ctx, rpID, credentialID, bytes.Repeat([]byte{8}, 32), false)
	if err != nil || bytes.Equal(other, silent) {
		t.Error("another salt gave the same output")
	}

	// Another key does not know the credential.
	stranger := open(t, bus, bus.Plug(&fidotest.Authenticator{}))
	_, err = stranger.HMACSecret(ctx, rpID, credentialID, salt, false)
	if !errors.Is(err, fido.ErrNoCredentials) {
		t.Errorf("foreign key: %v, want ErrNoCredentials", err)
	}
}

func TestSilentRefusedAndPIN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bus := fidotest.NewTransport()
	authenticator := &fidotest.Authenticator{PIN: "123456", NoSilent: true}
	key := open(t, bus, bus.Plug(authenticator))
	info, err := key.Info(ctx)
	if err != nil || !info.NeedsPIN() {
		t.Fatalf("NeedsPIN false for a key with a PIN (%v)", err)
	}
	cred := fido.Credential{RPID: rpID, UserID: []byte{1}, UserName: "neo"}
	if _, err := key.MakeCredential(ctx, cred, nil); !errors.Is(err, fido.ErrPINRequired) {
		t.Errorf("without PIN: %v", err)
	}
	if _, err := key.PINToken(ctx, "000000"); !errors.Is(err, fido.ErrPINInvalid) {
		t.Errorf("wrong PIN: %v", err)
	}
	token, err := key.PINToken(ctx, "123456")
	if err != nil {
		t.Fatal(err)
	}
	credentialID, err := key.MakeCredential(ctx, cred, token)
	if err != nil {
		t.Fatal(err)
	}
	salt := bytes.Repeat([]byte{1}, 32)
	if _, err := key.HMACSecret(ctx, rpID, credentialID, salt, false); !fido.NeedsTouch(err) {
		t.Errorf("silent on a key that refuses it: %v", err)
	}
	if _, err := key.HMACSecret(ctx, rpID, credentialID, salt, true); err != nil {
		t.Errorf("with a touch: %v", err)
	}
}

func TestRemovedDevice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bus := fidotest.NewTransport()
	id := bus.Plug(&fidotest.Authenticator{})
	key := open(t, bus, id)
	bus.Unplug(id)
	if _, err := key.Info(ctx); err == nil {
		t.Error("a removed key answered")
	}
}
