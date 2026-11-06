package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/config"
)

// newCrossOriginProtection builds the one rule writes and the terminal upgrade
// share. The CORS allowlist mirrors in as trusted origins; a wildcard cannot, and
// main.go warns. Our public URL covers a pre-2023 browser, which sends no
// Sec-Fetch-Site from behind a proxy that rewrites Host.
func newCrossOriginProtection(cfg *CORSConfig, publicURL string) *http.CrossOriginProtection {
	protection := http.NewCrossOriginProtection()

	var origins []string
	if cfg.Enabled() && !cfg.Wildcard() {
		origins = append(origins, cfg.AllowedOrigins...)
	}
	if publicURL != "" {
		origins = append(origins, publicURL)
	}

	for _, origin := range origins {
		if err := protection.AddTrustedOrigin(config.SerialiseOrigin(origin)); err != nil {
			slog.Warn(
				"ignoring an unusable origin",
				"setting", "server.cors.origins or server.public_url",
				"origin", origin,
				"error", err,
			)
		}
	}

	return protection
}

// crossOriginProtection refuses non-safe cross-origin browser requests. Not
// merely defence in depth: Tailscale, mTLS and proxy headers all authenticate
// a request carrying no cookie, so SameSite covers none of them.
func crossOriginProtection(protection *http.CrossOriginProtection) Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if carriesItsOwnProof(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Check rather than Handler: the stdlib's own deny path writes a
			// plain-text 403, and its error names which branch refused the
			// request, which is what an operator needs to fix the deployment.
			if err := protection.Check(foldOrigin(r)); err != nil {
				problem.Write(w, r, "CSR001", err.Error())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// foldOrigin is r as Check should read it: Check compares bytes, and the scheme
// and host of an origin are case-insensitive.
func foldOrigin(r *http.Request) *http.Request {
	origin := r.Header.Get("Origin")
	lowerOrigin, lowerHost := strings.ToLower(origin), strings.ToLower(r.Host)
	if origin == "" || (origin == lowerOrigin && r.Host == lowerHost) {
		return r
	}

	folded := r.WithContext(r.Context())
	folded.Host = lowerHost
	folded.Header = r.Header.Clone()
	folded.Header.Set("Origin", lowerOrigin)

	return folded
}

// carriesItsOwnProof names endpoints that authenticate from the request body
// rather than a cookie, so there is no ambient credential to protect.
// /oauth/authorize is absent: consent authenticates through the middleware, and
// refuses a credential this deployment issued rather than an upstream one.
func carriesItsOwnProof(path string) bool {
	// Spelled out because internal/api imports neither internal/mcp nor
	// internal/oauth, and these paths come from both.
	switch path {
	case "/mcp", "/oauth/token", "/oauth/revoke", "/oauth/register":
		return true
	default:
		return false
	}
}
