// Package netguard screens the addresses an outbound fetch may reach, for the
// fetches whose target a remote party names: a CIMD client_id, a registry.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
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

// Screen decides whether one resolved address of host may be dialled.
type Screen func(ctx context.Context, host string, ip net.IP) error

// DialContext resolves the host, screens every address and dials the first
// survivor by address, so a rebinding resolver cannot answer the screen and
// the connect differently.
func DialContext(
	dialer *net.Dialer,
	screen Screen,
) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("dial: invalid address %q: %w", address, err)
		}

		ips := []net.IP{net.ParseIP(host)}
		if ips[0] == nil {
			ips, err = net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("dial: lookup %q: %w", host, err)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("%w: no addresses resolved for %q", ErrBlocked, host)
		}

		var firstErr error
		for _, ip := range ips {
			if err := screen(ctx, host, ip); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = dialErr
			}
		}
		return nil, firstErr
	}
}
