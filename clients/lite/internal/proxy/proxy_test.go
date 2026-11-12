package proxy

import (
	"net/http"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

func TestFor(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	remote, _ := http.NewRequest("GET", "https://cloud.example.com/api", nil)
	local, _ := http.NewRequest("GET", "http://127.0.0.1:3344/api", nil)

	if got, _ := For(config.GlobalSettings{Proxy: config.ProxyNone})(remote); got != nil {
		t.Errorf("none: %v", got)
	}
	manual := For(config.GlobalSettings{Proxy: config.ProxyManual, ProxyURL: "socks5://proxy.lan:1080"})
	if got, _ := manual(remote); got == nil || got.String() != "socks5://proxy.lan:1080" {
		t.Errorf("manual: %v", got)
	}
	if got, _ := manual(local); got != nil {
		t.Errorf("manual for a loopback server: %v", got)
	}
	if _, err := For(config.GlobalSettings{Proxy: config.ProxyManual, ProxyURL: "ftp://x"})(remote); err == nil {
		t.Error("a bad manual proxy was used")
	}
	t.Setenv("HTTPS_PROXY", "http://env.lan:8080")
	if got, _ := For(config.GlobalSettings{})(remote); got == nil || got.Host != "env.lan:8080" {
		t.Errorf("system from the environment: %v", got)
	}
}
