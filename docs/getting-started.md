---
title: Getting started
description: Install Cetacean, deploy it to a Docker Swarm cluster, and open the dashboard.
category: guide
tags: [installation, docker, swarm, quickstart]
---

# Getting started

## Requirements

- A Docker Swarm Mode cluster. Single-node swarms work.
- A manager node to run Cetacean on. Cetacean reads the swarm API, which only managers serve.
- The Docker socket, mounted read-only into the container.

Cetacean needs nothing else: no database, no agents on worker nodes, no configuration file.

## Run it

Container images are published to `ghcr.io/radiergummi/cetacean`. Pick one of the paths below, run it on a manager
node, and open `http://<manager>:9000`.

### Single container

```bash
docker run -d --name cetacean \
  -p 9000:9000 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v cetacean_data:/data \
  -e CETACEAN_DATA_DIR=/data \
  ghcr.io/radiergummi/cetacean:latest
```

Cetacean talks to the cluster over the Docker socket. The `:ro` flag applies to the socket file rather than to the
API, so write operations still work. The `cetacean_data` volume holds the state snapshot and the task restart
history behind the flaky-service recommendation; without it, both are lost whenever the container is replaced.

### Stack deployment

`compose.yaml` in the repository deploys the same container as a stack service, pinned to a manager, with resource
limits and a volume for its state. It joins the shared `monitoring` overlay network as an external network, so that
network has to exist first. Either create it, or deploy the [monitoring stack](#add-monitoring) first, which creates
it for you.

```bash
docker network create --driver overlay monitoring
docker stack deploy -c compose.yaml cetacean
```

The snapshot volume is node-local, and `node.role == manager` pins the task to any manager rather than a specific
one. On a swarm with several managers, a reschedule lands on a different node and binds a fresh empty volume. Pin
the task with `node.hostname == <your-manager>` instead, or use a volume driver whose storage every manager shares.

### From source

Building requires Go 1.26+ and Node.js 24+. The frontend has to be built first, because the Go binary embeds it.

```bash
cd frontend && npm install && npm run build && npm run build:widgets && cd ..
go build -o cetacean .
./cetacean
```

The binary defaults to `unix:///var/run/docker.sock` and listens on `:9000`.

## First load

Cetacean syncs the full swarm state on startup, then follows the Docker event stream. The built-in `HEALTHCHECK`
runs `cetacean healthcheck`, which polls `/-/ready` and only passes once that first sync has completed, so the task
shows as healthy only when the dashboard has data to serve. Swarm ignores Compose's `depends_on`, so ordering
between services is not something a stack file can express.

The dashboard opens on the cluster overview: health cards for nodes, services and failed tasks, a capacity section,
and a feed of recent resource changes. Charts are absent until you configure Prometheus, and a banner at the top of
the page says what is missing.

## Add monitoring

Cetacean works without Prometheus. Adding it unlocks CPU and memory charts, resource gauges, capacity bars, and
sizing recommendations.

Deploy the bundled stack ([Prometheus](https://prometheus.io/),
[node-exporter](https://github.com/prometheus/node_exporter) and
[cAdvisor](https://github.com/google/cadvisor)):

```bash
docker stack deploy -c compose.monitoring.yaml monitoring
```

Then point Cetacean at it and redeploy:

```yaml
environment:
  CETACEAN_PROMETHEUS_URL: http://prometheus:9090
```

Both stacks have to share an overlay network for that URL to resolve. See [Monitoring](monitoring) for the full
setup, including scrape configuration and the detection banner.

## Add authentication

By default anyone who can reach Cetacean can read everything and perform operational writes (scale, update image, roll back, restart), so do this before exposing it beyond a trusted network.
Set `auth.mode` to `oidc`, `tailscale`, `cert`, or `headers`; see [Authentication](authentication) for the settings
each mode needs. TLS termination is available in any mode via `tls.cert` and `tls.key`.

Once callers are identified, [Authorization](authorization) adds per-resource read and write grants.

## Where to go next

- [Dashboard](dashboard) for navigation, the command palette, and the chart and log viewer controls
- [Configuration](configuration) for every setting. Start with `server.operations_level`, which selects how much
  Cetacean may change (default `1`, safe operations like scale and restart; `0` for read-only),
  `server.base_path` for deployments behind a reverse proxy on a sub-path, and `storage.snapshot` for state
  persistence across restarts
- [MCP Server](mcp) to give an AI agent the same read and write access, gated by the same operations level and grants
- [API guide](api) for the REST endpoints, SSE streams, and Atom feeds. Every resource list and detail page also
  serves an Atom feed: click the feed icon in the page header, or append `.atom` to any resource URL
