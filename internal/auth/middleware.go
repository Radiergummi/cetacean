package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

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
