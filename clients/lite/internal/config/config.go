// Package config holds the lite client's settings: the server, the device
// token and the sync pairs. They live in one TOML file in the OS config
// directory (or $NEOCLOUD_LITE_HOME), written with 0600 because it carries
// the bearer token. With the security key lock on, the file holds only
// the sealed token and Token is the unlocked copy in memory.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Directions a pair can sync in.
const (
	Both = "both"
	Up   = "up"
	Down = "down"
)

// Pair maps a local folder to a folder on the server.
type Pair struct {
	ID        string   `toml:"id" json:"id"`
	Local     string   `toml:"local" json:"local"`
	Remote    string   `toml:"remote" json:"remote"`
	Direction string   `toml:"direction" json:"direction"`
	Ignore    []string `toml:"ignore,omitempty" json:"ignore"`
	Paused    bool     `toml:"paused,omitempty" json:"paused"`
	// Created is set when the app made the local folder (the cloud-first
	// flow). Only such folders get the NeoCloud icon and the sidebar entry.
	Created bool `toml:"created,omitempty" json:"created"`
	PairSettings
}

// KeyLock is an enrolled security key lock (internal/keylock): a
// non-resident FIDO2 credential with hmac-secret and the device token
// sealed with a key derived from its output. Binary values are base64.
type KeyLock struct {
	RPID         string `toml:"rp_id"`
	CredentialID string `toml:"credential_id"`
	Salt         string `toml:"salt"`
	SealedToken  string `toml:"sealed_token"`
}

// Config is the whole file.
type Config struct {
	Server string `toml:"server"`
	// Token is the device token. It is never written while KeyLock is set.
	Token      string   `toml:"token,omitempty"`
	KeyLock    *KeyLock `toml:"key_lock,omitempty"`
	Username   string   `toml:"username,omitempty"`
	UserID     int64    `toml:"user_id,omitempty"`
	DeviceName string   `toml:"device_name,omitempty"`
	Paused     bool     `toml:"paused,omitempty"`
	// AutostartDefaulted is set once the tray app has turned on start at
	// login by default; from then on only the user's choice counts.
	AutostartDefaulted bool `toml:"autostart_defaulted,omitempty"`
	// Seconds between journal polls (docs/clients.md suggests 15).
	PollSeconds int `toml:"poll_seconds,omitempty"`
	// Seconds between full local rescans, the safety net behind the watcher.
	RescanSeconds int `toml:"rescan_seconds,omitempty"`
	// Bandwidth caps in KiB/s; 0 is unlimited.
	UploadKiBps   int64 `toml:"upload_kibps,omitempty"`
	DownloadKiBps int64 `toml:"download_kibps,omitempty"`
	GlobalSettings
	// Pinned is the folder the app put in the sidebar, so it can be taken
	// out again when it is no longer wanted.
	Pinned string `toml:"pinned,omitempty"`
	Pairs  []Pair `toml:"pairs"`
}

// LoggedIn reports whether a device token is stored, in the clear or
// sealed by the key lock.
func (c Config) LoggedIn() bool { return c.Server != "" && (c.Token != "" || c.KeyLock != nil) }

// Locked reports whether the token is sealed and not unlocked.
func (c Config) Locked() bool { return c.KeyLock != nil && c.Token == "" }

// Pair returns the pair with the given id.
func (c Config) Pair(id string) (Pair, bool) {
	for _, pair := range c.Pairs {
		if pair.ID == id {
			return pair, true
		}
	}
	return Pair{}, false
}

// Dir is where config and state live: $NEOCLOUD_LITE_HOME, else
// <user config dir>/neocloud-lite.
func Dir() (string, error) {
	if home := os.Getenv("NEOCLOUD_LITE_HOME"); home != "" {
		return home, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "neocloud-lite"), nil
}

// Store guards the config shared by the engine, tray and settings page.
type Store struct {
	path string
	mu   sync.Mutex
	cfg  Config
}

// Open loads (or starts) the config file in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	store := &Store{path: filepath.Join(dir, "config.toml")}
	if _, err := toml.DecodeFile(store.path, &store.cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading %s: %w", store.path, err)
	}
	if store.cfg.KeyLock != nil {
		store.cfg.Token = "" // a stray plaintext token next to a lock is not used, and dropped on the next write
	}
	for i := range store.cfg.Pairs {
		store.cfg.Pairs[i].normalize()
	}
	return store, nil
}

// Path is the config file's location.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current config.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cfg)
}

// Update applies fn to the config and saves it; nothing is saved when fn fails.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	if err := write(s.path, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

// SetUnlockedToken keeps the token the key lock unsealed in memory only;
// "" locks again. It never touches the file.
func (s *Store) SetUnlockedToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.KeyLock == nil {
		return errors.New("the security key lock is off")
	}
	s.cfg.Token = token
	return nil
}

func clone(c Config) Config {
	if c.KeyLock != nil {
		lock := *c.KeyLock
		c.KeyLock = &lock
	}
	c.Pairs = append([]Pair(nil), c.Pairs...)
	for i := range c.Pairs {
		c.Pairs[i].Ignore = append([]string(nil), c.Pairs[i].Ignore...)
		c.Pairs[i].ExcludePaths = append([]string(nil), c.Pairs[i].ExcludePaths...)
	}
	return c
}

// write replaces the file atomically so a crash never leaves half a config.
// With a key lock the plaintext token stays in memory.
func write(path string, cfg Config) error {
	if cfg.KeyLock != nil {
		cfg.Token = ""
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// NewPair validates and normalizes a pair. The local folder must exist (an
// unmounted drive must never look like "everything was deleted"), unless
// create is set: then it must not exist yet, and the caller makes it with
// CreateLocal once the pair was accepted.
func NewPair(local, remote, direction string, ignore []string, create bool) (Pair, error) {
	abs, err := filepath.Abs(local)
	if err != nil {
		return Pair{}, err
	}
	info, err := os.Stat(abs)
	switch {
	case create && err == nil:
		return Pair{}, fmt.Errorf("%s already exists; choose another name", abs)
	case create && !errors.Is(err, os.ErrNotExist):
		return Pair{}, fmt.Errorf("local folder: %w", err)
	case !create && err != nil:
		return Pair{}, fmt.Errorf("local folder: %w", err)
	case !create && !info.IsDir():
		return Pair{}, fmt.Errorf("%s is not a folder", abs)
	}
	if direction == "" {
		direction = Both
	}
	if direction != Both && direction != Up && direction != Down {
		return Pair{}, fmt.Errorf("direction must be both, up or down")
	}
	pair := Pair{
		ID:        randomID(),
		Local:     abs,
		Remote:    CleanRemote(remote),
		Direction: direction,
		Ignore:    ignore,
		Created:   create,
	}
	if pair.Remote == "" {
		// The whole cloud: a folder someone adds later is not pulled down
		// without asking.
		pair.NewFolders = NewFoldersAsk
	}
	return pair, nil
}

// CleanRemote turns "/a/b/", "a/b" or "" into "a/b" or "" (the root).
func CleanRemote(remote string) string {
	parts := strings.FieldsFunc(remote, func(r rune) bool { return r == '/' || r == '\\' })
	return strings.Join(parts, "/")
}

// AddPair appends a pair, refusing local folders that overlap another pair:
// two pairs syncing the same files would fight over them.
func (c *Config) AddPair(pair Pair) error {
	for _, other := range c.Pairs {
		if overlaps(other.Local, pair.Local) {
			return fmt.Errorf("%s overlaps the pair %s", pair.Local, other.Local)
		}
	}
	c.Pairs = append(c.Pairs, pair)
	return nil
}

// RemovePair drops a pair by id or by local path.
func (c *Config) RemovePair(key string) (Pair, bool) {
	for i, pair := range c.Pairs {
		if pair.ID == key || pair.Local == key {
			c.Pairs = append(c.Pairs[:i], c.Pairs[i+1:]...)
			return pair, true
		}
	}
	return Pair{}, false
}

func overlaps(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	sep := string(filepath.Separator)
	return a == b || strings.HasPrefix(a, b+sep) || strings.HasPrefix(b, a+sep)
}

func randomID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// DefaultDeviceName is the host name, used in conflict copies and the
// device list in the web app.
func DefaultDeviceName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "neocloud-lite"
	}
	// "neo-mbp.local" or "neo-mbp.fritz.box" → "neo-mbp"
	name, _, _ := strings.Cut(host, ".")
	return name
}
