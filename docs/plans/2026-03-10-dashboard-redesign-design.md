# Cluster Overview Dashboard Redesign

**Date:** 2026-03-10
**Status:** Approved

## Goal

Replace the current counts-heavy ClusterOverview with a health-first, capacity-aware dashboard that answers "is my cluster healthy?" at a glance, then "how loaded is it?", then "what's been happening?".

## Layout

Layout B: full-width health row at top, two-column below (capacity left, activity right).

Page sections top to bottom:
1. Prometheus setup banner (dismissible, localStorage-persisted)
2. Health row — 4 cards spanning full width
3. Two-column area: Capacity (left) | Activity Feed (right)

Notification rules removed from the overview page.

## Prometheus Banner

- Full-width card above the health row, blue-tinted background
- Content: brief message + setup hint ("Set `CETACEAN_PROMETHEUS_URL` to enable CPU, memory, and disk metrics")
- Dismiss button (X) stores `cetacean:dismiss-prom-banner` in localStorage
- Only shown when `prometheusConfigured === false` AND not dismissed
- If Prometheus is later configured, the condition is false and the banner disappears naturally

## Health Row

4 clickable cards:

| Card | Primary | Secondary | Color | Links to |
|------|---------|-----------|-------|----------|
| Nodes | `3/3 ready` | `all ready` or `1 down, 1 drain` | Green all ready, red any down, amber any draining | `/nodes` |
| Services | `10/12 converged` | `all healthy` or `2 degraded` | Green all converged, amber any degraded | `/services` |
| Failed Tasks | `3` | `across 2 services` | Red when > 0, neutral when 0 | `/tasks` |
| Tasks | `47 running` | `52 total · 8 stacks` | Always neutral | `/tasks` |

Delta trending arrows only on the Failed Tasks card.

### Service Convergence

A service is "converged" when running task count >= desired replica count. Global services are converged when they have a running task on every eligible node. Services with 0 desired replicas are always converged.

## Capacity Column

### With Prometheus

Three utilization bars (CPU, Memory, Disk):
- Progress bar with percentage label
- "used / total" beneath (e.g., "18.6 / 30 cores")
- Color: blue < 70%, amber 70-90%, red > 90%
- Data from new `GET /api/cluster/metrics` endpoint, auto-refreshes every 30s
- Clicking a bar navigates to `/nodes`

### Without Prometheus

Two reservation bars (CPU, Memory):
- Same progress bar style, labeled "reserved"
- "18 / 30 cores reserved"
- Data from `ClusterSnapshot` (new `reservedCPU`/`reservedMemory` fields)
- No disk bar (Docker API has no disk reservations)
- Color: blue < 80%, amber 80-95%, red > 95%

## New Backend Endpoint: `GET /api/cluster/metrics`

Returns cluster-wide utilization from Prometheus:
```json
{
  "cpu": { "used": 18.6, "total": 30, "percent": 62 },
  "memory": { "used": 47400000000, "total": 64000000000, "percent": 74 },
  "disk": { "used": 205000000000, "total": 500000000000, "percent": 41 }
}
```

- Three PromQL queries run concurrently (same pattern as `HandleStackSummary`)
- Returns 404 when Prometheus is not configured
- 5s context timeout, errors cause frontend fallback to reservation bars

## ClusterSnapshot Extensions

Add to `cache.ClusterSnapshot`:
- `servicesConverged int` — services where running >= desired
- `servicesDegraded int` — services where running < desired
- `reservedCPU int64` — sum of NanoCPUs from all service resource reservations
- `reservedMemory int64` — sum of MemoryBytes from all service resource reservations
- `nodesDraining int` — nodes with availability "drain"

These are computed in `Snapshot()` alongside the existing iteration.

## Activity Feed

- Reuse existing `ActivityFeed` component
- 25 entries instead of 20
- Fixed height matching capacity column, internal scroll via `overflow-y: auto`
- Live SSE updates via existing mechanism

## Responsive Behavior

- Desktop (md+): health row 4 columns, capacity + activity side by side
- Mobile (<md): health row 2x2 grid, capacity and activity stack vertically
- Prometheus banner always full-width
