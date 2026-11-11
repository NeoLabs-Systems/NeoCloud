// Package config holds the lite client's settings: the server, the device
// token and the sync pairs. They live in one TOML file in the OS config
// directory (or $NEOCLOUD_LITE_HOME), written with 0600 because it carries
// the bearer token.
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
}

// Config is the whole file.
type Config struct {
	Server     string `toml:"server"`
	Token      string `toml:"token,omitempty"`
	Username   string `toml:"username,omitempty"`
	UserID     int64  `toml:"user_id,omitempty"`
	DeviceName string `toml:"device_name,omitempty"`
	Paused     bool   `toml:"paused,omitempty"`
	// Seconds between journal polls (docs/clients.md suggests 15).
	PollSeconds int `toml:"poll_seconds,omitempty"`
	// Seconds between full local rescans, the safety net behind the watcher.
	RescanSeconds int `toml:"rescan_seconds,omitempty"`
	// Bandwidth caps in KiB/s; 0 is unlimited.
	UploadKiBps   int64  `toml:"upload_kibps,omitempty"`
	DownloadKiBps int64  `toml:"download_kibps,omitempty"`
	Pairs         []Pair `toml:"pairs"`
}

// LoggedIn reports whether a device token is stored.
func (c Config) LoggedIn() bool { return c.Server != "" && c.Token != "" }

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

func clone(c Config) Config {
	c.Pairs = append([]Pair(nil), c.Pairs...)
	for i := range c.Pairs {
		c.Pairs[i].Ignore = append([]string(nil), c.Pairs[i].Ignore...)
	}
	return c
}

// write replaces the file atomically so a crash never leaves half a config.
func write(path string, cfg Config) error {
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
// unmounted drive must never look like "everything was deleted").
func NewPair(local, remote, direction string, ignore []string) (Pair, error) {
	abs, err := filepath.Abs(local)
	if err != nil {
		return Pair{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Pair{}, fmt.Errorf("local folder: %w", err)
	}
	if !info.IsDir() {
		return Pair{}, fmt.Errorf("%s is not a folder", abs)
	}
	if direction == "" {
		direction = Both
	}
	if direction != Both && direction != Up && direction != Down {
		return Pair{}, fmt.Errorf("direction must be both, up or down")
	}
	return Pair{
		ID:        randomID(),
		Local:     abs,
		Remote:    CleanRemote(remote),
		Direction: direction,
		Ignore:    ignore,
	}, nil
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
