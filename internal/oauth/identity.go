package oauth

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// ProviderName identifies identities derived from MCP bearer tokens. Stamped
// on auth.Identity.Provider so downstream code can distinguish OAuth-vended
// MCP identities from regular Cetacean auth provider identities.
const ProviderName = "mcp-oauth"

// Identify returns the identity a bearer token carries, so a resource server
// needs no knowledge of the claim set. The error is VerifyAccessToken's own,
// unwrapped: callers distinguish the failure modes with errors.Is to decide
// whether a token was ours at all.
func (s *Server) Identify(token string) (*auth.Identity, error) {
	claims, err := s.VerifyAccessToken(token)
	if err != nil {
		return nil, err
	}

	return &auth.Identity{
		Subject:  claims.Subject,
		Groups:   claims.Groups,
		Provider: ProviderName,
	}, nil
}
