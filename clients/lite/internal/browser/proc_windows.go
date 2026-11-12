package browser

import "os/exec"

// detach is a no-op: a GUI process started from the tray has no console
// whose Ctrl-C it could share.
func detach(*exec.Cmd) {}

// CloseApp is a no-op: on Windows Chromium quits with its last window.
func CloseApp(string) error { return nil }
