# Cetacean

A lightweight, read-only observability platform for Docker Swarm Mode clusters. Single Go binary with an embedded React frontend. Replaces heavyweight tools like SwarmPit with a focused monitoring experience.

## Features

- **Real-time cluster overview** -- node health, task states, service/stack counts, all updated live via SSE
- **Node monitoring** -- status, role, availability, engine version, OS, labels, manager status, with historical CPU/memory/disk/network charts via Prometheus
- **Service management** -- replica counts, images, update status, ports, task lists with error details and exit codes
- **Service logs** -- stream and view logs directly from the Docker daemon
- **Stack visibility** -- aggregate view of all resources in a stack (services, configs, secrets, networks, volumes) with resolved names and links
- **Task debugging** -- task state, error messages, container exit codes, timestamps, failed task highlighting
- **Search and filtering** -- search bar on every list page, server-side `?search=` filtering
- **Prometheus metrics** -- pre-built charts for node and service resources with configurable time ranges (1h/6h/24h/7d) and auto-refresh
- **Mobile responsive** -- hamburger menu, responsive grids, horizontally scrollable tables
- **Live connection indicator** -- green/red SSE status dot in the navbar
- **Batteries-included deployment** -- ships with Prometheus, cAdvisor, and node-exporter in the Docker Compose stack

## Architecture

```
                ┌──────────────┐
                │   Browser    │
                └──────┬───────┘
                       │ HTTP
                ┌──────▼───────┐
                │  Go Server   │
                │              │
                │  /*        → Embedded React SPA
                │  /api/*    → REST endpoints
                │  /api/events → SSE stream
                └──┬───────┬──┘
                   │       │
          ┌────────▼┐  ┌──▼──────────┐
          │ Docker   │  │ Prometheus   │
          │ Socket   │  │ Server       │
          └──────────┘  └─────────────┘
```

The Docker watcher connects to the Docker socket, performs a full sync on startup, then subscribes to the event stream for incremental updates. All state is held in a thread-safe in-memory cache. REST handlers serve cache data as JSON. SSE broadcasts cache mutations to connected frontends in real time. Prometheus queries are proxied through the backend to keep Prometheus unexposed to browsers.

## Quick Start

### Prerequisites

- Docker with Swarm initialized (`docker swarm init`)
- Prometheus server (included in the Compose stack)

### Deploy as a Swarm Stack

```bash
# Build the image
docker build -t cetacean:latest .

# Deploy the full stack (cetacean + prometheus + cadvisor + node-exporter)
docker stack deploy -c docker-compose.yml cetacean
```

The dashboard is available at `http://<manager-node>:9000`.

### Run Locally (Development)

```bash
# Backend
export CETACEAN_PROMETHEUS_URL=http://localhost:9090
go run .

# Frontend (separate terminal)
cd frontend
npm install
npm run dev
```

The Vite dev server at `http://localhost:5173` proxies `/api` requests to the Go backend on port 9000.

### Build from Source

```bash
cd frontend && npm install && npm run build && cd ..
go build -o cetacean .
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `CETACEAN_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon socket |
| `CETACEAN_PROMETHEUS_URL` | *(required)* | Prometheus base URL |
| `CETACEAN_LISTEN_ADDR` | `:9000` | HTTP listen address |

## API

All endpoints are `GET` and return JSON unless noted.

### Cluster

| Endpoint | Description |
|----------|-------------|
| `GET /api/cluster` | Cluster snapshot: node/service/task/stack counts, task state breakdown, node health |

### Resources

| Endpoint | Description |
|----------|-------------|
| `GET /api/nodes` | All nodes |
| `GET /api/nodes/{id}` | Node detail |
| `GET /api/nodes/{id}/tasks` | Tasks running on a node |
| `GET /api/services` | All services |
| `GET /api/services/{id}` | Service detail |
| `GET /api/services/{id}/tasks` | Tasks for a service |
| `GET /api/services/{id}/logs?tail=200` | Service logs (plain text) |
| `GET /api/tasks` | All tasks |
| `GET /api/tasks/{id}` | Task detail |
| `GET /api/stacks` | All stacks (derived from `com.docker.stack.namespace` labels) |
| `GET /api/stacks/{name}` | Stack detail with resource IDs |
| `GET /api/configs` | All configs |
| `GET /api/secrets` | All secrets (metadata only, never values) |
| `GET /api/networks` | All networks |
| `GET /api/volumes` | All volumes |

List endpoints support `?search=<term>` for case-insensitive name filtering.

### Metrics

| Endpoint | Description |
|----------|-------------|
| `GET /api/metrics/query?query=...` | Proxied Prometheus instant query |
| `GET /api/metrics/query_range?query=...&start=...&end=...&step=...` | Proxied Prometheus range query |

### SSE

| Endpoint | Description |
|----------|-------------|
| `GET /api/events?types=node,service,task` | Server-Sent Events stream of state changes |

Event format:
```
event: service
data: {"type":"service","action":"update","id":"abc123","resource":{...}}
```

## Frontend Pages

| Route | View |
|-------|------|
| `/` | Cluster Overview -- live stat cards with task/node health breakdown |
| `/nodes` | Node list with search, status badges, links to detail |
| `/nodes/:id` | Node detail -- attributes, running tasks, CPU/memory/disk/network charts |
| `/stacks` | Stack list with resource counts |
| `/stacks/:name` | Stack detail -- services (linked), configs, secrets, networks, volumes |
| `/services` | Service list with image, mode, replicas, update status |
| `/services/:id` | Service detail -- spec, tasks with error info, logs viewer, CPU/memory charts |
| `/configs` | Config list |
| `/secrets` | Secret list (metadata only) |
| `/networks` | Network list |
| `/volumes` | Volume list |

## Project Structure

```
cetacean/
├── main.go                          # Entrypoint: config, wiring, embedded SPA, server
├── internal/
│   ├── config/config.go             # Environment variable parsing
│   ├── cache/cache.go               # Thread-safe in-memory state store
│   ├── docker/
│   │   ├── client.go                # Docker API client wrapper
│   │   └── watcher.go               # Event stream, full sync, reconnect
│   └── api/
│       ├── router.go                # Route registration, security headers
│       ├── handlers.go              # REST endpoint handlers with search
│       ├── sse.go                   # SSE broadcaster (256 max clients)
│       ├── prometheus.go            # Prometheus query proxy (path-restricted)
│       └── spa.go                   # SPA file server with index.html fallback
├── frontend/                        # React SPA (Vite + TypeScript)
│   └── src/
│       ├── api/                     # API client, TypeScript types
│       ├── hooks/                   # useSSE, useSwarmResource, useMetrics
│       ├── components/              # SearchInput, ConnectionStatus, charts, shadcn/ui
│       └── pages/                   # All route pages
├── Dockerfile                       # Multi-stage build (Node + Go + Alpine)
├── docker-compose.yml               # Swarm stack with Prometheus, cAdvisor, node-exporter
└── prometheus.yml                   # Prometheus config with Swarm service discovery
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.25, stdlib `net/http`, Docker Engine API |
| Frontend | React 19, TypeScript, Vite |
| Styling | Tailwind CSS v4, shadcn/ui |
| Charts | uPlot |
| Metrics | Prometheus (proxied) |
| Real-time | Server-Sent Events |
| Container metrics | cAdvisor (global service) |
| Host metrics | Node Exporter (global service) |

## Security

- **Read-only** -- no write operations against the Docker API
- **Secrets safe** -- Docker secret values are never exposed, metadata only
- **Prometheus proxy restricted** -- only `/query` and `/query_range` paths allowed, 10MB response limit, 30s timeout
- **Security headers** -- `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`
- **SSE limits** -- max 256 concurrent clients, 503 when exceeded
- **Non-root container** -- runs as unprivileged `cetacean` user
- **Auth-unaware** -- designed to run behind a reverse proxy (OIDC, Tailscale, etc.) for authentication

## Required Prometheus Exporters

The included `docker-compose.yml` deploys these automatically:

| Exporter | Deployed As | Provides |
|----------|-------------|----------|
| cAdvisor | Global service (every node) | Container CPU, memory, network, disk I/O |
| Node Exporter | Global service (every node) | Host CPU, memory, disk, network, filesystem |

Prometheus discovers targets via `dockerswarm_sd_configs`. No manual target management needed.

## License

MIT
