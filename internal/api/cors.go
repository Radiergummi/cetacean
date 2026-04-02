package api

import (
	"net/http"
	"strings"
)

// CORSConfig holds CORS middleware settings. A nil or zero-value
// config disables CORS entirely (no headers emitted).
type CORSConfig struct {
	// AllowedOrigins is the list of origins permitted to make
	// cross-origin requests. Use ["*"] to allow any origin (not
	// recommended with credentialed requests).
	AllowedOrigins []string
}

// Enabled reports whether CORS is configured.
func (c *CORSConfig) Enabled() bool {
	return c != nil && len(c.AllowedOrigins) > 0
}

// exposedHeaders lists response headers the browser should expose to
// JavaScript in cross-origin requests.
var exposedHeaders = strings.Join([]string{
	"ETag",
	"Link",
	"Allow",
	"Location",
	"Retry-After",
	"Request-Id",
}, ", ")

// allowedMethods lists methods the API supports.
var allowedMethods = strings.Join([]string{
	"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS",
}, ", ")

// allowedHeaders lists request headers the API accepts in cross-origin
// requests.
var allowedHeaders = strings.Join([]string{
	"Accept",
	"Authorization",
	"Content-Type",
	"If-None-Match",
	"Request-Id",
}, ", ")

func cors(cfg *CORSConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled() {
		return func(next http.Handler) http.Handler { return next }
	}

	wildcard := len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*"

	// Build a set for O(1) origin lookup (skip for wildcard).
	var allowed map[string]struct{}
	if !wildcard {
		allowed = make(map[string]struct{}, len(cfg.AllowedOrigins))
		for _, o := range cfg.AllowedOrigins {
			allowed[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !wildcard {
				if _, ok := allowed[origin]; !ok {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Reflect the request origin (required when credentials
			// are in use; safe for wildcard mode too).
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", exposedHeaders)
			w.Header().Add("Vary", "Origin")

			// Preflight
			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
