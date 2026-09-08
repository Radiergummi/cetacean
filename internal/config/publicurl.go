package config

import (
	"fmt"
	"net/url"
)

// ValidatePublicURL checks server.public_url is an http(s) origin: a scheme
// and a host, and nothing after them.
//
// A path is rejected rather than accepted and ignored, because
// server.base_path already carries the external prefix in both directions —
// basePathMiddleware strips it from inbound requests and absPath prepends it
// to outbound links — and two settings for one concept drift apart.
//
// An empty value is valid and means "unset": every consumer keeps the
// fallback it had before this setting existed.
func ValidatePublicURL(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("server.public_url is not a valid URL %q: %w", raw, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"server.public_url must use http or https, got %q",
			raw,
		)
	}

	if u.Hostname() == "" {
		return fmt.Errorf("server.public_url must include a host, got %q", raw)
	}

	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf(
			"server.public_url must not include a path, got %q; set server.base_path to %q instead",
			raw, u.Path,
		)
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("server.public_url must not include a query or fragment, got %q", raw)
	}

	return nil
}
