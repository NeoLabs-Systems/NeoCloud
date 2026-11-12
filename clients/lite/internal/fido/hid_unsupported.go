//go:build !linux && !(darwin && cgo)

package fido

import "errors"

// System reports that this OS has no transport. On Windows (10 1903 and
// later) only administrators may open FIDO HID devices, and webauthn.dll
// always shows its dialog and asks for a touch (it has no up=false), so a
// silent unlock while the key is plugged in is not possible there; the
// app leaves the lock out on such systems (keylock.Supported).
func System() (Transport, error) {
	return nil, errors.New("the security key lock is not available on this platform")
}
