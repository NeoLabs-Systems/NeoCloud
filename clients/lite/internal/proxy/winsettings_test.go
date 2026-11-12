package proxy

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestParseProxyServer(t *testing.T) {
	cases := []struct{ value, http, https string }{
		{"proxy.lan:3128", "http://proxy.lan:3128", "http://proxy.lan:3128"},
		{"http://proxy.labgate.bwg:8080", "http://proxy.labgate.bwg:8080", "http://proxy.labgate.bwg:8080"},
		{"http://proxy.lan:8080/", "http://proxy.lan:8080", "http://proxy.lan:8080"},
		{"proxy.lan", "http://proxy.lan", "http://proxy.lan"},
		{"http=a.lan:80;https=http://b.lan:443;socks=c.lan:1080", "http://a.lan:80", "http://b.lan:443"},
		{"socks=c.lan:1080", "", ""},
		{"socks://c.lan:1080", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		got := parseProxyServer(c.value)
		if str(got.http) != c.http || str(got.https) != c.https {
			t.Errorf("%q: http %q https %q", c.value, str(got.http), str(got.https))
		}
	}
}

func str(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

func TestBypassList(t *testing.T) {
	list := parseBypassList("<local>; *.labgate.bwg;10.*")
	for host, want := range map[string]bool{
		"intranet":           true,
		"files.labgate.bwg":  true,
		"10.1.2.3":           true,
		"cloud2.neo.eu.com":  false,
		"labgate.bwg.evil.x": false,
	} {
		if got := bypasses(list, host); got != want {
			t.Errorf("%s: %v", host, got)
		}
	}
}

func TestUnreachableSystemProxyGoesDirect(t *testing.T) {
	settings := parseProxyServer("http://proxy.labgate.bwg:8080")
	settings.bypass = parseBypassList("<local>")
	up := false
	defer func(d func(string) error) { dialProxy = d }(dialProxy)
	dialProxy = func(string) error {
		if up {
			return nil
		}
		return errors.New("no such host")
	}
	req, _ := http.NewRequest("GET", "https://cloud2.neo.eu.com/api", nil)
	reachCache = map[string]reachResult{}
	if got := fromSettings(req, settings); got != nil {
		t.Errorf("unreachable proxy was used: %v", got)
	}
	up = true
	reachCache = map[string]reachResult{}
	if got := fromSettings(req, settings); got == nil || got.Host != "proxy.labgate.bwg:8080" {
		t.Errorf("reachable proxy not used: %v", got)
	}
	intranet, _ := http.NewRequest("GET", "https://intranet/api", nil)
	if got := fromSettings(intranet, settings); got != nil {
		t.Errorf("bypassed host went through the proxy: %v", got)
	}
}
