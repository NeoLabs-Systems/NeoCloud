//go:build !linux && !(darwin && cgo)

package fido

import (
	"errors"
	"runtime"
)

// System reports that this OS has no transport. On Windows (10 1903 and
// later) only administrators may open FIDO HID devices directly, and
// webauthn.dll always shows its own dialog and asks for a touch, so a
// silent unlock on plug-in is not possible there.
func System() (Transport, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("not available on Windows: only administrators may talk to security keys directly, and the Windows security key API cannot unlock without a dialog and a touch. NeoCloud Lite keeps the normal sign-in")
	}
	return nil, errors.New("not available on this system; NeoCloud Lite keeps the normal sign-in")
}
