package auth

import (
	"context"
	"net/netip"
)

// Peer is the edge's view of where a request came from: the immediate network
// peer's address, observed before any middleware rewrote RemoteAddr, and the
// verdict on whether that peer is a configured trusted proxy. The decision is
// made once, in the outermost middleware, and read from here downstream.
type Peer struct {
	// Addr is the address the connection actually arrived from.
	Addr netip.Addr

	// Trusted reports whether Addr falls inside the configured trusted-proxy
	// set. False when no trusted proxies are configured.
	Trusted bool
}

type peerCtxKey struct{}

// ContextWithPeer returns a context carrying the request's peer.
func ContextWithPeer(ctx context.Context, p Peer) context.Context {
	return context.WithValue(ctx, peerCtxKey{}, p)
}

// PeerFromContext returns the peer recorded at the edge. The second result is
// false when nothing recorded one, which callers must treat as untrusted.
func PeerFromContext(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerCtxKey{}).(Peer)
	return p, ok
}

// FromTrustedProxy reports whether the request reached us through a configured
// trusted proxy. Credentials carried in request headers must ask this before
// they may be believed.
func FromTrustedProxy(ctx context.Context) bool {
	p, ok := PeerFromContext(ctx)
	return ok && p.Trusted
}
