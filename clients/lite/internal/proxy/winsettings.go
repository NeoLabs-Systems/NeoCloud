package proxy

import (
	"net"
	"net/url"
	"path"
	"strings"
)

// parseProxyServer reads the WinINET ProxyServer value: "host:port",
// "http://host:port", or per scheme "http=host:port;https=host:port"
// (entries may carry a scheme too). SOCKS and FTP entries are ignored.
func parseProxyServer(value string) systemSettings {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "=") {
		both := proxyEntry(value)
		return systemSettings{http: both, https: both}
	}
	var settings systemSettings
	for _, part := range strings.Split(value, ";") {
		scheme, address, _ := strings.Cut(part, "=")
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "http":
			settings.http = proxyEntry(address)
		case "https":
			settings.https = proxyEntry(address)
		}
	}
	return settings
}

// proxyEntry turns "proxy.lan:3128" or "http://proxy.lan:3128/" into a
// proxy URL; anything else (socks://…, empty) is nil.
func proxyEntry(entry string) *url.URL {
	entry = strings.TrimSpace(entry)
	if scheme, rest, ok := strings.Cut(entry, "://"); ok {
		switch strings.ToLower(scheme) {
		case "http", "https":
			entry = rest
		default:
			return nil
		}
	}
	entry = strings.TrimRight(entry, "/")
	if host, port, err := net.SplitHostPort(entry); err == nil {
		return hostPort(host, port)
	}
	return hostPort(entry, "")
}

// parseBypassList reads ProxyOverride: "<local>;*.corp.lan;10.*".
func parseBypassList(value string) []string {
	var list []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ' ' || r == ',' }) {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			list = append(list, item)
		}
	}
	return list
}

// bypasses reports whether host (no port) is on the bypass list. "<local>"
// matches names without a dot; other entries are wildcards like *.corp.lan.
func bypasses(list []string, host string) bool {
	host = strings.ToLower(host)
	for _, pattern := range list {
		if pattern == "<local>" {
			if !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(pattern, host); ok {
			return true
		}
	}
	return false
}
