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
npm run dev                               # Vite dev server on :5173 (proxies /api to :9000)
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
docker stack deploy -c docker-compose.yml cetacean          # Deploy full stack (requires swarm)
docker stack deploy -c docker-compose.monitoring.yml monitoring  # Deploy standalone monitoring stack (Prometheus + cAdvisor + node-exporter)
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
| `CETACEAN_NOTIFICATION_RULES` | — | No (path to notification rules YAML file) |
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
- **`api/router.go`** — stdlib `net/http.ServeMux` with Go 1.22+ method routing (`"GET /api/..."`). Middleware chain: requestID → recovery → securityHeaders → requestLogger. SPA fallback registered last on `/`.
- **`api/handlers.go`** — REST handlers. All read-only, serve cache data as JSON. List endpoints support `?search=`, `?filter=` (expr-lang expressions), `?sort=`, `?dir=`, `?limit=`, `?offset=`. Detail endpoints for all resource types return the resource + cross-referenced services. `HandleSearch` provides cross-resource global search. `DockerLogStreamer` interface decouples log streaming for testability. Task list/detail endpoints return `EnrichedTask` (adds `ServiceName`, `NodeHostname` to raw `swarm.Task`). Log-tail SSE connections are capped at 128 concurrent.
- **`api/sse.go`** — `Broadcaster` manages up to 256 SSE clients. Clients can filter by `?types=node,service,task`. Event batching within configurable interval. Slow clients get events dropped (non-blocking send to buffered channel).
- **`api/prometheus.go`** — Reverse proxy to Prometheus, only allows `/query` and `/query_range` paths. 10MB response limit, 30s timeout. `HandleMonitoringStatus` — `GET /api/metrics/status` probes Prometheus for `up{job="node-exporter"}` and `up{job="cadvisor"}` targets, compares target count against cluster node count. Returns detection status for guided setup UI.
- **`api/spa.go`** — Serves the embedded `frontend/dist/` filesystem with index.html fallback for client-side routing.
- **`filter/`** — Expression-based filtering using `expr-lang/expr`. Each resource type has an env builder exposing fields for filter expressions.
- **`notify/`** — Webhook notification system with expr-lang rule matching, cooldown, and circuit breaker (5 failures → open, 30s half-open).

### Frontend (`frontend/src/`)
- React 19 + TypeScript + Vite, Tailwind CSS v4, shadcn/ui components, uPlot for time-series charts
- **`api/client.ts`** — Fetch wrapper for all API endpoints. Detail methods: `api.node(id)`, `api.service(id)`, `api.task(id)`, `api.stack(name)`, `api.config(id)`, `api.secret(id)`, `api.network(id)`, `api.volume(name)`. Global search: `api.search(q, limit?)`.
- **`api/types.ts`** — TypeScript types matching the Go JSON responses. Detail response types bundle the resource + `ServiceRef[]` cross-references.
- **`hooks/SSEContext.tsx`** — Shared SSE provider (single EventSource connection for the whole app). Uses `Map<symbol, listener>` registry. Event types: node, service, task, config, secret, network, volume, stack, batch.
- **`hooks/useSSE.ts`** — Re-exports `useSSESubscribe` from SSEContext. Used by both list pages (via `useSwarmResource`) and detail pages (direct subscription for re-fetch on change).
- **`hooks/useSwarmResource.ts`** — Generic fetch + SSE subscription hook for resource lists. Performs optimistic in-place updates (upsert/remove) without full refetch.
- **`hooks/useMonitoringStatus.ts`** — Replaces `usePrometheusConfigured`. Returns full detection status (Prometheus reachable, node-exporter/cAdvisor target counts vs. cluster node count). Used by `MonitoringStatus` component (replaces `PrometheusBanner`).
- **`components/`** — Key components:
  - `DataTable` — auto-virtualizes above 100 rows via `@tanstack/react-virtual`
  - `LogViewer` — 800+ line component with live SSE tail, regex search, JSON formatting, stream filtering
  - `TimeSeriesChart` — uPlot canvas with gradient fill, threshold annotations, cursor sync
  - `MetricsPanel` — wraps N charts with shared range picker (1h/6h/24h/7d, URL-persisted)
  - `ResourceGauge` — SVG half-circle gauge with threshold coloring
  - `GlobalSearch` — nav bar trigger + Cmd+K listener, opens `SearchPalette`
  - `SearchPalette` — command palette overlay with grouped results, keyboard nav, live polling (2s refresh)
  - `InfoCard`, `ResourceCard`, `PageHeader`, `FetchError`, `EmptyState`, `LoadingSkeleton`
- **`lib/searchConstants.ts`** — Shared constants for search UI: `TYPE_ORDER`, `TYPE_LABELS`, `STATE_COLORS`, `resourcePath`
- **`pages/`** — All resource types have list + detail pages. Detail pages subscribe to SSE for real-time updates. List pages use `useSwarmResource` for SSE-driven optimistic updates. `SearchPage` at `/search?q=` for full global search results.
- Path alias: `@/` maps to `frontend/src/`

### Embedding
`main.go` uses `//go:embed frontend/dist/*` to embed the built frontend into the Go binary. The frontend must be built before `go build`.

## Key Conventions
- All API endpoints are GET-only (read-only system)
- Uses Docker Engine API types directly (e.g., `swarm.Node`, `swarm.Service`) — no separate domain models
- Detail endpoints return `{ resource: T, services: ServiceRef[] }` bundles for cross-referencing
- Stacks are a derived concept from `com.docker.stack.namespace` labels, not a Docker primitive
- Volumes are keyed by Name, everything else by ID — volume detail route uses `{name}` not `{id}`
- Networks use `network.Summary` (which aliases `network.Inspect`) — the list and inspect types are identical in the Docker SDK
- Secret data is always cleared before API responses (`sec.Spec.Data = nil`) — in list, detail, stack detail, and search endpoints
- Config data is returned base64-encoded (from Docker SDK); frontend decodes with `atob()`
- No authentication — designed to run behind a reverse proxy
- pprof endpoints are opt-in via `CETACEAN_PPROF=true`, exposed at `/debug/pprof/` (registered without method prefix to support POST for `go tool pprof`)
- Global search (`GET /api/search?q=&limit=`) searches names, images, labels across all resource types. `limit=0` returns all results; default is 3 per type. Response includes optional `state` field for services (derived from running/desired tasks + UpdateStatus) and tasks
- Task list endpoints return `EnrichedTask` with `ServiceName` and `NodeHostname` populated from cache cross-references
- Log-tail SSE endpoints have a 128-connection limit (returns 503 when exceeded)
- `since`/`until` log params are validated server-side; invalid values return 400

## Frontend Patterns
- **List pages**: `useSwarmResource` hook + `DataTable`/`ResourceCard` with `onRowClick`/`to` for navigation
- **Detail pages**: `useState` + `useCallback` fetch + `useSSE` subscription that re-fetches on matching events
- **URL as state**: search (`?q=`), sort (`?sort=&dir=`), metrics range (`?range=`) all URL-persisted
- **View persistence**: table/grid toggle saved in localStorage per resource type via `useViewMode`
- **Error tiers**: `FetchError` (page-level), `ErrorBoundary` (wraps MetricsPanel/LogViewer), component-internal error states
- **Global search**: `Cmd+K` opens `SearchPalette` command palette; `/search?q=` for full-page results. Palette polls every 2s for live state updates (orbs/spinners) but preserves result order to avoid UI jank. Results grouped by type in fixed order: services > stacks > nodes > tasks > configs > secrets > networks > volumes
- **Cross-references**: detail pages show "Used by Services" tables with links; configs/secrets link to stacks via label
- Structured logging throughout backend via `log/slog`

## Known Pre-existing Issues
None — all previously known test failures have been fixed.

## Design Documents
Design specs and implementation plans are in `docs/plans/`. Key recent ones:
- `2026-03-10-global-search-design.md` — Global search feature design
- `2026-03-10-global-search.md` — Global search implementation plan
