package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// realIP returns middleware that resolves the client IP from X-Forwarded-For
// when the direct peer is a trusted proxy. It rewrites r.RemoteAddr so all
// downstream code (logging, auth, etc.) sees the real client address.
//
// If trusted is empty the middleware is a no-op.
func realIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	if len(trusted) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if clientIP, ok := resolveClientIP(r, trusted); ok {
				r.RemoteAddr = clientIP
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveClientIP walks X-Forwarded-For right-to-left, returning the
// first (rightmost) entry that is NOT a trusted proxy. This is the
// standard algorithm for extracting the real client IP behind a chain
// of trusted proxies.
func resolveClientIP(r *http.Request, trusted []netip.Prefix) (string, bool) {
	peerHost, peerPort, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", false
	}

	peerIP, err := netip.ParseAddr(peerHost)
	if err != nil {
		return "", false
	}

	if !isTrusted(peerIP, trusted) {
		return "", false
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return "", false
	}

	// Walk right-to-left: the rightmost non-trusted entry is the client.
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
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
