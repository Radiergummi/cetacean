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

	// Our own origin, for the fallback path a pre-2023 browser takes: with no
	// Sec-Fetch-Site the stdlib compares Origin against r.Host, which is the
	// internal name behind a proxy that rewrites Host. Naming it here also
	// spares an operator listing their own origin in server.cors.origins,
	// which would additionally switch on CORS reflection.
	if publicURL != "" {
		trust("server.public_url", publicURL)
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
				writeErrorCode(w, r, "CSR001", err.Error())
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// carriesItsOwnProof names the endpoints that authenticate from the request
// body, with no ambient credential for a cross-origin page to borrow — so the
// protection defends nothing and only breaks browser-based MCP clients.
// /oauth/authorize is excluded: consent runs under the session cookie.
func carriesItsOwnProof(path string) bool {
	// Spelled out because internal/api does not import internal/mcp.
	switch path {
	case "/mcp", "/oauth/token", "/oauth/revoke", "/oauth/register":
		return true
	default:
		return false
	}
}
