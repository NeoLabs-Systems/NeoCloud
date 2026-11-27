// Package proxytrust implements Express's TRUST_PROXY policy: which peers may
// name the client's address and protocol in X-Forwarded-* headers.
package proxytrust

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// Trusts reports whether the address `hop` proxies away from the server (0 is
// the TCP peer) is a trusted proxy. Invalid values fail closed.
func Trusts(address, policy string, hop int) bool {
	policy = strings.TrimSpace(policy)
	switch strings.ToLower(policy) {
	case "", "false", "no", "off", "0":
		return false
	case "true", "yes", "on":
		return hop < 1
	}
	if hops, err := strconv.Atoi(policy); err == nil {
		return hop < hops
	}
	peer, err := netip.ParseAddr(host(address))
	if err != nil {
		return false
	}
	peer = peer.Unmap()
	for _, raw := range strings.Split(policy, ",") {
		entry := strings.TrimSpace(raw)
		if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Contains(peer) {
			return true
		}
		if addr, err := netip.ParseAddr(entry); err == nil && addr.Unmap() == peer {
			return true
		}
		switch strings.ToLower(entry) {
		case "loopback":
			if peer.IsLoopback() {
				return true
			}
		case "linklocal":
			if peer.IsLinkLocalUnicast() {
				return true
			}
		case "uniquelocal":
			if peer.IsPrivate() {
				return true
			}
		}
	}
	return false
}

// ClientAddress is the nearest address that is not a trusted proxy, walking
// X-Forwarded-For from the entry the closest proxy appended.
func ClientAddress(remoteAddr string, forwardedFor []string, policy string) string {
	current := host(remoteAddr)
	var chain []string
	for _, value := range forwardedFor {
		chain = append(chain, strings.Split(value, ",")...)
	}
	for hop := 0; len(chain) > 0 && Trusts(current, policy, hop); hop++ {
		next := strings.TrimSpace(chain[len(chain)-1])
		chain = chain[:len(chain)-1]
		if _, err := netip.ParseAddr(next); err != nil {
			break
		}
		current = next
	}
	return strings.TrimPrefix(current, "::ffff:")
}

// Middleware drops forwarding headers from untrusted peers and puts the client
// address behind trusted proxies into RemoteAddr, so that everything after it
// (rate limits, session records, logs) sees the client.
func Middleware(policy string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.WithContext(r.Context())
		if !Trusts(r.RemoteAddr, policy, 0) {
			request.Header = r.Header.Clone()
			request.Header.Del("X-Forwarded-Proto")
		} else if client := ClientAddress(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), policy); client != host(r.RemoteAddr) {
			request.RemoteAddr = net.JoinHostPort(client, "0")
		}
		next.ServeHTTP(w, request)
	})
}

func host(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}
