package acl

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// OIDCSource extracts grants from a custom OIDC token claim.
type OIDCSource struct {
	Claim string // e.g. "cetacean_grants"
}

func (s *OIDCSource) GrantsFor(id *auth.Identity) []Grant {
	if s.Claim == "" || id == nil || id.Raw == nil {
		return nil
	}

	claimData, ok := id.Raw[s.Claim].([]any)
	if !ok {
		return nil
	}

	return extractGrantsFromRaw(claimData)
}
