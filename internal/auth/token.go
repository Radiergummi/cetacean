package auth

import (
	"log/slog"
	"net/http"
)

// TokenVerifier verifies a bearer token this deployment's own authorization
// server issued. The interface is declared here and satisfied elsewhere:
// internal/api must not import the authorization server, and a verifier living
// in that package would make its import of this one a cycle.
type TokenVerifier interface {
	// Identify returns the identity the token carries, or an error classifying
	// why it was refused. resource is the audience the token must name.
	Identify(token, resource string) (*Identity, error)

	// Foreign reports whether an Identify error means the token was issued by
	// somebody else, so the request belongs to the upstream provider instead.
	Foreign(err error) bool

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

// authenticateBearer resolves an Authorization: Bearer credential against the
// verifier. handled is false when the request should continue to the provider —
// no bearer token, no verifier, or a token that is somebody else's. When it is
// true the caller must stop: either id is the authenticated identity, or the
// refusal has already been written.
//
// Two bearer dialects share the one header — ours, and in oidc mode an
// IdP-issued ID token — so a token that is not ours must reach the provider
// while one of ours that fails verification must not. Getting that backwards
// either breaks bearer auth against the IdP or opens a fall-through for forged
// tokens.
func (t APITokens) authenticateBearer(
	w http.ResponseWriter,
	r *http.Request,
) (id *Identity, handled bool) {
	if t.Verifier == nil {
		return nil, false
	}

	token := ExtractBearerToken(r)
	if token == "" {
		return nil, false
	}

	id, err := t.Verifier.Identify(token, t.Resource)
	if err == nil {
		return id, true
	}

	if t.Verifier.Foreign(err) {
		return nil, false
	}

	// Refused for cause. The response is a 401 with WWW-Authenticate and never
	// the provider's redirect-to-login branch: a client holding a token needs to
	// be told the token is bad, not handed a login page it cannot render.
	slog.Warn("bearer token refused",
		"path", r.URL.Path,
		"error", err,
	)
	w.Header().Set("WWW-Authenticate", t.Verifier.UnauthorizedHeader(t.Resource, "invalid_token"))
	writeError(w, r, http.StatusUnauthorized, "AUT001", "authentication required")

	return nil, true
}
