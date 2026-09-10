package api

import (
	"log/slog"
	"net/http"
)

// crossOriginProtection refuses non-safe cross-origin browser requests. Not
// only defence in depth: Tailscale, mTLS and trusted-proxy headers all
// authenticate a browser request that carries no cookie at all, so SameSite
// covers none of them.
//
// The CORS allowlist is mirrored in as trusted origins — an origin CORS admits
// must not then be refused here. A wildcard cannot be ("*" is not an origin);
// main.go warns about that at startup.
func crossOriginProtection(cfg *CORSConfig) Constructor {
	protection := http.NewCrossOriginProtection()

	if cfg.Enabled() && !cfg.Wildcard() {
		for _, origin := range cfg.AllowedOrigins {
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
