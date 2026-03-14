package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// AuthError is an authentication error that carries a WWW-Authenticate
// header value per RFC 9110. Providers return this to advertise their
// authentication scheme in the 401 response.
type AuthError struct {
	Msg            string
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
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
