//go:build !linux && !windows

package sendmenu

const supported = false

// Apply does nothing here: macOS has no entry to add without a signed app.
func Apply(bool, string) error { return nil }
