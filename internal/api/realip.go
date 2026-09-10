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

// resolveClientIP walks the forwarding chain right-to-left, returning the
// first (rightmost) node that is NOT a trusted proxy, joined with the peer's
// port. This is the standard algorithm for extracting the real client IP
// behind a chain of trusted proxies. The caller has already established that
// the peer itself is trusted.
//
// RFC 7239's Forwarded is preferred over the de-facto X-Forwarded-For, and
// both are walked the same way — the two order their nodes identically, first
// proxy first. A Forwarded header naming no node at all (carrying only proto
// or host, say) leaves X-Forwarded-For as the only statement about the client,
// so the fallback is on the absence of nodes rather than of the header.
func resolveClientIP(r *http.Request, peerPort string, trusted []netip.Prefix) (string, bool) {
	nodes := forwardedNodes(r.Header.Values("Forwarded"))
	if len(nodes) == 0 {
		nodes = forwardedForNodes(r.Header.Values("X-Forwarded-For"))
	}

	// Walk right-to-left: the rightmost node that is not a trusted proxy is
	// the client. A node naming no address — "unknown", an obfuscated
	// identifier, a malformed entry — is read past rather than treated as the
	// boundary. RemoteAddr is informational once realIP has recorded the trust
	// verdict, and naming the client behind an anonymised hop serves a log
	// better than naming the proxy.
	for _, node := range slices.Backward(nodes) {
		addr, ok := nodeAddr(node)
		if !ok || isTrusted(addr, trusted) {
			continue
		}

		return net.JoinHostPort(addr.String(), peerPort), true
	}

	return "", false
}

// forwardedForNodes splits X-Forwarded-For's comma-separated list into node
// identifiers, in the header's own order. Repeated header lines are one list,
// per RFC 9110 §5.3.
func forwardedForNodes(values []string) []string {
	var nodes []string

	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				nodes = append(nodes, part)
			}
		}
	}

	return nodes
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
