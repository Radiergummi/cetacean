---
title: Getting started
description: Install Cetacean, deploy it to a Docker Swarm cluster, and open the dashboard.
category: guide
tags: [ installation, docker, swarm, quickstart ]
---

# Getting started

## Requirements

- A Docker Swarm Mode cluster. Single-node swarms work.
- A manager node to run Cetacean on. Cetacean reads the swarm API, which only managers serve.
- The Docker socket, mounted read-only into the container.

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

`compose.yaml` deploys the same container as a stack service, pinned to a manager, with resource limits and a
volume for its state. It has no prerequisites:

```bash
curl -O https://cetacean.mazetti.me/dist/compose.yaml
docker stack deploy -c compose.yaml cetacean
```

On a swarm with several managers, pin the task to one of them with `node.hostname == <your-manager>`, or give it
a volume driver every manager shares. The default `node.role == manager` constraint lets a reschedule land on a
different node, where it finds an empty volume and loses the cached state.

### From source

Building requires Go 1.26+ and Node.js 24+. `make build` installs the frontend dependencies, builds the dashboard
and the MCP widgets, and compiles the binary, which embeds both.

```bash
git clone https://github.com/Radiergummi/cetacean.git
cd cetacean
make build
./cetacean
```

The binary defaults to `unix:///var/run/docker.sock` and listens on `:9000`.

## What you'll see

The dashboard opens on the cluster overview: health cards for nodes, services and failed tasks, a capacity
section, and a feed of recent resource changes. The overview fills in as Cetacean reads the cluster, so give it a
moment on a large swarm. `GET /-/ready` turns 200 at the same point, which is what to gate a load balancer on.

Charts are missing until you configure Prometheus, and a banner at the top of the page says so.

## Add monitoring

Cetacean works without Prometheus. Adding it unlocks CPU and memory charts, resource gauges, capacity bars, and
sizing recommendations.

The bundled stack runs [Prometheus](https://prometheus.io/),
[node-exporter](https://github.com/prometheus/node_exporter) and [cAdvisor](https://github.com/google/cadvisor).
It reads `prometheus.yml` from the directory you deploy it from, so download both:

```bash
curl -O https://cetacean.mazetti.me/dist/compose.monitoring.yaml
curl -O https://cetacean.mazetti.me/dist/prometheus.yml
docker stack deploy -c compose.monitoring.yaml monitoring
```

That creates a `monitoring` overlay network. `compose.prometheus.yaml` is a small overlay that joins Cetacean to
it and sets the Prometheus URL; deploy it on top of `compose.yaml`:

```bash
curl -O https://cetacean.mazetti.me/dist/compose.prometheus.yaml
docker stack deploy -c compose.yaml -c compose.prometheus.yaml cetacean
```

Keep passing both files on every later redeploy. Deploying `compose.yaml` alone drops the network and the URL
again, and the charts go empty.

See [Monitoring][monitoring] for what Cetacean needs from the scrape if you already run Prometheus, and for the
detection banner that tells you which exporter is missing.

## Add authentication

By default anyone who can reach Cetacean can read everything and perform operational writes (scale, update image,
roll back, restart), so do this before exposing it beyond a trusted network. Set [`auth.mode`][auth.mode] to
`oidc`, `tailscale`, `cert`, or `headers`; see [Authentication][authentication] for the settings each mode needs.
TLS termination is available in any mode via [`tls.cert`][tls.cert] and [`tls.key`][tls.key].

Independently of who is signed in, [`server.operations_level`][server.operations_level] caps what Cetacean may
change at all. Set it to `0` for a read-only deployment.

## When something is wrong

| Symptom                                                            | What it means                                                                                                                                                                                                                                                                                                                                                                                         |
|--------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Every list is empty, and [`GET /-/ready`][api.ready] answers `503` | Cetacean cannot read the swarm. Resource endpoints answer `503` with code `ENG001`, and the log carries one `full sync resource failed` line per resource with the Docker error on it. Either the socket is not mounted or not readable, or the node is a worker—only managers serve the swarm API. Check with `docker info --format '{{.Swarm.ControlAvailable}}'`, which prints `true` on a manager |
| Cetacean exits at startup with `bind: address already in use`      | Something else holds the port. Move it with [`server.listen_addr`][server.listen_addr]                                                                                                                                                                                                                                                                                                                |
| Action buttons are missing from a detail page                      | Your [operations level][server.operations_level] or your [grants][authorization] do not allow that write. Fetch the resource with `Accept: application/json` and read the `Allow` response header: `GET, HEAD` alone means no write is available to you                                                                                                                                               |
| Charts are empty and a banner says so                              | Prometheus is unset, unreachable, or an exporter is not reporting. [`GET /metrics/status`][api.metrics] says which; see&nbsp;[Monitoring][monitoring]                                                                                                                                                                                                                                                 |
| Restarting loses the recent-failure history                        | The data directory is not on a volume—see the note under [Stack deployment](#stack-deployment)                                                                                                                                                                                                                                                                                                        |

[`GET /-/health`][api.health] answers `200` whenever the process is up, including while Cetacean cannot reach Docker.
[`/-/ready`][api.ready] is the one that tracks whether it has actually read the cluster.

## Where to go next

| Page                           | Covers                                                                                    |
|--------------------------------|-------------------------------------------------------------------------------------------|
| [Dashboard][dashboard]         | Navigation, the command palette, and the chart and log viewer controls                    |
| [Authorization][authorization] | Per-resource read and write grants, once callers are identified                           |
| [MCP Server][mcp]              | The same access for an AI agent, under the same operations level and grants               |
| [API guide][api]               | REST endpoints, SSE streams, and the Atom feed on every resource page                     |
| [Configuration][configuration] | Every setting, including [`server.base_path`][server.base_path] for a proxy on a sub-path |

[api]: api
[api.health]: api/explorer#tag/meta/GET/-/health
[api.ready]: api/explorer#tag/meta/GET/-/ready
[api.metrics]: api/explorer#tag/monitoring/GET/metrics/status
[auth.mode]: configuration#auth.mode
[authentication]: authentication
[authorization]: authorization
[configuration]: configuration
[dashboard]: dashboard
[mcp]: mcp
[monitoring]: monitoring
[recommendations]: recommendations
[server.base_path]: configuration#server.base_path
[server.listen_addr]: configuration#server.listen_addr
[server.operations_level]: configuration#server.operations_level
[tls.cert]: configuration#tls.cert
[tls.key]: configuration#tls.key
