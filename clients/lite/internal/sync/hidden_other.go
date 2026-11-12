//go:build !windows

package sync

// hasHiddenAttr: outside Windows, hidden means a leading dot, which the
// Matcher handles on both sides.
func hasHiddenAttr(string) bool { return false }
