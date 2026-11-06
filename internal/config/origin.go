package config

import (
	"net/url"
	"strings"
)

// SerialiseOrigin is RFC 6454's serialisation of a scheme://host[:port]
// origin: scheme and host lower-cased, the scheme's default port dropped.
// Anything else, "null" and "*" among them, is returned as written.
func SerialiseOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Opaque != "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return origin
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	switch port := u.Port(); {
	case port == "", scheme == "http" && port == "80", scheme == "https" && port == "443":
		return scheme + "://" + host
	default:
		return scheme + "://" + host + ":" + port
	}
}
