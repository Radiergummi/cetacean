package api

import (
	"log/slog"
	"net/http"
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

	if cfg.Enabled() && !cfg.Wildcard() {
		for _, origin := range cfg.AllowedOrigins {
			trust("server.cors.origins", origin)
		}
	}

	// Our own origin, for the fallback a pre-2023 browser takes: with no
	// Sec-Fetch-Site the stdlib compares Origin against r.Host, the internal
	// name behind a proxy that rewrites it. Naming it here also spares
	// listing it in server.cors.origins, which would switch on CORS reflection.
	if publicURL != "" {
		trust("server.public_url", publicURL)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check rather than Handler: the stdlib's own deny path writes a
			// plain-text 403, and its error names which branch refused the
			// request, which is what an operator needs to fix the deployment.
			if err := protection.Check(r); err != nil {
				writeErrorCode(w, r, "CSR001", err.Error())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
