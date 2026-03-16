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
| `CETACEAN_LOG_FORMAT` | `json` | No |
| `CETACEAN_LOG_LEVEL` | `info` | No |
| `CETACEAN_SSE_BATCH_INTERVAL` | `100ms` | No |
| `CETACEAN_PPROF` | `false` | No (enable pprof endpoints at `/debug/pprof/`) |

## Architecture

### Data flow
Docker Socket → `docker/watcher.go` (full sync + event stream) → `cache/cache.go` (in-memory maps, `sync.RWMutex`) → `api/handlers.go` (REST JSON) + `api/sse.go` (real-time broadcast) → Browser

### Backend (`internal/`)
- **`config/`** — Env var parsing. All config is optional; Prometheus metrics disabled if URL unset.
- **`cache/`** — Thread-safe in-memory store using `sync.RWMutex`. Holds nodes, services, tasks, configs, secrets, networks, volumes. Stacks are derived from `com.docker.stack.namespace` labels and rebuilt on every mutation. Cross-reference methods (`ServicesUsingConfig`, `ServicesUsingSecret`, `ServicesUsingNetwork`, `ServicesUsingVolume`) scan services to find which ones use a given resource. Every Set/Delete fires an `OnChangeFunc` callback that feeds the SSE broadcaster.
- **`cache/history.go`** — Ring buffer (10,000 entries) of resource change events, queryable by type/resourceId.
- **`cache/snapshot.go`** — Atomic disk persistence with versioned JSON format.
- **`docker/client.go`** — Thin wrapper over the Docker Engine API. List, Inspect, and Events methods for all resource types.
- **`docker/watcher.go`** — Full sync on startup (7 parallel goroutines), then subscribes to Docker event stream. Re-syncs every 5 minutes and on reconnect. Container events are mapped to task updates via `com.docker.swarm.task.id` attribute. 50ms debounce with 4-worker inspect pool.
- **`api/router.go`** — stdlib `net/http.ServeMux` with Go 1.22+ method routing. Resources live at top-level paths (e.g., `GET /nodes`, `GET /services/{id}`), meta endpoints under `/-/` (health, ready, metrics), and API docs at `/api`. Content negotiation middleware (`negotiate`) resolves `Accept` header or `.json`/`.html` extension suffix, storing the result in context. Dispatch helpers (`contentNegotiated`, `contentNegotiatedWithSSE`, `sseOnly`) route to JSON handler, SSE handler, or SPA based on negotiated type. All list and detail endpoints support per-resource SSE streaming via `contentNegotiatedWithSSE` — `streamList` filters by type, `streamResource` filters by type+id. Additional non-CRUD endpoints: `/cluster`, `/cluster/metrics`, `/swarm`, `/disk-usage`, `/plugins`, `/stacks/summary`, `/history`, `/topology/networks`, `/topology/placement`. Middleware chain: requestID → recovery → securityHeaders → negotiate → discoveryLinks → requestLogger. SPA fallback registered last on `/`.
- **`api/handlers.go`** — REST handlers. All read-only, serve cache data as JSON. List endpoints support `?search=`, `?filter=` (expr-lang expressions), `?sort=`, `?dir=`, `?limit=`, `?offset=` and return `CollectionResponse` with JSON-LD metadata + pagination Link headers. Detail endpoints return JSON-LD wrapped responses (`@context`, `@id`, `@type`) with the resource + cross-referenced services. All JSON responses include ETag for conditional 304 responses. `HandleSearch` provides cross-resource global search. `DockerLogStreamer` interface decouples log streaming for testability. Task list/detail endpoints return `EnrichedTask` (adds `ServiceName`, `NodeHostname` to raw `swarm.Task`). Log-tail SSE connections are capped at 128 concurrent.
- **`api/sse.go`** — `Broadcaster` manages up to 256 SSE clients. Per-resource SSE: list endpoints stream events filtered by type, detail endpoints stream events filtered by type+id. Legacy `GET /events` endpoint supports `?types=` filter. Event batching within configurable interval. Slow clients get events dropped (non-blocking send to buffered channel). SSE events include the full resource in the `resource` field for optimistic client-side updates.
- **`api/prometheus.go`** — Reverse proxy to Prometheus, only allows `/query` and `/query_range` paths with whitelisted query params. 10MB response limit, 30s timeout.
- **`api/metricsstream.go`** — SSE streaming handler for Prometheus queries. Registered at `GET /-/metrics/query_range` via content negotiation (JSON → proxy, SSE → stream handler). Periodically runs instant queries and pushes `point` events; sends full `initial` event on connect. 64-connection limit, 15s keepalive, skips ticks if previous query is in-flight.
- **`api/promquery.go`** — `PromClient` for direct Prometheus instant queries. Used by `HandleClusterMetrics` and `HandleMonitoringStatus` (`GET /-/metrics/status` — probes for node-exporter/cAdvisor targets, returns detection status for guided setup UI). Also provides `RangeQueryRaw` and `InstantQueryRaw` for raw JSON byte responses (used by metrics stream handler).
- **`api/spa.go`** — Serves the embedded `frontend/dist/` filesystem with index.html fallback for client-side routing.
- **`api/negotiate.go`** — Content negotiation middleware. Resolves `Accept` header or `.json`/`.html` extension suffix to `ContentType` enum stored in context. `ContentTypeFromContext` used by handlers.
- **`api/dispatch.go`** — `contentNegotiated`, `contentNegotiatedWithSSE`, and `sseOnly` dispatch helpers that route requests to JSON handler, SSE handler, or SPA based on negotiated content type.
- **`api/problem.go`** — RFC 9457 problem details (`application/problem+json`). `writeProblem` and `writeProblemTyped` produce structured error responses with JSON-LD `@context`.
- **`api/jsonld.go`** — JSON-LD response helpers. `DetailResponse` uses custom `MarshalJSON` with deterministic key ordering (`@context`, `@id`, `@type` first, then extras sorted) for stable ETags. `CollectionResponse` generic struct wraps list endpoints with pagination metadata.
- **`api/etag.go`** — `writeJSONWithETag` computes SHA-256 ETag (truncated to 16 bytes) and returns 304 Not Modified on `If-None-Match` match. `etagMatch` supports multiple comma-separated ETags, weak ETags (`W/"..."`), and wildcard `*` per RFC 9110 weak comparison.
- **`api/context.go`** — Serves the JSON-LD context document at `/api/context.jsonld`.
- **`api/apidoc.go`** — Serves `/api` endpoint: HTML gets Scalar API playground (with embedded JS bundle at `/api/scalar.js`), otherwise returns OpenAPI spec as JSON. YAML spec is converted to JSON at startup.
- **`filter/`** — Expression-based filtering using `expr-lang/expr`. Each resource type has an env builder exposing fields for filter expressions.

### Frontend (`frontend/src/`)
- React 19 + TypeScript + Vite, Tailwind CSS v4, shadcn/ui components, Chart.js for time-series and doughnut charts
- **`api/client.ts`** — Fetch wrapper for all API endpoints. All requests include `Accept: application/json` header (required for content negotiation — without it, resource paths serve the SPA HTML). Detail methods: `api.node(id)`, `api.service(id)`, `api.task(id)`, `api.stack(name)`, `api.config(id)`, `api.secret(id)`, `api.network(id)`, `api.volume(name)`. Global search: `api.search(q, limit?)`.
- **`api/types.ts`** — TypeScript types matching the Go JSON responses. Detail response types bundle the resource + `ServiceRef[]` cross-references.
- **`hooks/useResourceStream.ts`** — Opens a per-path `EventSource` and dispatches parsed SSE events to a listener. Replaces the old single-connection SSE context. Also exports `ConnectionProvider`/`useConnection` for connection status UI.
- **`hooks/useDetailResource.ts`** — Generic hook for detail pages: fetches resource + history, subscribes to per-resource SSE stream via `useResourceStream`, re-fetches on change events.
- **`hooks/useSwarmResource.ts`** — Generic fetch + SSE subscription hook for resource lists. Connects to per-resource SSE path (e.g. `/nodes`, `/services`) via `useResourceStream`. Performs optimistic in-place updates (upsert/remove) on SSE events without full refetch; falls back to full reload on `sync` events.
- **`hooks/useMonitoringStatus.ts`** — Replaces `usePrometheusConfigured`. Returns full detection status (Prometheus reachable, node-exporter/cAdvisor target counts vs. cluster node count). Used by `MonitoringStatus` component (replaces `PrometheusBanner`).
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
- **`pages/`** — All resource types have list + detail pages. Detail pages subscribe to SSE for real-time updates. List pages use `useSwarmResource` for SSE-driven optimistic updates. `SearchPage` at `/search?q=` for full global search results. `SwarmPage` at `/swarm` shows cluster info, join tokens, raft/CA/orchestration config, task defaults, and installed plugins. `Topology` at `/topology` with logical (services grouped by stack) and physical (tasks grouped by node) views.
- Path alias: `@/` maps to `frontend/src/`

### Embedding
`main.go` uses `//go:embed frontend/dist/*` to embed the built frontend into the Go binary. The frontend must be built before `go build`.

## Releases
- **Always sign release tags** with `git tag -s` (never `git tag -a`). Unsigned tags show as "unverified" on GitHub and immutable releases prevent fixing this after the fact.
- **Always update `CHANGELOG.md`** when committing user-facing changes (features, fixes, security). Add entries under `[Unreleased]`. When cutting a release, move unreleased entries to a new version heading with the release date.
- **Changelog entries must be user-facing and concise.** No implementation details, internal refactoring, pixel values, or code-level specifics. Write from the perspective of someone using the dashboard, not developing it. Consolidate related changes into a single entry (e.g. three doughnut chart tweaks → "Simplify disk usage chart"). If a user wouldn't notice or care about a change, don't list it.

## Key Conventions
- All API endpoints are GET-only (read-only system)
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
- No authentication — designed to run behind a reverse proxy
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
- **SSE streaming**: live range charts (1h/6h/24h/7d) open an `EventSource` to `/-/metrics/query_range` after the initial JSON fetch, receiving `initial` (full range) and `point` (single value) events for real-time updates. Custom time ranges use JSON-only.
- **Error tiers**: `FetchError` (page-level), `ErrorBoundary` (wraps MetricsPanel/LogViewer), component-internal error states
- **Global search**: `Cmd+K` opens `SearchPalette` command palette; `/search?q=` for full-page results. Palette polls every 2s for live state updates (orbs/spinners) but preserves result order to avoid UI jank. Results grouped by type in fixed order: services > stacks > nodes > tasks > configs > secrets > networks > volumes
- **Cross-references**: detail pages show "Used by Services" tables with links; configs/secrets link to stacks via label
- Structured logging throughout backend via `log/slog`

## API Documentation
- **OpenAPI spec**: `api/openapi.yaml`, served at `GET /api` (JSON for programmatic clients, Scalar HTML playground for browsers)
- **JSON-LD context**: `internal/api/context.go`, served at `GET /api/context.jsonld`
- **Markdown docs**: `docs/api.md`
- Meta endpoints (`/-/health`, `/-/ready`, `/-/metrics/`) have no content negotiation or discovery links

## Known Pre-existing Issues
None — all previously known test failures have been fixed.

## Design Documents
Design specs and implementation plans are in `docs/plans/` and `docs/superpowers/`. Key recent ones:
- `docs/plans/2026-03-10-global-search-design.md` — Global search feature design
- `docs/plans/2026-03-10-global-search.md` — Global search implementation plan
- `docs/plans/2026-03-11-monitoring-onboarding.md` — Monitoring onboarding design (auto-detection, compose split, guided setup UI)
- `docs/plans/2026-03-11-monitoring-onboarding-plan.md` — Monitoring onboarding implementation plan
- `docs/superpowers/specs/2026-03-15-chart-refinements-design.md` — Chart refinements design (CVD-safe palette, linked crosshairs, click-to-isolate, brush-to-zoom, stack drill-down, tooltip transitions)
- `docs/superpowers/plans/2026-03-15-chart-refinements.md` — Chart refinements implementation plan
- `docs/superpowers/specs/2026-03-15-new-chart-types-design.md` — New chart types design (stacked area, horizontal bar, multi-ring doughnut)
- `docs/superpowers/plans/2026-03-15-new-chart-types.md` — New chart types implementation plan
