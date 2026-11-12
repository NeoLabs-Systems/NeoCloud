//go:build !darwin

package tray

// onReopen is macOS only; elsewhere a second launch is a new process,
// which internal/instance hands over to the running one.
func onReopen(func()) {}
