package auth

import (
	"errors"
	"net/http"
)

// ErrForeignToken marks a bearer token this deployment did not issue. In oidc
// mode an IdP ID token shares the header, so a foreign token must reach the
// provider while one of ours that failed verification must not. A verifier
// that forgets to mark a foreign token fails closed.
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

// Challenge is the WWW-Authenticate value naming where a token for this resource
// comes from, or "" when no verifier is configured. errorCode is empty for a
// request that carried no credential, which RFC 6750 §3.1 asks for.
func (t APITokens) Challenge(errorCode string) string {
	if t.Verifier == nil {
		return ""
	}

	return t.Verifier.UnauthorizedHeader(t.Resource, errorCode)
}

// authenticate resolves an Authorization: Bearer credential against the
// verifier. ok is false when the request belongs to the provider instead. A
// refusal is an AuthError so the middleware answers 401 with WWW-Authenticate,
// never the provider's login redirect, which a token-holding client can't follow.
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
			WWWAuthenticate: t.Challenge("invalid_token"),
		}, true
	}
}
