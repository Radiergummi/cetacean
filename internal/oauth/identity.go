package oauth

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// ProviderName identifies identities derived from a bearer token this server
// issued. Stamped on auth.Identity.Provider so downstream code can tell them
// from identities an upstream auth provider established.
const ProviderName = "oauth"

// Identify returns the identity a bearer token carries, so a resource server
// needs no knowledge of the claim set. The error is the verifier's own,
// unwrapped: callers distinguish the failure modes with errors.Is to decide
// whether a token was ours at all.
func (s *Server) Identify(token string) (*auth.Identity, error) {
	claims, err := s.tokenIssuer.VerifyAccessToken(token)
	if err != nil {
		return nil, err
	}

	return &auth.Identity{
		Subject:  claims.Subject,
		Groups:   claims.Groups,
		Provider: ProviderName,
	}, nil
}
