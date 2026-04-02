# Monitoring

Cetacean works without Prometheus, but you’ll miss out on CPU/memory charts, resource gauges, capacity bars, per-task
sparklines, and stack-level drill-downs.

| Component         | What it does                           | What you get in Cetacean                                   |
|-------------------|----------------------------------------|------------------------------------------------------------|
| **Prometheus**    | Stores and queries metrics             | Anything metrics-related at all                            |
| **node-exporter** | Host-level metrics (CPU, memory, disk) | Node gauges, cluster capacity bars                         |
| **cAdvisor**      | Container-level metrics                | Per-service charts, per-task sparklines, stack drill-downs |

Prometheus alone gets you the query proxy. Add node-exporter for host metrics. Add cAdvisor for container metrics.

## Quick Setup

Deploy the recommended monitoring stack alongside Cetacean:

```bash
docker stack deploy --compose-file compose.monitoring.yaml monitoring
```

Then tell Cetacean where Prometheus lives:

```yaml
environment:
  CETACEAN_PROMETHEUS_URL: http://prometheus:9090
```

The monitoring stack and Cetacean should share an overlay network (`monitoring`) so they can find each other by service
name.

## The Monitoring Stack

`compose.monitoring.yaml` deploys:

**Prometheus** (manager only)

- Scrapes node-exporter and cAdvisor targets
- 1 CPU / 2GB memory limit
- Data stored in a named volume

**node-exporter** (global—one per node)

- Exports host CPU, memory, disk, network metrics
- 0.5 CPU / 128MB memory limit
- Mounts `/proc`, `/sys`, `/` read-only

**cAdvisor** (global—one per node)

- Exports per-container CPU, memory, network metrics
- 0.5 CPU / 256MB memory limit
- Mounts Docker socket and cgroup filesystem

All three run on the `monitoring` overlay network.

## Auto-Detection

Cetacean automatically detects your monitoring setup and shows a status banner on the cluster overview. The banner has
four states:

| State            | What it means                                                      | What to do                       |
|------------------|--------------------------------------------------------------------|----------------------------------|
| **Unconfigured** | `prometheus.url` is not set                                        | Set it and restart               |
| **Unreachable**  | Prometheus URL is set but not responding                           | Check the URL, check the network |
| **Partial**      | Prometheus is up but node-exporter or cAdvisor targets are missing | Deploy the monitoring stack      |
| **Healthy**      | Everything is working                                              | Nothing. Enjoy the charts.       |

The detection endpoint is `GET /metrics/status`. It probes Prometheus for active scrape targets and compares them
against your cluster’s node count.

## Prometheus Proxy

Cetacean proxies Prometheus queries through its own API, so the browser never talks to Prometheus directly. This means:

- No CORS configuration needed on Prometheus
- Prometheus doesn’t need to be exposed outside the swarm network
- Queries are restricted to read-only operations

**Endpoints:**

| Path                         | Maps to                                  | Description                                                                                                  |
|------------------------------|------------------------------------------|--------------------------------------------------------------------------------------------------------------|
| `GET /metrics`               | `/api/v1/query` or `/api/v1/query_range` | Instant or range query (determined by `start`+`end` params). Content-negotiated: JSON, SSE, or HTML console. |
| `GET /metrics/labels`        | `/api/v1/labels`                         | Label names (optional `match[]` filter)                                                                      |
| `GET /metrics/labels/{name}` | `/api/v1/label/{name}/values`            | Label values                                                                                                 |

**Allowed parameters:** `query`, `time`, `timeout`, `start`, `end`, `step`

**Limits:** 10MB response size, 30s timeout.

The proxy strips everything else. You can’t write rules, delete series, or do anything that isn’t a read.

## What Gets Queried

Cetacean runs these Prometheus queries for the built-in dashboards:

**Cluster overview** (instant queries, 10s timeout):

| Metric          | Query                                                                                               | Notes                                               |
|-----------------|-----------------------------------------------------------------------------------------------------|-----------------------------------------------------|
| CPU utilization | `sum(rate(node_cpu_seconds_total{mode!="idle"}[5m])) / sum(rate(node_cpu_seconds_total[5m])) * 100` |                                                     |
| Memory used     | `sum(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes)`                                  | Total comes from Docker swarm state, not Prometheus |
| Disk total      | `sum(node_filesystem_size_bytes{mountpoint="/"})`                                                   |                                                     |
| Disk available  | `sum(node_filesystem_avail_bytes{mountpoint="/"})`                                                  | Used = total - available, computed server-side      |

**Charts** (range queries via the browser, through the proxy):

The frontend builds PromQL queries dynamically for per-node, per-service, per-task, and per-stack views using
`container_cpu_usage_seconds_total`, `container_memory_usage_bytes`, and the `node_*` metric families.

## Custom Prometheus Setup

If you already run Prometheus, point Cetacean at it:

```bash
CETACEAN_PROMETHEUS_URL=http://your-prometheus:9090 ./cetacean
```

Make sure your Prometheus scrapes node-exporter and cAdvisor targets. The standard metric names are expected—Cetacean
doesn’t support custom metric relabeling.

If Prometheus is behind authentication, Cetacean currently doesn’t support passing credentials to the upstream. The
proxy connects without auth.

## Self-Metrics

Cetacean exposes Prometheus metrics about its own operation at `GET /-/metrics`. This endpoint serves a standard
Prometheus exposition format and is enabled by default.

**Available metric families:**

| Metric                                            | Type      | Description                              |
|---------------------------------------------------|-----------|------------------------------------------|
| `cetacean_http_requests_total`                    | counter   | HTTP requests by method, handler, status |
| `cetacean_http_request_duration_seconds`          | histogram | Request latency by method and handler    |
| `cetacean_http_request_size_bytes`                | histogram | Request body size by method and handler  |
| `cetacean_http_response_size_bytes`               | histogram | Response body size by method and handler |
| `cetacean_sse_connections_active`                 | gauge     | Active SSE connections                   |
| `cetacean_sse_events_broadcast_total`             | counter   | SSE events broadcast                     |
| `cetacean_sse_events_dropped_total`               | counter   | SSE events dropped (slow clients)        |
| `cetacean_cache_resources`                        | gauge     | Cached resources by type                 |
| `cetacean_cache_sync_duration_seconds`            | histogram | Full-sync duration                       |
| `cetacean_cache_mutations_total`                  | counter   | Cache mutations by type and action       |
| `cetacean_prometheus_requests_total`              | counter   | Prometheus proxy requests by status      |
| `cetacean_prometheus_request_duration_seconds`    | histogram | Prometheus proxy request duration        |
| `cetacean_recommendations_check_duration_seconds` | histogram | Recommendation checker run duration      |
| `cetacean_recommendations_total`                  | gauge     | Active recommendations by severity       |

To scrape Cetacean’s own metrics, add it as a Prometheus target:

```yaml
# In your prometheus.yml
scrape_configs:
  - job_name: cetacean
    static_configs:
      - targets: [ "cetacean:9000" ]
    metrics_path: /-/metrics
```

To disable self-metrics, set `server.self_metrics` to `false`.

## Resource Overhead

The monitoring stack is designed to be lightweight:

| Component     | CPU limit | Memory limit | Instances   |
|---------------|-----------|--------------|-------------|
| Prometheus    | 1.0       | 2GB          | 1 (manager) |
| node-exporter | 0.5       | 128MB        | 1 per node  |
| cAdvisor      | 0.5       | 256MB        | 1 per node  |

For a 10-node cluster, that’s about 6 CPUs and 5.8GB memory total across all nodes. Prometheus is the heaviest
component; its memory usage scales with the number of time series (containers x metrics x label cardinality).
