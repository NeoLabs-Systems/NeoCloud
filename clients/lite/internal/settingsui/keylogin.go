package settingsui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/securitykey"
)

// keyCodeState is a security key sign-in whose account still wants the
// authenticator code (the key confirmed presence but not the user).
type keyCodeState struct {
	server      string
	challengeID string
}

// keyStep tells the page what the sign-in needs next: "pin" (the key's
// PIN), "code" (the authenticator code) or "" when signed in. Error is
// shown with the field, for a retry.
type keyStep struct {
	Success bool   `json:"success"`
	Next    string `json:"next,omitempty"`
	Error   string `json:"error,omitempty"`
}

// startKeyLogin is "Sign in with security key": { server, username, pin }.
// It answers once the key was touched, or with the next step. The PIN is
// used for this request only.
func (s *Server) startKeyLogin(r *http.Request) (any, error) {
	var body struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		PIN      string `json:"pin"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	server, err := api.CleanServer(body.Server)
	if err != nil {
		return nil, err
	}
	if !s.keying.TryLock() {
		return nil, errors.New("a security key sign-in is already running")
	}
	defer s.keying.Unlock()
	platform, err := s.keys()
	if err != nil {
		return nil, err
	}
	outcome, err := securitykey.SignIn(r.Context(), api.New(server, ""), platform,
		strings.TrimSpace(body.Username), body.PIN, config.DefaultDeviceName())
	switch {
	case errors.Is(err, securitykey.ErrPINRequired):
		return keyStep{Next: "pin"}, nil
	case errors.Is(err, securitykey.ErrWrongPIN):
		return keyStep{Next: "pin", Error: err.Error()}, nil
	case err != nil:
		return nil, reachError(server, err)
	case outcome.Token == nil:
		s.mu.Lock()
		s.keyCode = keyCodeState{server: server, challengeID: outcome.TwoFactorChallenge}
		s.mu.Unlock()
		return keyStep{Next: "code"}, nil
	}
	return s.finishKeyLogin(server, outcome.Token)
}

// keyLoginCode is the authenticator code after a presence-only key.
func (s *Server) keyLoginCode(r *http.Request) (any, error) {
	var body struct {
		Code string `json:"code"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	pending := s.keyCode
	s.mu.Unlock()
	if pending.challengeID == "" {
		return nil, errors.New("start the sign-in with your security key again")
	}
	token, err := api.New(pending.server, "").SecurityKeyTwoFactor(r.Context(), pending.challengeID, strings.TrimSpace(body.Code))
	switch {
	case api.HasCode(err, "INVALID_CODE"):
		return keyStep{Next: "code", Error: "wrong code; try again"}, nil
	case err != nil:
		s.mu.Lock()
		s.keyCode = keyCodeState{}
		s.mu.Unlock()
		if api.HasCode(err, "TWO_FACTOR_EXPIRED") || api.HasCode(err, "TWO_FACTOR_ATTEMPTS_EXCEEDED") {
			return nil, errors.New("the code step expired; sign in with your security key again")
		}
		return nil, err
	}
	return s.finishKeyLogin(pending.server, token)
}

// finishKeyLogin stores the token exactly as the device-code flow does and
// drops any sign-in still pending.
func (s *Server) finishKeyLogin(server string, token *api.DeviceToken) (any, error) {
	if err := s.engine.SignIn(server, token); err != nil {
		return nil, err
	}
	s.forgetQuota()
	s.mu.Lock()
	if s.login.cancel != nil {
		s.login.cancel()
	}
	s.login = loginState{}
	s.keyCode = keyCodeState{}
	s.mu.Unlock()
	return keyStep{Success: true}, nil
}
