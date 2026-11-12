package api

import (
	"context"
	"encoding/base64"
	"net/http"
)

// SecurityKeyOptions is the answer to POST /api/devices/webauthn/options:
// the server's PublicKeyCredentialRequestOptions, the id its challenge is
// kept under and the origin the key's clientDataJSON must name.
type SecurityKeyOptions struct {
	ChallengeID string `json:"challengeId"`
	Origin      string `json:"origin"`
	Options     struct {
		Challenge        string `json:"challenge"`
		RPID             string `json:"rpId"`
		Timeout          int    `json:"timeout"`
		UserVerification string `json:"userVerification"`
		AllowCredentials []struct {
			ID string `json:"id"`
		} `json:"allowCredentials"`
	} `json:"options"`
}

// KeyAssertion is a signed answer of a security key, in raw bytes.
type KeyAssertion struct {
	CredentialID      []byte
	ClientDataJSON    []byte
	AuthenticatorData []byte
	Signature         []byte
	UserHandle        []byte
}

// SecurityKeyResult is the answer to POST /api/devices/webauthn/verify:
// a token, or RequiresTwoFactor when the key did not verify the user and
// the account has two-factor sign-in on.
type SecurityKeyResult struct {
	DeviceToken
	RequiresTwoFactor bool   `json:"requiresTwoFactor"`
	ChallengeID       string `json:"challengeId"`
}

// SecurityKeyOptions starts a security key sign-in. username may be empty.
func (c *Client) SecurityKeyOptions(ctx context.Context, username string) (*SecurityKeyOptions, error) {
	var out SecurityKeyOptions
	body := map[string]string{}
	if username != "" {
		body["username"] = username
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/devices/webauthn/options", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// SecurityKeyVerify sends the assertion as a browser's
// PublicKeyCredential JSON and asks for a device token.
func (c *Client) SecurityKeyVerify(ctx context.Context, challengeID string, assertion KeyAssertion, name, platform string) (*SecurityKeyResult, error) {
	response := map[string]any{
		"clientDataJSON":    b64(assertion.ClientDataJSON),
		"authenticatorData": b64(assertion.AuthenticatorData),
		"signature":         b64(assertion.Signature),
	}
	if len(assertion.UserHandle) > 0 {
		response["userHandle"] = b64(assertion.UserHandle)
	}
	body := map[string]any{
		"challengeId": challengeID,
		"name":        name,
		"platform":    platform,
		"response": map[string]any{
			"id":                     b64(assertion.CredentialID),
			"rawId":                  b64(assertion.CredentialID),
			"type":                   "public-key",
			"clientExtensionResults": map[string]any{},
			"response":               response,
		},
	}
	var out SecurityKeyResult
	if err := c.doJSON(ctx, http.MethodPost, "/api/devices/webauthn/verify", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecurityKeyTwoFactor finishes a sign-in that needs the authenticator code.
func (c *Client) SecurityKeyTwoFactor(ctx context.Context, challengeID, code string) (*DeviceToken, error) {
	var out DeviceToken
	body := map[string]string{"challengeId": challengeID, "code": code}
	if err := c.doJSON(ctx, http.MethodPost, "/api/devices/webauthn/2fa", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
