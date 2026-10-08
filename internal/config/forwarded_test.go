package config

import (
	"strings"
	"testing"
)

func TestParseForwardedHeaders(t *testing.T) {
	valid := map[string]ForwardedHeaders{
		"x-forwarded":   XForwardedHeaders,
		"X-Forwarded":   XForwardedHeaders,
		"  forwarded  ": RFC7239Headers,
		"FORWARDED":     RFC7239Headers,
	}

	for raw, want := range valid {
		got, err := parseForwardedHeaders(raw)
		if err != nil {
			t.Errorf("parseForwardedHeaders(%q) = %v, want nil", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseForwardedHeaders(%q) = %q, want %q", raw, got, want)
		}
	}

	invalid := []string{"", "x-forwarded-for", "rfc7239", "both", "none"}

	for _, raw := range invalid {
		_, err := parseForwardedHeaders(raw)
		if err == nil {
			t.Errorf("parseForwardedHeaders(%q) = nil, want error", raw)
			continue
		}
		if !strings.Contains(err.Error(), "server.forwarded_headers") {
			t.Errorf("parseForwardedHeaders(%q) = %q, want it to name the setting", raw, err)
		}
	}
}
