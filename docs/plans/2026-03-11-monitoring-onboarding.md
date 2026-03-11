# Monitoring Onboarding Design

## Problem

Cetacean relies on Prometheus + cAdvisor + node-exporter for cluster metrics (CPU, memory, disk, container stats). Without these, all metrics panels are blank. The current UX for this state is a single dismissible banner saying "set `CETACEAN_PROMETHEUS_URL`", which doesn't guide users through the full setup.

Swarmpit gets around this by calling the Docker stats API directly, but that only works for containers on the local daemon — useless in a multi-node Swarm where Cetacean runs on a single manager. Rather than building a half-working stats fallback, we should make the Prometheus path feel effortless.

## Goals

1. A user goes from zero monitoring to full metrics with one `docker stack deploy` command
2. Cetacean auto-detects which metrics sources are available and shows targeted guidance for what's missing
3. Every metrics surface degrades gracefully — helpful empty states, not errors

## Non-Goals

- Built-in time-series storage or Docker stats API integration
- Bundling Prometheus inside the Cetacean binary
- Write operations (scaling, deploying, etc.)

## Design

### 1. Separate Monitoring Compose Overlay

Split the monitoring stack out of the main `docker-compose.yml` into a standalone overlay file that users can deploy independently.

**`docker-compose.monitoring.yml`** — standalone stack containing:
- Prometheus (manager node, scrape config embedded or mounted)
- cAdvisor (global service)
- node-exporter (global service)
- `prometheus.yml` config (embedded via `configs:` or volume mount)

**`docker-compose.yml`** — Cetacean only, no monitoring services. `CETACEAN_PROMETHEUS_URL` commented out with instructions.

**Usage:**
```bash
# Cetacean only (no metrics)
docker stack deploy -c docker-compose.yml cetacean

# Full stack with monitoring
docker stack deploy -c docker-compose.monitoring.yml cetacean-monitoring
docker stack deploy -c docker-compose.yml cetacean
# (set CETACEAN_PROMETHEUS_URL=http://prometheus:9090 or use shared network)
```

Alternatively, a single combined file for the "just give me everything" case:
```bash
docker stack deploy -c docker-compose.yml -c docker-compose.monitoring.yml cetacean
```

The monitoring stack uses a named overlay network (`cetacean-monitoring`) that Cetacean joins to reach Prometheus.

### 2. Backend: Metrics Source Detection

New endpoint: **`GET /api/metrics/status`**

When Prometheus is configured, probe it to detect which metric sources are actually scraping data. Run three instant queries:

| Source | Probe Query | What It Tells Us |
|--------|------------|------------------|
| Prometheus | (connection succeeds) | Prometheus is reachable |
| node-exporter | `up{job="node-exporter"}` | How many node-exporters are reporting |
| cAdvisor | `up{job="cadvisor"}` | How many cAdvisors are reporting |

Compare target counts against the known node count from the cache.

**Response:**
```json
{
  "prometheusConfigured": true,
  "prometheusReachable": true,
  "nodeExporter": { "targets": 3, "nodes": 3 },
  "cadvisor": { "targets": 3, "nodes": 3 }
}
```

When Prometheus is not configured:
```json
{
  "prometheusConfigured": false,
  "prometheusReachable": false,
  "nodeExporter": null,
  "cadvisor": null
}
```

Cache this response for 60 seconds (server-side) since it changes rarely.

### 3. Frontend: Setup Guidance

Replace the current `PrometheusBanner` with a **`MonitoringStatus`** component that adapts based on the detection response.

#### States

**A. Nothing configured** (`prometheusConfigured: false`)

Show on ClusterOverview and any page with metrics panels. Expandable banner:

> **Monitoring not configured**
> Deploy the monitoring stack to enable CPU, memory, and disk metrics across your cluster.
> ```
> docker stack deploy -c docker-compose.monitoring.yml cetacean-monitoring
> ```
> Then set `CETACEAN_PROMETHEUS_URL=http://prometheus:9090` and restart Cetacean.

Dismissible. Persisted in localStorage.

**B. Prometheus configured but unreachable** (`prometheusReachable: false`)

Warning banner (amber), not dismissible:

> **Cannot reach Prometheus** at `<url>` — metrics unavailable. Check that the Prometheus service is running and reachable from Cetacean.

**C. Prometheus connected, partial sources** (e.g., node-exporter targets < node count, or cAdvisor missing)

Info banner with specific guidance:

> **Monitoring partially configured** — cAdvisor not detected. Container-level metrics (service CPU/memory) require cAdvisor running on each node. [Show setup instructions]

Or: "node-exporter reporting on 1 of 3 nodes — deploy as a global service for full coverage."

**D. Everything healthy** — no banner shown.

#### Where Banners Appear

- **ClusterOverview**: always show status when not fully healthy (state A/B/C)
- **NodeDetail / ServiceDetail**: show only when that page's metrics are affected (e.g., service detail shows cAdvisor hint only when cAdvisor is missing)
- Metrics panels themselves show a minimal empty state with a one-line hint, not the full banner

### 4. Metrics Panel Empty States

When a metrics panel has no data (Prometheus not configured, or specific source missing):

- Show the panel frame with title, but replace the chart area with a centered muted message:
  - "No data — monitoring not configured" (link to setup)
  - "No data — cAdvisor not detected" (for container metrics)
  - "No data — node-exporter not detected" (for node metrics)
- Do NOT show error styling (red). This is an expected state, not a failure.
- The panel should still be visible so users know what they're missing.

### 5. Capacity Section Degradation

The CapacitySection already shows reservation-based data when Prometheus is unavailable. Keep this behavior. When Prometheus is configured but a specific source is missing, show whichever bars have data and a hint for the missing ones.

## Implementation Scope

### Backend
1. New `GET /api/metrics/status` endpoint with cached probe results
2. Extract monitoring services from `docker-compose.yml` into `docker-compose.monitoring.yml`
3. Update `docker-compose.yml` to be Cetacean-only

### Frontend
1. New `useMonitoringStatus()` hook (replaces `usePrometheusConfigured`)
2. New `MonitoringStatus` banner component (replaces `PrometheusBanner`)
3. Update `MetricsPanel` / `TimeSeriesChart` empty states
4. Update all pages that use `usePrometheusConfigured` to use the new hook

### Files Changed
- `internal/api/handlers.go` — new handler
- `internal/api/promquery.go` — new probe queries
- `internal/api/router.go` — register endpoint
- `docker-compose.yml` — remove monitoring services
- `docker-compose.monitoring.yml` — new file
- `frontend/src/hooks/useMonitoringStatus.ts` — new hook (replaces usePrometheusConfigured)
- `frontend/src/components/metrics/MonitoringStatus.tsx` — new component (replaces PrometheusBanner)
- `frontend/src/components/metrics/MetricsPanel.tsx` — empty state update
- `frontend/src/components/metrics/CapacitySection.tsx` — use new hook
- `frontend/src/pages/ClusterOverview.tsx` — swap banner component
- `frontend/src/pages/NodeDetail.tsx` — add contextual hint
- `frontend/src/pages/ServiceDetail.tsx` — add contextual hint
- `frontend/src/api/client.ts` — add monitoringStatus() method
- `frontend/src/api/types.ts` — add MonitoringStatus type
- Delete `frontend/src/hooks/usePrometheusConfigured.ts`
- Delete `frontend/src/components/metrics/PrometheusBanner.tsx`
