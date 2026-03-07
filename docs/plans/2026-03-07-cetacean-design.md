# Cetacean: Docker Swarm Observability Platform — Design Document

Date: 2026-03-07

## Overview

Cetacean is a lightweight, read-only observability platform for Docker Swarm Mode clusters. It replaces heavyweight tools like SwarmPit with a focused monitoring experience that provides real-time insight into cluster state and resource metrics. It does not deploy or manage services.

Cetacean is a single Go binary with an embedded React frontend. It requires two external dependencies: a Docker socket and a Prometheus server.

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Single Go binary, monolithic | Deployment simplicity, sufficient for ops-team-sized user base |
| Cluster state source | Docker API | Authoritative, real-time, supports all resource types |
| Metrics source | Prometheus | Industry standard, time-series queries via PromQL |
| Frontend delivery | Embedded SPA via `embed.FS` | Single binary deployment; Vite dev server in development |
| Real-time updates | SSE (Server-Sent Events) | Simpler than WebSockets, one-directional, auto-reconnect |
| API style | REST + SSE | REST for resource endpoints, SSE for live deltas |
| Prometheus integration | Pre-defined dashboards only | Grafana available for custom queries; avoids scope creep |
| Authentication | External (OIDC / Tailscale proxy) | Cetacean is auth-unaware, optionally reads identity headers |
| Authorization | None | Network-level access control; read-only tool, no destructive actions |
| Actions | Read-only (v1) | Light operational actions (restart, scale) possible in future |

## Architecture

```
                    ┌──────────────┐
                    │   Browser    │
                    └──────┬───────┘
                           │ HTTP
                    ┌──────▼───────┐
                    │  Go Server   │
                    │              │
                    │  /app/*    → Embedded React SPA
                    │  /api/*    → REST endpoints
                    │  /api/events → SSE stream
                    └──┬───────┬──┘
                       │       │
              ┌────────▼┐  ┌──▼──────────┐
              │ Docker   │  │ Prometheus   │
              │ Socket   │  │ Server       │
              └──────────┘  └─────────────┘
```

### Components

| Component | Responsibility |
|-----------|---------------|
| Docker Watcher | Connects to Docker socket. Full sync on startup, then subscribes to Docker event stream for incremental updates. |
| State Cache | Thread-safe in-memory store of all Swarm state. Indexed by resource ID. RWMutex for concurrent access. |
| SSE Broadcaster | Pushes cache change events to connected frontend clients. Clients can filter by resource type. |
| REST API | Serves current state from cache. Read-only endpoints for all Swarm resource types. |
| Prometheus Client | Proxies PromQL queries to the configured Prometheus URL. Keeps Prometheus unexposed to browsers. |
| SPA Server | Serves embedded React build via `embed.FS`. Falls back to `index.html` for client-side routing. |

## State Cache & Docker Watcher

### Initial Sync

On startup, parallel Docker API calls populate the cache:

- `docker service ls` — services (includes stack labels)
- `docker node ls` — nodes
- `docker service ps *` — tasks (replica instances)
- `docker config ls` — configs
- `docker secret ls` — secrets (metadata only, never values)
- `docker network ls` — networks
- `docker volume ls` — volumes

Stacks are derived by grouping resources sharing a `com.docker.stack.namespace` label.

### Event-Driven Updates

After initial sync, the watcher subscribes to Docker's event stream. On each event:

1. Determine affected resource type and ID
2. Re-fetch that specific resource from the Docker API
3. Update the cache atomically
4. Notify the SSE broadcaster

### Cache Structure

```go
type Cache struct {
    mu        sync.RWMutex
    nodes     map[string]swarm.Node
    services  map[string]swarm.Service
    tasks     map[string]swarm.Task
    configs   map[string]swarm.Config
    secrets   map[string]swarm.Secret
    networks  map[string]network.Summary
    volumes   map[string]volume.Volume
    stacks    map[string]Stack
}

type Stack struct {
    Name     string
    Services []string
    Configs  []string
    Secrets  []string
    Networks []string
    Volumes  []string
}
```

### Resilience

- On event stream disconnect: reconnect and full re-sync to avoid missed events.
- Periodic full re-sync every 5 minutes as a safety net.

## REST API

### Resource Endpoints

All endpoints are `GET`, returning JSON.

| Endpoint | Returns |
|----------|---------|
| `GET /api/cluster` | Cluster overview: node count, service count, task counts, resource utilization summary |
| `GET /api/nodes` | All nodes with status, role, availability, resource usage |
| `GET /api/nodes/:id` | Node detail + tasks running on it |
| `GET /api/stacks` | All stacks with service counts, aggregate status |
| `GET /api/stacks/:name` | Stack detail: services, configs, secrets, networks, volumes |
| `GET /api/services` | All services with replica count, image, update state |
| `GET /api/services/:id` | Service detail: spec, tasks, placement, ports |
| `GET /api/tasks` | All tasks with state, node assignment, timestamps |
| `GET /api/configs` | Config list with metadata |
| `GET /api/secrets` | Secret list (metadata only) |
| `GET /api/networks` | Networks with driver, scope, attached services |
| `GET /api/volumes` | Volumes with driver, labels |

### Filtering & Pagination

List endpoints support query parameters:

- `?stack=<name>` — filter by stack
- `?node=<id>` — filter by node
- `?status=<state>` — filter by status
- `?limit=100&offset=0` — pagination

### Prometheus Proxy

| Endpoint | Purpose |
|----------|---------|
| `GET /api/metrics/query` | Proxies to Prometheus instant query |
| `GET /api/metrics/query_range` | Proxies to Prometheus range query |

The proxy keeps Prometheus unexposed to browsers and allows Cetacean to inject contextual label matchers.

### SSE Stream

| Endpoint | Purpose |
|----------|---------|
| `GET /api/events` | SSE stream of state changes |

Event format:

```
event: service
data: {"action":"update","id":"abc123","resource":{...}}

event: node
data: {"action":"remove","id":"def456"}
```

Clients filter via `?types=service,node,task`. No full state dump on connect — the client fetches initial state via REST, then applies SSE deltas.

## Frontend

### Technology

- React + TypeScript + Vite
- Tailwind CSS + shadcn/ui
- uPlot for time-series charts

### Pages

| Route | View |
|-------|------|
| `/` | Cluster Overview — node health grid, service/task counts, resource utilization charts |
| `/nodes` | Node List — table with status, role, CPU/memory gauges |
| `/nodes/:id` | Node Detail — info, tasks, resource usage over time |
| `/stacks` | Stack List — stacks with aggregate health |
| `/stacks/:name` | Stack Detail — services, configs, secrets, networks, volumes |
| `/services` | Service List — replica count, image, update status |
| `/services/:id` | Service Detail — spec, task list, resource charts |
| `/configs` | Config List |
| `/secrets` | Secret List (metadata only) |
| `/networks` | Network List |
| `/volumes` | Volume List |

### Real-Time Data Flow

1. Page mounts — REST fetch for initial state
2. SSE hook connects to `/api/events?types=<relevant>`
3. Incoming events update local state via reducer
4. React re-renders affected components

A `useSwarmResource` hook encapsulates this pattern.

### Charts

uPlot renders Prometheus data. Each detail page has contextual charts:

- **Node**: CPU %, memory %, disk I/O, network I/O
- **Service**: CPU/memory per replica, restart count

Pre-built PromQL queries scoped to the resource being viewed. Time range selector (1h, 6h, 24h, 7d).

## Prometheus Metrics Sources

### Required Exporters

| Exporter | Deployed As | Provides |
|----------|-------------|----------|
| cAdvisor | Global service (every node) | Container CPU, memory, network, disk I/O |
| Node Exporter | Global service (every node) | Host CPU, memory, disk, network, filesystem |

### Discovery

Prometheus discovers targets via `dockerswarm_sd_configs`. No manual target management.

### Expected Metrics

- `container_cpu_usage_seconds_total` (cAdvisor)
- `container_memory_usage_bytes` (cAdvisor)
- `node_cpu_seconds_total` (Node Exporter)
- `node_memory_MemAvailable_bytes` (Node Exporter)

Charts show "no data" gracefully if a metric is unavailable.

## Project Structure

```
cetacean/
├── cmd/
│   └── cetacean/
│       └── main.go           # Entrypoint: config, wiring, server start
├── internal/
│   ├── docker/
│   │   ├── watcher.go        # Event stream, full sync
│   │   └── client.go         # Docker API client wrapper
│   ├── cache/
│   │   └── cache.go          # Thread-safe in-memory state store
│   ├── api/
│   │   ├── router.go         # Route registration
│   │   ├── handlers.go       # REST endpoint handlers
│   │   ├── sse.go            # SSE broadcaster
│   │   └── prometheus.go     # Prometheus query proxy
│   └── config/
│       └── config.go         # Env/flag parsing
├── frontend/                 # React SPA (Vite + TypeScript)
│   ├── src/
│   │   ├── api/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── hooks/
│   │   ├── lib/
│   │   └── App.tsx
│   ├── index.html
│   ├── vite.config.ts
│   ├── tailwind.config.ts
│   └── package.json
├── embed.go                  # //go:embed frontend/dist/*
├── go.mod
├── go.sum
├── Dockerfile                # Multi-stage build
└── docker-compose.yml        # Swarm stack definition
```

## Deployment

### Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `CETACEAN_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket path |
| `CETACEAN_PROMETHEUS_URL` | (required) | Prometheus base URL |
| `CETACEAN_LISTEN_ADDR` | `:9000` | Bind address |

### Docker Compose / Swarm Stack

The shipped stack includes all components for a batteries-included setup:

- **cetacean** — the app itself, with Docker socket mounted read-only
- **prometheus** — pre-configured with Swarm SD for cAdvisor and Node Exporter
- **cadvisor** — global service on every node
- **node-exporter** — global service on every node

### Build

```
1. cd frontend && npm install && npm run build
2. go build ./cmd/cetacean
```

Multi-stage Dockerfile handles both steps, producing a minimal image.

## Future Considerations (Not in v1)

- Light operational actions (restart service, force update, scale replicas)
- Namespace-scoped visibility via role-based header mapping
- Custom PromQL panels via configuration
- Alerting integration (link to Alertmanager)