package oauth

import (
	"github.com/radiergummi/cetacean/internal/auth"
)

// ProviderName identifies identities derived from a bearer token this server
// issued. Stamped on auth.Identity.Provider so downstream code can tell them
// from identities an upstream auth provider established.
const ProviderName = "oauth"

// Identify returns the identity a bearer token carries, so a resource server
// needs no knowledge of the claim set. resource is the identifier of the caller's
// own resource: a token minted for a different one is refused with
// ErrAudienceMismatch rather than accepted here. The error is the verifier's own,
// unwrapped: callers distinguish the failure modes with errors.Is to decide
// whether a token was ours at all.
func (s *Server) Identify(token, resource string) (*auth.Identity, error) {
	claims, err := s.tokenIssuer.VerifyAccessToken(token, resource)
	if err != nil {
		return nil, err
	}

	return &auth.Identity{
		Subject:     claims.Subject,
		DisplayName: claims.DisplayName,
		Email:       claims.Email,
		Groups:      claims.Groups,
		Provider:    ProviderName,
	}, nil
}

// ResourceIdentifier is the identifier of the resource at path, which a resource
// server passes back to Identify and WriteUnauthorized. Deriving it here keeps
// the issuer and base path in one place rather than having each consumer
// reassemble them and hope they agree.
func (s *Server) ResourceIdentifier(path string) string {
	return Resource{Path: trimmedPath(path)}.identifier(s.cfg.issuerID())
}
