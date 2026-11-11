package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceAuthorization is the answer to POST /api/devices/authorize.
type DeviceAuthorization struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// DeviceToken is the one-time answer to a successful POST /api/devices/token.
type DeviceToken struct {
	Token  string `json:"token"`
	Device Device `json:"device"`
	User   User   `json:"user"`
}

// Device-login outcomes other than success; see docs/clients.md §1.
var (
	ErrAccessDenied = errors.New("the sign-in was denied in the web app")
	ErrExpired      = errors.New("the sign-in code expired; start again")
)

// Authorize starts a device-code login. VerificationURL is always safe to
// open (see verificationLink).
func (c *Client) Authorize(ctx context.Context, name, platform string) (*DeviceAuthorization, error) {
	var out DeviceAuthorization
	body := map[string]string{"name": name, "platform": platform}
	if err := c.doJSON(ctx, http.MethodPost, "/api/devices/authorize", nil, body, &out); err != nil {
		return nil, err
	}
	out.VerificationURL = verificationLink(c.BaseURL, out.VerificationURL, out.UserCode)
	return &out, nil
}

// CleanServer trims a server address the user typed and checks that it is
// an http(s) URL with a host.
func CleanServer(raw string) (string, error) {
	server := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(server)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("the server address must start with https:// (or http://)")
	}
	return server, nil
}

// verificationLink keeps the server's verificationUrl only when it is an
// http(s) URL on the configured server's host. Anything else (another
// host, file:, a UNC path, a custom scheme) would let the server make the
// desktop open an arbitrary URI, so the link is built locally instead:
// <server>/app/device?code=<userCode>.
func verificationLink(server, link, userCode string) string {
	base, baseErr := url.Parse(server)
	parsed, err := url.Parse(link)
	if baseErr == nil && err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.User == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, base.Host) {
		return parsed.String()
	}
	return strings.TrimRight(server, "/") + "/app/device?code=" + url.QueryEscape(userCode)
}

// PollToken asks once whether the user approved. It returns (nil, nil)
// while the approval is still pending.
func (c *Client) PollToken(ctx context.Context, deviceCode string) (*DeviceToken, error) {
	var out DeviceToken
	err := c.doJSON(ctx, http.MethodPost, "/api/devices/token", nil, map[string]string{"deviceCode": deviceCode}, &out)
	switch {
	case err == nil:
		return &out, nil
	case HasCode(err, "AUTHORIZATION_PENDING"):
		return nil, nil
	case HasCode(err, "ACCESS_DENIED"):
		return nil, ErrAccessDenied
	case HasCode(err, "EXPIRED_TOKEN"):
		return nil, ErrExpired
	}
	return nil, err
}

// WaitForToken polls at the server's interval until the user decides, the
// code expires or ctx is cancelled.
func (c *Client) WaitForToken(ctx context.Context, auth *DeviceAuthorization) (*DeviceToken, error) {
	interval := time.Duration(max(auth.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(auth.ExpiresIn, 1)) * time.Second)
	for {
		token, err := c.PollToken(ctx, auth.DeviceCode)
		if err != nil || token != nil {
			return token, err
		}
		if time.Now().After(deadline) {
			return nil, ErrExpired
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Me checks the stored token and returns who it belongs to.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	if err := c.doJSON(ctx, http.MethodGet, "/api/devices/me", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
