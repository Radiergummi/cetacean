---
title: Monitoring
description: Set up Prometheus, node-exporter, and cAdvisor so Cetacean can chart CPU, memory, disk, and capacity.
category: guide
tags: [monitoring, prometheus, node-exporter, cadvisor, metrics]
---

# Monitoring

Cetacean runs without Prometheus. Without it you lose CPU and memory charts, resource gauges, capacity bars,
per-task sparklines, stack drill-downs, and every sizing [recommendation][recommendations].

| Component                                                        | Provides                               | Unlocks                                                    |
| ---------------------------------------------------------------- | -------------------------------------- | ---------------------------------------------------------- |
| [Prometheus](https://prometheus.io/)                             | Stores and queries metrics             | Anything metrics-related at all                            |
| [node-exporter](https://github.com/prometheus/node_exporter)     | Host metrics (CPU, memory, disk)       | Node gauges, cluster capacity bars                         |
| [cAdvisor](https://github.com/google/cadvisor)                   | Container metrics                      | Per-service charts, per-task sparklines, stack drill-downs |

## Setup

Deploy the bundled monitoring stack first. It reads `prometheus.yml` from the directory you deploy it from, and
creates the `monitoring` overlay network Cetacean joins:

```bash
curl -O https://raw.githubusercontent.com/Radiergummi/cetacean/main/compose.monitoring.yaml
curl -O https://raw.githubusercontent.com/Radiergummi/cetacean/main/prometheus.yml
docker stack deploy -c compose.monitoring.yaml monitoring
```

`compose.prometheus.yaml` is a small overlay for `compose.yaml` that joins that network and sets
[`prometheus.url`][prometheus.url]. Pass both files, on this deploy and every later one:

```bash
curl -O https://raw.githubusercontent.com/Radiergummi/cetacean/main/compose.prometheus.yaml
docker stack deploy -c compose.yaml -c compose.prometheus.yaml cetacean
```

Pointing at a Prometheus you already run is the same setting by another route — set
[`prometheus.url`][prometheus.url] through the environment or a [config file][config-file], and make sure
Cetacean shares a network with it.

The stack runs Prometheus on a manager node, with node-exporter and cAdvisor on every node.

> [!WARNING]
> The bundled Prometheus runs as `root` so it can read the mounted Docker socket. In front of a production
> cluster, put a socket proxy in its place instead of granting root.

## What Cetacean needs from the scrape

If you already run Prometheus, set `prometheus.url` and check the scrape against the three requirements below. The
bundled `prometheus.yml` and `compose.monitoring.yaml` already satisfy all three.

### The instance label must identify the node

Cetacean matches metrics to nodes by the `instance` label. Scraping over an overlay network leaves it set to the
task's overlay IP, which matches no node: Prometheus looks healthy and every node chart is empty.

Rewrite `instance` to the Swarm node address, as the bundled config does:

```yaml
relabel_configs:
  - source_labels: [__meta_dockerswarm_node_address]
    target_label: instance
    replacement: ${1}:9100
```

Relabeling `instance` is required, not optional, whenever the scrape goes over an overlay network.

The [dashboard][dashboard] also maps a node to its instance through `node_uname_info`. Run node-exporter with
`hostname: "{{.Node.Hostname}}"` so its `nodename` label reports the Swarm node hostname.

### cAdvisor must emit the Swarm service label

Container metrics are attributed to services through `container_label_com_docker_swarm_service_name`. cAdvisor emits
it when started with `--store_container_labels=true`.

cAdvisor also needs containerd's socket, not just Docker's. Without it Prometheus looks healthy and `container_*`
metrics exist, but none carries the service label, so per-service charts stay empty and no sizing recommendation is
ever produced. Docker Engine keeps its containerd under `/run/docker/containerd`, so mount:

```yaml
- type: bind
  source: /run/docker/containerd
  target: /run/containerd
  read_only: true
```

If your Docker uses a system containerd instead of its own, use `/run/containerd` as the source. To check which case
you are in, count the labelled series:

```promql
count(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name!=""})
```

One or zero means the factory did not register. `docker service logs monitoring_cadvisor` names the socket it could
not reach.

### The cAdvisor scrape job must be named `cadvisor`

Cetacean detects cAdvisor with `up{job="cadvisor"}`. Under any other job name the dashboard treats container metrics
as unavailable and skips them on the service, task, and node pages. node-exporter is detected by the presence of
`node_uname_info` and does not depend on its job name.

## Status banner

The cluster overview and the metrics console show a banner when something is missing:

| State                | Shown when                                                    | Banner                                                                                |
| -------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Healthy              | Prometheus answers and both exporters cover every node        | Nothing                                                                               |
| Not configured       | `prometheus.url` is unset                                     | Deploy instructions. Dismissible                                                      |
| Unreachable          | `prometheus.url` is set but queries fail                      | Warning with the connection error. Not dismissible                                    |
| Partially configured | Prometheus answers, an exporter reports on 0 or some nodes    | One line per exporter naming what is missing and on how many nodes. Dismissible       |

The [MCP][mcp-tools] `get_metrics` tool reports the same gap rather than charting zeros. When every series comes
back empty it probes for the exporter and answers "cAdvisor is not reporting per-container metrics for this
cluster" or the node-exporter equivalent.

## Prometheus proxy

Browsers query Prometheus through Cetacean rather than directly, so Prometheus needs no CORS configuration and
never has to be reachable from outside the swarm network. See the [API reference][api] for the proxy endpoints.

## Self-metrics

Cetacean exposes Prometheus metrics about its own operation at `/-/metrics`: HTTP requests and latency, SSE
connections and dropped events, cache size and sync duration, Prometheus proxy latency, and recommendation check
duration and counts. Scrape it as a standard target:

```yaml
scrape_configs:
  - job_name: cetacean
    static_configs:
      - targets: ["cetacean:9000"]
    metrics_path: /-/metrics
```

Turn it off with [`server.self_metrics`][server.self_metrics].

[api]: api
[config-file]: configuration#config-file
[dashboard]: dashboard
[mcp-tools]: mcp-tools
[prometheus.url]: configuration#prometheus.url
[recommendations]: recommendations
[server.self_metrics]: configuration#server.self_metrics
