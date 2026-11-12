package securitykey

import (
	"testing"
	"unsafe"
)

// The offsets the C compiler gives webauthn.h's structs on 64-bit Windows
// (amd64 and arm64: 4-byte DWORD/BOOL, 8-byte pointers, natural alignment).
func TestWebAuthnLayout(t *testing.T) {
	var options webauthnGetAssertionOptions
	var assertion webauthnAssertion
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(WEBAUTHN_CLIENT_DATA)", unsafe.Sizeof(webauthnClientData{}), 24},
		{"sizeof(WEBAUTHN_CREDENTIAL)", unsafe.Sizeof(webauthnCredential{}), 24},
		{"sizeof(WEBAUTHN_CREDENTIALS)", unsafe.Sizeof(webauthnCredentials{}), 16},
		{"options.CredentialList", unsafe.Offsetof(options.CredentialList), 8},
		{"options.Extensions", unsafe.Offsetof(options.Extensions), 24},
		{"options.dwAuthenticatorAttachment", unsafe.Offsetof(options.AuthenticatorAttachment), 40},
		{"options.dwUserVerificationRequirement", unsafe.Offsetof(options.UserVerificationRequirement), 44},
		{"options.dwFlags", unsafe.Offsetof(options.Flags), 48},
		{"options.pwszU2fAppId", unsafe.Offsetof(options.U2fAppID), 56},
		{"options.pbU2fAppId", unsafe.Offsetof(options.U2fAppIDUsed), 64},
		{"options.pCancellationId", unsafe.Offsetof(options.CancellationID), 72},
		{"options.pAllowCredentialList", unsafe.Offsetof(options.AllowCredentialList), 80},
		{"sizeof(options v4)", unsafe.Sizeof(options), 88},
		{"assertion.pbAuthenticatorData", unsafe.Offsetof(assertion.AuthenticatorData), 8},
		{"assertion.cbSignature", unsafe.Offsetof(assertion.SignatureLen), 16},
		{"assertion.pbSignature", unsafe.Offsetof(assertion.Signature), 24},
		{"assertion.Credential", unsafe.Offsetof(assertion.Credential), 32},
		{"assertion.cbUserId", unsafe.Offsetof(assertion.UserIDLen), 56},
		{"assertion.pbUserId", unsafe.Offsetof(assertion.UserID), 64},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}
