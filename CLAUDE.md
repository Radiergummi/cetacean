# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Cetacean is a read-only observability dashboard for Docker Swarm Mode clusters. Single Go binary with an embedded React SPA. Connects to the Docker socket, caches all swarm state in memory, and pushes updates to browsers via SSE.

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

## Architecture

### Data flow
Docker Socket → `docker/watcher.go` (full sync + event stream) → `cache/cache.go` (in-memory maps, `sync.RWMutex`) → `api/handlers.go` (REST JSON) + `api/sse.go` (real-time broadcast) → Browser

### Backend (`internal/`)
- **`config/`** — Env var parsing. `CETACEAN_PROMETHEUS_URL` is the only required var.
- **`cache/`** — Thread-safe in-memory store using `sync.RWMutex`. Holds nodes, services, tasks, configs, secrets, networks, volumes. Stacks are derived from `com.docker.stack.namespace` labels and rebuilt on every mutation to services/configs/secrets/networks/volumes. Every Set/Delete fires an `OnChangeFunc` callback that feeds the SSE broadcaster.
- **`docker/client.go`** — Thin wrapper over the Docker Engine API. List, Inspect, and Events methods.
- **`docker/watcher.go`** — Full sync on startup (7 parallel goroutines), then subscribes to Docker event stream. Re-syncs every 5 minutes and on reconnect. Container events are mapped to task updates via `com.docker.swarm.task.id` attribute.
- **`api/router.go`** — stdlib `net/http.ServeMux` with Go 1.22+ method routing (`"GET /api/..."`) . SPA fallback is registered last on `/`.
- **`api/handlers.go`** — REST handlers. All read-only, serve cache data as JSON. List endpoints support `?search=` for case-insensitive name filtering. `DockerLogStreamer` interface decouples log streaming for testability.
- **`api/sse.go`** — `Broadcaster` manages up to 256 SSE clients. Clients can filter by `?types=node,service,task`. Slow clients get events dropped (non-blocking send to buffered channel).
- **`api/prometheus.go`** — Reverse proxy to Prometheus, only allows `/query` and `/query_range` paths. 10MB response limit, 30s timeout.
- **`api/spa.go`** — Serves the embedded `frontend/dist/` filesystem with index.html fallback for client-side routing.

### Frontend (`frontend/src/`)
- React 19 + TypeScript + Vite, Tailwind CSS v4, shadcn/ui components, uPlot for time-series charts
- **`api/client.ts`** — Fetch wrapper for all API endpoints
- **`api/types.ts`** — TypeScript types matching the Go JSON responses
- **`hooks/SSEContext.tsx`** — Shared SSE provider (single EventSource connection for the whole app)
- **`hooks/useSSE.ts`** — Re-exports `useSSESubscribe` from SSEContext
- **`hooks/useSwarmResource.ts`** — Generic fetch + SSE subscription hook for resource lists/details
- **`components/`** — SearchInput, ConnectionStatus, InfoCard, TaskStatusBadge, MetricsPanel, TimeSeriesChart, plus shadcn/ui primitives
- **`pages/`** — One page per route: ClusterOverview, NodeList/Detail, ServiceList/Detail, StackList/Detail, ConfigList, SecretList, NetworkList, VolumeList
- Path alias: `@/` maps to `frontend/src/`

### Embedding
`main.go` uses `//go:embed frontend/dist/*` to embed the built frontend into the Go binary. The frontend must be built before `go build`.

## Key Conventions
- All API endpoints are GET-only (read-only system)
- Uses Docker Engine API types directly (e.g., `swarm.Node`, `swarm.Service`) — no separate domain models
- Stacks are a derived concept from `com.docker.stack.namespace` labels, not a Docker primitive
- Volumes are keyed by Name, everything else by ID
- No authentication — designed to run behind a reverse proxy
