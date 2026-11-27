package neorouter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// "Connect NeoRouter" is NeoRouter's OAuth 2.0 device flow (RFC 8628): the
// person approves NeoCloud in NeoRouter while this server polls for the key.
// No callback reaches NeoCloud, so a NeoRouter on the home network works the
// same as a public one.

const (
	clientID       = "neocloud"
	deviceScopes   = "chat embedding transcription image"
	flowLifetime   = 10 * time.Minute
	maxReplyBytes  = 64 << 10
	requestTimeout = 15 * time.Second
	flowExpiryText = "The sign-in expired. Start again."
)

// ConnectError is a refused connection attempt with its API error code.
type ConnectError struct{ Code, Message string }

func (e *ConnectError) Error() string { return e.Message }

type deviceFlow struct {
	scope      string
	userID     int64
	apiBase    string
	deviceCode string
	trusted    bool
	interval   time.Duration
	expiresAt  time.Time
	status     string
	errorText  string
}

type oauthReply struct {
	DeviceCode              string  `json:"device_code"`
	UserCode                string  `json:"user_code"`
	VerificationURI         string  `json:"verification_uri"`
	VerificationURIComplete string  `json:"verification_uri_complete"`
	ExpiresIn               float64 `json:"expires_in"`
	Interval                float64 `json:"interval"`
	AccessToken             string  `json:"access_token"`
	Error                   string  `json:"error"`
	ErrorDescription        string  `json:"error_description"`
}

func postForm(ctx context.Context, trusted bool, endpoint string, values url.Values) (oauthReply, int, error) {
	response, err := transport.FetchText(ctx, transport.Config{TrustedBaseURL: trusted}.Client(), transport.FetchOptions{
		Method: http.MethodPost, URL: endpoint, Body: []byte(values.Encode()),
		Header:   map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
		MaxBytes: maxReplyBytes, Service: "NeoRouter sign-in", Timeout: requestTimeout, Provider: ProviderID,
	})
	if err != nil {
		return oauthReply{}, 0, err
	}
	var reply oauthReply
	// A body that is not JSON reads as an empty reply.
	_ = json.Unmarshal([]byte(response.Text), &reply)
	return reply, response.Status, nil
}

// Begin starts connecting and returns the link that opens NeoRouter's approval
// page with the code it shows.
func (c *RuntimeConnector) Begin(ctx context.Context, scope string, userID int64, address string) (any, error) {
	apiBase := APIBase(address)
	trusted := scope == "server" || apiBase != "" && apiBase == c.ServerConnection().APIBase
	// A person's own address is user input pointed at from the server: it may
	// not reach the server's network, unless it is the server's own NeoRouter.
	if scope == "user" && apiBase != "" && !trusted {
		if err := CheckPublicURL(ctx, apiBase, c.resolveHost); err != nil {
			return nil, &ConnectError{Code: "URL_NOT_ALLOWED", Message: err.Error()}
		}
	}
	if apiBase == "" {
		return nil, &ConnectError{Code: "INVALID_ADDRESS", Message: "Enter the address of your NeoRouter, for example https://router.example.com."}
	}
	origin := Connection{APIBase: apiBase}.Origin()
	hostname, err := c.hostname()
	if err != nil {
		hostname = ""
	}
	reply, status, err := postForm(ctx, trusted, origin+"/oauth/device/code", url.Values{
		"client_id": {clientID}, "client_name": {"NeoCloud (" + hostname + ")"}, "scope": {deviceScopes},
	})
	if err != nil {
		return nil, &ConnectError{Code: "NEOROUTER_UNREACHABLE", Message: "Could not reach NeoRouter at " + origin + "."}
	}
	verification := reply.VerificationURIComplete
	if verification == "" {
		verification = reply.VerificationURI
	}
	// The link is opened by the person's browser, so only a web address passes.
	_, linkErr := ssrf.CheckURL(verification)
	if status < 200 || status >= 300 || reply.DeviceCode == "" || linkErr != nil {
		return nil, &ConnectError{Code: "NEOROUTER_UNREACHABLE", Message: origin + " did not answer like a NeoRouter. Check the address."}
	}
	var secret [18]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(secret[:])
	interval := time.Duration(min(max(reply.Interval, 0), 60) * float64(time.Second))
	if reply.Interval == 0 {
		interval = 5 * time.Second
	}
	interval = max(interval, time.Second)
	lifetime := flowLifetime
	if reply.ExpiresIn > 0 && reply.ExpiresIn < flowLifetime.Seconds() {
		lifetime = time.Duration(reply.ExpiresIn * float64(time.Second))
	}
	c.flowMu.Lock()
	c.pruneLocked()
	c.flows[id] = &deviceFlow{scope: scope, userID: userID, apiBase: apiBase, deviceCode: reply.DeviceCode, trusted: trusted,
		interval: interval, expiresAt: c.now().Add(lifetime), status: "pending"}
	c.flowMu.Unlock()
	c.schedule(id, interval)
	return map[string]string{"id": id, "url": verification, "userCode": reply.UserCode}, nil
}

// CheckPublicURL refuses an address a person typed when it names or resolves to
// the server's own loopback or private network. The reason never includes the
// address a hostname resolved to. A nil resolve uses the system resolver.
func CheckPublicURL(ctx context.Context, raw string, resolve func(context.Context, string) ([]netip.Addr, error)) error {
	parsed, err := ssrf.CheckURL(raw)
	if err != nil {
		return errors.New("Base URL must be a valid http(s) URL.")
	}
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	host := parsed.Hostname()
	private := errors.New(host + " is a local or private network address, which is not reachable from here.")
	if PrivateName(host) {
		return private
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if ssrf.CheckAddr(address) != nil {
			return private
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := resolve(ctx, host)
	if err != nil || len(addresses) == 0 {
		return errors.New("Could not resolve " + host + ". Check that the domain is spelled correctly and exists.")
	}
	if slices.ContainsFunc(addresses, func(address netip.Addr) bool { return ssrf.CheckAddr(address) != nil }) {
		return errors.New(host + " resolves to a local or private network address, which is not reachable from here.")
	}
	return nil
}

// PrivateName says whether a host is, by its spelling alone, on the server's own
// machine or network. Names are not resolved.
func PrivateName(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return ssrf.CheckAddr(address) != nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "localhost.localdomain" {
		return true
	}
	return strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".localhost")
}

func (c *RuntimeConnector) schedule(id string, delay time.Duration) {
	time.AfterFunc(delay, func() { c.poll(id) })
}

func (c *RuntimeConnector) poll(id string) {
	c.flowMu.Lock()
	c.pruneLocked()
	flow := c.flows[id]
	if flow == nil || flow.status != "pending" {
		c.flowMu.Unlock()
		return
	}
	current := *flow
	c.flowMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	reply, status, err := postForm(ctx, current.trusted, Connection{APIBase: current.apiBase}.Origin()+"/oauth/token", url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {current.deviceCode}, "client_id": {clientID},
	})
	switch {
	case err == nil && status >= 200 && status < 300 && reply.AccessToken != "":
		if err := c.store(ctx, current, strings.TrimSpace(reply.AccessToken)); err != nil {
			c.finish(id, "failed", "Could not save the NeoRouter connection.")
			return
		}
		c.finish(id, "completed", "")
	case err != nil || reply.Error == "authorization_pending" || reply.Error == "slow_down":
		interval := current.interval
		if reply.Error == "slow_down" {
			interval += 5 * time.Second
			c.flowMu.Lock()
			if flow := c.flows[id]; flow != nil {
				flow.interval = interval
			}
			c.flowMu.Unlock()
		}
		c.schedule(id, interval)
	case reply.Error == "access_denied":
		c.finish(id, "failed", "The connection was declined in NeoRouter.")
	case reply.ErrorDescription != "":
		c.finish(id, "failed", reply.ErrorDescription)
	default:
		c.finish(id, "failed", "NeoRouter did not hand out a key.")
	}
}

func (c *RuntimeConnector) store(ctx context.Context, flow deviceFlow, token string) error {
	// The key goes into the env file, so it may not carry control characters.
	if token == "" || len(token) > 8192 || strings.IndexFunc(token, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return errors.New("neorouter: invalid token")
	}
	if flow.scope == "server" {
		return c.persistServer(map[string]string{envBaseURL: flow.apiBase, envAPIKey: token, envRoles: strings.Join(Roles, ",")})
	}
	return c.persistUser(ctx, flow.userID, flow.apiBase, token, Roles)
}

func (c *RuntimeConnector) finish(id, status, message string) {
	c.flowMu.Lock()
	defer c.flowMu.Unlock()
	if flow := c.flows[id]; flow != nil && flow.status == "pending" {
		flow.status, flow.errorText = status, message
	}
}

// Status is polled by the app. Only the person who started a sign-in may read
// it, and a finished result is handed out once.
func (c *RuntimeConnector) Status(id string, userID int64) (any, bool) {
	c.flowMu.Lock()
	defer c.flowMu.Unlock()
	c.pruneLocked()
	flow := c.flows[id]
	if flow == nil || flow.userID != userID {
		return nil, false
	}
	result := map[string]any{"status": flow.status, "error": nil}
	if flow.errorText != "" {
		result["error"] = flow.errorText
	}
	if flow.status != "pending" {
		delete(c.flows, id)
	}
	return result, true
}

// pruneLocked ends expired sign-ins and drops the ones nobody asked about.
func (c *RuntimeConnector) pruneLocked() {
	now := c.now()
	for id, flow := range c.flows {
		switch {
		case !now.Before(flow.expiresAt.Add(flowLifetime)):
			delete(c.flows, id)
		case !now.Before(flow.expiresAt) && flow.status == "pending":
			flow.status, flow.errorText = "failed", flowExpiryText
		}
	}
}
