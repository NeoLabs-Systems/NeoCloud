package proxy

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// readSystem reads the WinINET settings (Settings › Network › Proxy, the
// manual proxy). Automatic configuration scripts are not evaluated.
func readSystem() systemSettings {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return systemSettings{}
	}
	defer key.Close()
	if enabled, _, err := key.GetIntegerValue("ProxyEnable"); err != nil || enabled == 0 {
		return systemSettings{}
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return systemSettings{}
	}
	return parseProxyServer(server)
}

// parseProxyServer reads "host:port" or "http=host:port;https=host:port".
func parseProxyServer(value string) systemSettings {
	if !strings.Contains(value, "=") {
		host, port, _ := strings.Cut(strings.TrimSpace(value), ":")
		both := hostPort(host, port)
		return systemSettings{http: both, https: both}
	}
	var settings systemSettings
	for _, part := range strings.Split(value, ";") {
		scheme, address, _ := strings.Cut(part, "=")
		host, port, _ := strings.Cut(strings.TrimSpace(address), ":")
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "http":
			settings.http = hostPort(host, port)
		case "https":
			settings.https = hostPort(host, port)
		}
	}
	return settings
}
