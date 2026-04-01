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
// header value per RFC 9110. Providers return this to advertise their
// authentication scheme in the 401 response.
type AuthError struct {
	Msg             string
	WWWAuthenticate string
}

func (e *AuthError) Error() string { return e.Msg }

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
				var authErr *AuthError
				if errors.As(err, &authErr) && authErr.WWWAuthenticate != "" {
					w.Header().Set("WWW-Authenticate", authErr.WWWAuthenticate)
				}
				writeError(w, r, http.StatusUnauthorized, "AUT001", "authentication required")
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
	default:
		return false
	}
}
