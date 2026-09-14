package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
)

// ErrorWriter writes a structured error response. The code and detail
// parameters map to the API error registry (e.g. "AUT001"). This type
// decouples the auth package from the API error-response implementation,
// avoiding a circular dependency.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, code, detail string)

// globalErrorWriter is set once during initialization via SetErrorWriter.
var globalErrorWriter atomic.Pointer[ErrorWriter]

// SetErrorWriter registers the structured error writer used by the auth
// middleware and providers. Must be called during initialization, before
// any requests are served.
func SetErrorWriter(ew ErrorWriter) {
	globalErrorWriter.Store(&ew)
}

// writeError writes a structured error response if an ErrorWriter has been
// registered, otherwise falls back to http.Error.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	if ew := globalErrorWriter.Load(); ew != nil {
		(*ew)(w, r, code, detail)
		return
	}

	http.Error(w, detail, status)
}

// AuthError is an authentication error that carries a WWW-Authenticate
// header value per RFC 9110. The challenge is what earns a 401; Code names a
// registry entry to answer with instead of the default refusal, and makes Msg
// the client-visible detail rather than only a log line.
type AuthError struct {
	Msg             string
	WWWAuthenticate string
	Status          int
	Code            string
}

func (e *AuthError) Error() string { return e.Msg }

// writeAuthFailure answers a failed Authenticate, for both callers that answer
// one — MCP's bypass falls through to its own bearer challenge. RFC 9110
// §15.5.2 admits no 401 without a challenge, so a refusal that produced none
// is 403; a Code names a refusal of its own. Status is the fallback's only.
func writeAuthFailure(w http.ResponseWriter, r *http.Request, err error) {
	status, code, detail := http.StatusForbidden, "AUT006", "authentication refused"

	var authErr *AuthError
	if errors.As(err, &authErr) {
		if authErr.WWWAuthenticate != "" {
			w.Header().Set("WWW-Authenticate", authErr.WWWAuthenticate)
			status, code, detail = http.StatusUnauthorized, "AUT001", "authentication required"
		}
		if authErr.Code != "" {
			code, detail = authErr.Code, authErr.Msg
			if authErr.Status != 0 {
				status = authErr.Status
			}
		}
	}

	writeError(w, r, status, code, detail)
}

// Middleware returns HTTP middleware that authenticates requests using the
// given provider. Exempt paths (meta endpoints, API docs, static assets,
// auth callbacks) bypass authentication entirely.
func Middleware(provider Provider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			id, err := provider.Authenticate(w, r)
			if err != nil {
				slog.Warn("authentication failed",
					"path", r.URL.Path,
					"error", err,
				)
				writeAuthFailure(w, r, err)
				return
			}

			// Provider handled the response (e.g. redirect).
			if id == nil {
				return
			}

			ctx := ContextWithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isExempt returns true for paths that should skip authentication.
func isExempt(path string) bool {
	switch {
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
