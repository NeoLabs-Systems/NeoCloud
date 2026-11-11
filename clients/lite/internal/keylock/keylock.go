// Package keylock is the lite client's optional security key lock: the
// device token is sealed with a key that only a FIDO2 security key can
// recreate (hmac-secret), and the client syncs only while that key is
// plugged in.
//
// Enrolling creates a non-resident credential (one touch) and derives the
// sealing key from the credential's hmac-secret output for a random salt.
// While enrolled, Run watches the FIDO HID devices: a key that holds the
// credential is asked for the output without a touch (up=false), or with
// one when it refuses; the token is unsealed into memory and sync resumes.
// When that key goes away the token is dropped and sync stops. No PIN is
// sent with getAssertion, so the output is the key's "without UV" one and
// unlocking never needs the PIN, even when enrolling did.
package keylock

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	gosync "sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/fido"
)

const (
	// RPID names the credential on the key. Non-resident credentials
	// keep nothing on the key, so it is only a label bound into them.
	RPID   = "neocloud-lite.local"
	rpName = "NeoCloud Lite"

	pollInterval  = 2 * time.Second
	silentTimeout = 5 * time.Second
	touchTimeout  = 30 * time.Second

	// Messages shown in the tray, the settings page and the CLI.
	MessageLocked   = "Locked — insert your security key"
	MessageRemoved  = "Locked — security key removed"
	MessageTouch    = "Locked — touch your security key to unlock"
	MessageUnlocked = "Unlocked by the security key"
	MessageOff      = "Off"
)

// ErrPINRequired means enrolling needs the key's PIN.
var ErrPINRequired = errors.New("this security key has a PIN; enter it to turn on the lock")

var errNoKey = errors.New("no security key is plugged in")

// Hooks lets the lock stop and resume syncing (sync.Engine).
type Hooks interface {
	Locked(message string)
	Unlocked()
}

type nopHooks struct{}

func (nopHooks) Locked(string) {}
func (nopHooks) Unlocked()     {}

// Status is the lock's state for the settings page and the CLI.
type Status struct {
	Supported bool   `json:"supported"`
	Enrolled  bool   `json:"enrolled"`
	Locked    bool   `json:"locked"`
	Message   string `json:"message"`
}

// Lock watches security keys and holds the unsealed token in the config
// store's memory while the right one is plugged in.
type Lock struct {
	store       *config.Store
	hid         fido.Transport // nil where the OS has no transport
	unsupported error
	hooks       Hooks

	// Prompt, when set, shows a request to the user ("touch your key").
	Prompt func(string)
	// Logf, when set, records state changes.
	Logf func(format string, args ...any)

	devMu gosync.Mutex // one conversation with the keys at a time

	mu      gosync.Mutex
	message string
	device  string          // ID of the key that unlocked, while unlocked
	skip    map[string]bool // keys tried without success, until unplugged
}

// New returns a lock on the OS's HID transport. hooks may be nil.
func New(store *config.Store, hooks Hooks) *Lock {
	hid, err := fido.System()
	return newLock(store, hid, err, hooks)
}

func newLock(store *config.Store, hid fido.Transport, unsupported error, hooks Hooks) *Lock {
	if hooks == nil {
		hooks = nopHooks{}
	}
	return &Lock{store: store, hid: hid, unsupported: unsupported, hooks: hooks, skip: map[string]bool{}}
}

// Status reports whether the lock is available, on and locked.
func (l *Lock) Status() Status {
	cfg := l.store.Get()
	st := Status{Supported: l.hid != nil, Enrolled: cfg.KeyLock != nil, Locked: cfg.Locked()}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.hid == nil:
		st.Message = l.unsupported.Error()
	case cfg.KeyLock == nil:
		st.Message = MessageOff
	case l.message != "":
		st.Message = l.message
	case st.Locked:
		st.Message = MessageLocked
	default:
		st.Message = MessageUnlocked
	}
	return st
}

func (l *Lock) setMessage(message string) {
	l.mu.Lock()
	changed := l.message != message
	l.message = message
	l.mu.Unlock()
	if changed && l.Logf != nil {
		l.Logf("security key lock: %s", message)
	}
}

func (l *Lock) prompt(message string) {
	l.setMessage(message)
	if l.Prompt != nil {
		l.Prompt(message)
	}
}

// Run checks the keys every two seconds until ctx ends.
func (l *Lock) Run(ctx context.Context) {
	if l.hid == nil {
		return
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		l.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check is one step of the watcher. Unlocked: lock when the key that
// unlocked is gone. Locked: try each newly plugged-in key. A key without
// the credential counts as absent and is not asked again until it is
// unplugged.
func (l *Lock) Check(ctx context.Context) {
	l.devMu.Lock()
	defer l.devMu.Unlock()
	cfg := l.store.Get()
	if l.hid == nil || cfg.KeyLock == nil {
		return
	}
	devices, err := l.hid.Devices()
	if err != nil {
		devices = nil // cannot see the key: treat it as gone
		l.setMessage("Cannot list security keys: " + err.Error())
	}
	present := map[string]bool{}
	for _, info := range devices {
		present[info.ID] = true
	}
	l.mu.Lock()
	unlockedBy := l.device
	for id := range l.skip {
		if !present[id] {
			delete(l.skip, id)
		}
	}
	l.mu.Unlock()

	if !cfg.Locked() {
		if unlockedBy != "" && present[unlockedBy] {
			return
		}
		l.lock(MessageRemoved)
		return
	}
	var tried []fido.DeviceInfo
	for _, info := range devices {
		l.mu.Lock()
		skipped := l.skip[info.ID]
		l.mu.Unlock()
		if !skipped {
			tried = append(tried, info)
		}
	}
	if len(tried) == 0 {
		return
	}
	touch := func(message string) {
		l.prompt(message)
		l.hooks.Locked(message)
	}
	token, info, err := l.find(ctx, *cfg.KeyLock, cfg.Server, tried, touch)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return
	case errors.Is(err, errNoKey):
		l.lock(MessageLocked)
		return
	default:
		l.lock("Locked — " + err.Error())
		return
	}
	l.unlock(info.ID, token)
}

func (l *Lock) lock(message string) {
	l.store.SetUnlockedToken("")
	l.mu.Lock()
	l.device = ""
	l.mu.Unlock()
	l.setMessage(message)
	l.hooks.Locked(message)
}

func (l *Lock) unlock(device, token string) {
	if err := l.store.SetUnlockedToken(token); err != nil {
		return // the lock was turned off meanwhile
	}
	l.mu.Lock()
	l.device = device
	l.mu.Unlock()
	l.setMessage(MessageUnlocked)
	l.hooks.Unlocked()
}

// find asks each device in turn for the token. Devices that turn out not
// to hold the credential, time out waiting for a touch or do not open the
// token are skipped until they are unplugged; other errors (a busy key,
// no access) are retried on the next check. It returns the last error
// when no device unlocked.
func (l *Lock) find(ctx context.Context, lock config.KeyLock, server string, devices []fido.DeviceInfo, touch func(string)) (string, fido.DeviceInfo, error) {
	lastErr := errNoKey
	for _, info := range devices {
		token, err := l.unsealWith(ctx, info, lock, server, touch)
		if err == nil {
			return token, info, nil
		}
		if ctx.Err() != nil {
			return "", info, ctx.Err()
		}
		if errors.Is(err, fido.ErrNoCredentials) {
			l.mu.Lock()
			l.skip[info.ID] = true
			l.mu.Unlock()
			continue // another key: as good as absent
		}
		if errors.Is(err, fido.ErrActionTimeout) || errors.Is(err, errWrongSecret) {
			l.mu.Lock()
			l.skip[info.ID] = true
			l.mu.Unlock()
			if errors.Is(err, fido.ErrActionTimeout) {
				err = errors.New("the key was not touched; unplug it and plug it in again to retry")
			}
		}
		lastErr = err
	}
	return "", fido.DeviceInfo{}, lastErr
}

// unsealWith gets the hmac-secret output from one key and unseals the
// token with it. touch is called when the key needs a touch.
func (l *Lock) unsealWith(ctx context.Context, info fido.DeviceInfo, lock config.KeyLock, server string, touch func(string)) (string, error) {
	credentialID, err1 := base64.StdEncoding.DecodeString(lock.CredentialID)
	salt, err2 := base64.StdEncoding.DecodeString(lock.Salt)
	if err := errors.Join(err1, err2); err != nil {
		return "", fmt.Errorf("the key lock in the config is damaged: %w", err)
	}
	key, err := fido.Open(ctx, l.hid, info)
	if err != nil {
		return "", err
	}
	defer key.Close()
	secret, err := hmacSecret(ctx, key, lock.RPID, credentialID, salt, func() { touch(MessageTouch) })
	if err != nil {
		return "", err
	}
	return unseal(secret, lock.SealedToken, server)
}

// hmacSecret asks the key for the output, first without a touch; a key
// that refuses gets touch called and one more try that waits for it.
func hmacSecret(ctx context.Context, key *fido.Authenticator, rpID string, credentialID, salt []byte, touch func()) ([]byte, error) {
	silent, cancel := context.WithTimeout(ctx, silentTimeout)
	secret, err := key.HMACSecret(silent, rpID, credentialID, salt, false)
	cancel()
	if !fido.NeedsTouch(err) {
		return secret, err
	}
	touch()
	waiting, cancel := context.WithTimeout(ctx, touchTimeout)
	defer cancel()
	return key.HMACSecret(waiting, rpID, credentialID, salt, true)
}

// Unlock unseals the token with whichever plugged-in key holds the
// credential, for one-shot commands (`sync --once`). It is a no-op when
// the lock is off or already unlocked.
func (l *Lock) Unlock(ctx context.Context) error {
	l.devMu.Lock()
	defer l.devMu.Unlock()
	cfg := l.store.Get()
	if !cfg.Locked() {
		return nil
	}
	if l.hid == nil {
		return fmt.Errorf("the token is locked by a security key, but %w", l.unsupported)
	}
	devices, err := l.hid.Devices()
	if err != nil {
		return err
	}
	token, info, err := l.find(ctx, *cfg.KeyLock, cfg.Server, devices, l.prompt)
	if err != nil {
		return fmt.Errorf("locked: %w", err)
	}
	l.unlock(info.ID, token)
	return nil
}

// Enroll turns the lock on with the one plugged-in key: one touch to
// create the credential (a second only for keys that refuse silent
// assertions). pin is needed when the key has a PIN set and does not
// allow creating credentials without it; it is used once and not kept.
func (l *Lock) Enroll(ctx context.Context, pin string) error {
	l.devMu.Lock()
	defer l.devMu.Unlock()
	if l.hid == nil {
		return l.unsupported
	}
	cfg := l.store.Get()
	if !cfg.LoggedIn() {
		return errors.New("sign in first")
	}
	if cfg.KeyLock != nil {
		return errors.New("the security key lock is already on")
	}
	devices, err := l.hid.Devices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return errNoKey
	}
	if len(devices) > 1 {
		return errors.New("more than one security key is plugged in; keep only the one to use")
	}
	info := devices[0]
	credentialID, salt, secret, err := l.createCredential(ctx, info, cfg, pin)
	if err != nil {
		return err
	}
	sealed, err := seal(secret, cfg.Token, cfg.Server)
	if err != nil {
		return err
	}
	err = l.store.Update(func(c *config.Config) error {
		if c.Token != cfg.Token || c.Server != cfg.Server {
			return errors.New("the account changed while enrolling; try again")
		}
		c.KeyLock = &config.KeyLock{
			RPID:         RPID,
			CredentialID: base64.StdEncoding.EncodeToString(credentialID),
			Salt:         base64.StdEncoding.EncodeToString(salt),
			SealedToken:  sealed,
		}
		return nil
	})
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.device = info.ID
	l.skip = map[string]bool{}
	l.mu.Unlock()
	l.setMessage(MessageUnlocked)
	return nil
}

func (l *Lock) createCredential(ctx context.Context, info fido.DeviceInfo, cfg config.Config, pin string) (credentialID, salt, secret []byte, err error) {
	key, err := fido.Open(ctx, l.hid, info)
	if err != nil {
		return nil, nil, nil, err
	}
	defer key.Close()
	keyInfo, err := key.Info(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if !keyInfo.HasExtension("hmac-secret") {
		return nil, nil, nil, errors.New("this security key does not support hmac-secret, which the lock needs")
	}
	if keyInfo.Options["alwaysUv"] {
		return nil, nil, nil, errors.New("this security key asks for its PIN on every use (alwaysUv), so it cannot unlock automatically; turn that off or use another key")
	}
	var pinToken []byte
	if keyInfo.NeedsPIN() {
		if pin == "" {
			return nil, nil, nil, ErrPINRequired
		}
		if pinToken, err = key.PINToken(ctx, pin); err != nil {
			return nil, nil, nil, err
		}
	}
	userID := make([]byte, 16)
	salt = make([]byte, 32)
	if _, err := rand.Read(userID); err != nil {
		return nil, nil, nil, err
	}
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if err != nil {
			l.setMessage(MessageOff)
		}
	}()
	l.prompt("Touch your security key to turn on the lock")
	waiting, cancel := context.WithTimeout(ctx, touchTimeout)
	credentialID, err = key.MakeCredential(waiting, fido.Credential{
		RPID:     RPID,
		RPName:   rpName,
		UserID:   userID,
		UserName: userName(cfg),
	}, pinToken)
	cancel()
	if err != nil {
		return nil, nil, nil, err
	}
	// No PIN token here: unlocking will not have one either, and the key
	// derives a different output with user verification.
	secret, err = hmacSecret(ctx, key, RPID, credentialID, salt, func() { l.prompt("Touch your security key once more") })
	if err != nil {
		return nil, nil, nil, err
	}
	return credentialID, salt, secret, nil
}

// userName labels the credential for key managers that list them.
func userName(cfg config.Config) string {
	host, _ := os.Hostname()
	if host == "" {
		host = cfg.DeviceName
	}
	return cfg.Username + " on " + host
}

// Remove turns the lock off and stores the token in the clear again. It
// needs the key: the token is unsealed afresh, which also proves the key
// is present.
func (l *Lock) Remove(ctx context.Context) error {
	l.devMu.Lock()
	defer l.devMu.Unlock()
	cfg := l.store.Get()
	if cfg.KeyLock == nil {
		return errors.New("the security key lock is off")
	}
	if l.hid == nil {
		return l.unsupported
	}
	devices, err := l.hid.Devices()
	if err != nil {
		return err
	}
	touch := func(string) { l.prompt("Touch your security key to turn off the lock") }
	token, _, err := l.find(ctx, *cfg.KeyLock, cfg.Server, devices, touch)
	if err != nil {
		return fmt.Errorf("insert the security key that has the lock: %w", err)
	}
	err = l.store.Update(func(c *config.Config) error {
		c.KeyLock = nil
		c.Token = token
		return nil
	})
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.device = ""
	l.skip = map[string]bool{}
	l.mu.Unlock()
	l.setMessage(MessageOff)
	if cfg.Locked() {
		l.hooks.Unlocked()
	}
	return nil
}
