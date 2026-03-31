package acl

import (
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
)

// HeadersSource extracts grants from a proxy-injected HTTP header.
type HeadersSource struct {
	Header string // header name containing JSON grants
}

func (s *HeadersSource) GrantsFor(id *auth.Identity) []Grant {
	if s.Header == "" || id == nil || id.Raw == nil {
		return nil
	}

	headerVal, ok := id.Raw[s.Header].(string)
	if !ok || headerVal == "" {
		return nil
	}

	var raw []any
	if err := json.Unmarshal([]byte(headerVal), &raw); err != nil {
		return nil
	}

	return extractGrantsFromRaw(raw)
}
