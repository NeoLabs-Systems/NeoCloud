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
	settings := cachedSystem()
	if req.URL.Scheme == "https" && settings.https != nil {
		return settings.https, nil
	}
	return settings.http, nil
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
type systemSettings struct{ http, https *url.URL }

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
