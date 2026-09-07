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

Docker Swarm doesn't come with a dashboard. You get `docker service ls` and that's about it. Cetacean fills the gap:
run one container on a manager node and see the whole cluster, with live updates as things change.

It connects to the Docker socket, caches swarm state in memory, and pushes changes to the browser over SSE. No
database, no agents on worker nodes, no configuration required.

## Quick start

Run it on a manager node:

```bash
docker run -d --name cetacean \
  -p 9000:9000 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/radiergummi/cetacean:latest
```

Open `http://<manager>:9000`. Or deploy the bundled stack, which adds a volume for persisted state:

```bash
docker network create --driver overlay monitoring
docker stack deploy -c compose.yaml cetacean
```

See [Getting started](docs/getting-started.md) for the monitoring and authentication setup.

## What you get

- Cluster overview with live health cards, capacity bars, and an activity feed
- Nodes, services, tasks, stacks, configs, secrets, networks, volumes and plugins, all cross-referenced
- Log viewer with live tailing, regex search, JSON formatting, and time range filtering
- Topology views: logical (services grouped by stack, linked by shared overlay networks) and physical (tasks by node)
- Metrics via optional Prometheus integration, plus a PromQL console and sizing recommendations
- Write operations (scale, restart, rollback, image and spec edits, node drain) gated by an operations level
- Pluggable authentication: anonymous, OIDC, Tailscale, mTLS, or trusted proxy headers, with per-resource RBAC
- REST API with search, filtering, pagination, JSON-LD, OpenAPI, SSE, and Atom feeds
- Embedded MCP server, so an AI agent reads and operates the cluster through the same permissions

## Comparison

|                    | Portainer          | Swarmpit        | Cetacean         |
|--------------------|--------------------|-----------------|------------------|
| Deploy complexity  | DB + agents + auth | CouchDB + agent | Single container |
| Time to first page | Minutes            | Minutes         | Seconds          |
| Real-time updates  | Polling            | Polling         | SSE push         |
| Metrics            | Built-in           | Built-in        | Prometheus       |

## Documentation

Full documentation is at [cetacean.mazetti.me](https://cetacean.mazetti.me), and in [`docs/`](docs) in this
repository:

- [Getting started](docs/getting-started.md)
- [Configuration](docs/configuration.mdx)
- [Monitoring](docs/monitoring.md)
- [Authentication](docs/authentication.md) and [Authorization](docs/authorization.md)
- [Dashboard](docs/dashboard.md)
- [API guide](docs/api.md)
- [MCP server](docs/mcp.md) and [MCP tools and resources](docs/mcp-tools.md)

## Build from source

Requires Go 1.26+ and Node.js 24+. The frontend has to be built first, because the binary embeds it:

```bash
cd frontend && npm install && npm run build && npm run build:widgets && cd ..
go build -o cetacean .
```

Cetacean needs a swarm to connect to; `docker swarm init` gives you a single-node one for local work. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, the dev server, and the `make` targets.

## License

[GNU General Public License v3.0](LICENSE)

Third-party components bundled with Cetacean are listed in
[THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES) (includes the Lucide/Feather icon
set used for the UI and the embedded MCP tool and resource icons).
