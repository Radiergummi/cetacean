# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Cetacean is a read-only observability dashboard for Docker Swarm Mode clusters. Single Go binary with an embedded React SPA. Connects to the Docker socket, caches all swarm state in memory, and pushes updates to browsers via SSE. The goal is to be a complete replacement for the Docker CLI when it comes to understanding a Swarm cluster — all resources should be browsable with clickable cross-references between them.

## Commands

### Backend
```bash
go build -o cetacean .                    # Build binary (requires frontend/dist/ to exist)
go test ./...                             # Run all Go tests
go test ./internal/cache/                 # Run tests for a single package
go run .                                  # Run locally (metrics require CETACEAN_PROMETHEUS_URL)
```

### Frontend
```bash
cd frontend
npm install                               # Install dependencies
npm run dev                               # Vite dev server on :5173 (proxies resource paths to :9000)
npm run build                             # Build to frontend/dist/
npm run lint                              # oxlint
npm run fmt                               # oxfmt (write)
npm run fmt:check                         # oxfmt (check only)
npx tsc -b --noEmit                       # Type check only (faster than full build)
npx vitest run                            # Run all frontend tests
```

### Lint & Format (Makefile)
```bash
make lint                                 # golangci-lint + oxlint
make fmt                                  # gofmt + oxfmt (write)
make fmt-check                            # Check formatting without modifying
make check                                # lint + fmt-check + test
make test                                 # go test ./...
make build                                # frontend build + go build
```

### Full build from source
```bash
cd frontend && npm install && npm run build && cd ..
go build -o cetacean .
```

### Docker
```bash
docker build -t cetacean:latest .                           # Multi-stage build
docker stack deploy -c compose.yaml cetacean          # Deploy full stack (requires swarm)
docker stack deploy -c compose.monitoring.yaml monitoring  # Deploy standalone monitoring stack (Prometheus + cAdvisor + node-exporter)
```

### Environment variables
| Variable | Default | Required |
|---|---|---|
| `CETACEAN_PROMETHEUS_URL` | — | No (metrics disabled if unset) |
| `CETACEAN_DOCKER_HOST` | `unix:///var/run/docker.sock` | No |
| `CETACEAN_LISTEN_ADDR` | `:9000` | No |
| `CETACEAN_BASE_PATH` | — | No (serve at root by default) |
| `CETACEAN_LOG_FORMAT` | `json` | No |
| `CETACEAN_LOG_LEVEL` | `info` | No |
| `CETACEAN_SSE_BATCH_INTERVAL` | `100ms` | No |
| `CETACEAN_PPROF` | `false` | No (enable pprof endpoints at `/debug/pprof/`) |
| `CETACEAN_SELF_METRICS` | `true` | No (expose Prometheus metrics at `/-/metrics`) |
| `CETACEAN_RECOMMENDATIONS` | `true` | No (enable recommendation engine) |
| `CETACEAN_OPERATIONS_LEVEL` | `1` | No (0=read-only, 1=operational, 2=configuration, 3=impactful) |
| `CETACEAN_SNAPSHOT` | `true` | No (enable disk persistence of swarm state) |
| `CETACEAN_DATA_DIR` | `./data` | No (directory for snapshot file) |
| `CETACEAN_CONFIG` | — | No (path to TOML config file) |
| `CETACEAN_AUTH_MODE` | `none` | No (`none`, `oidc`, `tailscale`, `cert`, `headers`) |
| `CETACEAN_AUTH_OIDC_ISSUER` | — | Yes (if OIDC mode) |
| `CETACEAN_AUTH_OIDC_CLIENT_ID` | — | Yes (if OIDC mode) |
| `CETACEAN_AUTH_OIDC_CLIENT_SECRET` | — | Yes (if OIDC mode) |
| `CETACEAN_AUTH_OIDC_REDIRECT_URL` | — | Yes (if OIDC mode) |
| `CETACEAN_AUTH_OIDC_SCOPES` | `openid,profile,email` | No |
| `CETACEAN_AUTH_TAILSCALE_MODE` | `local` | No (`local` or `tsnet`) |
| `CETACEAN_AUTH_TAILSCALE_AUTHKEY` | — | Yes (if tsnet mode) |
| `CETACEAN_AUTH_TAILSCALE_HOSTNAME` | `cetacean` | No |
| `CETACEAN_AUTH_TAILSCALE_STATE_DIR` | — | No |
| `CETACEAN_AUTH_TAILSCALE_CAPABILITY` | — | No (app capability key for group extraction) |
| `CETACEAN_AUTH_CERT_CA` | — | Yes (if cert mode) |
| `CETACEAN_AUTH_HEADERS_SUBJECT` | — | Yes (if headers mode) |
| `CETACEAN_AUTH_HEADERS_NAME` | — | No |
| `CETACEAN_AUTH_HEADERS_EMAIL` | — | No |
| `CETACEAN_AUTH_HEADERS_GROUPS` | — | No |
| `CETACEAN_AUTH_HEADERS_SECRET_HEADER` | — | No |
| `CETACEAN_AUTH_HEADERS_SECRET_VALUE` | — | Yes (if secret header set) |
| `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` | — | No (comma-separated CIDR/IP allowlist) |
| `CETACEAN_TLS_CERT` | — | No (Yes for cert mode) |
| `CETACEAN_TLS_KEY` | — | No (Yes for cert mode) |
| `CETACEAN_SIZING_HEADROOM_MULTIPLIER` | `2.0` | No |
| `CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED` | `0.20` | No |
| `CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT` | `0.80` | No |
| `CETACEAN_SIZING_THRESHOLD_AT_LIMIT` | `0.95` | No |
| `CETACEAN_SIZING_LOOKBACK` | `168h` | No |
| `CETACEAN_ACL_POLICY` | — | No (inline JSON/YAML/TOML policy) |
| `CETACEAN_ACL_POLICY_FILE` | — | No (path to policy file, watched for hot reload) |
| `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` | — | No (Tailscale CapMap key for per-user grants) |
| `CETACEAN_AUTH_OIDC_ACL_CLAIM` | — | No (OIDC token claim containing grants) |
| `CETACEAN_AUTH_HEADERS_ACL` | — | No (HTTP header containing grants JSON) |

## Architecture

### Data flow
Docker Socket → `docker/watcher.go` (full sync + event stream) → `cache/cache.go` (in-memory maps, `sync.RWMutex`) → `api/handlers.go` (REST JSON) + `api/sse.go` (real-time broadcast) → Browser

### Backend (`internal/`)
- **`auth/`** — Pluggable authentication. `Provider` interface with `Authenticate(w, r) (*Identity, error)` and `RegisterRoutes(mux)`. Five providers: `NoneProvider` (anonymous), `OIDCProvider` (auth code flow + Bearer tokens, signed ephemeral session cookies), `TailscaleProvider` (local daemon or tsnet), `CertProvider` (mTLS client certs + SPIFFE), `HeadersProvider` (trusted proxy headers). Auth middleware sits between `securityHeaders` and `negotiate` in the chain. Routes `/-/*`, `/api*`, `/assets/*`, `/auth/*` are exempt from auth. Each provider registers `GET /auth/whoami`.
- **`acl/`** — Grant-based RBAC authorization. Resources are `type:pattern` expressions (service, stack, node, task, config, secret, network, volume, plugin, swarm) with glob wildcards. Audience is `user:pattern` (matches Subject + Email) or `group:pattern`. Permissions: `read` (view) and `write` (mutate, implies read). Additive-only (no deny rules). Policy from file or inline env var, with `fsnotify` hot reload on file changes. Provider grant sources: Tailscale CapMap, OIDC token claim, proxy header. `Evaluator.Can()` checks permission, `acl.Filter()` filters lists. Stack grants cover member resources; tasks inherit from parent service. Default: no policy = full access for authenticated users; with policy = default-deny.
- **`config/`** — Env var parsing. All config is optional; Prometheus metrics disabled if URL unset. Auth config via `LoadAuth()`, TLS config via `LoadTLS()`, ACL config via `LoadACL()`.
- **`cache/`** — Thread-safe in-memory store using `sync.RWMutex`. Holds nodes, services, tasks, configs, secrets, networks, volumes. Stacks are derived from `com.docker.stack.namespace` labels and rebuilt on every mutation. Cross-reference methods (`ServicesUsingConfig`, `ServicesUsingSecret`, `ServicesUsingNetwork`, `ServicesUsingVolume`) scan services to find which ones use a given resource. Every Set/Delete fires an `OnChangeFunc` callback that feeds the SSE broadcaster.
- **`cache/history.go`** — Ring buffer (10,000 entries) of resource change events, queryable by type/resourceId.
- **`cache/snapshot.go`** — Atomic disk persistence with versioned JSON format.
- **`docker/client.go`** — Thin wrapper over the Docker Engine API. List, Inspect, and Events methods for all resource types. Also implements `DockerWriteClient` (see `api/write_handlers.go`) with: `ScaleService`, `UpdateServiceImage`, `RollbackService`, `RestartService`, `UpdateNodeAvailability`, `RemoveTask`, `InspectServiceSpec`, `UpdateServiceEnv`, `InspectNodeSpec`, `UpdateNodeLabels`, `UpdateServiceResources`, `UpdateServicePlacement`, `UpdateServicePorts`, `UpdateServiceUpdatePolicy`, `UpdateServiceRollbackPolicy`, `UpdateServiceLogDriver`.
- **`docker/watcher.go`** — Full sync on startup (7 parallel goroutines), then subscribes to Docker event stream. Re-syncs every 5 minutes and on reconnect. Container events are mapped to task updates via `com.docker.swarm.task.id` attribute. 50ms debounce with 4-worker inspect pool.
- **`api/router.go`** — stdlib `net/http.ServeMux` with Go 1.22+ method routing. Resources live at top-level paths (e.g., `GET /nodes`, `GET /services/{id}`), meta endpoints under `/-/` (health, ready, metrics), and API docs at `/api`. Content negotiation middleware (`negotiate`) resolves `Accept` header or `.json`/`.html` extension suffix, storing the result in context. Dispatch helpers (`contentNegotiated`, `contentNegotiatedWithSSE`, `sseOnly`) route to JSON handler, SSE handler, or SPA based on negotiated type. All list and detail endpoints support per-resource SSE streaming via `contentNegotiatedWithSSE` — `streamList` filters by type, `streamResource` filters by type+id. Additional non-CRUD endpoints: `/cluster`, `/cluster/metrics`, `/swarm`, `/disk-usage`, `/plugins`, `/stacks/summary`, `/history`, `/topology/networks`, `/topology/placement`, `/recommendations`. Write endpoints: `PUT /services/{id}/scale`, `PUT /services/{id}/image`, `POST /services/{id}/rollback`, `POST /services/{id}/restart`, `PUT /nodes/{id}/availability`, `DELETE /tasks/{id}`, `GET|PATCH /services/{id}/env`, `GET|PATCH /nodes/{id}/labels`, `GET|PATCH /services/{id}/resources`, `GET|PUT /services/{id}/placement`, `GET|PATCH /services/{id}/ports`, `GET|PATCH /services/{id}/update-policy`, `GET|PATCH /services/{id}/rollback-policy`, `GET|PATCH /services/{id}/log-driver`. Middleware chain: requestID → recovery → securityHeaders → auth → negotiate → discoveryLinks → requestLogger. SPA fallback registered last on `/`.
- **`api/handlers.go`** — REST handlers. Serve cache data as JSON. List endpoints support `?search=`, `?filter=` (expr-lang expressions), `?sort=`, `?dir=`, `?limit=`, `?offset=` and return `CollectionResponse` with JSON-LD metadata + pagination Link headers. Detail endpoints return JSON-LD wrapped responses (`@context`, `@id`, `@type`) with the resource + cross-referenced services. All JSON responses include ETag for conditional 304 responses. `HandleSearch` provides cross-resource global search. `DockerLogStreamer` interface decouples log streaming for testability. `DockerWriteClient` interface decouples write operations for testability. Task list/detail endpoints return `EnrichedTask` (adds `ServiceName`, `NodeHostname` to raw `swarm.Task`). Log-tail SSE connections are capped at 128 concurrent.
- **`api/write_handlers.go`** — Write operation handlers. `DockerWriteClient` interface defines all mutating Docker API calls. Handlers: `HandleScaleService` (`PUT /services/{id}/scale`), `HandleUpdateServiceImage` (`PUT /services/{id}/image`), `HandleRollbackService` (`POST /services/{id}/rollback`), `HandleRestartService` (`POST /services/{id}/restart`), `HandleUpdateNodeAvailability` (`PUT /nodes/{id}/availability`), `HandleRemoveTask` (`DELETE /tasks/{id}`), `HandleGetServiceEnv`/`HandlePatchServiceEnv` (`GET/PATCH /services/{id}/env`), `HandleGetNodeLabels`/`HandlePatchNodeLabels` (`GET/PATCH /nodes/{id}/labels`), `HandleGetServiceResources`/`HandlePatchServiceResources` (`GET/PATCH /services/{id}/resources`), `HandleGetServicePlacement`/`HandlePutServicePlacement` (`GET/PUT /services/{id}/placement`), `HandleGetServicePorts`/`HandlePatchServicePorts` (`GET/PATCH /services/{id}/ports`), `HandleGetServiceUpdatePolicy`/`HandlePatchServiceUpdatePolicy` (`GET/PATCH /services/{id}/update-policy`), `HandleGetServiceRollbackPolicy`/`HandlePatchServiceRollbackPolicy` (`GET/PATCH /services/{id}/rollback-policy`), `HandleGetServiceLogDriver`/`HandlePatchServiceLogDriver` (`GET/PATCH /services/{id}/log-driver`). All mutating handlers check for version conflicts and return 409 on Docker API conflict errors.
- **`api/write_middleware.go`** — `requireLevel` middleware gates write endpoints by operations level (0=read-only, 1=operational, 2=configuration, 3=impactful). `requireWriteACL` middleware checks per-resource ACL write permission. Both are composed on every write endpoint. Returns 403 (OPS001 or ACL002) when access is denied.
- **`api/allow.go`** — Sets `Allow` response header on GET/HEAD responses. Combines operations level tier check with ACL write permission to determine which methods are available for each resource.
- **`api/jsonpatch.go`** — JSON Patch (RFC 6902) and JSON Merge Patch (RFC 7396) application logic for string maps (env vars, labels).
- **`api/sse.go`** — `Broadcaster` manages up to 256 SSE clients. Per-resource SSE: list endpoints stream events filtered by type, detail endpoints stream events filtered by type+id. Legacy `GET /events` endpoint supports `?types=` filter. Event batching within configurable interval. Slow clients get events dropped (non-blocking send to buffered channel). SSE events include the full resource in the `resource` field for optimistic client-side updates.
- **`api/prometheus.go`** — Reverse proxy to Prometheus. `HandleMetrics` is the content-negotiated handler at `/metrics` (routes instant vs range by `start`+`end` params). `HandleMetricsLabels` and `HandleMetricsLabelValues` serve label metadata at `/-/metrics/labels` and `/-/metrics/labels/{name}`. All handlers use nil-receiver checks (return 503 when Prometheus is not configured). 10MB response limit, 30s timeout.
- **`api/metricsstream.go`** — SSE streaming handler for Prometheus queries. Registered at `GET /metrics` via content negotiation (JSON → proxy, SSE → stream handler, HTML → SPA). Periodically runs instant queries and pushes `point` events; sends full `initial` event on connect. 64-connection limit, 15s keepalive, skips ticks if previous query is in-flight.
- **`api/promquery.go`** — `PromClient` for direct Prometheus instant queries. Used by `HandleClusterMetrics` and `HandleMonitoringStatus` (`GET /-/metrics/status` — probes for node-exporter/cAdvisor targets, returns detection status for guided setup UI). Also provides `RangeQueryRaw` and `InstantQueryRaw` for raw JSON byte responses (used by metrics stream handler).
- **`api/spa.go`** — Serves the embedded `frontend/dist/` filesystem with index.html fallback for client-side routing.
- **`api/negotiate.go`** — Content negotiation middleware. Resolves `Accept` header or `.json`/`.html` extension suffix to `ContentType` enum stored in context. `ContentTypeFromContext` used by handlers.
- **`api/dispatch.go`** — `contentNegotiated`, `contentNegotiatedWithSSE`, and `sseOnly` dispatch helpers that route requests to JSON handler, SSE handler, or SPA based on negotiated content type.
- **`api/problem.go`** — RFC 9457 problem details (`application/problem+json`). `writeProblem` and `writeProblemTyped` produce structured error responses with JSON-LD `@context`.
- **`api/jsonld.go`** — JSON-LD response helpers. `DetailResponse` uses custom `MarshalJSON` with deterministic key ordering (`@context`, `@id`, `@type` first, then extras sorted) for stable ETags. `CollectionResponse` generic struct wraps list endpoints with pagination metadata.
- **`api/etag.go`** — `writeJSONWithETag` computes SHA-256 ETag (truncated to 16 bytes) and returns 304 Not Modified on `If-None-Match` match. `etagMatch` supports multiple comma-separated ETags, weak ETags (`W/"..."`), and wildcard `*` per RFC 9110 weak comparison.
- **`api/context.go`** — Serves the JSON-LD context document at `/api/context.jsonld`.
- **`api/apidoc.go`** — Serves `/api` endpoint: HTML gets Scalar API playground (with embedded JS bundle at `/api/scalar.js`), otherwise returns OpenAPI spec as JSON. YAML spec is converted to JSON at startup.
- **`recommendations/`** — Unified recommendation engine. `Engine` runs registered `Checker` implementations on per-checker intervals (cache-only every 60s, Prometheus-dependent every 5min). Four checkers: `SizingChecker` (resource right-sizing via Prometheus), `ConfigChecker` (missing health checks, restart policies), `OperationalChecker` (flaky services, disk/memory pressure via Prometheus), `ClusterChecker` (single replicas, manager workloads, uneven distribution). Configurable sizing thresholds via `[sizing]` TOML section or `CETACEAN_SIZING_*` env vars.
- **`filter/`** — Expression-based filtering using `expr-lang/expr`. Each resource type has an env builder exposing fields for filter expressions.

### Frontend (`frontend/src/`)
- React 19 + TypeScript + Vite, Tailwind CSS v4, shadcn/ui components, Chart.js for time-series and doughnut charts
- **`api/client.ts`** — Fetch wrapper for all API endpoints. All requests include `Accept: application/json` header (required for content negotiation — without it, resource paths serve the SPA HTML). Detail methods: `api.node(id)`, `api.service(id)`, `api.task(id)`, `api.stack(name)`, `api.config(id)`, `api.secret(id)`, `api.network(id)`, `api.volume(name)`. Global search: `api.search(q, limit?)`. Mutation helpers: `api.put(path, body)`, `api.post(path, body?)`, `api.patch(path, body, contentType)`, `api.del(path)` — used by write action handlers.
- **`api/types.ts`** — TypeScript types matching the Go JSON responses. Detail response types bundle the resource + `ServiceRef[]` cross-references.
- **`hooks/useResourceStream.ts`** — Opens a per-path `EventSource` and dispatches parsed SSE events to a listener. Replaces the old single-connection SSE context. Also exports `ConnectionProvider`/`useConnection` for connection status UI.
- **`hooks/useDetailResource.ts`** — Generic hook for detail pages: fetches resource + history, subscribes to per-resource SSE stream via `useResourceStream`, re-fetches on change events.
- **`hooks/useSwarmResource.ts`** — Generic fetch + SSE subscription hook for resource lists. Connects to per-resource SSE path (e.g. `/nodes`, `/services`) via `useResourceStream`. Performs optimistic in-place updates (upsert/remove) on SSE events without full refetch; falls back to full reload on `sync` events.
- **`hooks/useMonitoringStatus.ts`** — Replaces `usePrometheusConfigured`. Returns full detection status (Prometheus reachable, node-exporter/cAdvisor target counts vs. cluster node count). Used by `MonitoringStatus` component (replaces `PrometheusBanner`).
- **`hooks/useAuth.ts`** — Auth context and `useAuth` hook. `AuthProvider` wraps the app, fetches identity from `/auth/whoami` on mount. `UserBadge` displays identity in the nav bar (hidden in `none` mode).
- **`components/`** — Key components:
  - `DataTable` — auto-virtualizes above 100 rows via `@tanstack/react-virtual`
  - `log/` — Modular log viewer: `LogViewer` (orchestrator), `LogTable` (virtual/plain rendering), `LogMessage` (line renderer with JSON pretty-print), `LogToolbar` (time range, stream/level filters), `useLogData` (fetch + SSE streaming + pagination), `useLogFilter` (search/level/task filtering), `useLogTimeRange` (URL-persisted time range), `log-utils` (types, constants, formatters)
  - `metrics/` — Chart.js-based visualizations with shared CVD-safe color palette (`lib/chartColors.ts`). `TimeSeriesChart` (line + stacked area toggle, linked crosshairs via `ChartSyncProvider`, click-to-isolate, brush-to-zoom via chartjs-plugin-zoom), `StackDrillDownChart` (wraps TimeSeriesChart with double-click stack→service drill-down + toggleable legend), `MetricsPanel` (range picker with presets + custom datetime range, auto-refresh, wraps charts in `ChartSyncProvider` + `MetricsPanelContext`), `ResourceAllocationChart` (horizontal bar chart showing reserved vs. actual usage with limit markers), `RangePicker` (custom date-time range dropdown with quick presets), `ResourceGauge` (SVG half-circle), `NodeResourceGauges`, `CapacitySection` (cluster utilization bars), `MonitoringStatus` (auto-detection banner with 4 states: unconfigured, unreachable, partial, healthy)
  - `search/` — `GlobalSearch` (nav bar trigger + Cmd+K), `SearchPalette` (command palette with grouped results, keyboard nav, 2s polling), `SearchInput`
  - `ActivitySection` — recent resource change history feed (wraps `ActivityFeed`)
  - `CollapsibleSection` — collapsible wrapper with localStorage-persisted open/closed state
  - `SimpleTable` — lightweight generic table (used where `DataTable` is overkill)
  - `data/LabelSection` — key-value label display section
  - `InfoCard`, `ResourceCard`, `PageHeader`, `FetchError`, `EmptyState`, `LoadingSkeleton`
- **`lib/chartColors.ts`** — Shared IBM Carbon/Wong CVD-safe chart color palette (10 colors). `getChartColor(index)` reads from CSS custom properties (`--chart-1` through `--chart-10`) with hex fallbacks, cached after first call.
- **`lib/chartTooltip.ts`** — Shared `CHART_TOOLTIP_CLASS` constant for chart tooltip styling (used by both React and imperative tooltip renderers)
- **`lib/mockChartData.ts`** — Dev-only mock data generator for charts when Prometheus returns empty (used via `import.meta.env.DEV` guard)
- **`lib/searchConstants.ts`** — Shared constants for search UI: `TYPE_ORDER`, `TYPE_LABELS`, `statusColor`, `resourcePath`
- **`lib/parseStackLabels.ts`** — Filters out `com.docker.stack.namespace` from labels, returns remaining entries + stack name
- **`pages/`** — All resource types have list + detail pages. Detail pages subscribe to SSE for real-time updates. List pages use `useSwarmResource` for SSE-driven optimistic updates. `SearchPage` at `/search?q=` for full global search results. `SwarmPage` at `/swarm` shows cluster info, join tokens, raft/CA/orchestration config, task defaults, and installed plugins. `Topology` at `/topology` with logical (services grouped by stack) and physical (tasks grouped by node) views. Service detail page includes action buttons (scale, update image, rollback, restart) and inline env/resources editors. Node detail page includes availability selector and inline labels editor. Task detail page includes a remove (force-shutdown) action button.
- Path alias: `@/` maps to `frontend/src/`

### Embedding
`main.go` uses `//go:embed frontend/dist/*` to embed the built frontend into the Go binary. The frontend must be built before `go build`.

## Releases
- **Always sign release tags** with `git tag -s` (never `git tag -a`). Unsigned tags show as "unverified" on GitHub and immutable releases prevent fixing this after the fact.
- **Always update `CHANGELOG.md`** when committing user-facing changes (features, fixes, security). Add entries under `[Unreleased]`. When cutting a release, move unreleased entries to a new version heading with the release date.
- **Changelog entries must be user-facing and concise.** No implementation details, internal refactoring, pixel values, or code-level specifics. Write from the perspective of someone using the dashboard, not developing it. Consolidate related changes into a single entry (e.g. three doughnut chart tweaks → "Simplify disk usage chart"). If a user wouldn't notice or care about a change, don't list it.

## Code Style
- **No abbreviations in JavaScript/TypeScript code.** Use full words for identifiers — `formatNumber` not `fmtNum`, `formatUnit` not `fmtUnit`, `index` not `idx`. Abbreviations that are industry-standard terms (e.g., `URL`, `API`, `SSE`, `HTML`) are fine.
- **TSX/TS formatting:** Always brace single-statement `if` bodies (no braceless returns). Add blank lines around logical blocks — after `if` blocks, after variable declarations before logic, between `case` branches, and before `return` after logic. Use destructuring in callbacks (`({ value }) => value`, not `(s) => s.value`). Put JSX props on separate lines when there are 3+ props or long lines. Use camelCase for module-level constants (`knownStates`, not `KNOWN_STATES`), with `as const` where applicable. Use multi-line JSDoc (`/**\n *\n */`).

## Key Conventions
- Most API endpoints are read-only (GET). Write operations use: PUT for idempotent updates (scale, image, availability), POST for non-idempotent actions (rollback, restart), PATCH for partial updates (env vars, labels, resources), DELETE for removal (tasks, services). All write endpoints go through `requireLevel` (operations tier) AND `requireWriteACL` (per-resource ACL write check). `Allow` header on all GET/HEAD responses indicates available methods based on ops level + ACL. ACL error codes: ACL001 (read denied), ACL002 (write denied). PATCH handlers validate Content-Type (`application/json-patch+json` or `application/merge-patch+json`) and return 415 for mismatches.
- Uses Docker Engine API types directly (e.g., `swarm.Node`, `swarm.Service`) — no separate domain models
- All responses include JSON-LD `@context`, `@id`, `@type` metadata; detail endpoints return `{ @context, @id, @type, <resource>, services? }` wrappers for cross-referencing
- List responses return `CollectionResponse` with `items`, `total`, `limit`, `offset` + pagination Link headers (RFC 8288)
- Self-discovery Link headers (RFC 8631) on all non-meta responses: `</api>; rel="service-desc"` and `</api/context.jsonld>; rel="describedby"`
- Error responses use RFC 9457 (`application/problem+json`) with `@context`, `type`, `title`, `status`, `detail`, `instance`, `requestId`
- Content negotiation: `Accept` header or `.json`/`.html` extension suffix; resource URLs serve SPA for HTML, JSON for `application/json`
- ETag + conditional 304 responses on all JSON endpoints
- Stacks are a derived concept from `com.docker.stack.namespace` labels, not a Docker primitive
- Volumes are keyed by Name, everything else by ID — volume detail route uses `{name}` not `{id}`
- Networks use `network.Summary` (which aliases `network.Inspect`) — the list and inspect types are identical in the Docker SDK
- Secret data is always cleared before API responses (`sec.Spec.Data = nil`) — in list, detail, stack detail, and search endpoints
- Config data is returned base64-encoded (from Docker SDK); frontend decodes with `atob()`
- Authentication is pluggable via `CETACEAN_AUTH_MODE` (default `none`). Modes: `none` (anonymous), `oidc`, `tailscale`, `cert`, `headers`. Auth middleware exempts `/-/*`, `/api*`, `/assets/*`, `/auth/*` routes. `GET /auth/whoami` returns the current identity (includes `permissions` map when ACL is active). OIDC uses signed ephemeral cookies (invalidate on restart) for browser sessions and Bearer token validation for machines. TLS termination available in any mode via `CETACEAN_TLS_CERT`/`KEY`, required for cert mode.
- Authorization via grant-based RBAC (`CETACEAN_ACL_POLICY` or `CETACEAN_ACL_POLICY_FILE`). Grants are `(resources, audience, permissions)` tuples. No policy = full access for authenticated users. With policy = default-deny. Auth mode `none` bypasses ACL entirely. Provider grant sources complement file policy (Tailscale CapMap, OIDC claim, proxy header).
- pprof endpoints are opt-in via `CETACEAN_PPROF=true`, exposed at `/debug/pprof/` (registered without method prefix to support POST for `go tool pprof`)
- Global search (`GET /search?q=&limit=`) searches names, images, labels across all resource types. `limit=0` returns up to 1000 per type; default is 3 per type. Response includes optional `state` field for services (derived from running/desired tasks + UpdateStatus) and tasks
- Task list endpoints return `EnrichedTask` with `ServiceName` and `NodeHostname` populated from cache cross-references
- SSE connection limits return 429 (not 503) with `Retry-After` header
- Log-tail SSE endpoints have a 128-connection limit; log `after`/`before` params are validated server-side (invalid values return 400)

## Frontend Patterns
- **List pages**: `useSwarmResource` hook + `DataTable`/`ResourceCard` with `onRowClick`/`to` for navigation
- **Detail pages**: `useDetailResource` hook (or manual `useState` + `useResourceStream`) connects to per-resource SSE path (e.g. `/nodes/{id}`) and re-fetches on change events. Each detail page gets its own `EventSource` connection scoped to that resource.
- **URL as state**: search (`?q=`), sort (`?sort=&dir=`), metrics range (`?range=`), custom time range (`?from=&to=`) all URL-persisted
- **View persistence**: table/grid toggle saved in localStorage per resource type via `useViewMode`
- **Chart interactions**: click-to-isolate (single click dims other series to 30%), linked crosshairs across charts in same `MetricsPanel` (dashed line + dots on siblings), brush-to-zoom (drag to select time range, 5px threshold distinguishes from clicks), stacked area toggle (line/area icons in chart header), double-click to drill down (stack→services in `StackDrillDownChart`)
- **Chart architecture**: `ChartSyncProvider` (pub/sub context for crosshair sync), `MetricsPanelContext` (provides range/refresh/onRangeSelect to children), memoized Chart.js plugins read state via refs to avoid stale closures, `chartjs-plugin-zoom` for brush-to-zoom
- **SSE streaming**: live range charts (1h/6h/24h/7d) open an `EventSource` to `/metrics` after the initial JSON fetch, receiving `initial` (full range) and `point` (single value) events for real-time updates. Custom time ranges use JSON-only.
- **Error tiers**: `FetchError` (page-level), `ErrorBoundary` (wraps MetricsPanel/LogViewer), component-internal error states
- **Global search**: `Cmd+K` opens `SearchPalette` command palette; `/search?q=` for full-page results. Palette polls every 2s for live state updates (orbs/spinners) but preserves result order to avoid UI jank. Results grouped by type in fixed order: services > stacks > nodes > tasks > configs > secrets > networks > volumes
- **Cross-references**: detail pages show "Used by Services" tables with links; configs/secrets link to stacks via label
- Structured logging throughout backend via `log/slog`

## API Documentation
- **OpenAPI spec**: `api/openapi.yaml`, served at `GET /api` (JSON for programmatic clients, Scalar HTML playground for browsers)
- **JSON-LD context**: `internal/api/context.go`, served at `GET /api/context.jsonld`
- **Markdown docs**: `docs/api.md`
- Meta endpoints (`/-/health`, `/-/ready`, `/-/metrics/status`, `/-/metrics/labels`) have no content negotiation or discovery links

## Known Pre-existing Issues
None — all previously known test failures have been fixed.

## End-User Documentation
- `docs/getting-started.md` — Installation, quick start, first run
- `docs/configuration.md` — Environment variables, CLI flags, health checks, timeouts
- `docs/monitoring.md` — Prometheus, node-exporter, cAdvisor setup
- `docs/authentication.md` — Authentication providers (none, OIDC, Tailscale, cert, headers)
- `docs/authorization.md` — Grant-based RBAC: resource/audience expressions, policy files, provider sources
- `docs/dashboard.md` — UI guide: navigation, keyboard shortcuts, search, charts, logs
- `docs/recommendations.md` — Recommendation engine: categories, thresholds, configuration, UI, API
- `docs/api.md` — API reference: endpoints, query parameters, filters, response formats, SSE
