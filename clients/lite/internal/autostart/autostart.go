// Package autostart starts the client at login: a LaunchAgent on macOS,
// an XDG autostart entry on Linux and the HKCU Run key on Windows. All
// three are per-user, so no admin rights are needed.
package autostart

import "os"

// Label identifies the entry on every platform.
const Label = "com.neolabs.neocloud-lite"

// Enable registers the running executable to start with `run` at login.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return enable(exe)
}

// Disable removes the entry; a missing entry is not an error.
func Disable() error { return disable() }

// Enabled reports whether the entry exists.
func Enabled() bool { return enabled() }
