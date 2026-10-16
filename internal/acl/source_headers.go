package acl

import (
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
)

// HeadersSource extracts grants from a proxy-injected HTTP header.
// With Capability set, the header is an object keyed by capability name, as
// tailscale serve sends Tailscale-App-Capabilities, and only that key's array
// is read.
type HeadersSource struct {
	Header     string // header name containing JSON grants
	Capability string // key of the grants array, if the header is an object
}

func (s *HeadersSource) GrantsFor(id *auth.Identity) []Grant {
	if s.Header == "" || id == nil || id.Raw == nil {
		return nil
	}

	headerVal, ok := id.Raw[s.Header].(string)
	if !ok || headerVal == "" {
		return nil
	}

	if s.Capability != "" {
		var caps map[string]any
		if err := json.Unmarshal([]byte(headerVal), &caps); err != nil {
			return nil
		}

		raw, _ := caps[s.Capability].([]any)

		return extractGrantsFromRaw(raw)
	}

	var raw []any
	if err := json.Unmarshal([]byte(headerVal), &raw); err != nil {
		return nil
	}

	return extractGrantsFromRaw(raw)
}
