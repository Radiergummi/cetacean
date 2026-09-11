package api

import (
	"net/netip"
	"strings"
)

// forwardedParams returns the value every RFC 7239 Forwarded element gives the
// named parameter, in the order the elements appear — leftmost, the hop
// closest to the client, first. Values are returned as they stand; what a
// given parameter may say is its caller's business.
func forwardedParams(values []string, name string) []string {
	var found []string

	for _, value := range values {
		for _, element := range splitOutsideQuotes(value, ',') {
			for _, pair := range splitOutsideQuotes(element, ';') {
				key, param, ok := strings.Cut(pair, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), name) {
					continue
				}
				found = append(found, unquote(strings.TrimSpace(param)))
			}
		}
	}

	return found
}

// forwardedNodes returns the node identifier named by each Forwarded element's
// "for" parameter, first proxy first — the ordering X-Forwarded-For uses.
// nodeAddr decides which of them name an address.
func forwardedNodes(values []string) []string {
	return forwardedParams(values, "for")
}

// forwardedOrigin returns the "proto" and "host" the first Forwarded element
// names, empty for either the header does not carry. These describe the
// connection the *client* made, so the leftmost element is the one that saw
// it — later elements describe hops between proxies.
func forwardedOrigin(values []string) (proto, host string) {
	if protos := forwardedParams(values, "proto"); len(protos) > 0 {
		proto = protos[0]
	}

	if hosts := forwardedParams(values, "host"); len(hosts) > 0 {
		host = hosts[0]
	}

	return proto, host
}

// nodeAddr returns the IP address a Forwarded node identifier names. Of RFC
// 7239 §6's four nodename forms only an IPv4 address and a bracketed IPv6
// address name one; "unknown" and obfuscated identifiers report false. An
// IPv4-mapped address is unmapped, so ::ffff:10.0.0.2 still matches an IPv4
// trusted-proxy prefix.
func nodeAddr(node string) (netip.Addr, bool) {
	host := node

	switch {
	case strings.HasPrefix(node, "["):
		end := strings.IndexByte(node, ']')
		if end < 0 {
			return netip.Addr{}, false
		}
		host = node[1:end]

	// A lone colon separates an IPv4 address from its port; an unbracketed
	// address carrying more parses as IPv6 below.
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
// quoted-string, where a backslash escapes the character that follows.
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
