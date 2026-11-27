package proxytrust

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustsConfiguredProxyPeers(t *testing.T) {
	cases := []struct {
		remote string
		policy string
		want   bool
	}{
		{"127.0.0.1:3000", "loopback,linklocal,uniquelocal", true},
		{"[::1]:3000", "loopback", true},
		{"172.18.0.2:3000", "172.18.0.0/16", true},
		{"192.168.1.5:3000", "uniquelocal", true},
		{"203.0.113.4:3000", "loopback,linklocal,uniquelocal", false},
		{"203.0.113.4:3000", "172.18.0.2", false},
		{"203.0.113.4:3000", "false", false},
		{"203.0.113.4:3000", "1", true},
	}
	for _, tc := range cases {
		t.Run(tc.remote+"/"+tc.policy, func(t *testing.T) {
			if got := Trusts(tc.remote, tc.policy, 0); got != tc.want {
				t.Fatalf("Trusts(%q, %q) = %v, want %v", tc.remote, tc.policy, got, tc.want)
			}
		})
	}
}

func TestClientAddressNeverBelievesUntrustedHops(t *testing.T) {
	cases := []struct {
		name, remote, forwarded, policy, want string
	}{
		{"no proxy configured", "203.0.113.4:1", "1.1.1.1", "false", "203.0.113.4"},
		{"peer not a proxy", "203.0.113.4:1", "1.1.1.1", "loopback", "203.0.113.4"},
		{"one proxy ignores forged entries", "127.0.0.1:1", "6.6.6.6, 198.51.100.7", "loopback", "198.51.100.7"},
		{"hop count", "203.0.113.4:1", "6.6.6.6, 198.51.100.7", "1", "198.51.100.7"},
		{"two hops", "203.0.113.4:1", "6.6.6.6, 198.51.100.7", "2", "6.6.6.6"},
		{"chain of private proxies", "10.0.0.1:1", "198.51.100.7, 10.0.0.2", "uniquelocal", "198.51.100.7"},
		{"garbage entry", "127.0.0.1:1", "not-an-address", "loopback", "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientAddress(tc.remote, []string{tc.forwarded}, tc.policy); got != tc.want {
				t.Fatalf("ClientAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMiddlewareSanitizesForwardingHeaders(t *testing.T) {
	var remote, proto string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		remote, proto = r.RemoteAddr, r.Header.Get("X-Forwarded-Proto")
	})
	serve := func(peer string) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = peer
		request.Header.Set("X-Forwarded-For", "198.51.100.7")
		request.Header.Set("X-Forwarded-Proto", "https")
		Middleware("loopback", next).ServeHTTP(httptest.NewRecorder(), request)
	}
	serve("127.0.0.1:55")
	if remote != "198.51.100.7:0" || proto != "https" {
		t.Fatalf("trusted proxy: remote=%q proto=%q", remote, proto)
	}
	serve("203.0.113.4:55")
	if remote != "203.0.113.4:55" || proto != "" {
		t.Fatalf("untrusted peer: remote=%q proto=%q", remote, proto)
	}
}
