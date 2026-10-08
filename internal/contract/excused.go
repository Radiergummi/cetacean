package contract

// The excuse lists record what this package does not hold the product to.
// Every entry carries a reason a reader can evaluate; "not yet" is not one.
// The drift tests fail on an excuse no longer needed, not only a missing one.

// excusedUndocumented holds routes that are registered but deliberately absent
// from api/openapi.yaml. Keys are Route.String(), e.g. "GET /nodes".
var excusedUndocumented = map[string]string{
	// SPA fallback: also serves /assets/* (which never gets its own mux
	// pattern; static assets fall through this same catch-all), verified in
	// internal/api/router.go's "SPA fallback (must be last)" registration.
	"/": "serves the embedded SPA and /assets/*; not an API surface",

	// The canonical URL is /-/sbom.cdx.json; router.go registers the pattern
	// without the extension because negotiate strips a recognized suffix
	// before dispatch. Not a mismatch — the spec's counterpart is excused in
	// excusedUnregistered below.
	"GET /-/sbom.cdx": "canonical URL is /-/sbom.cdx.json; the negotiate " +
		"middleware strips the .json suffix before dispatch, so the mux " +
		"pattern omits it",

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

// excusedUncovered holds routes no sweep in this package exercises, keyed by
// Route.String(). It lives here because `unused` fails a package variable
// nothing reads. gosec flags the "secrets" substring in the keys.
//
//nolint:gosec // G101
var excusedUncovered = map[string]string{
	// SPA fallback: also serves /assets/* (which never gets its own mux
	// pattern; static assets fall through this same catch-all), same as its
	// treatment in excusedUndocumented above.
	"/": "serves the embedded SPA",

	// GET /events is deliberately not excused: the handler only blocks for an
	// SSE Accept header, and every sweep here asks for application/json, which
	// returns immediately.

	// Prometheus-backed: each hard-errors (MTR001) when h.promClient or
	// metricsProxy is nil, which it is in this world. The four that read
	// h.promClient and degrade gracefully are genuine gaps below, not excused.
	"GET /cluster/metrics":       "needs a Prometheus; covered by the e2e harness's 503 case and deferred to phase two",
	"GET /metrics":               "needs a Prometheus; covered by the e2e harness's 503 case and deferred to phase two",
	"GET /metrics/labels":        "needs a Prometheus; covered by the e2e harness's 503 case and deferred to phase two",
	"GET /metrics/labels/{name}": "needs a Prometheus; covered by the e2e harness's 503 case and deferred to phase two",

	// Daemon-backed: each reaches a Docker client world.go leaves nil, or, for
	// POST /-/resync, a Resyncer it never sets — so router.go does not even
	// register that route here. The /plugins* handlers call their nil client
	// with no nil check, so driving them would panic the test binary.
	"DELETE /plugins/{name}":         "reads the Docker daemon directly; covered by the e2e harness",
	"GET /disk-usage":                "reads the Docker daemon directly; covered by the e2e harness",
	"GET /plugins":                   "reads the Docker daemon directly; covered by the e2e harness",
	"GET /plugins/{name}":            "reads the Docker daemon directly; covered by the e2e harness",
	"GET /services/{id}/logs":        "reads the Docker daemon directly; covered by the e2e harness",
	"GET /swarm":                     "reads the Docker daemon directly; covered by the e2e harness",
	"GET /swarm/plugins":             "reads the Docker daemon directly; covered by the e2e harness",
	"GET /swarm/unlock-key":          "reads the Docker daemon directly; covered by the e2e harness",
	"GET /tasks/{id}/logs":           "reads the Docker daemon directly; covered by the e2e harness",
	"PATCH /plugins/{name}/settings": "reads the Docker daemon directly; covered by the e2e harness",
	"POST /-/resync":                 "reads the Docker daemon directly; covered by the e2e harness",
	"POST /plugins":                  "reads the Docker daemon directly; covered by the e2e harness",
	"POST /plugins/privileges":       "reads the Docker daemon directly; covered by the e2e harness",
	"POST /plugins/{name}/disable":   "reads the Docker daemon directly; covered by the e2e harness",
	"POST /plugins/{name}/enable":    "reads the Docker daemon directly; covered by the e2e harness",
	"POST /plugins/{name}/upgrade":   "reads the Docker daemon directly; covered by the e2e harness",

	// Write endpoints: every one mutates through h.systemClient, h.pluginClient
	// or the DockerWriteClient composite, all of which world.go leaves nil.
	"DELETE /configs/{id}":                  "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /networks/{id}":                 "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /nodes/{id}":                    "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /secrets/{id}":                  "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /services/{id}":                 "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /stacks/{name}":                 "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /tasks/{id}":                    "mutates through the Docker daemon; covered by the e2e write lane",
	"DELETE /volumes/{name}":                "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /configs/{id}/labels":            "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /nodes/{id}/labels":              "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /secrets/{id}/labels":            "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/configs":          "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/container-config": "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/env":              "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/healthcheck":      "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/labels":           "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/log-driver":       "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/mounts":           "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/networks":         "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/ports":            "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/resources":        "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/rollback-policy":  "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/secrets":          "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /services/{id}/update-policy":    "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /swarm/ca":                       "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /swarm/dispatcher":               "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /swarm/encryption":               "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /swarm/orchestration":            "mutates through the Docker daemon; covered by the e2e write lane",
	"PATCH /swarm/raft":                     "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /configs":                         "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /secrets":                         "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /services/{id}/restart":           "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /services/{id}/rollback":          "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /swarm/force-rotate-ca":           "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /swarm/rotate-token":              "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /swarm/rotate-unlock-key":         "mutates through the Docker daemon; covered by the e2e write lane",
	"POST /swarm/unlock":                    "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /nodes/{id}/availability":          "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /nodes/{id}/role":                  "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /services/{id}/endpoint-mode":      "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /services/{id}/healthcheck":        "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /services/{id}/image":              "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /services/{id}/placement":          "mutates through the Docker daemon; covered by the e2e write lane",
	"PUT /services/{id}/scale":              "mutates through the Docker daemon; covered by the e2e write lane",

	// Genuine gaps: none of the categories above applies. A later slice adds
	// the sweep; see the campaign defect list for the count.
	"/debug/pprof/":                       "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"/debug/pprof/cmdline":                "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"/debug/pprof/profile":                "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"/debug/pprof/symbol":                 "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"/debug/pprof/trace":                  "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/docker-latest-version":        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/health":                       "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/licenses":                     "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/licenses/texts/{id}":          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/metrics":                      "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/notices":                      "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/ready":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /-/sbom.cdx":                     "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /.well-known/api-catalog":        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api":                            "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/asyncapi":                   "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/asyncapi.yaml":              "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/context.jsonld":             "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/errors":                     "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/errors/{code}":              "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/openapi.yaml":               "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /api/scalar.js":                  "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /auth/whoami":                    "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /cluster":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /cluster/capacity":               "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /configs":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /configs/{id}/labels":            "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /history":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /index":                          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /metrics/status":                 "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /networks":                       "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /nodes":                          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /nodes/{id}/labels":              "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /nodes/{id}/role":                "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /nodes/{id}/tasks":               "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /opensearch.xml":                 "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /profile":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /recommendations":                "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /search":                         "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /secrets":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /secrets/{id}/labels":            "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/configs":          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/container-config": "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/endpoint-mode":    "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/env":              "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/healthcheck":      "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/labels":           "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/log-driver":       "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/mode":             "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/mounts":           "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/networks":         "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/placement":        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/ports":            "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/resources":        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/rollback-policy":  "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/secrets":          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/tasks":            "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /services/{id}/update-policy":    "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /stacks":                         "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /stacks/summary":                 "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /tasks":                          "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /topology":                       "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /topology/networks":              "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /topology/placement":             "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /volumes":                        "gap: no sweep in the first slice reaches this; see the campaign defect list",
	"GET /{$}":                            "gap: no sweep in the first slice reaches this; see the campaign defect list",
}

// knownTransportDivergences records places where REST and MCP disagree about
// whether a resource is readable, keyed "<singular type> by <id|name>". Each is
// a defect, not an accepted behaviour: the entry exists so the invariant keeps
// guarding every other combination while the disagreement stands.
var knownTransportDivergences = map[string]string{}
