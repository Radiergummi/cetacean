package contract

// The excuse lists are the honest record of what this package does not hold the
// product to. Every entry carries a reason a reader can evaluate, and "not yet"
// is not one — an entry that means "we have not got round to it" is a coverage
// gap wearing a disguise, and should be a defect on the list instead.
//
// A stale entry is worse than a missing one: it hides the next real drift. The
// drift tests therefore fail on an excuse that is no longer needed, not only on
// one that is missing.

// excusedUndocumented holds routes that are registered but deliberately absent
// from api/openapi.yaml. Keys are Route.String(), e.g. "GET /nodes".
var excusedUndocumented = map[string]string{
	// SPA fallback: also serves /assets/* (which never gets its own mux
	// pattern; static assets fall through this same catch-all), verified in
	// internal/api/router.go's "SPA fallback (must be last)" registration.
	"/": "serves the embedded SPA and /assets/*; not an API surface",

	// The canonical URL is /-/sbom.cdx.json; router.go registers the mux
	// pattern without the extension because the content-negotiation
	// middleware strips a recognized .json/.html suffix before dispatch (see
	// the comment directly above the registration in router.go). This is the
	// same suffix-stripping every negotiated endpoint gets, not a mismatch:
	// the spec's counterpart is excused in excusedUnregistered below.
	"GET /-/sbom.cdx": "canonical URL is /-/sbom.cdx.json; the negotiate " +
		"middleware strips the .json suffix before dispatch, so the mux " +
		"pattern omits it",

	// DEFECT: HandleResync is a real, unconditionally-wired (main.go always
	// sets Resyncer) operational endpoint with its own doc comment
	// explaining what it does and why it needs no operations-level gate, but
	// api/openapi.yaml never grew an entry for it.
	"POST /-/resync": "DEFECT: real endpoint (manual cache resync), " +
		"unconditionally wired from main.go, but api/openapi.yaml does not " +
		"document it — see the campaign defect list",

	// DEFECT: an alias of GET /plugins (identical handler) used by the
	// /swarm dashboard page; the spec documents /plugins but never grew an
	// entry for this alias path.
	"GET /swarm/plugins": "DEFECT: alias of GET /plugins (same handler) " +
		"for the swarm page; api/openapi.yaml documents /plugins but not " +
		"this alias — see the campaign defect list",

	// Both routes are 410 Gone forever — verified against router.go, which
	// registers them purely to answer API012 for the two projections removed
	// in 0.13.0. There is no live operation left to document.
	"GET /topology/networks": "removed in 0.13.0; router.go registers this " +
		"only to answer 410 Gone with API012 — no live operation to document",
	"GET /topology/placement": "removed in 0.13.0; router.go registers " +
		"this only to answer 410 Gone with API012 — no live operation to document",

	// Opt-in Go stdlib debugging surface (CETACEAN_PPROF=true), not part of
	// Cetacean's documented API.
	"/debug/pprof/":        "opt-in pprof debug endpoint (CETACEAN_PPROF=true); Go stdlib debug surface, not an API operation",
	"/debug/pprof/cmdline": "opt-in pprof debug endpoint (CETACEAN_PPROF=true); Go stdlib debug surface, not an API operation",
	"/debug/pprof/profile": "opt-in pprof debug endpoint (CETACEAN_PPROF=true); Go stdlib debug surface, not an API operation",
	"/debug/pprof/symbol":  "opt-in pprof debug endpoint (CETACEAN_PPROF=true); Go stdlib debug surface, not an API operation",
	"/debug/pprof/trace":   "opt-in pprof debug endpoint (CETACEAN_PPROF=true); Go stdlib debug surface, not an API operation",

	// The embedded MCP JSON-RPC server (CETACEAN_MCP=true) speaks protocol
	// 2026-07-28 over its own transport; it is documented in docs/mcp.md, not
	// modeled as an OpenAPI operation.
	"/mcp": "MCP JSON-RPC endpoint (CETACEAN_MCP=true); a separate protocol documented in docs/mcp.md, not an OpenAPI operation",

	// Serves the embedded Scalar JS bundle backing the HTML playground at
	// GET /api; a browser asset, not a JSON API operation (the same
	// treatment /assets/* gets under "/").
	"GET /api/scalar.js": "serves the embedded Scalar JS bundle for the API playground; a browser asset, not a JSON API operation",
}

// excusedUnregistered holds operations documented in api/openapi.yaml that
// internal/api/router.go does not register. Keys are Operation.String().
var excusedUnregistered = map[string]string{
	// Counterpart of "GET /-/sbom.cdx" above: same endpoint, reached through
	// content-negotiation suffix stripping rather than a second mux pattern.
	"GET /-/sbom.cdx.json": "canonical URL for GET /-/sbom.cdx; router.go " +
		"registers the mux pattern without the .json suffix, which the " +
		"negotiate middleware strips before dispatch",

	// Registered by auth.OIDCProvider.RegisterRoutes (internal/auth/oidc.go),
	// not internal/api/router.go — verified: only the OIDC provider
	// implements a non-empty RegisterRoutes; None/Tailscale/Cert/Headers are
	// all no-ops.
	"GET /auth/login":    "registered by auth.OIDCProvider.RegisterRoutes, not router.go",
	"GET /auth/callback": "registered by auth.OIDCProvider.RegisterRoutes, not router.go",
	"POST /auth/logout":  "registered by auth.OIDCProvider.RegisterRoutes, not router.go",
}
