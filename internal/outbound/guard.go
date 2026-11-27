// Package outbound holds what the server needs when it connects to hosts a
// person typed (cloud storage and imports): a guard that resolves a name once,
// checks every address and dials exactly a checked one, and the reader for
// another server's WebDAV answers.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

const (
	connectTimeout        = 20 * time.Second
	responseHeaderTimeout = time.Minute
	idleTimeout           = 2 * time.Minute
)

// ErrBlocked reports an address the guard refuses to reach.
var ErrBlocked = ssrf.ErrBlocked

// Resolver turns a host name into its addresses.
type Resolver func(context.Context, string) ([]netip.Addr, error)

// Guard decides where connections may go. A nil Check allows every address, a
// nil Resolve uses the system resolver.
type Guard struct {
	Check   func(netip.Addr) error
	Resolve Resolver
}

var metadata = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
	netip.MustParsePrefix("fe80::/10"),
}

// BlockMetadata refuses link-local and cloud metadata addresses only.
func BlockMetadata(addr netip.Addr) error {
	addr = addr.Unmap().WithZone("")
	for _, prefix := range metadata {
		if prefix.Contains(addr) {
			return fmt.Errorf("%w: %s", ErrBlocked, addr)
		}
	}
	return nil
}

// PublicOnly refuses everything that is not a public internet address.
func PublicOnly(addr netip.Addr) error {
	if err := BlockMetadata(addr); err != nil {
		return err
	}
	return ssrf.CheckAddr(addr)
}

// Lookup resolves host (an IP literal stays as it is) and refuses it when any
// of its addresses is not allowed.
func (g Guard) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		resolve := g.Resolve
		if resolve == nil {
			resolve = func(ctx context.Context, name string) ([]netip.Addr, error) {
				return net.DefaultResolver.LookupNetIP(ctx, "ip", name)
			}
		}
		if addresses, err = resolve(ctx, host); err != nil {
			return nil, err
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: no addresses for %s", ErrBlocked, host)
	}
	if g.Check != nil {
		for _, address := range addresses {
			if err := g.Check(address); err != nil {
				return nil, err
			}
		}
	}
	return addresses, nil
}

// DialContext connects to a checked address, so a name that answers differently
// a moment later cannot slip between the check and the connection. The
// connection drops after two minutes without a byte either way.
func (g Guard) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := g.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: connectTimeout}
	var failures []error
	for _, candidate := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if err == nil {
			return &idleConn{conn}, nil
		}
		failures = append(failures, err)
	}
	return nil, errors.Join(failures...)
}

// HTTPClient returns a client bound to this guard. It ignores proxy settings
// and never follows redirects: the caller decides where credentials may go.
func (g Guard) HTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           g.DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type idleConn struct{ net.Conn }

func (c *idleConn) Read(p []byte) (int, error) {
	if err := c.SetDeadline(time.Now().Add(idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if err := c.SetDeadline(time.Now().Add(idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
