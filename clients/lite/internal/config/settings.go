package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Values of the per-pair and global settings (docs/clients.md "Folder
// settings"). Every field's zero value means the default, so a config
// written before a setting existed keeps its behaviour.
const (
	ModeMirror = "mirror"

	NewFoldersSync = "sync"
	NewFoldersAsk  = "ask"
	NewFoldersSkip = "skip"

	SymlinksSkip = "skip"

	ConflictKeepBoth     = "keep_both"
	ConflictPreferLocal  = "prefer_local"
	ConflictPreferRemote = "prefer_remote"
	ConflictNewer        = "newer"

	NotifyAll    = "all"
	NotifyErrors = "errors"
	NotifyOff    = "off"

	ProxySystem = "system"
	ProxyNone   = "none"
	ProxyManual = "manual"
)

// Defaults of the settings below.
const (
	DefaultBigFolderMB   = 500
	DefaultTrashDays     = 30
	DefaultGuardFiles    = 500
	DefaultGuardPercent  = 50
	DefaultNotifications = NotifyErrors
)

// PairSettings are a pair's options beyond where and which way. Pointers
// are "unset": the default, or the global value where there is one.
type PairSettings struct {
	// Mode is how files land locally; only "mirror" (full copies) for now.
	Mode string `toml:"mode,omitempty" json:"mode"`
	// ExcludePaths are folders (relative to the pair's remote folder) that
	// are not synced: selective sync. Their local copy goes to the local
	// trash; the cloud copy is never touched.
	ExcludePaths []string `toml:"exclude_paths,omitempty" json:"excludePaths"`
	// NewFolders says what happens to a new folder at the top of the
	// remote folder: sync it, ask first, or skip it (exclude it).
	NewFolders string `toml:"new_folders,omitempty" json:"newFolders"`
	// BigFolderPromptMB makes a new top-level folder bigger than this ask
	// first even with NewFolders "sync"; 0 turns it off.
	BigFolderPromptMB *int `toml:"big_folder_prompt_mb,omitempty" json:"bigFolderPromptMB"`
	// PropagateDeletes: false never deletes on the receiving side (a
	// backup with direction "up", an archive with "down"; with "both" a
	// deleted file comes back from the other side).
	PropagateDeletes  *bool  `toml:"propagate_deletes,omitempty" json:"propagateDeletes"`
	UseDefaultIgnores *bool  `toml:"use_default_ignores,omitempty" json:"useDefaultIgnores"`
	IncludeHidden     *bool  `toml:"include_hidden,omitempty" json:"includeHidden"`
	MaxFileSizeMB     int    `toml:"max_file_size_mb,omitempty" json:"maxFileSizeMB"`
	Symlinks          string `toml:"symlinks,omitempty" json:"symlinks"`
	// Conflict is what happens when both sides changed a file; the
	// version that loses goes to the local trash.
	Conflict string `toml:"conflict,omitempty" json:"conflict"`
	// TrashRetentionDays: 0 keeps the local trash forever.
	TrashRetentionDays *int `toml:"trash_retention_days,omitempty" json:"trashRetentionDays"`
	DeleteGuardFiles   *int `toml:"delete_guard_files,omitempty" json:"deleteGuardFiles"`
	DeleteGuardPercent *int `toml:"delete_guard_percent,omitempty" json:"deleteGuardPercent"`
	// RespectNetworkRules: false keeps the pair syncing on a metered
	// network or in battery saver.
	RespectNetworkRules *bool `toml:"respect_network_rules,omitempty" json:"respectNetworkRules"`
}

func orTrue(value *bool) bool { return value == nil || *value }

func orInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func orString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// MirrorDeletes reports whether deletions reach the other side.
func (s PairSettings) MirrorDeletes() bool { return orTrue(s.PropagateDeletes) }

// DefaultIgnores reports whether the built-in ignore list applies.
func (s PairSettings) DefaultIgnores() bool { return orTrue(s.UseDefaultIgnores) }

// Hidden reports whether dot files (and, on Windows, hidden files) sync.
func (s PairSettings) Hidden() bool { return orTrue(s.IncludeHidden) }

// FollowsNetworkRules reports whether metered and battery pausing apply.
func (s PairSettings) FollowsNetworkRules() bool { return orTrue(s.RespectNetworkRules) }

// ConflictPolicy is Conflict with its default.
func (s PairSettings) ConflictPolicy() string { return orString(s.Conflict, ConflictKeepBoth) }

// NewFolderPolicy is NewFolders with its default.
func (s PairSettings) NewFolderPolicy() string { return orString(s.NewFolders, NewFoldersSync) }

// TrashDays is TrashRetentionDays with its default; 0 is forever.
func (s PairSettings) TrashDays() int { return orInt(s.TrashRetentionDays, DefaultTrashDays) }

// MaxFileSize is the size limit in bytes; 0 is none.
func (s PairSettings) MaxFileSize() int64 { return int64(s.MaxFileSizeMB) << 20 }

// Excluded reports whether rel (relative to the pair's remote folder) is
// an excluded folder or inside one.
func (s PairSettings) Excluded(rel string) bool {
	for _, excluded := range s.ExcludePaths {
		if rel == excluded || strings.HasPrefix(rel, excluded+"/") {
			return true
		}
	}
	return false
}

// Validate checks the values and cleans ExcludePaths ("/a/b/" → "a/b",
// no duplicates, nothing inside another entry).
func (s *PairSettings) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	check(s.Mode == "" || s.Mode == ModeMirror, "mode must be mirror (on-demand files are not available yet)")
	check(oneOf(s.NewFolders, NewFoldersSync, NewFoldersAsk, NewFoldersSkip), "new_folders must be sync, ask or skip")
	check(oneOf(s.Symlinks, SymlinksSkip), "symlinks must be skip")
	check(oneOf(s.Conflict, ConflictKeepBoth, ConflictPreferLocal, ConflictPreferRemote, ConflictNewer),
		"conflict must be keep_both, prefer_local, prefer_remote or newer")
	check(s.MaxFileSizeMB >= 0, "max_file_size_mb must not be negative")
	check(orInt(s.BigFolderPromptMB, 0) >= 0, "big_folder_prompt_mb must not be negative")
	check(orInt(s.TrashRetentionDays, 0) >= 0, "trash_retention_days must not be negative")
	check(orInt(s.DeleteGuardFiles, 1) >= 1, "delete_guard_files must be at least 1")
	percent := orInt(s.DeleteGuardPercent, 50)
	check(percent >= 1 && percent <= 100, "delete_guard_percent must be between 1 and 100")
	var cleaned []string
	for _, raw := range s.ExcludePaths {
		rel := CleanRemote(raw)
		if rel == "" || strings.Contains(rel, "\x00") || hasDotSegment(rel) {
			errs = append(errs, fmt.Errorf("exclude path %q is not a folder path", raw))
			continue
		}
		cleaned = append(cleaned, rel)
	}
	s.ExcludePaths = minimalPaths(cleaned)
	return errors.Join(errs...)
}

// hasDotSegment reports whether rel has a "." or ".." segment.
func hasDotSegment(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}

// minimalPaths drops duplicates and paths inside another one.
func minimalPaths(paths []string) []string {
	var out []string
	for _, candidate := range paths {
		covered := false
		for _, other := range paths {
			if other != candidate && strings.HasPrefix(candidate, other+"/") {
				covered = true
				break
			}
		}
		if !covered && !contains(out, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	return value == "" || contains(allowed, value)
}

// normalize repairs a hand-edited pair on load instead of refusing the
// whole file: on-demand mode is not available, so it falls back to mirror.
func (s *PairSettings) normalize() {
	if s.Mode != "" && s.Mode != ModeMirror {
		s.Mode = ModeMirror
	}
}

// GlobalSettings are the app-wide options at the top of config.toml.
type GlobalSettings struct {
	BigFolderPromptMB  *int `toml:"big_folder_prompt_mb,omitempty" json:"bigFolderPromptMB"`
	DeleteGuardFiles   *int `toml:"delete_guard_files,omitempty" json:"deleteGuardFiles"`
	DeleteGuardPercent *int `toml:"delete_guard_percent,omitempty" json:"deleteGuardPercent"`
	// PauseOnMetered stops pairs on a metered connection (Windows, Linux).
	PauseOnMetered *bool `toml:"pause_on_metered,omitempty" json:"pauseOnMetered"`
	// PauseOnBatterySaver stops pairs while the battery saver is on.
	PauseOnBatterySaver *bool  `toml:"pause_on_battery_saver,omitempty" json:"pauseOnBatterySaver"`
	Notifications       string `toml:"notifications,omitempty" json:"notifications"`
	// FileManagerSidebar shows the NeoCloud folder the app created in the
	// Finder sidebar, the Explorer navigation pane or the GTK bookmarks.
	FileManagerSidebar *bool  `toml:"file_manager_sidebar,omitempty" json:"fileManagerSidebar"`
	Proxy              string `toml:"proxy,omitempty" json:"proxy"`
	ProxyURL           string `toml:"proxy_url,omitempty" json:"proxyUrl"`
}

// Validate checks the global values.
func (g GlobalSettings) Validate() error {
	var errs []error
	if orInt(g.BigFolderPromptMB, 0) < 0 {
		errs = append(errs, errors.New("big_folder_prompt_mb must not be negative"))
	}
	if orInt(g.DeleteGuardFiles, 1) < 1 {
		errs = append(errs, errors.New("delete_guard_files must be at least 1"))
	}
	if percent := orInt(g.DeleteGuardPercent, 50); percent < 1 || percent > 100 {
		errs = append(errs, errors.New("delete_guard_percent must be between 1 and 100"))
	}
	if !oneOf(g.Notifications, NotifyAll, NotifyErrors, NotifyOff) {
		errs = append(errs, errors.New("notifications must be all, errors or off"))
	}
	if !oneOf(g.Proxy, ProxySystem, ProxyNone, ProxyManual) {
		errs = append(errs, errors.New("proxy must be system, none or manual"))
	}
	if g.Proxy == ProxyManual {
		if _, err := ParseProxyURL(g.ProxyURL); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ParseProxyURL accepts http://, https:// and socks5:// proxies.
func ParseProxyURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || !oneOf(parsed.Scheme, "http", "https", "socks5") || parsed.Scheme == "" {
		return nil, errors.New("proxy_url must be an http://, https:// or socks5:// address")
	}
	return parsed, nil
}

// MeteredPause is PauseOnMetered with its default (on).
func (g GlobalSettings) MeteredPause() bool { return orTrue(g.PauseOnMetered) }

// BatteryPause is PauseOnBatterySaver with its default (on).
func (g GlobalSettings) BatteryPause() bool { return orTrue(g.PauseOnBatterySaver) }

// NotifyLevel is Notifications with its default.
func (g GlobalSettings) NotifyLevel() string { return orString(g.Notifications, DefaultNotifications) }

// Sidebar is FileManagerSidebar with its default (on).
func (g GlobalSettings) Sidebar() bool { return orTrue(g.FileManagerSidebar) }

// ProxyMode is Proxy with its default.
func (g GlobalSettings) ProxyMode() string { return orString(g.Proxy, ProxySystem) }

// BigFolderMB is the big-folder threshold of pair: its own, else the
// global one; 0 is off.
func (c Config) BigFolderMB(pair Pair) int {
	return orInt(pair.BigFolderPromptMB, orInt(c.BigFolderPromptMB, DefaultBigFolderMB))
}

// Guard is the mass-delete guard of pair: a pass that would delete more
// than files files, or more than percent % of the tracked ones, asks first.
func (c Config) Guard(pair Pair) (files, percent int) {
	files = orInt(pair.DeleteGuardFiles, orInt(c.DeleteGuardFiles, DefaultGuardFiles))
	percent = orInt(pair.DeleteGuardPercent, orInt(c.DeleteGuardPercent, DefaultGuardPercent))
	return files, percent
}
