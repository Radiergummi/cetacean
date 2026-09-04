---
title: Monitoring
description: Set up Prometheus, node-exporter, and cAdvisor for metrics, charts, and capacity bars.
category: guide
tags: [monitoring, prometheus, node-exporter, cadvisor, metrics]
---

# Monitoring

Cetacean works without Prometheus, but you'll miss out on CPU/memory charts, resource gauges, capacity bars, per-task
sparklines, and stack-level drill-downs.

| Component                                                            | What it does                           | What you get                                               |
| -------------------------------------------------------------------- | -------------------------------------- | ---------------------------------------------------------- |
| **[Prometheus](https://prometheus.io/)**                             | Stores and queries metrics             | Anything metrics-related at all                            |
| **[node-exporter](https://github.com/prometheus/node_exporter)**     | Host-level metrics (CPU, memory, disk) | Node gauges, cluster capacity bars                         |
| **[cAdvisor](https://github.com/google/cadvisor)**                   | Container-level metrics                | Per-service charts, per-task sparklines, stack drill-downs |

## Setup

Deploy the bundled monitoring stack **first** — it creates the shared `monitoring` overlay network that
Cetacean's own compose file joins as an external network — then deploy Cetacean and point it at Prometheus:

```bash
docker stack deploy -c compose.monitoring.yaml monitoring
docker stack deploy -c compose.yaml cetacean
```

```yaml
environment:
  CETACEAN_PROMETHEUS_URL: http://prometheus:9090
```

The compose file deploys Prometheus on a manager node, with node-exporter and cAdvisor as global services (one per
node). Prometheus discovers them through the Docker socket (`dockerswarm_sd_configs`) rather than DNS, which is what
supplies the node each task is running on.

### The instance label

Cetacean selects node series with `instance=~"<node address>:.*"`, falling back to the node's hostname. Scraping an
exporter over an overlay network leaves `instance` set to the task's overlay IP, which matches no node — Prometheus
looks healthy, and every node chart is empty.

The bundled `prometheus.yml` therefore rewrites `instance` to the Swarm node address:

```yaml
relabel_configs:
  - source_labels: [__meta_dockerswarm_node_address]
    target_label: instance
    replacement: ${1}:9100
```

If you already run Prometheus, set the URL and make sure it scrapes node-exporter and cAdvisor with `instance`
carrying the node's address (or hostname). Cetacean expects the standard metric names, but relabeling `instance` is
required, not optional, whenever the scrape goes over an overlay network.

Cetacean auto-detects your monitoring setup and shows a status banner on the cluster overview when components are
missing or unreachable.

## Prometheus Proxy

Cetacean proxies read-only Prometheus queries through its own API, so the browser never talks to Prometheus directly.
This means no CORS configuration on Prometheus and no need to expose it outside the swarm network. See the
[API reference](/api) for the proxy endpoints.

## Self-Metrics

Cetacean exposes Prometheus metrics about its own operation at `/-/metrics` (HTTP requests, SSE connections, cache
state, proxy latency, recommendation runs). Scrape it as a standard Prometheus target:

```yaml
scrape_configs:
  - job_name: cetacean
    static_configs:
      - targets: ["cetacean:9000"]
    metrics_path: /-/metrics
```

Disable with `server.self_metrics = false`.
