package auth

import (
	"net"
	"net/netip"
	"strings"
)

// HostName strips the port, an IPv6 literal's brackets, and a fully qualified
// name's trailing dot from a Host header's host[:port].
func HostName(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}

	return strings.TrimSuffix(strings.Trim(host, "[]"), ".")
}

// IsLoopbackHost reports whether host is "localhost" or a loopback IP literal.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip, err := netip.ParseAddr(host)

	return err == nil && ip.IsLoopback()
}
