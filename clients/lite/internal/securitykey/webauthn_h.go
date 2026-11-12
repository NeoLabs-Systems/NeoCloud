package securitykey

// webauthn.h (github.com/microsoft/webauthn) as Go structs for the
// Windows platform (system_windows.go). Kept free of build tags so the
// layout test runs on every OS: Go lays these out as the C compiler does
// on 64-bit Windows (4-byte DWORD/BOOL, 8-byte pointers, natural
// alignment), and amd64 and arm64 agree.

// guid is a Windows GUID (16 bytes, 4-byte aligned).
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// webauthn.h constants.
const (
	webauthnAPIVersion1                         = 1          // WEBAUTHN_API_VERSION_1, Windows 10 1903
	webauthnClientDataCurrentVersion            = 1          // WEBAUTHN_CLIENT_DATA_CURRENT_VERSION
	webauthnCredentialCurrentVersion            = 1          // WEBAUTHN_CREDENTIAL_CURRENT_VERSION
	webauthnGetAssertionOptionsVersion4         = 4          // WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS_VERSION_4, the API_VERSION_1 baseline
	webauthnAttachmentAny                       = 0          // WEBAUTHN_AUTHENTICATOR_ATTACHMENT_ANY
	webauthnUserVerificationRequired            = 1          // WEBAUTHN_USER_VERIFICATION_REQUIREMENT_REQUIRED
	webauthnUserVerificationPreferred           = 2          // WEBAUTHN_USER_VERIFICATION_REQUIREMENT_PREFERRED
	webauthnUserVerificationDiscouraged         = 3          // WEBAUTHN_USER_VERIFICATION_REQUIREMENT_DISCOURAGED
	hresultUserCancelled                uintptr = 0x80090036 // NTE_USER_CANCELLED
	hresultNotFound                     uintptr = 0x80090011 // NTE_NOT_FOUND
	hresultDeviceNotFound               uintptr = 0x80090035 // NTE_DEVICE_NOT_FOUND
	hresultTimeout                      uintptr = 0x800705b4 // HRESULT_FROM_WIN32(ERROR_TIMEOUT)
	hresultCancelled                    uintptr = 0x800704c7 // HRESULT_FROM_WIN32(ERROR_CANCELLED), WebAuthNCancelCurrentOperation
)

// WEBAUTHN_CLIENT_DATA:
//
//	DWORD dwVersion; DWORD cbClientDataJSON; PBYTE pbClientDataJSON; LPCWSTR pwszHashAlgId;
type webauthnClientData struct {
	Version           uint32
	ClientDataJSONLen uint32
	ClientDataJSON    *byte
	HashAlgID         *uint16
}

// WEBAUTHN_CREDENTIAL:
//
//	DWORD dwVersion; DWORD cbId; PBYTE pbId; LPCWSTR pwszCredentialType;
type webauthnCredential struct {
	Version        uint32
	IDLen          uint32
	ID             *byte
	CredentialType *uint16
}

// WEBAUTHN_CREDENTIALS: DWORD cCredentials; PWEBAUTHN_CREDENTIAL pCredentials;
type webauthnCredentials struct {
	Count       uint32
	Credentials *webauthnCredential
}

// WEBAUTHN_EXTENSIONS: DWORD cExtensions; PWEBAUTHN_EXTENSION pExtensions;
type webauthnExtensions struct {
	Count      uint32
	Extensions uintptr
}

// WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS up to version 4. Later
// versions only append fields, and dwVersion tells the DLL where this
// struct ends.
type webauthnGetAssertionOptions struct {
	// Version 1.
	Version                     uint32              // DWORD dwVersion
	TimeoutMilliseconds         uint32              // DWORD dwTimeoutMilliseconds
	CredentialList              webauthnCredentials // WEBAUTHN_CREDENTIALS CredentialList
	Extensions                  webauthnExtensions  // WEBAUTHN_EXTENSIONS Extensions
	AuthenticatorAttachment     uint32              // DWORD dwAuthenticatorAttachment
	UserVerificationRequirement uint32              // DWORD dwUserVerificationRequirement
	Flags                       uint32              // DWORD dwFlags
	// Version 2.
	U2fAppID     *uint16 // PCWSTR pwszU2fAppId
	U2fAppIDUsed *int32  // BOOL *pbU2fAppId
	// Version 3.
	CancellationID *guid // GUID *pCancellationId
	// Version 4: when set, replaces CredentialList.
	AllowCredentialList uintptr // PWEBAUTHN_CREDENTIAL_LIST pAllowCredentialList
}

// WEBAUTHN_ASSERTION, the version 1 fields (the only ones read; later
// versions append after pbUserId).
type webauthnAssertion struct {
	Version              uint32             // DWORD dwVersion
	AuthenticatorDataLen uint32             // DWORD cbAuthenticatorData
	AuthenticatorData    *byte              // PBYTE pbAuthenticatorData
	SignatureLen         uint32             // DWORD cbSignature
	Signature            *byte              // PBYTE pbSignature
	Credential           webauthnCredential // WEBAUTHN_CREDENTIAL Credential
	UserIDLen            uint32             // DWORD cbUserId
	UserID               *byte              // PBYTE pbUserId
}
