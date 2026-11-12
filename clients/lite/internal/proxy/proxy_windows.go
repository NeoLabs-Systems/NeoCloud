package proxy

import (
	"golang.org/x/sys/windows/registry"
)

// readSystem reads the WinINET settings (Settings › Network › Proxy, the
// manual proxy and its "don't use the proxy for" list). Automatic
// configuration scripts are not evaluated.
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
	settings := parseProxyServer(server)
	if override, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		settings.bypass = parseBypassList(override)
	}
	return settings
}
