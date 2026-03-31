package acl

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// TailscaleSource extracts grants from Tailscale peer capabilities.
type TailscaleSource struct {
	Capability string // e.g. "example.com/cap/cetacean"
}

func (s *TailscaleSource) GrantsFor(id *auth.Identity) []Grant {
	if s.Capability == "" || id == nil || id.Raw == nil {
		return nil
	}

	caps, ok := id.Raw["caps"].(map[string]any)
	if !ok {
		return nil
	}

	capData, ok := caps[s.Capability].([]any)
	if !ok {
		return nil
	}

	return extractGrantsFromRaw(capData)
}
