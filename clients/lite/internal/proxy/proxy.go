// Package proxy turns the proxy setting into the function the API client
// asks for every request: "system" (environment variables, else the
// system's proxy settings), "none", or "manual" with proxy_url.
package proxy

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// For returns the proxy function of the settings.
func For(g config.GlobalSettings) func(*http.Request) (*url.URL, error) {
	switch g.ProxyMode() {
	case config.ProxyNone:
		return func(*http.Request) (*url.URL, error) { return nil, nil }
	case config.ProxyManual:
		manual, err := config.ParseProxyURL(g.ProxyURL)
		return func(req *http.Request) (*url.URL, error) {
			if err != nil || loopback(req) {
				return nil, err
			}
			return manual, nil
		}
	}
	return systemProxy
}

// systemProxy honours HTTPS_PROXY / HTTP_PROXY / NO_PROXY first, then
// what the system's network settings say.
func systemProxy(req *http.Request) (*url.URL, error) {
	if fromEnv, err := http.ProxyFromEnvironment(req); fromEnv != nil || err != nil {
		return fromEnv, err
	}
	if loopback(req) {
		return nil, nil
	}
	return fromSettings(req, cachedSystem()), nil
}

// fromSettings picks the OS settings' proxy for req; nil is direct.
func fromSettings(req *http.Request, settings systemSettings) *url.URL {
	if bypasses(settings.bypass, req.URL.Hostname()) {
		return nil
	}
	chosen := settings.http
	if req.URL.Scheme == "https" && settings.https != nil {
		chosen = settings.https
	}
	// A system proxy left over from another network (the office proxy at
	// home, a VPN that is off) would block every request: go direct while
	// it can't be reached. A manual proxy is always used.
	if chosen != nil && !reachable(chosen.Host) {
		return nil
	}
	return chosen
}

var (
	reachMu    sync.Mutex
	reachCache = map[string]reachResult{}
	dialProxy  = func(address string) error {
		conn, err := net.DialTimeout("tcp", address, 3*time.Second)
		if err == nil {
			conn.Close()
		}
		return err
	}
)

type reachResult struct {
	ok bool
	at time.Time
}

// reachable reports whether a TCP connection to the proxy opens, checked
// at most every two minutes per proxy.
func reachable(address string) bool {
	reachMu.Lock()
	defer reachMu.Unlock()
	if r, ok := reachCache[address]; ok && time.Since(r.at) < 2*time.Minute {
		return r.ok
	}
	ok := dialProxy(address) == nil
	reachCache[address] = reachResult{ok: ok, at: time.Now()}
	return ok
}

func loopback(req *http.Request) bool {
	host := req.URL.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// systemSettings are the proxies of the OS settings; nil is direct.
type systemSettings struct {
	http, https *url.URL
	bypass      []string // hosts that go direct (Windows ProxyOverride)
}

var (
	cacheMu  sync.Mutex
	cached   systemSettings
	cachedAt time.Time
)

// cachedSystem reads the OS settings at most every five minutes (on macOS
// that starts scutil).
func cachedSystem() systemSettings {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if time.Since(cachedAt) > 5*time.Minute {
		cached, cachedAt = readSystem(), time.Now()
	}
	return cached
}

// hostPort turns "proxy.lan:3128" into an http:// proxy URL.
func hostPort(host, port string) *url.URL {
	if host == "" {
		return nil
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	}
	return &url.URL{Scheme: "http", Host: host}
}
