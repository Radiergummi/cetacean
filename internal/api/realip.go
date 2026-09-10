package api

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

// realIP returns middleware that records the request's peer and — when that
// peer is a trusted proxy — rewrites r.RemoteAddr to the client address the
// proxy reported.
//
// The verdict is recorded on the original peer address, before the rewrite,
// and recorded always, so downstream code can tell "untrusted" from "nobody
// decided". Readers use auth.FromTrustedProxy, never RemoteAddr.
func realIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peerHost, peerPort, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			peerIP, err := netip.ParseAddr(peerHost)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			peer := auth.Peer{Addr: peerIP, Trusted: isTrusted(peerIP, trusted)}
			r = r.WithContext(auth.ContextWithPeer(r.Context(), peer))

			if peer.Trusted {
				if clientIP, ok := resolveClientIP(r, peerPort, trusted); ok {
					r.RemoteAddr = clientIP
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveClientIP returns the rightmost X-Forwarded-For entry that is not a
// trusted proxy, joined with the peer's port. The caller has already
// established that the peer itself is trusted.
func resolveClientIP(r *http.Request, peerPort string, trusted []netip.Prefix) (string, bool) {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return "", false
	}

	parts := strings.Split(xff, ",")
	for _, part := range slices.Backward(parts) {
		ip, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if !isTrusted(ip, trusted) {
			return net.JoinHostPort(ip.String(), peerPort), true
		}
	}

	return "", false
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
