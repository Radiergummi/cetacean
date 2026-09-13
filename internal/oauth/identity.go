package oauth

import (
	"errors"
	"fmt"
	"slices"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// ProviderName identifies identities derived from a bearer token this server
// issued. Stamped on auth.Identity.Provider so downstream code can tell them
// from identities an upstream auth provider established.
const ProviderName = "oauth"

// Identify returns the identity a bearer token carries, so a resource server
// needs no knowledge of the claim set. resource is the identifier of the caller's
// own resource: a token minted for a different one is refused with
// ErrAudienceMismatch rather than accepted here.
//
// A token this server did not issue is reported as auth.ErrForeignToken, so a
// caller sharing the Authorization header with an upstream provider can hand it
// on instead of refusing it. The discriminator is the issuer, not the outcome of
// verification: a token under another `iss`, and one that is not a JWT at all —
// which is what an opaque provider token looks like — are somebody else's.
// Everything else is a token claiming to be ours that failed to prove it, and is
// final, because falling through would let a forged token be judged on weaker
// evidence. The verifier's own error is wrapped alongside, so a caller that wants
// the specific failure can still reach it with errors.Is.
func (s *Server) Identify(token, resource string) (*auth.Identity, error) {
	claims, err := s.tokenIssuer.VerifyAccessToken(token, resource)
	if err != nil {
		if errors.Is(err, ErrIssuerMismatch) || errors.Is(err, ErrMalformedToken) {
			return nil, fmt.Errorf("%w: %w", auth.ErrForeignToken, err)
		}

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
// server passes back to Identify and UnauthorizedHeader. Deriving it here keeps
// the issuer and base path in one place rather than having each consumer
// reassemble them and hope they agree.
func (s *Server) ResourceIdentifier(path string) string {
	return s.cfg.issuerID() + config.NormalizeBasePath(path)
}

// ResourceIdentifiers lists every identifier this server issues tokens for, so a
// caller can report the set without reassembling it.
func (s *Server) ResourceIdentifiers() []string {
	return slices.Clone(s.resources.identifiers)
}
