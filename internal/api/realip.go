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
			peer, peerPort := peerOf(r, trusted)
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

// peerOf resolves the address the connection arrived from, the verdict on
// whether it is a configured trusted proxy, and the port to carry onto a
// resolved client address. An unparseable RemoteAddr yields an untrusted
// verdict rather than none, so no verdict at all means this never ran.
func peerOf(r *http.Request, trusted []netip.Prefix) (auth.Peer, string) {
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return auth.Peer{}, ""
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return auth.Peer{}, ""
	}

	return auth.Peer{Addr: addr, Trusted: isTrusted(addr, trusted)}, port
}

// resolveClientIP returns the rightmost node in the forwarding chain that is
// not a trusted proxy, joined with the peer's port. The caller has already
// established that the peer itself is trusted.
//
// Forwarded (RFC 7239) is preferred over X-Forwarded-For and walked the same
// way, both ordering nodes first proxy first. One naming no address makes no
// statement about the client, so the fallback turns on the absence of an
// address rather than of the header.
func resolveClientIP(r *http.Request, peerPort string, trusted []netip.Prefix) (string, bool) {
	nodes := forwardedNodes(r.Header.Values("Forwarded"))
	if !namesAnyAddr(nodes) {
		nodes = forwardedForNodes(r.Header.Values("X-Forwarded-For"))
	}

	// A node naming no address is read past rather than treated as the
	// boundary: naming the client behind an anonymised hop serves a log better
	// than naming the proxy.
	for _, node := range slices.Backward(nodes) {
		addr, ok := nodeAddr(node)
		if !ok || isTrusted(addr, trusted) {
			continue
		}

		return net.JoinHostPort(addr.String(), peerPort), true
	}

	return "", false
}

// namesAnyAddr reports whether any of the nodes names an address at all.
func namesAnyAddr(nodes []string) bool {
	return slices.ContainsFunc(nodes, func(node string) bool {
		_, ok := nodeAddr(node)
		return ok
	})
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
