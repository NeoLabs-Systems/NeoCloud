// Package fido talks CTAP2 to FIDO2 security keys over USB HID: just
// enough of the protocol for the lite client's security-key lock
// (getInfo, clientPIN getPinToken, makeCredential and getAssertion with
// the hmac-secret extension, PIN/UV auth protocol 1).
//
// The HID transport is per OS: hidraw on Linux (pure Go), IOKit on macOS
// (cgo, which the macOS tray already needs). Windows has none, see
// System.
package fido

import (
	"errors"
	"time"
)

// reportSize is the size of every CTAPHID packet.
const reportSize = 64

// ErrTimeout is what Device.Read returns when no report arrived in time.
var ErrTimeout = errors.New("fido: read timed out")

// Device is one opened FIDO HID interface. It moves raw 64-byte reports
// (without a report ID). A Device is used by one goroutine from Open to
// Close: on macOS it is bound to that goroutine's OS thread.
type Device interface {
	Write(report []byte) error
	// Read waits up to timeout for the next input report.
	Read(timeout time.Duration) ([]byte, error)
	Close() error
}

// DeviceInfo identifies a plugged-in key.
type DeviceInfo struct {
	// ID is unique per attachment: the same key unplugged and plugged
	// in again gets a new ID, so a removal is never missed.
	ID      string
	Path    string
	Product string
}

// Transport lists and opens FIDO HID devices.
type Transport interface {
	Devices() ([]DeviceInfo, error)
	Open(DeviceInfo) (Device, error)
}

// isFIDODescriptor reports whether a HID report descriptor declares the
// FIDO usage page 0xF1D0 with usage 0x01 (CTAPHID). It walks the short
// items (HID 1.11 §6.2.2.2) and skips long ones.
func isFIDODescriptor(desc []byte) bool {
	var usagePage uint32
	for i := 0; i < len(desc); {
		prefix := desc[i]
		if prefix == 0xfe { // long item: 0xfe, size, tag, data
			if i+1 >= len(desc) {
				return false
			}
			i += 3 + int(desc[i+1])
			continue
		}
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			return false
		}
		var value uint32
		for j := 0; j < size; j++ {
			value |= uint32(desc[i+1+j]) << (8 * j)
		}
		switch prefix & 0xfc {
		case 0x04: // global: usage page
			usagePage = value
		case 0x08: // local: usage
			if usagePage == 0xf1d0 && value == 0x01 {
				return true
			}
		}
		i += 1 + size
	}
	return false
}
