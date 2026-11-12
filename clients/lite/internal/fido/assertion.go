package fido

import (
	"context"
	"errors"
)

// authenticatorGetNextAssertion (CTAP 2.1 §6.3).
const ctapGetNextAssertion = 0x08

// AssertionRequest is a WebAuthn getAssertion for a relying party.
type AssertionRequest struct {
	RPID string
	// ClientDataHash is SHA-256 of the clientDataJSON the platform built.
	ClientDataHash []byte
	// AllowList holds credential ids; empty asks for the key's
	// discoverable credentials of RPID.
	AllowList [][]byte
	// PINToken, from PINToken, verifies the user with the key's PIN.
	PINToken []byte
	// UV asks a key with built-in user verification (a fingerprint) to
	// use it. Ignored when PINToken is set.
	UV bool
}

// Assertion is one signed answer of the key (WebAuthn §5.2.2).
type Assertion struct {
	CredentialID      []byte
	AuthenticatorData []byte
	Signature         []byte
	// UserHandle is set for discoverable credentials.
	UserHandle []byte
}

// signInAssertionRequest is authenticatorGetAssertion's parameter map
// (CTAP 2.1 §6.2.2) without extensions.
type signInAssertionRequest struct {
	RPID              string                 `cbor:"1,keyasint"`
	ClientDataHash    []byte                 `cbor:"2,keyasint"`
	AllowList         []credentialDescriptor `cbor:"3,keyasint,omitempty"`
	Options           map[string]bool        `cbor:"5,keyasint,omitempty"`
	PinUvAuthParam    []byte                 `cbor:"6,keyasint,omitempty"`
	PinUvAuthProtocol uint                   `cbor:"7,keyasint,omitempty"`
}

type publicKeyUser struct {
	ID []byte `cbor:"id"`
}

// signInAssertionResponse is the response map (CTAP 2.1 §6.2.2).
type signInAssertionResponse struct {
	Credential          *credentialDescriptor `cbor:"1,keyasint"`
	AuthData            []byte                `cbor:"2,keyasint"`
	Signature           []byte                `cbor:"3,keyasint"`
	User                *publicKeyUser        `cbor:"4,keyasint"`
	NumberOfCredentials int                   `cbor:"5,keyasint"`
}

// HasPIN reports whether the key has a PIN set.
func (i Info) HasPIN() bool { return i.Options["clientPin"] }

// HasBuiltInUV reports whether the key verifies users itself (biometrics)
// and has that set up.
func (i Info) HasBuiltInUV() bool { return i.Options["uv"] }

func (r AssertionRequest) encode() signInAssertionRequest {
	req := signInAssertionRequest{
		RPID:           r.RPID,
		ClientDataHash: r.ClientDataHash,
		Options:        map[string]bool{"up": true},
	}
	for _, id := range r.AllowList {
		req.AllowList = append(req.AllowList, credentialDescriptor{ID: id, Type: "public-key"})
	}
	switch {
	case r.PINToken != nil:
		req.PinUvAuthParam = authenticate(r.PINToken, r.ClientDataHash)
		req.PinUvAuthProtocol = pinProtocolOne
	case r.UV:
		req.Options["uv"] = true
	}
	return req
}

// toAssertion checks one response; single names the credential when the
// allow list had exactly one entry, which keys may leave out (§6.2.2).
func (resp signInAssertionResponse) toAssertion(single []byte) (Assertion, error) {
	out := Assertion{AuthenticatorData: resp.AuthData, Signature: resp.Signature}
	switch {
	case resp.Credential != nil && len(resp.Credential.ID) > 0:
		out.CredentialID = resp.Credential.ID
	case single != nil:
		out.CredentialID = single
	default:
		return out, errors.New("fido: the key named no credential")
	}
	if len(out.AuthenticatorData) < 37 || len(out.Signature) == 0 {
		return out, errors.New("fido: incomplete assertion")
	}
	if resp.User != nil {
		out.UserHandle = resp.User.ID
	}
	return out, nil
}

// GetAssertion signs req.ClientDataHash with a credential of req.RPID,
// waiting for a touch. With an empty allow list a key holding several
// discoverable credentials for the relying party returns all of them
// (getNextAssertion), most recently created first.
func (a *Authenticator) GetAssertion(ctx context.Context, req AssertionRequest) ([]Assertion, error) {
	if len(req.ClientDataHash) != 32 {
		return nil, errors.New("fido: the client data hash must be 32 bytes")
	}
	var single []byte
	if len(req.AllowList) == 1 {
		single = req.AllowList[0]
	}
	var resp signInAssertionResponse
	if err := a.call(ctx, ctapGetAssertion, req.encode(), &resp); err != nil {
		return nil, err
	}
	first, err := resp.toAssertion(single)
	if err != nil {
		return nil, err
	}
	out := []Assertion{first}
	for i := 1; i < resp.NumberOfCredentials; i++ {
		var next signInAssertionResponse
		if err := a.call(ctx, ctapGetNextAssertion, nil, &next); err != nil {
			return nil, err
		}
		assertion, err := next.toAssertion(single)
		if err != nil {
			return nil, err
		}
		out = append(out, assertion)
	}
	return out, nil
}
