package securitykey

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// Outcome is a finished ceremony: a token, or the challenge id the
// authenticator code goes with (api.Client.SecurityKeyTwoFactor).
type Outcome struct {
	Token              *api.DeviceToken
	TwoFactorChallenge string
}

// ceremonyTimeout bounds the wait for the touch (and the PIN on Windows).
const ceremonyTimeout = 2 * time.Minute

// SignIn runs the whole sign-in against client's server: options, the
// key's assertion for the server's origin, and the swap for a device
// token named deviceName. ErrPINRequired and ErrWrongPIN ask the caller
// for the key's PIN and another try.
func SignIn(ctx context.Context, client *api.Client, platform Platform, username, pin, deviceName string) (*Outcome, error) {
	origin, err := Origin(client.BaseURL)
	if err != nil {
		return nil, err
	}
	options, err := client.SecurityKeyOptions(ctx, username)
	if err != nil {
		if api.HasCode(err, "NO_SECURITY_KEYS") {
			return nil, errors.New("no security keys are registered on this server yet; add one in NeoCloud under Settings › Security first")
		}
		return nil, err
	}
	// The key signs for the address typed here, as a browser would for
	// the address in its bar; the server only accepts its public one.
	if options.Origin != "" && options.Origin != origin {
		return nil, errors.New("this server signs in security keys at " + options.Origin + "; enter that address instead")
	}
	req := Request{
		Origin:           origin,
		RPID:             options.Options.RPID,
		Challenge:        options.Options.Challenge,
		UserVerification: options.Options.UserVerification,
		Timeout:          ceremonyTimeout,
	}
	for _, entry := range options.Options.AllowCredentials {
		id, err := base64.RawURLEncoding.DecodeString(entry.ID)
		if err != nil || len(id) == 0 {
			return nil, errors.New("the server sent an invalid credential id")
		}
		req.AllowCredentials = append(req.AllowCredentials, id)
	}
	assertion, err := platform.Get(ctx, req, pin)
	if err != nil {
		return nil, err
	}
	result, err := client.SecurityKeyVerify(ctx, options.ChallengeID, api.KeyAssertion(*assertion), deviceName, "lite")
	switch {
	case api.HasCode(err, "SECURITY_KEY_UNKNOWN"):
		return nil, ErrNotRegistered
	case api.HasCode(err, "SECURITY_KEY_CHALLENGE_EXPIRED"):
		return nil, errors.New("the sign-in took too long; try again")
	case err != nil:
		return nil, err
	case result.RequiresTwoFactor:
		return &Outcome{TwoFactorChallenge: result.ChallengeID}, nil
	}
	return &Outcome{Token: &result.DeviceToken}, nil
}
