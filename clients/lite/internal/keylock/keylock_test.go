package keylock

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/fido/fidotest"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

const token = "nc_device_token"

// hooks records what the lock told the engine.
type hooks struct {
	mu     gosync.Mutex
	events []string
}

func (h *hooks) Locked(message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "locked: "+message)
}

func (h *hooks) Unlocked() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "unlocked")
}

func (h *hooks) last() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) == 0 {
		return ""
	}
	return h.events[len(h.events)-1]
}

type fixture struct {
	store *config.Store
	bus   *fidotest.Transport
	hooks *hooks
	lock  *Lock
	ctx   context.Context
}

func setup(t *testing.T) *fixture {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = store.Update(func(c *config.Config) error {
		c.Server, c.Token, c.Username = "https://cloud.test", token, "neo"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	f := &fixture{store: store, bus: fidotest.NewTransport(), hooks: &hooks{}, ctx: ctx}
	f.lock = newLock(store, f.bus, nil, f.hooks)
	return f
}

// reopen is a fresh process: the config from disk, a new lock.
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	store, err := config.Open(filepath.Dir(f.store.Path()))
	if err != nil {
		t.Fatal(err)
	}
	f.store, f.hooks = store, &hooks{}
	f.lock = newLock(store, f.bus, nil, f.hooks)
}

func TestSealRoundTrip(t *testing.T) {
	secret := bytes.Repeat([]byte{3}, 32)
	sealed, err := seal(secret, token, "https://cloud.test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, token) {
		t.Fatal("token visible in the sealed value")
	}
	if got, err := unseal(secret, sealed, "https://cloud.test"); err != nil || got != token {
		t.Fatalf("unseal = %q, %v", got, err)
	}
	if _, err := unseal(bytes.Repeat([]byte{4}, 32), sealed, "https://cloud.test"); !errors.Is(err, errWrongSecret) {
		t.Errorf("another secret: %v", err)
	}
	if _, err := unseal(secret, sealed, "https://other.test"); !errors.Is(err, errWrongSecret) {
		t.Errorf("another server: %v", err)
	}
	again, _ := seal(secret, token, "https://cloud.test")
	if again == sealed {
		t.Error("two seals of the same token are identical (nonce reuse)")
	}
}

func TestEnrollUnlockRemoveFlow(t *testing.T) {
	f := setup(t)
	key := &fidotest.Authenticator{}
	f.lock.Check(f.ctx) // off: nothing happens
	if f.hooks.last() != "" || f.lock.Status().Message != MessageOff {
		t.Fatalf("lock off: hooks %q, status %+v", f.hooks.last(), f.lock.Status())
	}
	if err := f.lock.Enroll(f.ctx, ""); !errors.Is(err, errNoKey) {
		t.Fatalf("enroll without a key: %v", err)
	}
	keyID := f.bus.Plug(key)
	if err := f.lock.Enroll(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	if key.Touches() != 1 {
		t.Errorf("enrolling took %d touches, want 1", key.Touches())
	}
	raw, _ := os.ReadFile(f.store.Path())
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), "sealed_token") {
		t.Fatalf("plaintext token still on disk:\n%s", raw)
	}
	if st := f.lock.Status(); !st.Enrolled || st.Locked || f.store.Get().Token != token {
		t.Fatalf("after enrolling: %+v", st)
	}
	if err := f.lock.Enroll(f.ctx, ""); err == nil {
		t.Error("enrolled twice")
	}

	// Key stays: nothing changes. Key removed: locked, token wiped.
	f.lock.Check(f.ctx)
	if f.hooks.last() != "" {
		t.Errorf("unexpected hook %q", f.hooks.last())
	}
	f.bus.Unplug(keyID)
	f.lock.Check(f.ctx)
	if cfg := f.store.Get(); !cfg.Locked() || cfg.Token != "" {
		t.Fatal("token still in memory after the key was removed")
	}
	if f.hooks.last() != "locked: "+MessageRemoved {
		t.Errorf("hook %q", f.hooks.last())
	}

	// Plugged in again: unlocked silently, no touch.
	f.bus.Plug(key)
	f.lock.Check(f.ctx)
	if cfg := f.store.Get(); cfg.Locked() || cfg.Token != token {
		t.Fatal("not unlocked by the enrolled key")
	}
	if f.hooks.last() != "unlocked" || key.Touches() != 1 {
		t.Errorf("hook %q, touches %d", f.hooks.last(), key.Touches())
	}

	// A new process starts locked and unlocks with the key.
	f.reopen(t)
	if !f.store.Get().Locked() || !f.lock.Status().Locked {
		t.Fatal("a new process starts unlocked")
	}
	f.lock.Check(f.ctx)
	if f.store.Get().Token != token {
		t.Fatal("not unlocked after a restart")
	}

	// Turning it off stores the token in the clear again.
	if err := f.lock.Remove(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if cfg := f.store.Get(); cfg.KeyLock != nil || cfg.Token != token || cfg.Locked() {
		t.Errorf("after remove: lock %v, token %q", cfg.KeyLock, cfg.Token)
	}
}

func TestForeignKeyCountsAsAbsent(t *testing.T) {
	f := setup(t)
	mine := &fidotest.Authenticator{}
	mineID := f.bus.Plug(mine)
	if err := f.lock.Enroll(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	f.bus.Unplug(mineID)
	f.lock.Check(f.ctx)
	stranger := &fidotest.Authenticator{}
	f.bus.Plug(stranger)
	f.lock.Check(f.ctx)
	f.lock.Check(f.ctx) // asked once, then skipped until unplugged
	if !f.store.Get().Locked() {
		t.Fatal("a foreign key unlocked the token")
	}
	if f.hooks.last() != "locked: "+MessageLocked {
		t.Errorf("hook %q", f.hooks.last())
	}
	if err := f.lock.Remove(f.ctx); err == nil {
		t.Error("turned off without the enrolled key")
	}
	f.bus.Plug(mine)
	f.lock.Check(f.ctx)
	if f.store.Get().Locked() {
		t.Error("the enrolled key next to a foreign one did not unlock")
	}
}

func TestTouchFallbackAndTimeout(t *testing.T) {
	f := setup(t)
	key := &fidotest.Authenticator{NoSilent: true}
	keyID := f.bus.Plug(key)
	var prompts []string
	f.lock.Prompt = func(message string) { prompts = append(prompts, message) }
	if err := f.lock.Enroll(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	if key.Touches() != 2 || len(prompts) != 2 {
		t.Errorf("a key without silent assertions: %d touches, prompts %q", key.Touches(), prompts)
	}
	f.bus.Unplug(keyID)
	f.lock.Check(f.ctx)

	// Nobody touches it: stays locked and is not asked again until replugged.
	key.NoTouch = true
	keyID = f.bus.Plug(key)
	f.lock.Check(f.ctx)
	if !f.store.Get().Locked() || !strings.Contains(f.hooks.last(), "not touched") {
		t.Fatalf("after a touch timeout: locked %v, hook %q", f.store.Get().Locked(), f.hooks.last())
	}
	if !strings.Contains(strings.Join(f.hooks.events, "\n"), MessageTouch) {
		t.Error("the tray was not asked to show the touch prompt")
	}
	key.NoTouch = false
	f.lock.Check(f.ctx)
	if !f.store.Get().Locked() {
		t.Fatal("asked again without being replugged")
	}
	f.bus.Unplug(keyID)
	f.lock.Check(f.ctx)
	f.bus.Plug(key)
	f.lock.Check(f.ctx)
	if f.store.Get().Locked() {
		t.Error("touching after replugging did not unlock")
	}
}

func TestEnrollWithPIN(t *testing.T) {
	f := setup(t)
	key := &fidotest.Authenticator{PIN: "4321"}
	keyID := f.bus.Plug(key)
	if err := f.lock.Enroll(f.ctx, ""); !errors.Is(err, ErrPINRequired) {
		t.Fatalf("no PIN: %v", err)
	}
	if err := f.lock.Enroll(f.ctx, "1111"); err == nil || f.store.Get().KeyLock != nil {
		t.Fatalf("wrong PIN: %v", err)
	}
	if err := f.lock.Enroll(f.ctx, "4321"); err != nil {
		t.Fatal(err)
	}
	// Unlocking never needs the PIN.
	f.bus.Unplug(keyID)
	f.lock.Check(f.ctx)
	f.bus.Plug(key)
	f.lock.Check(f.ctx)
	if f.store.Get().Token != token {
		t.Error("a key with a PIN did not unlock silently")
	}
}

func TestEnrollRefusals(t *testing.T) {
	f := setup(t)
	f.bus.Plug(&fidotest.Authenticator{AlwaysUV: true})
	if err := f.lock.Enroll(f.ctx, ""); err == nil || !strings.Contains(err.Error(), "alwaysUv") {
		t.Errorf("alwaysUv key: %v", err)
	}
	f.bus.Plug(&fidotest.Authenticator{})
	if err := f.lock.Enroll(f.ctx, ""); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Errorf("two keys: %v", err)
	}

	unsupported := newLock(f.store, nil, errors.New("not available on Windows"), nil)
	if st := unsupported.Status(); st.Supported || st.Message != "not available on Windows" {
		t.Errorf("unsupported status %+v", st)
	}
	if err := unsupported.Enroll(f.ctx, ""); err == nil {
		t.Error("enrolled without a transport")
	}
	unsupported.Run(f.ctx) // returns at once
}

func TestUnlockForOneShotCommands(t *testing.T) {
	f := setup(t)
	key := &fidotest.Authenticator{}
	f.bus.Plug(key)
	if err := f.lock.Enroll(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if err := f.lock.Unlock(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.store.Get().Token != token {
		t.Error("Unlock did not unseal the token")
	}
	f.reopen(t)
	f.bus = fidotest.NewTransport() // no key at all
	f.lock = newLock(f.store, f.bus, nil, nil)
	if err := f.lock.Unlock(f.ctx); err == nil || !f.store.Get().Locked() {
		t.Errorf("unlocked without a key: %v", err)
	}
}

// The real engine as hooks: locked means no client and a Locked status;
// unlocking hands the token back.
func TestEngineFollowsTheLock(t *testing.T) {
	f := setup(t)
	db, err := state.Open(filepath.Dir(f.store.Path()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := sync.New(f.store, db, nil)
	f.lock = newLock(f.store, f.bus, nil, engine)
	keyID := f.bus.Plug(&fidotest.Authenticator{})
	if err := f.lock.Enroll(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Client(); err != nil {
		t.Fatalf("unlocked engine: %v", err)
	}
	f.bus.Unplug(keyID)
	f.lock.Check(f.ctx)
	if _, err := engine.Client(); !errors.Is(err, sync.ErrLocked) {
		t.Errorf("locked engine handed out a client: %v", err)
	}
	if st := engine.Status(); st.State != sync.StateLocked || st.Message != MessageRemoved {
		t.Errorf("locked status %+v", st)
	}
	if err := engine.SyncOnce(f.ctx); err != nil {
		t.Errorf("a locked sync should do nothing: %v", err)
	}
	f.bus.Plug(&fidotest.Authenticator{}) // someone else's key
	f.lock.Check(f.ctx)
	if st := engine.Status(); st.State != sync.StateLocked {
		t.Errorf("a foreign key changed the status to %+v", st)
	}
}
