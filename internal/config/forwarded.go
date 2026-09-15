package config

import (
	"fmt"
	"strings"
)

// ForwardedHeaders names the forwarding header family this deployment's
// proxies write. Reading both would let a client name its own address: a
// proxy writing one passes the other through untouched.
type ForwardedHeaders string

const (
	// XForwardedHeaders reads X-Forwarded-For, X-Forwarded-Proto and
	// X-Forwarded-Host.
	XForwardedHeaders ForwardedHeaders = "x-forwarded"

	// RFC7239Headers reads Forwarded.
	RFC7239Headers ForwardedHeaders = "forwarded"
)

// parseForwardedHeaders resolves a server.forwarded_headers value.
func parseForwardedHeaders(s string) (ForwardedHeaders, error) {
	switch v := ForwardedHeaders(strings.ToLower(strings.TrimSpace(s))); v {
	case XForwardedHeaders, RFC7239Headers:
		return v, nil
	default:
		return "", fmt.Errorf(
			"server.forwarded_headers: %q is not %q or %q",
			s,
			XForwardedHeaders,
			RFC7239Headers,
		)
	}
}
