package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// ProviderName identifies identities derived from a bearer token this server
// issued. Stamped on auth.Identity.Provider so downstream code can tell them
// from identities an upstream auth provider established.
const ProviderName = auth.ProviderToken

// Identify returns the identity a bearer token carries for resource, refusing a
// token minted for another with ErrAudienceMismatch. A non-JWT or another `iss`
// is auth.ErrForeignToken, for the caller to hand on; any other failure is final,
// or a forged token would be judged on weaker evidence. Errors wrap the verifier's.
func (s *Server) Identify(token, resource string) (*auth.Identity, error) {
	claims, err := s.tokenIssuer.VerifyAccessToken(token, resource)
	if err != nil {
		if errors.Is(err, ErrIssuerMismatch) || errors.Is(err, ErrMalformedToken) {
			return nil, fmt.Errorf("%w: %w", auth.ErrForeignToken, err)
		}

		return nil, err
	}

	id := &auth.Identity{
		Subject:     claims.Subject,
		DisplayName: claims.DisplayName,
		Email:       claims.Email,
		Groups:      claims.Groups,
		Provider:    ProviderName,
		ExpiresAt:   claims.ExpiresAt,
		ClientID:    claims.ClientID,
	}

	var grants any
	if len(claims.Grants) > 0 && json.Unmarshal(claims.Grants, &grants) == nil {
		id.Raw = map[string]any{auth.RawTokenGrants: grants}
	}

	return id, nil
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
