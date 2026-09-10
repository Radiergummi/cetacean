package api

import (
	"log/slog"
	"net/http"
)

// crossOriginProtection refuses non-safe cross-origin browser requests, using
// the stdlib's Fetch Metadata check (Go 1.25+). Session cookies are already
// SameSite=Lax, which covers the cookie-bearing modes; this also covers the
// modes whose credentials are ambient rather than cookie-borne — Tailscale,
// mTLS and trusted-proxy headers all authenticate a browser request that
// carries no cookie at all.
//
// Safe methods pass untouched, as do requests carrying neither Sec-Fetch-Site
// nor Origin — curl, the MCP transport, and every other non-browser client.
//
// The CORS allowlist is mirrored in as trusted origins, because the two
// configurations describe one trust decision: an origin CORS admits must not
// then be refused here. A wildcard cannot be mirrored — "*" is not an origin,
// and reading it as "trust everyone" would disable the protection through a
// setting that says nothing about CSRF — so it warns instead.
func crossOriginProtection(cfg *CORSConfig) func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()

	if cfg.Enabled() {
		for _, origin := range cfg.AllowedOrigins {
			if origin == "*" {
				slog.Warn(
					"server.cors.origins is a wildcard, which cannot be a trusted "+
						"origin for cross-origin protection: cross-origin writes from "+
						"a browser will be refused",
					"suggestion", "list the origins explicitly",
				)
				continue
			}

			if err := protection.AddTrustedOrigin(origin); err != nil {
				slog.Warn(
					"ignoring an unusable entry in server.cors.origins",
					"origin", origin,
					"error", err,
				)
			}
		}
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
