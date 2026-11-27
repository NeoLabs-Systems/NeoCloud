package auth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

func TestSecurityKeyChallengesPersistAcrossHandlersAndAreOneUse(t *testing.T) {
	home := t.TempDir()
	runtime, err := runtimeapp.Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	newHandler := func() *Handler {
		h, err := New(Options{DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first, second := newHandler(), newHandler()
	var value securityKeyChallenge
	value.Stage = "assertion"
	value.Expires = now.Add(5 * time.Minute)
	var pending webauthnx.Pending
	if err := json.Unmarshal([]byte(`{"session":{"challenge":"session-state"},"rpId":"example.test","expiresAt":1795781100}`), &pending); err != nil {
		t.Fatal(err)
	}
	value.Pending = &pending
	if err := first.storeSecurityKeyChallenge(context.Background(), "cross-handler-id", value); err != nil {
		t.Fatal(err)
	}
	got, ok, err := second.takeSecurityKeyChallenge(context.Background(), "cross-handler-id", "assertion")
	if err != nil || !ok {
		t.Fatalf("cross-handler consume: ok=%v err=%v", ok, err)
	}
	if got.Pending == nil || got.Pending.Session.Challenge != "session-state" {
		t.Fatalf("pending state did not persist: %#v", got.Pending)
	}
	if _, ok, err = first.takeSecurityKeyChallenge(context.Background(), "cross-handler-id", "assertion"); err != nil || ok {
		t.Fatalf("challenge replay was accepted: ok=%v err=%v", ok, err)
	}
}

func TestSecurityKeyChallengesPersistStageFailureAndExpire(t *testing.T) {
	home := t.TempDir()
	runtime, err := runtimeapp.Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	newHandler := func() *Handler {
		h, err := New(Options{DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first, second := newHandler(), newHandler()
	challenge := securityKeyChallenge{Stage: "second_factor", UserID: 42, Name: "Laptop", Platform: "macos", Expires: now.Add(time.Minute), Failures: 2}
	if err := first.storeSecurityKeyChallenge(context.Background(), "two-factor-id", challenge); err != nil {
		t.Fatal(err)
	}
	got, ok, err := second.takeSecurityKeyChallenge(context.Background(), "two-factor-id", "second_factor")
	if err != nil || !ok || got.UserID != 42 || got.Name != "Laptop" || got.Platform != "macos" || got.Failures != 2 {
		t.Fatalf("persisted second-factor state: %#v ok=%v err=%v", got, ok, err)
	}
	got.Failures++
	if err := second.storeSecurityKeyChallenge(context.Background(), "two-factor-id", got); err != nil {
		t.Fatal(err)
	}
	got, ok, err = first.takeSecurityKeyChallenge(context.Background(), "two-factor-id", "second_factor")
	if err != nil || !ok || got.Failures != 3 {
		t.Fatalf("failure count was not persisted: %#v ok=%v err=%v", got, ok, err)
	}
	got.Expires = now.Add(-time.Second)
	if err := first.storeSecurityKeyChallenge(context.Background(), "expired-id", got); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = second.takeSecurityKeyChallenge(context.Background(), "expired-id", "second_factor"); err != nil || ok {
		t.Fatalf("expired challenge was accepted: ok=%v err=%v", ok, err)
	}
}
