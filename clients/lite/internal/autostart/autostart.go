// Package autostart starts the client at login: a LaunchAgent on macOS,
// an XDG autostart entry on Linux and the HKCU Run key on Windows. All
// three are per-user, so no admin rights are needed.
package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// Label identifies the entry on every platform.
const Label = "com.neolabs.neocloud-lite"

// Enable registers the running executable to start with `run` at login.
func Enable() error {
	exe, err := Executable()
	if err != nil {
		return err
	}
	return enable(exe)
}

// Disable removes the entry; a missing entry is not an error.
func Disable() error { return disable() }

// Enabled reports whether the entry exists.
func Enabled() bool { return enabled() }

// Executable is the path a login entry should start: the AppImage itself
// rather than its temporary mount, otherwise the running executable.
func Executable() (string, error) {
	if image := os.Getenv("APPIMAGE"); image != "" {
		return image, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// Installed reports whether exe sits where it will still be at the next
// login: not a `go run` build in the temp folder, and on macOS not an app
// run from its disk image or from a quarantine copy (App Translocation).
func Installed(exe string) bool {
	if within(exe, os.TempDir()) {
		return false
	}
	return !strings.Contains(exe, "/AppTranslocation/") && !strings.HasPrefix(exe, "/Volumes/")
}

// within reports whether path is inside dir, as given or with its
// symlinks resolved (macOS: /var is /private/var).
func within(path, dir string) bool {
	dirs := []string{dir}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dirs = append(dirs, resolved)
	}
	for _, d := range dirs {
		rel, err := filepath.Rel(d, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
