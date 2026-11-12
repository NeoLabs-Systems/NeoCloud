//go:build windows

package securitykey

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows (10 1903 and later) lets only administrators open FIDO HID
// devices, so the ceremony goes through the OS WebAuthn API instead. It
// shows the Windows Security dialog, which asks for the PIN itself and
// also offers Windows Hello and phone passkeys. No cgo: the structs are in
// webauthn_h.go.

var (
	webauthnDLL          = windows.NewLazySystemDLL("webauthn.dll")
	procAPIVersion       = webauthnDLL.NewProc("WebAuthNGetApiVersionNumber")
	procGetAssertion     = webauthnDLL.NewProc("WebAuthNAuthenticatorGetAssertion")
	procFreeAssertion    = webauthnDLL.NewProc("WebAuthNFreeAssertion")
	procCancellationID   = webauthnDLL.NewProc("WebAuthNGetCancellationId")
	procCancelOperation  = webauthnDLL.NewProc("WebAuthNCancelCurrentOperation")
	procGetTopWindow     = windows.NewLazySystemDLL("user32.dll").NewProc("GetTopWindow")
	sha256AlgID, _       = windows.UTF16PtrFromString("SHA-256")    // WEBAUTHN_HASH_ALGORITHM_SHA_256
	publicKeyCredType, _ = windows.UTF16PtrFromString("public-key") // WEBAUTHN_CREDENTIAL_TYPE_PUBLIC_KEY
)

// System is this OS's platform: webauthn.dll.
func System() (Platform, error) {
	if procGetAssertion.Find() != nil || procAPIVersion.Find() != nil {
		return nil, errors.New("security key sign-in needs Windows 10 version 1903 or later; use Continue to approve this computer in the browser")
	}
	if version, _, _ := procAPIVersion.Call(); version < webauthnAPIVersion1 {
		return nil, errors.New("this Windows has no usable security key API; use Continue to approve this computer in the browser")
	}
	return windowsPlatform{}, nil
}

type windowsPlatform struct{}

func copyBytes(p *byte, n uint32) []byte {
	if p == nil || n == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(p, n)...)
}

// Get runs WebAuthNAuthenticatorGetAssertion. Windows asks for the PIN
// itself, so pin is unused.
func (windowsPlatform) Get(ctx context.Context, req Request, _ string) (*Assertion, error) {
	data, _, err := req.clientData()
	if err != nil {
		return nil, err
	}
	rpID, err := windows.UTF16PtrFromString(req.RPID)
	if err != nil {
		return nil, err
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	clientData := &webauthnClientData{
		Version:           webauthnClientDataCurrentVersion,
		ClientDataJSONLen: uint32(len(data)),
		ClientDataJSON:    &data[0],
		HashAlgID:         sha256AlgID,
	}
	credentials := make([]webauthnCredential, len(req.AllowCredentials))
	for i, id := range req.AllowCredentials {
		credentials[i] = webauthnCredential{
			Version:        webauthnCredentialCurrentVersion,
			IDLen:          uint32(len(id)),
			ID:             &id[0],
			CredentialType: publicKeyCredType,
		}
	}
	options := &webauthnGetAssertionOptions{
		Version:                     webauthnGetAssertionOptionsVersion4,
		TimeoutMilliseconds:         uint32(timeout / time.Millisecond),
		AuthenticatorAttachment:     webauthnAttachmentAny,
		UserVerificationRequirement: userVerification(req.UserVerification),
	}
	if len(credentials) > 0 {
		options.CredentialList = webauthnCredentials{Count: uint32(len(credentials)), Credentials: &credentials[0]}
	}
	// Closing the app window (the page's request) cancels the dialog, where
	// this webauthn.dll can; its own Cancel button always works.
	cancellation := new(guid)
	if cancellable(cancellation) {
		options.CancellationID = cancellation
		stop := context.AfterFunc(ctx, func() {
			procCancelOperation.Call(uintptr(unsafe.Pointer(cancellation)))
		})
		defer stop()
	}

	// The call blocks on the modal dialog; keep it on one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var out *webauthnAssertion
	hr, _, _ := procGetAssertion.Call(
		dialogOwner(),
		uintptr(unsafe.Pointer(rpID)),
		uintptr(unsafe.Pointer(clientData)),
		uintptr(unsafe.Pointer(options)),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(data)
	runtime.KeepAlive(credentials)
	runtime.KeepAlive(req.AllowCredentials)
	if uint32(hr) != 0 {
		if ctx.Err() != nil {
			return nil, ErrCancelled
		}
		return nil, hresultError(hr)
	}
	if out == nil {
		return nil, errors.New("windows returned no assertion")
	}
	defer procFreeAssertion.Call(uintptr(unsafe.Pointer(out)))
	assertion := &Assertion{
		CredentialID:      copyBytes(out.Credential.ID, out.Credential.IDLen),
		ClientDataJSON:    data,
		AuthenticatorData: copyBytes(out.AuthenticatorData, out.AuthenticatorDataLen),
		Signature:         copyBytes(out.Signature, out.SignatureLen),
		UserHandle:        copyBytes(out.UserID, out.UserIDLen),
	}
	if len(assertion.CredentialID) == 0 && len(req.AllowCredentials) == 1 {
		assertion.CredentialID = req.AllowCredentials[0]
	}
	return assertion, nil
}

// cancellable fills id with a fresh cancellation id (API version 1 has the
// two calls, but a LazyProc panics on a missing export).
func cancellable(id *guid) bool {
	if procCancellationID.Find() != nil || procCancelOperation.Find() != nil {
		return false
	}
	hr, _, _ := procCancellationID.Call(uintptr(unsafe.Pointer(id)))
	return uint32(hr) == 0
}

// dialogOwner is the window the Windows Security dialog opens over: the
// app window the user just clicked in, else the top window, as libfido2
// does (webauthn.dll wants a window handle).
func dialogOwner() uintptr {
	if hwnd := windows.GetForegroundWindow(); hwnd != 0 {
		return uintptr(hwnd)
	}
	hwnd, _, _ := procGetTopWindow.Call(0)
	return hwnd
}

func userVerification(value string) uint32 {
	switch value {
	case "required":
		return webauthnUserVerificationRequired
	case "discouraged":
		return webauthnUserVerificationDiscouraged
	}
	return webauthnUserVerificationPreferred
}

func hresultError(hr uintptr) error {
	switch uint32(hr) {
	case uint32(hresultUserCancelled), uint32(hresultCancelled):
		return ErrCancelled
	case uint32(hresultNotFound):
		return ErrNotRegistered
	case uint32(hresultDeviceNotFound):
		return ErrNoKey
	case uint32(hresultTimeout):
		return ErrTimeout
	}
	return fmt.Errorf("the security key sign-in failed (Windows error 0x%08x)", uint32(hr))
}
