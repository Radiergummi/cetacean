package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

// AuthError is an authentication error that carries a WWW-Authenticate
// header value per RFC 9110. The challenge is what earns a 401; Code names a
// registry entry to answer with instead of the default refusal, and makes Msg
// the client-visible detail rather than only a log line.
type AuthError struct {
	Msg             string
	WWWAuthenticate string
	Code            string
}

func (e *AuthError) Error() string { return e.Msg }

// writeAuthFailure answers a failed Authenticate, for both callers that answer
// one — MCP's bypass falls through to its own bearer challenge. RFC 9110
// §15.5.2 admits no 401 without a challenge, so a refusal that produced none
// is 403 (AUT006); a Code names a refusal of its own.
func writeAuthFailure(w http.ResponseWriter, r *http.Request, err error, extra ...string) {
	code, detail := "AUT006", "authentication refused"

	var named bool

	if authErr, ok := errors.AsType[*AuthError](err); ok {
		if authErr.WWWAuthenticate != "" {
			w.Header().Set("WWW-Authenticate", authErr.WWWAuthenticate)
			code, detail = "AUT001", "authentication required"
		}
		if authErr.Code != "" {
			code, detail, named = authErr.Code, authErr.Msg, true
		}
	}

	// A challenge the caller can act on separates 401 from 403, so one added here
	// promotes a bare refusal — never one that named its own code, which the
	// challenge informs but cannot answer. Added as its own field line rather
	// than appended: a first scheme taking no parameters parses ambiguously.
	for _, challenge := range extra {
		if challenge == "" {
			continue
		}

		w.Header().Add("WWW-Authenticate", challenge)
		if !named && code == "AUT006" {
			code, detail = "AUT001", "authentication required"
		}
	}

	problem.Write(w, r, code, detail)
}

// Middleware returns HTTP middleware that authenticates requests using the
// given provider; exempt paths bypass authentication entirely. A token this
// deployment issued is consulted first, so an explicit credential outranks an
// ambient session cookie. tokens may be its zero value (no authorization server).
func Middleware(provider Provider, tokens APITokens) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// A token of ours settles the request on its own; anything else —
			// including a token somebody else issued — goes to the provider.
			id, err, fromToken := tokens.authenticate(r)
			if !fromToken {
				id, err = provider.Authenticate(w, r)
			}

			if err != nil {
				slog.WarnContext(r.Context(), "authentication failed",
					"path", r.URL.Path,
					"error", err,
				)
				// A request with no credential gets the resource's own challenge too, so a
				// client can follow RFC 9728 cold: read the 401, follow resource_metadata.
				// Not on /oauth/*, which refuses a token this server issued.
				resourceChallenge := ""
				if ExtractBearerToken(r) == "" && isProtectedResource(r.URL.Path) {
					resourceChallenge = tokens.Challenge("")
				}

				writeAuthFailure(w, r, err, resourceChallenge)
				return
			}

			// Provider handled the response (e.g. redirect).
			if id == nil {
				return
			}

			ctx, cancel := ContextUntilExpiry(r.Context(), id)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isProtectedResource excludes the authorization server's own endpoints from the
// bearer challenge. /oauth/authorize is the only one the middleware reaches, and
// it refuses a token this server issued: offering one there sends a client for a
// credential that answers 403.
func isProtectedResource(path string) bool {
	return !strings.HasPrefix(path, "/oauth/")
}

// isExempt returns true for paths that should skip authentication.
func isExempt(path string) bool {
	switch {
	case path == "/-/resync":
		// The one /-/ route that does work rather than reporting state: it
		// sweeps the whole Docker API, unbounded and unthrottled, so an
		// uncredentialed caller who can reach the port could amplify one
		// cheap request into a cluster enumeration at will.
		return false
	case strings.HasPrefix(path, "/-/"):
		return true
	case path == "/api" || strings.HasPrefix(path, "/api/"):
		return true
	case strings.HasPrefix(path, "/assets/"):
		return true
	case path == "/auth" || strings.HasPrefix(path, "/auth/"):
		return true
	case path == "/mcp":
		// MCP authenticates with its own bearer-token middleware (OAuth 2.1).
		return true
	case strings.HasPrefix(path, "/.well-known/"):
		// RFC 8414 / RFC 9728 discovery documents are unauthenticated by spec.
		return true
	case path == "/oauth/token" ||
		path == "/oauth/revoke" ||
		path == "/oauth/register" ||
		path == "/oauth/jwks":
		// Machine endpoints: the grants carry their own proof in the body, and
		// the key set is public. Consent (/oauth/authorize) is deliberately
		// absent — the user must be authenticated before granting access.
		return true
	default:
		return false
	}
}
