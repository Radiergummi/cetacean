<p align="center">
  <h1 align="center">Cetacean</h1>
  <p align="center">
    A fast, drop-in dashboard for Docker Swarm clusters.<br>
    Single binary. Zero config. Real-time updates.
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

Docker Swarm doesn't come with a dashboard. You get `docker service ls` and that's about it. Cetacean fills that gap:
deploy one container on a manager node and instantly see your entire cluster with live updates as things change.

```bash
docker stack deploy -c compose.yaml cetacean
# Open http://<manager>:9000 — done.
```

No database, no agents on worker nodes, no configuration required. It connects to the Docker socket, caches everything
in memory, and pushes changes to your browser over SSE.

## Features

- **Cluster overview** with live health cards, capacity bars, and activity feed
- **Browse everything**: nodes, services, tasks, stacks, configs, secrets, networks, volumes — all cross-referenced
- **Log viewer** with live-streaming, regex search, JSON formatting, and time range filtering
- **Topology views**: logical (service-to-service via overlay networks) and physical (task-to-node placement)
- **Metrics** via optional Prometheus integration: per-node, per-service, and per-stack CPU/memory charts
- **Real-time updates** via per-resource SSE: no polling, no refresh
- **Pluggable authentication**: anonymous, OIDC, Tailscale, mTLS, or trusted proxy headers
- **Full API**: REST with search, filtering, pagination, JSON-LD, OpenAPI spec, and SSE streaming

## Documentation

- **[Getting Started](docs/getting-started.md):** Installation, quick start, first run
- **[Configuration](docs/configuration.md):** CLI flags, env vars, config file, health checks
- **[Monitoring](docs/monitoring.md):** Prometheus, node-exporter, cAdvisor setup
- **[Authentication](docs/authentication.md):** OIDC, Tailscale, mTLS, proxy headers
- **[Dashboard Guide](docs/dashboard.md):** Navigation, keyboard shortcuts, search, charts, logs
- **[API Reference](docs/api.md):** Endpoints, query parameters, filters, SSE, response formats

## Comparison

|                        | Portainer          | Swarmpit        | Cetacean         |
|------------------------|--------------------|-----------------|------------------|
| **Deploy complexity**  | DB + agents + auth | CouchDB + agent | Single container |
| **Time to first page** | Minutes            | Minutes         | Seconds          |
| **Real-time updates**  | Polling            | Polling         | SSE push         |
| **Metrics**            | Built-in           | Built-in        | Prometheus       |

---

## Development

Requires Go 1.26+ and Node.js 24+. Cetacean needs a Docker Swarm to connect to:

```bash
docker swarm init  # single-node swarm for local dev
```

Run the backend and frontend dev server side by side:

```bash
# Terminal 1: Go backend
go run .

# Terminal 2: Frontend (hot reload, proxies to :9000)
cd frontend && npm install && npm run dev
```

Open `http://localhost:5173`. The Vite dev server proxies resource paths to the Go backend, so you get hot-reload with
live data.

### Make Targets

```bash
make check    # lint + format check + test (the full CI check)
make test     # go test ./...
make lint     # golangci-lint + oxlint
make fmt      # gofmt + oxfmt
make build    # frontend build + go build
```

### Tech Stack

**Backend**: Go, stdlib `net/http`, Docker Engine API, `log/slog`,
[expr](https://github.com/expr-lang/expr), [goccy/go-json](https://github.com/goccy/go-json)

**Frontend**: React 19, TypeScript, Vite, Tailwind CSS v4, shadcn/ui, Chart.js,
[React Flow](https://reactflow.dev/) + [ELK.js](https://github.com/kieler/elkjs),
[@tanstack/react-virtual](https://tanstack.com/virtual)

**Monitoring**: [Prometheus](https://prometheus.io/),
[cAdvisor](https://github.com/google/cadvisor),
[Node Exporter](https://github.com/prometheus/node_exporter)

## License

[GNU General Public License v3.0](LICENSE)
