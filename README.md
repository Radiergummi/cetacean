<p align="center">
  <h1 align="center">Cetacean</h1>
  <p align="center">
    A real-time observability dashboard for Docker Swarm clusters.<br>
    Single binary. Zero dependencies. Instant visibility.
  </p>
  <p align="center">
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPLv3-blue.svg" alt="License"></a>
    <img src="https://img.shields.io/badge/go-1.26+-00ADD8.svg" alt="Go 1.26+">
    <img src="https://img.shields.io/badge/react-19-61DAFB.svg" alt="React 19">
    <img src="https://img.shields.io/badge/docker-swarm-2496ED.svg" alt="Docker Swarm">
  </p>
</p>

<!-- TODO: Replace with actual screenshots
<p align="center">
  <img src="docs/screenshots/overview.png" alt="Cluster Overview" width="800">
</p>
-->

## Why Cetacean?

Most Docker Swarm tools try to do everything — manage services, deploy stacks, handle registries, run terminals.
Cetacean does one thing well: **let you see what's happening in your swarm.**

```bash
docker build -t cetacean:latest .
docker stack deploy -c docker-compose.yml cetacean
# Open http://<manager>:9000 — done.
```

No database. No agents on worker nodes. No authentication server. One binary on one manager node, and you have full
visibility into every resource in your cluster with live updates.

|                       | Portainer          | Swarmpit        | Cetacean       |
|-----------------------|--------------------|-----------------|----------------|
| **Purpose**           | Full management    | Full management | Observability  |
| **Deploy complexity** | DB + agents + auth | CouchDB + agent | Single binary  |
| **Read ops**          | Yes                | Yes             | Yes            |
| **Write ops**         | Yes                | Yes             | No (by design) |
| **Real-time updates** | Polling            | Polling         | SSE push       |
| **Metrics**           | Built-in           | Built-in        | Prometheus     |

Cetacean is for teams that manage their swarm via CLI or CI/CD and need a dashboard to **understand** the cluster, not
control it.

## Features

<!-- TODO: Add screenshots alongside each section for visual impact -->

### Cluster Overview

Live health dashboard with node, service, task, and stack counts. Trend indicators show changes since last sync.
Activity feed of recent events. At a glance: is your cluster healthy?

### Browse Everything

Every Docker Swarm resource type is browsable with full detail pages and cross-references between them:

- **Nodes** — sortable list with live CPU/memory/disk gauges; detail pages with resource charts, task tables, and
  activity history
- **Services** — full configuration inspection including container config, env vars, healthchecks, labels, ports,
  mounts, configs, secrets, deploy/update/rollback settings, task lists with state filtering, CPU/memory charts, and
  live logs
- **Stacks** — derived from `com.docker.stack.namespace` labels; shows all member services, configs, secrets, networks,
  and volumes
- **Tasks** — global and per-service/per-node views with status history
- **Configs & Secrets** — detail pages with "used by" cross-references (secret values are never exposed)
- **Networks & Volumes** — IPAM configuration, driver details, service cross-references

### Log Viewer

- Live SSE streaming or batch fetch with time range presets
- Regex search with inline highlighting
- stdout/stderr stream filtering
- Automatic JSON formatting with log level color coding
- Copy to clipboard, download as file

### Topology

- **Logical view**: service-to-service connections through overlay networks with ELK.js orthogonal routing
- **Physical view**: which services run on which nodes in a grid layout

### Metrics

Optional Prometheus integration for time-series charts:

- **Node**: CPU %, memory %, disk I/O, network I/O with live gauges
- **Service**: CPU and memory with limit/reservation threshold lines
- **Stack**: aggregated CPU and memory across all services
- Configurable time ranges (1h, 6h, 24h, 7d) with auto-refresh

### Real-Time Updates

Per-resource SSE connections push changes to the browser as they happen. List and detail endpoints each accept
`Accept: text/event-stream` for a filtered event stream scoped to that resource type or individual resource.
List pages perform optimistic in-place updates without full reloads. Connection status indicator shows you're always
current.

## Quick Start

### Docker Swarm (recommended)

Deploy the full observability stack — Cetacean + Prometheus + cAdvisor + Node Exporter:

```bash
docker build -t cetacean:latest .
docker stack deploy -c docker-compose.yml cetacean
```

Open `http://<manager-node>:9000`. Metrics will populate automatically as cAdvisor and Node Exporter start reporting.

### Without Metrics

Cetacean works without Prometheus — metrics panels simply won't appear:

```bash
docker build -t cetacean:latest .

docker service create \
  --name cetacean \
  --constraint node.role==manager \
  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
  --publish 9000:9000 \
  cetacean:latest
```

### From Source

Requires Go 1.26+ and Node.js 22+:

```bash
cd frontend && npm install && npm run build && cd ..
go build -o cetacean .
./cetacean
```

Set `CETACEAN_PROMETHEUS_URL` to enable metrics, or omit it to run without.

## Configuration

All configuration is via environment variables:

| Variable                      | Default                       | Description                                       |
|-------------------------------|-------------------------------|---------------------------------------------------|
| `CETACEAN_PROMETHEUS_URL`     | *(none)*                      | Prometheus server URL (metrics disabled if unset) |
| `CETACEAN_DOCKER_HOST`        | `unix:///var/run/docker.sock` | Docker socket path or TCP address                 |
| `CETACEAN_LISTEN_ADDR`        | `:9000`                       | HTTP listen address                               |
| `CETACEAN_LOG_LEVEL`          | `info`                        | Log level: `debug`, `info`, `warn`, `error`       |
| `CETACEAN_LOG_FORMAT`         | `json`                        | Log format: `json` or `text`                      |
| `CETACEAN_DATA_DIR`           | `./data`                      | Directory for snapshot persistence                |
| `CETACEAN_SNAPSHOT`           | `true`                        | Enable/disable disk snapshots                     |
| `CETACEAN_SSE_BATCH_INTERVAL` | `100ms`                       | SSE event batching window                         |
| `CETACEAN_PPROF`              | `false`                       | Enable pprof endpoints at `/debug/pprof/`         |

## Deployment Notes

- **Must run on a manager node** — Cetacean reads swarm state via the Docker socket, which is only available on managers
- **No authentication built in** — designed to run behind a reverse proxy (Traefik, Caddy, nginx) with your auth layer (
  OIDC, mTLS, Tailscale, etc.)
- **Read-only** — Cetacean never writes to the Docker API, so it can't accidentally modify your cluster
- **Disk snapshots** — on each sync, state is written to `data/snapshot.json` so the dashboard shows cached data
  immediately on restart while the first full sync completes

## Architecture

```
                    +----------------+
                    |    Browser     |
                    +-------+--------+
                            | HTTP / SSE
                    +-------v--------+
                    |   Go Server    |
                    |                |
                    |  /            -> Embedded React SPA
                    |  /{resource}  -> REST + SSE (read-only)
                    |  /events      -> Global SSE stream
                    |  /-/metrics/  -> Prometheus proxy
                    +---+--------+--+
                        |        |
               +--------v-+  +--v-----------+
               |  Docker   |  |  Prometheus   |
               |  Socket   |  |  (optional)   |
               +-----------+  +--------------+
```

### How It Works

1. **Docker Watcher** connects to the Docker socket, performs a full sync on startup (7 parallel goroutines), then
   subscribes to the event stream. Events are debounced (50ms) and inspected via a 4-worker pool. Re-syncs every 5
   minutes and on reconnect.

2. **State Cache** holds all swarm state in memory with `sync.RWMutex`-protected maps. Stacks are derived from labels
   and rebuilt on mutation. Every change fires callbacks that feed SSE.

3. **SSE Broadcaster** fans out events to up to 256 browser clients. Each list/detail endpoint can serve a filtered SSE
   stream scoped to that resource type or ID. Events include the full resource payload for optimistic client-side
   updates. Events are batched per-client (default 100ms). Slow clients get events dropped rather than backpressuring.

4. **REST API** serves cache state as JSON. All read-only. List endpoints support search, sort, pagination,
   and [expr](https://github.com/expr-lang/expr) filter expressions.

5. **Prometheus Proxy** forwards `/query` and `/query_range` to your Prometheus instance (10MB limit, 30s timeout),
   keeping it unexposed to browsers.

## API Reference

All endpoints are `GET` and return JSON.

### Resources

All list and detail endpoints support content negotiation: `Accept: application/json` for JSON, `Accept: text/event-stream` for per-resource SSE streaming, or browser `Accept` for the SPA.

| Endpoint                    | Description                                              |
|-----------------------------|----------------------------------------------------------|
| `GET /cluster`              | Cluster snapshot: counts, task states, CPU/memory totals |
| `GET /cluster/metrics`      | Cluster-wide Prometheus metrics                          |
| `GET /swarm`                | Swarm inspect info (join tokens, raft config, CA)        |
| `GET /disk-usage`           | Docker system disk usage                                 |
| `GET /plugins`              | Installed Docker plugins                                 |
| `GET /nodes`                | List nodes                                               |
| `GET /nodes/{id}`           | Node detail with cross-referenced tasks                  |
| `GET /nodes/{id}/tasks`     | Tasks running on a node                                  |
| `GET /services`             | List services                                            |
| `GET /services/{id}`        | Service detail                                           |
| `GET /services/{id}/tasks`  | Tasks for a service                                      |
| `GET /services/{id}/logs`   | Service logs (JSON or SSE via `Accept` header)           |
| `GET /tasks`                | List tasks                                               |
| `GET /tasks/{id}`           | Task detail                                              |
| `GET /tasks/{id}/logs`      | Task logs (JSON or SSE)                                  |
| `GET /stacks`               | List stacks                                              |
| `GET /stacks/{name}`        | Stack detail with all member resources                   |
| `GET /stacks/summary`       | Stack summary (service/task counts per stack)            |
| `GET /configs`              | List configs                                             |
| `GET /configs/{id}`         | Config detail with cross-referenced services             |
| `GET /secrets`              | List secrets (metadata only)                             |
| `GET /secrets/{id}`         | Secret detail with cross-referenced services             |
| `GET /networks`             | List networks                                            |
| `GET /networks/{id}`        | Network detail with cross-referenced services            |
| `GET /volumes`              | List volumes                                             |
| `GET /volumes/{name}`       | Volume detail with cross-referenced services             |
| `GET /search`               | Global cross-resource search (`?q=`, `?limit=`)         |

### Infrastructure

| Endpoint                    | Description                                             |
|-----------------------------|---------------------------------------------------------|
| `GET /events`               | Global SSE stream (filter with `?types=node,service,…`) |
| `GET /history`              | Event history (`?type=`, `?resourceId=`, `?limit=`)     |
| `GET /-/health`             | Health check                                            |
| `GET /-/ready`              | Readiness (503 until first sync completes)              |
| `GET /-/metrics/status`     | Monitoring auto-detection status                        |
| `GET /-/metrics/query`      | Prometheus instant query proxy                          |
| `GET /-/metrics/query_range`| Prometheus range query proxy                            |
| `GET /topology/networks`    | Service-to-service network topology                     |
| `GET /topology/placement`   | Node-to-service placement topology                      |
| `GET /api`                  | OpenAPI spec (JSON) / Scalar playground (HTML)          |
| `GET /api/context.jsonld`   | JSON-LD context document                                |
### Query Parameters

List endpoints:

- `?search=` — case-insensitive name search
- `?sort=<field>&dir=asc|desc` — sorting
- `?limit=N&offset=N` — pagination
- `?filter=<expr>` — filter expressions (e.g., `status == "ready" && role == "manager"`)

Log endpoints:

- `?limit=N` — line count (JSON mode)
- `?after=<timestamp>&before=<timestamp>` — time range (RFC3339 or Go duration)
- `?stream=stdout|stderr` — stream filter
- `Accept: text/event-stream` — switches to live streaming mode

## Development

Cetacean needs a Docker Swarm to connect to. The easiest way to develop locally is to init a single-node swarm on your
machine:

```bash
docker swarm init
```

Then run the Go backend and Vite dev server side by side:

```bash
# Terminal 1: Go backend (connects to local Docker socket)
go run .

# Terminal 2: Frontend dev server (hot reload, proxies resource paths to :9000)
cd frontend && npm install && npm run dev
```

Open `http://localhost:5173`. The Vite dev server proxies resource paths (e.g. `/nodes`, `/services`, `/events`) to the
Go backend on `:9000`, so you get frontend hot-reload and live backend data. Changes to Go code require restarting `go run .`; frontend changes apply
instantly.

```bash
make check          # lint + format check + test
make test           # go test ./...
make lint           # golangci-lint + oxlint
make fmt            # gofmt + oxfmt
```

## Tech Stack

**Backend**: Go 1.26, stdlib `net/http`, Docker Engine API,
`log/slog`, [expr](https://github.com/expr-lang/expr), [goccy/go-json](https://github.com/goccy/go-json)

**Frontend**: React 19, TypeScript, Vite, Tailwind CSS v4,
shadcn/ui, [uPlot](https://github.com/leeoniya/uPlot), [React Flow](https://reactflow.dev/) + [ELK.js](https://github.com/kieler/elkjs), [@tanstack/react-virtual](https://tanstack.com/virtual)

**Monitoring
**: [Prometheus](https://prometheus.io/), [cAdvisor](https://github.com/google/cadvisor), [Node Exporter](https://github.com/prometheus/node_exporter)

## Security

- **Read-only**: no write operations against the Docker API
- **Secrets safe**: secret values are never exposed in API responses
- **Prometheus proxy restricted**: only `/query` and `/query_range` paths, 10MB limit, 30s timeout
- **Security headers**: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`
- **Connection limits**: max 256 SSE clients, max 128 concurrent log streams
- **No auth built in**: run behind a reverse proxy for authentication

## License

[GNU General Public License v3.0](LICENSE)
