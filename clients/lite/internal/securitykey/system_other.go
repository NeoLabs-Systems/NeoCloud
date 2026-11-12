//go:build !windows

package securitykey

import (
	"errors"

	"github.com/neolabs/neocloud/clients/lite/internal/fido"
)

// System is this OS's platform: CTAP2 over HID.
func System() (Platform, error) {
	transport, err := fido.System()
	if err != nil {
		return nil, errors.New("security key sign-in is not available on this system; use Continue to approve this computer in the browser")
	}
	return HID(transport), nil
}
