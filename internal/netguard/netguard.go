// Package netguard screens the addresses an outbound fetch may reach, for the
// fetches whose target a remote party names: a CIMD client_id, a registry.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"syscall"
)

// ErrBlocked is an address the policy refuses to dial.
var ErrBlocked = errors.New("address not allowed")

// cgnatBlock is RFC 6598 (100.64.0.0/10) — carrier-grade NAT and Tailscale's
// default address pool. net.IP.IsPrivate covers RFC 1918 and ULA but not
// CGNAT, so we add it explicitly.
var cgnatBlock = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// specialUseBlocks are the RFC 6890 blocks not globally reachable that the
// net.IP predicates in Check do not already name.
var specialUseBlocks = func() []*net.IPNet {
	var blocks []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"100::/64", "2001::/23", "2001:db8::/32",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		blocks = append(blocks, n)
	}
	return blocks
}()

// Policy is what Check admits beyond globally reachable unicast. Link-local,
// unspecified, multicast and special-use addresses are refused regardless.
type Policy struct {
	AllowLoopback bool

	// AllowPrivate admits RFC 1918, ULA and CGNAT addresses.
	AllowPrivate bool
}

// Check returns an error wrapping ErrBlocked, naming the category, when the
// policy refuses ip.
func (p Policy) Check(ip net.IP) error {
	if ip.IsLoopback() && !p.AllowLoopback {
		return fmt.Errorf("%w: resolved to loopback address %s", ErrBlocked, ip)
	}
	if ip.IsPrivate() && !p.AllowPrivate {
		return fmt.Errorf("%w: resolved to private address %s", ErrBlocked, ip)
	}
	if cgnatBlock.Contains(ip) && !p.AllowPrivate {
		return fmt.Errorf("%w: resolved to CGNAT address %s", ErrBlocked, ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: resolved to link-local address %s", ErrBlocked, ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("%w: resolved to unspecified address %s", ErrBlocked, ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("%w: resolved to multicast address %s", ErrBlocked, ip)
	}
	if slices.ContainsFunc(specialUseBlocks, func(n *net.IPNet) bool { return n.Contains(ip) }) {
		return fmt.Errorf("%w: resolved to special-use address %s", ErrBlocked, ip)
	}
	return nil
}

// Private reports whether ip is in RFC 1918, ULA or CGNAT space.
func Private(ip net.IP) bool {
	return ip.IsPrivate() || cgnatBlock.Contains(ip)
}

// Screen decides whether ip, one address of the host:port the caller asked to
// dial, may be connected to.
type Screen func(ctx context.Context, address string, ip net.IP) error

type dialAddressKey struct{}

// DialContext dials through a copy of dialer that screens each address at
// connect time, after resolution, so a rebinding resolver cannot answer the
// screen and the connect differently, and the dialer keeps its parallel dials.
func DialContext(
	dialer *net.Dialer,
	screen Screen,
) func(ctx context.Context, network, address string) (net.Conn, error) {
	screened := *dialer
	screened.ControlContext = func(ctx context.Context, _, connect string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(connect)
		if err != nil {
			return err
		}

		addr, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("%w: %q is not an address", ErrBlocked, connect)
		}

		address, _ := ctx.Value(dialAddressKey{}).(string)

		return screen(ctx, address, net.IP(addr.Unmap().WithZone("").AsSlice()))
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx = context.WithValue(ctx, dialAddressKey{}, address)

		return screened.DialContext(ctx, network, address)
	}
}
