# Cetacean

A lightweight, real-time observability dashboard for Docker Swarm Mode clusters. Single Go binary with an embedded React frontend. Designed as a focused, read-only alternative to Swarmpit and Portainer for teams that need visibility into their swarm without the overhead of a full management platform.

Cetacean connects to the Docker socket, caches all swarm state in memory, and pushes live updates to browsers via Server-Sent Events (SSE). Prometheus integration provides resource metrics and time-series charts.

## Features

### Cluster Overview
- Live cluster health dashboard with node, service, task, and stack counts
- Trend indicators showing count changes since the last sync
- Activity feed of recent cluster events
- Notification rule status display

### Nodes
- Sortable, searchable list with live CPU/memory/disk gauges and CPU sparklines
- Node detail with resource gauges, task table with state filtering, Prometheus charts (CPU %, memory %, disk I/O, network I/O), and activity history

### Services
- Sortable, searchable list with replica mode, image, and update status
- Service detail with full configuration inspection:
  - Container config, environment variables, healthcheck settings
  - Labels, published ports, volume mounts
  - Attached configs and secrets
  - Deploy, update, and rollback configuration
  - Task list with state filter (running, complete, failed, etc.)
  - CPU and memory time-series charts with limit/reservation threshold lines
  - Live log viewer with streaming, search, and filtering
  - Activity history

### Stacks
- Derived from `com.docker.stack.namespace` labels (not a Docker primitive)
- Stack detail shows all member services, configs, secrets, networks, and volumes with task counts

### Log Viewer
- Dual mode: JSON batch fetch or live SSE streaming
- Time range presets (15m, 1h, 6h, 24h) and custom datetime range picker
- stdout/stderr stream filter
- Case-sensitive and regex search with inline match highlighting
- Automatic JSON formatting and log level detection with color coding
- Copy to clipboard and download as file
- Configurable line limits (100--5000)
- Auto-scroll with "jump to bottom" button

### Topology
- Logical view: service-to-service connections through overlay networks, rendered with ELK.js orthogonal edge routing
- Physical view: 3-column grid showing which services run on which nodes

### Additional Resources
- Configs, secrets, networks, and volumes lists with search and sort
- Table and grid view toggle on all list pages

### Real-Time Updates
- Single SSE connection shared across the entire app
- Granular in-place updates on list pages without full refetches
- Connection status indicator with last-event timestamp
- Automatic reconnection with full state re-sync on the backend

### Metrics
- Prometheus reverse proxy keeps Prometheus unexposed to browsers
- Pre-built charts for node and service resources
- Configurable time ranges (1h, 6h, 24h, 7d) with 30s auto-refresh toggle
- Cursor sync across charts within a panel

### Notification Webhooks
- Configurable rules with [expr](https://github.com/expr-lang/expr) expressions
- Per-rule cooldown periods
- Circuit breaker (opens after 5 consecutive failures, half-open retry every 30s)

## Architecture

```
                    +----------------+
                    |    Browser     |
                    +-------+--------+
                            | HTTP / SSE
                    +-------v--------+
                    |   Go Server    |
                    |                |
                    |  /           -> Embedded React SPA
                    |  /api/*     -> REST endpoints (read-only)
                    |  /api/events -> SSE stream
                    |  /api/metrics -> Prometheus proxy
                    +---+--------+--+
                        |        |
               +--------v-+  +--v-----------+
               |  Docker   |  |  Prometheus   |
               |  Socket   |  |  Server       |
               +-----------+  +--------------+
```

### How It Works

1. **Docker Watcher** connects to the Docker socket, performs a full sync of all swarm resources on startup (7 parallel goroutines), then subscribes to the Docker event stream for incremental updates. Events are debounced (50ms window) and coalesced, then inspected via a 4-worker pool. Periodic full re-sync every 5 minutes as a safety net. Automatic reconnection with full re-sync on event stream disconnection.

2. **State Cache** holds all swarm state in memory using `sync.RWMutex`-protected maps indexed by resource ID. Stacks are derived from `com.docker.stack.namespace` labels and rebuilt on every mutation. Every mutation fires a callback that feeds both the SSE broadcaster and the notification system.

3. **SSE Broadcaster** fans out change events to up to 256 connected browser clients. Events are batched per-client on a configurable interval (default 100ms). Clients can filter by resource type via `?types=`. Slow clients get events dropped rather than backpressuring the server.

4. **REST API** serves current cache state as JSON. All endpoints are read-only. List endpoints support search, sort, pagination, and filter expressions.

5. **Prometheus Proxy** forwards `/query` and `/query_range` requests to the configured Prometheus instance. 10MB response limit, 30s timeout.

6. **SPA Server** serves the embedded React build via `embed.FS` with `index.html` fallback for client-side routing.

### Disk Snapshots

On each sync, the cache is atomically written to disk (`data/snapshot.json`) so the dashboard can show cached data immediately on restart while the first full sync completes. Disable with `CETACEAN_SNAPSHOT=false`.

## Quick Start

### Docker Swarm (recommended)

The included `docker-compose.yml` deploys Cetacean with Prometheus, cAdvisor, and Node Exporter as a complete observability stack:

```bash
# Build the image
docker build -t cetacean:latest .

# Deploy the full stack (requires Docker Swarm mode)
docker stack deploy -c docker-compose.yml cetacean
```

Cetacean will be available at `http://<manager-node>:9000`.

The stack includes:
- **Cetacean** on the manager node (needs Docker socket access)
- **Prometheus** with DNS-based service discovery for cAdvisor and Node Exporter
- **cAdvisor** (global mode) for container resource metrics
- **Node Exporter** (global mode) for host-level metrics

### From Source

Requires Go 1.26+ and Node.js 22+:

```bash
# Build frontend
cd frontend && npm install && npm run build && cd ..

# Build binary
go build -o cetacean .

# Run (Prometheus URL is required)
CETACEAN_PROMETHEUS_URL=http://localhost:9090 ./cetacean
```

### Development

```bash
# Terminal 1: Frontend dev server (hot reload, proxies /api to :9000)
cd frontend && npm install && npm run dev

# Terminal 2: Go backend
CETACEAN_PROMETHEUS_URL=http://localhost:9090 go run .
```

The Vite dev server runs on `:5173` and proxies API requests to the Go backend on `:9000`.

**Useful commands:**

```bash
make check          # lint + format check + test (run before committing)
make test           # go test ./...
make lint           # golangci-lint + oxlint
make fmt            # gofmt + oxfmt (write)
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `CETACEAN_PROMETHEUS_URL` | *(required)* | Prometheus server URL |
| `CETACEAN_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket path or TCP address |
| `CETACEAN_LISTEN_ADDR` | `:9000` | HTTP listen address |
| `CETACEAN_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `CETACEAN_LOG_FORMAT` | `json` | Log format: `json` or `text` |
| `CETACEAN_DATA_DIR` | `./data` | Directory for snapshot persistence |
| `CETACEAN_SNAPSHOT` | `true` | Enable/disable disk snapshots |
| `CETACEAN_NOTIFICATIONS_FILE` | *(none)* | Path to notification webhook rules YAML |
| `CETACEAN_SSE_BATCH_INTERVAL` | `100ms` | SSE event batching window |

## API

All endpoints are `GET` and return JSON unless noted.

### Resources

| Endpoint | Description |
|---|---|
| `GET /api/cluster` | Cluster snapshot: counts, task states, CPU/memory totals |
| `GET /api/nodes` | List nodes |
| `GET /api/nodes/{id}` | Node detail |
| `GET /api/nodes/{id}/tasks` | Tasks running on a node |
| `GET /api/services` | List services |
| `GET /api/services/{id}` | Service detail |
| `GET /api/services/{id}/tasks` | Tasks for a service |
| `GET /api/services/{id}/logs` | Service logs (JSON or SSE depending on `Accept` header) |
| `GET /api/tasks` | List tasks |
| `GET /api/tasks/{id}` | Task detail |
| `GET /api/tasks/{id}/logs` | Task logs (JSON or SSE) |
| `GET /api/stacks` | List stacks (derived from labels) |
| `GET /api/stacks/{name}` | Stack detail with all member resources |
| `GET /api/configs` | List configs |
| `GET /api/secrets` | List secrets (metadata only, never values) |
| `GET /api/networks` | List networks |
| `GET /api/volumes` | List volumes |

### Infrastructure

| Endpoint | Description |
|---|---|
| `GET /api/events` | SSE stream (filter with `?types=node,service,...`) |
| `GET /api/health` | Health check (always 200) |
| `GET /api/ready` | Readiness check (503 until first sync completes) |
| `GET /api/history` | Event history (`?type=`, `?resourceId=`, `?limit=`) |
| `GET /api/metrics/query` | Prometheus instant query proxy |
| `GET /api/metrics/query_range` | Prometheus range query proxy |
| `GET /api/topology/networks` | Service-to-service network topology |
| `GET /api/topology/placement` | Node-to-service placement topology |
| `GET /api/notifications/rules` | Notification rule statuses |

### Query Parameters

List endpoints support:
- `?search=` -- case-insensitive name search (server-side)
- `?sort=<field>&dir=asc|desc` -- sorting (fields vary by resource)
- `?limit=N&offset=N` -- pagination
- `?filter=<expr>` -- filter expressions (e.g., `status == "ready" && role == "manager"`)

Log endpoints support:
- `?limit=N` -- number of log lines (JSON mode)
- `?before=<timestamp>&after=<timestamp>` -- time range
- `?stream=stdout|stderr` -- stream filter
- `Accept: text/event-stream` header switches to live streaming mode

### SSE Event Format

```
event: service
data: {"type":"service","action":"update","id":"abc123","resource":{...}}
```

Batch events (multiple changes within the batching window):

```
event: batch
data: [{"type":"service","action":"update",...},{"type":"task","action":"remove",...}]
```

## Notification Webhooks

Cetacean can send webhook notifications when cluster events match configurable rules. Create a YAML file and set `CETACEAN_NOTIFICATIONS_FILE`:

```yaml
- name: service-failures
  match: type == "task" && action == "update"
  condition: resource.Status.State == "failed"
  webhook: https://hooks.slack.com/services/...
  cooldown: 5m

- name: node-down
  match: type == "node" && action == "update"
  condition: resource.Status.State != "ready"
  webhook: https://hooks.slack.com/services/...
  cooldown: 10m
```

Rules use [expr](https://github.com/expr-lang/expr) expressions evaluated against Docker API types. The webhook system includes a circuit breaker (opens after 5 consecutive failures, half-open retry every 30s) and per-rule cooldown periods to avoid alert fatigue.

## Project Structure

```
cetacean/
├── main.go                          # Entrypoint: config, wiring, embedded SPA, server
├── internal/
│   ├── config/                      # Environment variable parsing
│   ├── cache/
│   │   ├── cache.go                 # Thread-safe in-memory state store
│   │   ├── stacks.go                # Stack derivation from labels
│   │   ├── history.go               # Ring buffer event log (10,000 entries)
│   │   └── snapshot.go              # Atomic disk persistence
│   ├── docker/
│   │   ├── client.go                # Docker API client wrapper
│   │   └── watcher.go               # Event stream, debouncing, worker pool, reconnect
│   ├── api/
│   │   ├── router.go                # Route registration, middleware chain
│   │   ├── handlers.go              # REST handlers with search/sort/filter/paginate
│   │   ├── sse.go                   # SSE broadcaster (256 clients, batching)
│   │   ├── prometheus.go            # Prometheus query proxy (allowlisted paths)
│   │   ├── logparse.go              # Docker multiplex log frame parser
│   │   ├── topology.go              # Network and placement topology computation
│   │   ├── middleware.go            # Request logging, recovery, security headers
│   │   └── spa.go                   # SPA file server with index.html fallback
│   ├── filter/                      # Expression filter compilation and evaluation
│   └── notify/                      # Webhook notifications, circuit breaker, rules
├── frontend/                        # React 19 SPA (Vite + TypeScript)
│   └── src/
│       ├── api/                     # Fetch client, TypeScript types
│       ├── hooks/                   # SSEContext, useSwarmResource, useViewMode
│       ├── components/              # DataTable, LogViewer, TimeSeriesChart, gauges, etc.
│       ├── pages/                   # All route pages
│       └── lib/                     # ELK layout helpers
├── Dockerfile                       # Multi-stage build (Node + Go + Alpine)
├── docker-compose.yml               # Swarm stack with Prometheus, cAdvisor, Node Exporter
└── prometheus.yml                   # Prometheus config with DNS service discovery
```

## Tech Stack

### Backend
- Go 1.26, stdlib `net/http` with Go 1.22+ method routing
- Docker Engine API via `github.com/docker/docker`
- Structured logging via `log/slog`
- [expr](https://github.com/expr-lang/expr) for notification rule evaluation
- [goccy/go-json](https://github.com/goccy/go-json) for fast JSON serialization

### Frontend
- React 19, TypeScript 5.9, Vite 7
- Tailwind CSS v4 with shadcn/ui components
- [uPlot](https://github.com/leeoniya/uPlot) for high-performance time-series charts
- [React Flow](https://reactflow.dev/) + [ELK.js](https://github.com/kieler/elkjs) for topology visualization
- [@tanstack/react-virtual](https://tanstack.com/virtual) for virtualized tables (auto-enabled above 100 rows)

### Monitoring Stack
- [Prometheus](https://prometheus.io/) for metrics storage and PromQL queries
- [cAdvisor](https://github.com/google/cadvisor) (global service) for container resource metrics
- [Node Exporter](https://github.com/prometheus/node_exporter) (global service) for host-level metrics

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Architecture | Single Go binary | Deployment simplicity, single artifact |
| State source | Docker API direct | Authoritative, real-time, all resource types |
| Metrics | Prometheus | Industry standard, PromQL time-series queries |
| Frontend delivery | Embedded via `embed.FS` | Single binary deployment |
| Real-time | SSE (not WebSockets) | Simpler, one-directional, native auto-reconnect |
| Authentication | External | Run behind a reverse proxy with OIDC/mTLS/Tailscale |
| Scope | Read-only | Focused on observability; no accidental destructive actions |

## Security

- **Read-only**: no write operations against the Docker API
- **Secrets safe**: Docker secret values are never exposed, metadata only
- **Prometheus proxy restricted**: only `/query` and `/query_range` paths allowed, 10MB response limit, 30s timeout
- **Security headers**: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`
- **SSE limits**: max 256 concurrent clients, 503 when exceeded
- **Auth-unaware**: designed to run behind a reverse proxy for authentication

## Requirements

- Docker Swarm Mode cluster
- Prometheus with cAdvisor and Node Exporter (included in the Compose stack)
- Deployment on a manager node (needs Docker socket access)
- Go 1.26+ and Node.js 22+ (for building from source)

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).
