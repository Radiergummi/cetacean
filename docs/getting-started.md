# Getting Started

Cetacean is a real-time observability and management dashboard for Docker Swarm Mode clusters. One binary, zero dependencies, real-time updates.
Think of it as a docker CLI with a UI and a memory.

## Requirements

- A Docker Swarm Mode cluster (even a single-node swarm works)
- Access to a manager node's Docker socket
- That's it. Seriously.

## Quick Start

### Docker (recommended)

```bash
docker stack deploy -c compose.yaml cetacean
```

Cetacean is now at [http://localhost:9000](http://localhost:9000). It'll take a second to sync your swarm state, then
you're in. The built-in image includes a `HEALTHCHECK` that gates on readiness — use `depends_on: { cetacean: { condition: service_healthy } }` for services that need Cetacean's data. Set `start_period` generously on large clusters.

### From Source

```bash
cd frontend && npm install && npm run build && cd ..
go build -o cetacean .
./cetacean
```

### Binary

Download a release, point it at a Docker socket, run it:

```bash
CETACEAN_DOCKER_HOST=unix:///var/run/docker.sock ./cetacean
```

## What You'll See

On first load, Cetacean connects to the Docker socket, pulls every node, service, task, config, secret, network, and
volume in your swarm, and caches it all in memory. This takes about a second for most clusters.

From there, it subscribes to the Docker event stream. Every change shows up in your browser in real time -- no polling,
no refresh button.

The cluster overview shows:

- **Health cards** -- node count, service convergence, failed tasks, running tasks
- **Capacity bars** -- cluster-wide CPU and memory utilization (requires [monitoring](monitoring.md))
- **Activity feed** -- the last 25 things that changed
- **Resource charts** -- CPU and memory by stack (requires [monitoring](monitoring.md))

Everything is clickable. Services link to their tasks. Tasks link to their nodes. Configs and secrets link to the
services that use them. It's cross-references all the way down.

## Placement

Cetacean needs access to a manager node's Docker socket. In a swarm deployment, constrain it to managers:

```yaml
deploy:
  placement:
    constraints:
      - node.role == manager
```

By default (operations level 1), Cetacean can perform safe operational actions like scaling and restarting services.
Set `server.operations_level` to `0` for a fully read-only dashboard. See [Configuration](configuration.md#operations-level) for details.

## Adding Monitoring

Cetacean works without Prometheus, but it's better with it. Metrics unlock CPU/memory charts, resource gauges, capacity
bars, and per-task sparklines.

See [Monitoring](monitoring.md) for the setup guide.

## Adding Authentication

By default, anyone who can reach Cetacean can see everything. That might be fine on a private network. If it's not,
see [Authentication](authentication.md) for OIDC, Tailscale, mTLS, and proxy header options.

Once authenticated, you can optionally restrict access to specific resources with [Authorization](authorization.md).

## Next Steps

- [Configuration](configuration.md) -- every knob you can turn
- [Monitoring](monitoring.md) -- Prometheus, node-exporter, cAdvisor
- [Authentication](authentication.md) -- lock it down
- [Authorization](authorization.md) -- per-resource access control
- [Dashboard Guide](dashboard.md) -- keyboard shortcuts, search, charts
- [API Reference](api.md) -- endpoints, query parameters, SSE
