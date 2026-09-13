package auth

import (
	"errors"
	"net/http"
)

// ErrForeignToken marks a bearer token that this deployment's authorization
// server did not issue. Two bearer dialects share the one header — ours, and in
// oidc mode an IdP-issued ID token — so a token that is not ours must reach the
// provider while one of ours that failed verification must not. Getting that
// backwards either breaks bearer auth against the IdP or opens a fall-through
// for forged tokens.
//
// The classification travels with the error rather than being asked for
// separately, so the two cannot disagree. A verifier that forgets to mark a
// foreign token fails closed: the token is treated as ours and refused.
var ErrForeignToken = errors.New("auth: token was not issued by this deployment")

// TokenVerifier verifies a bearer token this deployment's own authorization
// server issued. The interface is declared here and satisfied elsewhere:
// internal/api must not import the authorization server, and a verifier living
// in that package would make its import of this one a cycle.
type TokenVerifier interface {
	// Identify returns the identity the token carries. resource is the audience
	// the token must name. An error wrapping ErrForeignToken means the token was
	// somebody else's; any other means it claimed to be ours and could not prove
	// it.
	Identify(token, resource string) (*Identity, error)

	// UnauthorizedHeader is the WWW-Authenticate value for a refusal, naming
	// where a client obtains a token for resource.
	UnauthorizedHeader(resource, errorCode string) string
}

// APITokens is the optional bearer-token path through the middleware: a verifier
// and the resource identifier a token must be audienced for. A nil Verifier
// leaves every request to the provider, which is the deployment that runs no
// authorization server.
type APITokens struct {
	Verifier TokenVerifier
	Resource string
}

// authenticate resolves an Authorization: Bearer credential against the
// verifier. ok is false when the request belongs to the provider instead — no
// bearer token, no verifier, or a token that is somebody else's.
//
// A refusal is returned as an AuthError so the middleware answers it the way it
// answers every other one: a 401 carrying WWW-Authenticate, never the provider's
// redirect-to-login branch, which a client holding a token cannot follow.
func (t APITokens) authenticate(r *http.Request) (id *Identity, err error, ok bool) {
	if t.Verifier == nil {
		return nil, nil, false
	}

	token := ExtractBearerToken(r)
	if token == "" {
		return nil, nil, false
	}

	id, err = t.Verifier.Identify(token, t.Resource)
	switch {
	case err == nil:
		return id, nil, true
	case errors.Is(err, ErrForeignToken):
		return nil, nil, false
	default:
		return nil, &AuthError{
			Msg:             "invalid bearer token",
			WWWAuthenticate: t.Verifier.UnauthorizedHeader(t.Resource, "invalid_token"),
		}, true
	}
}
