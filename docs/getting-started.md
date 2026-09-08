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

Mounting the socket `:ro` protects the file, not the API—Cetacean can still scale and restart services.

Keep the `cetacean_data` volume. It holds the cached cluster state and the failure history behind the
flaky-service [recommendation][recommendations], both of which are lost with the container otherwise.

### Stack deployment

`compose.yaml` in the repository deploys the same container as a stack service, pinned to a manager, with resource
limits and a volume for its state. It joins the shared `monitoring` overlay network as an external network, so that
network has to exist first. Either create it, or deploy the [monitoring stack][add-monitoring] first, which creates
it for you.

```bash
docker network create --driver overlay monitoring
docker stack deploy -c compose.yaml cetacean
```

On a swarm with several managers, pin the task to one of them with `node.hostname == <your-manager>`, or give it
a volume driver every manager shares. The default `node.role == manager` constraint lets a reschedule land on a
different node, where it finds an empty volume and loses the cached state.

### From source

Building requires Go 1.26+ and Node.js 24+. The frontend has to be built first, because the Go binary embeds it.

```bash
cd frontend && npm install && npm run build && npm run build:widgets && cd ..
go build -o cetacean .
./cetacean
```

The binary defaults to `unix:///var/run/docker.sock` and listens on `:9000`.

## What you'll see

The dashboard opens on the cluster overview: health cards for nodes, services and failed tasks, a capacity
section, and a feed of recent resource changes. It reports healthy only once it has read the whole cluster, so
give it a moment on a large swarm.

Charts are missing until you configure Prometheus, and a banner at the top of the page says so.

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

Both stacks have to share an overlay network for that URL to resolve. See [Monitoring][monitoring] for the full
setup, including scrape configuration and the detection banner.

## Add authentication

By default anyone who can reach Cetacean can read everything and perform operational writes (scale, update image,
roll back, restart), so do this before exposing it beyond a trusted network. Set [`auth.mode`][auth.mode] to
`oidc`, `tailscale`, `cert`, or `headers`; see [Authentication][authentication] for the settings each mode needs.
TLS termination is available in any mode via [`tls.cert`][tls.cert] and [`tls.key`][tls.key].

Once callers are identified, [Authorization][authorization] adds per-resource read and write grants.

## Where to go next

- [Dashboard][dashboard] for navigation, the command palette, and the chart and log viewer controls
- [Configuration][configuration] for every setting. Start with
  [`server.operations_level`][server.operations_level], which selects how much Cetacean may
  change (default `1`, safe operations like scale and restart; `0` for read-only),
  [`server.base_path`][server.base_path] for deployments behind a reverse proxy on a sub-path,
  and [`storage.snapshot`][storage.snapshot] for state persistence across restarts
- [MCP Server][mcp] to give an AI agent the same read and write access, gated by the same [operations level][operations-level] and [grants][authorization]
- [API guide][api] for the REST endpoints, SSE streams, and Atom feeds. Every resource list and detail page also
  serves an Atom feed: click the feed icon in the page header, or append `.atom` to any resource URL

[add-monitoring]: #add-monitoring
[api]: api
[auth.mode]: configuration#auth.mode
[authentication]: authentication
[authorization]: authorization
[configuration]: configuration
[dashboard]: dashboard
[mcp]: mcp
[monitoring]: monitoring
[operations-level]: configuration#operations-level
[recommendations]: recommendations
[server.base_path]: configuration#server.base_path
[server.operations_level]: configuration#server.operations_level
[storage.snapshot]: configuration#storage.snapshot
[tls.cert]: configuration#tls.cert
[tls.key]: configuration#tls.key
