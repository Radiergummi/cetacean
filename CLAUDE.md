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
go run .                                  # Run locally (needs CETACEAN_PROMETHEUS_URL set)
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
```

### Environment variables
| Variable | Default | Required |
|---|---|---|
| `CETACEAN_PROMETHEUS_URL` | — | Yes |
| `CETACEAN_DOCKER_HOST` | `unix:///var/run/docker.sock` | No |
| `CETACEAN_LISTEN_ADDR` | `:9000` | No |
| `CETACEAN_LOG_FORMAT` | `json` | No |
| `CETACEAN_LOG_LEVEL` | `info` | No |
| `CETACEAN_SSE_BATCH_INTERVAL` | `100ms` | No |

## Architecture

### Data flow
Docker Socket → `docker/watcher.go` (full sync + event stream) → `cache/cache.go` (in-memory maps, `sync.RWMutex`) → `api/handlers.go` (REST JSON) + `api/sse.go` (real-time broadcast) → Browser

### Backend (`internal/`)
- **`config/`** — Env var parsing. `CETACEAN_PROMETHEUS_URL` is the only required var.
- **`cache/`** — Thread-safe in-memory store using `sync.RWMutex`. Holds nodes, services, tasks, configs, secrets, networks, volumes. Stacks are derived from `com.docker.stack.namespace` labels and rebuilt on every mutation. Cross-reference methods (`ServicesUsingConfig`, `ServicesUsingSecret`, `ServicesUsingNetwork`, `ServicesUsingVolume`) scan services to find which ones use a given resource. Every Set/Delete fires an `OnChangeFunc` callback that feeds the SSE broadcaster.
- **`cache/history.go`** — Ring buffer (10,000 entries) of resource change events, queryable by type/resourceId.
- **`cache/snapshot.go`** — Atomic disk persistence with versioned JSON format.
- **`docker/client.go`** — Thin wrapper over the Docker Engine API. List, Inspect, and Events methods for all resource types.
- **`docker/watcher.go`** — Full sync on startup (7 parallel goroutines), then subscribes to Docker event stream. Re-syncs every 5 minutes and on reconnect. Container events are mapped to task updates via `com.docker.swarm.task.id` attribute. 50ms debounce with 4-worker inspect pool.
- **`api/router.go`** — stdlib `net/http.ServeMux` with Go 1.22+ method routing (`"GET /api/..."`). Middleware chain: requestID → recovery → securityHeaders → requestLogger. SPA fallback registered last on `/`.
- **`api/handlers.go`** — REST handlers. All read-only, serve cache data as JSON. List endpoints support `?search=`, `?filter=` (expr-lang expressions), `?sort=`, `?dir=`, `?limit=`, `?offset=`. Detail endpoints for all resource types return the resource + cross-referenced services. `DockerLogStreamer` interface decouples log streaming for testability.
- **`api/sse.go`** — `Broadcaster` manages up to 256 SSE clients. Clients can filter by `?types=node,service,task`. Event batching within configurable interval. Slow clients get events dropped (non-blocking send to buffered channel).
- **`api/prometheus.go`** — Reverse proxy to Prometheus, only allows `/query` and `/query_range` paths. 10MB response limit, 30s timeout.
- **`api/spa.go`** — Serves the embedded `frontend/dist/` filesystem with index.html fallback for client-side routing.
- **`filter/`** — Expression-based filtering using `expr-lang/expr`. Each resource type has an env builder exposing fields for filter expressions.
- **`notify/`** — Webhook notification system with expr-lang rule matching, cooldown, and circuit breaker (5 failures → open, 30s half-open).

### Frontend (`frontend/src/`)
- React 19 + TypeScript + Vite, Tailwind CSS v4, shadcn/ui components, uPlot for time-series charts
- **`api/client.ts`** — Fetch wrapper for all API endpoints. Detail methods: `api.node(id)`, `api.service(id)`, `api.task(id)`, `api.stack(name)`, `api.config(id)`, `api.secret(id)`, `api.network(id)`, `api.volume(name)`.
- **`api/types.ts`** — TypeScript types matching the Go JSON responses. Detail response types bundle the resource + `ServiceRef[]` cross-references.
- **`hooks/SSEContext.tsx`** — Shared SSE provider (single EventSource connection for the whole app). Uses `Map<symbol, listener>` registry. Event types: node, service, task, config, secret, network, volume, stack, batch.
- **`hooks/useSSE.ts`** — Re-exports `useSSESubscribe` from SSEContext. Used by both list pages (via `useSwarmResource`) and detail pages (direct subscription for re-fetch on change).
- **`hooks/useSwarmResource.ts`** — Generic fetch + SSE subscription hook for resource lists. Performs optimistic in-place updates (upsert/remove) without full refetch.
- **`components/`** — Key components:
  - `DataTable` — auto-virtualizes above 100 rows via `@tanstack/react-virtual`
  - `LogViewer` — 800+ line component with live SSE tail, regex search, JSON formatting, stream filtering
  - `TimeSeriesChart` — uPlot canvas with gradient fill, threshold annotations, cursor sync
  - `MetricsPanel` — wraps N charts with shared range picker (1h/6h/24h/7d, URL-persisted)
  - `ResourceGauge` — SVG half-circle gauge with threshold coloring
  - `InfoCard`, `ResourceCard`, `PageHeader`, `FetchError`, `EmptyState`, `LoadingSkeleton`
- **`pages/`** — All resource types have list + detail pages. Detail pages subscribe to SSE for real-time updates. List pages use `useSwarmResource` for SSE-driven optimistic updates.
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
- Secret data is always cleared before API responses (`sec.Spec.Data = nil`)
- Config data is returned base64-encoded (from Docker SDK); frontend decodes with `atob()`
- No authentication — designed to run behind a reverse proxy
- pprof endpoints are always exposed at `/debug/pprof/`

## Frontend Patterns
- **List pages**: `useSwarmResource` hook + `DataTable`/`ResourceCard` with `onRowClick`/`to` for navigation
- **Detail pages**: `useState` + `useCallback` fetch + `useSSE` subscription that re-fetches on matching events
- **URL as state**: search (`?q=`), sort (`?sort=&dir=`), metrics range (`?range=`) all URL-persisted
- **View persistence**: table/grid toggle saved in localStorage per resource type via `useViewMode`
- **Error tiers**: `FetchError` (page-level), `ErrorBoundary` (wraps MetricsPanel/LogViewer), component-internal error states
- **Cross-references**: detail pages show "Used by Services" tables with links; configs/secrets link to stacks via label
- Structured logging throughout backend via `log/slog`

## Known Pre-existing Issues
- `TaskStateFilter.test.tsx` has 3 failing tests (label text changed but tests not updated)
- `topologyTransform.test.ts` has 4 TS errors (`'unknown'` type assertions)
- These are not regressions — they exist on main before any changes
