package api

import (
	"net/netip"
	"strings"
)

// forwardedNodes returns the node identifier named by the "for" parameter of
// each RFC 7239 Forwarded element, in the order the elements appear:
// left-to-right, first proxy first, the same ordering X-Forwarded-For uses.
//
// Nodes are returned as they stand, "unknown" and obfuscated identifiers
// included; nodeAddr decides which of them name an address.
func forwardedNodes(values []string) []string {
	var nodes []string

	for _, value := range values {
		for _, element := range splitOutsideQuotes(value, ',') {
			for _, pair := range splitOutsideQuotes(element, ';') {
				name, node, ok := strings.Cut(pair, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(name), "for") {
					continue
				}
				nodes = append(nodes, unquote(strings.TrimSpace(node)))
			}
		}
	}

	return nodes
}

// nodeAddr returns the IP address a Forwarded node identifier names.
//
// RFC 7239 §6 gives four forms for a nodename — an IPv4 address, a bracketed
// IPv6 address, the literal "unknown", and an obfuscated identifier beginning
// with "_" — each with an optional ":port". Only the first two name an
// address; the other two deliberately do not, and report false.
//
// An IPv4-mapped address is unmapped, so a proxy naming a hop as
// ::ffff:10.0.0.2 is still matched against an IPv4 trusted-proxy prefix.
func nodeAddr(node string) (netip.Addr, bool) {
	host := node

	switch {
	case strings.HasPrefix(node, "["):
		end := strings.IndexByte(node, ']')
		if end < 0 {
			return netip.Addr{}, false
		}
		host = node[1:end]

	// A single colon can only separate an IPv4 address from its port: an
	// address holding colons of its own is required to be bracketed. Where it
	// is not — which happens in the wild — the whole value parses as an IPv6
	// address below.
	case strings.Count(node, ":") == 1:
		host, _, _ = strings.Cut(node, ":")
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.Unmap(), true
}

// splitOutsideQuotes splits s on sep, ignoring separators inside a
// quoted-string. A quoted-pair escapes the character following the backslash,
// so an escaped DQUOTE does not end the string.
func splitOutsideQuotes(s string, sep byte) []string {
	var (
		parts   []string
		start   int
		inQuote bool
		escaped bool
	)

	for i := range len(s) {
		switch {
		case escaped:
			escaped = false
		case inQuote && s[i] == '\\':
			escaped = true
		case s[i] == '"':
			inQuote = !inQuote
		case s[i] == sep && !inQuote:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}

	return append(parts, s[start:])
}

// unquote strips the surrounding DQUOTEs from a quoted-string and resolves its
// quoted-pairs. A value that is not quoted is returned unchanged.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}

	inner := s[1 : len(s)-1]
	if !strings.Contains(inner, `\`) {
		return inner
	}

	var b strings.Builder
	b.Grow(len(inner))

	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}

	return b.String()
}
