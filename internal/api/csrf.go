package api

import (
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

// crossOriginProtection refuses non-safe cross-origin browser requests. Not
// merely defence in depth: Tailscale, mTLS and proxy headers all authenticate
// a request carrying no cookie, so SameSite covers none of them. The CORS
// allowlist mirrors in as trusted origins; a wildcard cannot, and main.go warns.
func crossOriginProtection(cfg *CORSConfig, publicURL string) Constructor {
	protection := http.NewCrossOriginProtection()

	trust := func(setting, origin string) {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			slog.Warn(
				"ignoring an unusable origin",
				"setting", setting,
				"origin", origin,
				"error", err,
			)
		}
	}

	for _, origin := range trustedOrigins(cfg, publicURL) {
		trust("server.cors.origins or server.public_url", origin)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if carriesItsOwnProof(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Check rather than Handler: the stdlib's own deny path writes a
			// plain-text 403, and its error names which branch refused the
			// request, which is what an operator needs to fix the deployment.
			if err := protection.Check(r); err != nil {
				problem.Write(w, r, "CSR001", err.Error())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// trustedOrigins is every origin a browser request may come from besides the
// request's own host, shared by the CSRF check and the WebSocket upgrade. Our
// public URL covers a pre-2023 browser, which sends no Sec-Fetch-Site from
// behind a proxy that rewrites Host, without switching on CORS reflection.
func trustedOrigins(cfg *CORSConfig, publicURL string) []string {
	var origins []string
	if cfg.Enabled() && !cfg.Wildcard() {
		origins = append(origins, cfg.AllowedOrigins...)
	}
	if publicURL != "" {
		origins = append(origins, publicURL)
	}

	return origins
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
