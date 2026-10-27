package auth

import (
	"net"
	"net/netip"
	"net/url"
	"slices"
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

// HostSet is the names a server answers for in a Host header. Loopback and IP
// literals are always among them: neither can be rebound to another server.
type HostSet struct {
	names []string
}

// NewHostSet holds publicURL's host and names, compared as HostName leaves
// them and without case. Blank entries are dropped, so none admits a missing Host.
func NewHostSet(publicURL string, names ...string) HostSet {
	var set HostSet

	if u, err := url.Parse(publicURL); publicURL != "" && err == nil {
		names = append([]string{u.Host}, names...)
	}

	for _, name := range names {
		if name = strings.ToLower(HostName(strings.TrimSpace(name))); name != "" {
			set.names = append(set.names, name)
		}
	}

	return set
}

// Admits reports whether a Host header's host[:port] is one of the set's.
func (s HostSet) Admits(hostport string) bool {
	host := HostName(hostport)
	if host == "" {
		return false
	}

	if _, err := netip.ParseAddr(host); err == nil || IsLoopbackHost(host) {
		return true
	}

	return slices.Contains(s.names, strings.ToLower(host))
}

// IsLoopbackHost reports whether host is "localhost" or a loopback IP literal.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip, err := netip.ParseAddr(host)

	return err == nil && ip.IsLoopback()
}
