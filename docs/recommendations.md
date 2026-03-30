# Recommendations

Cetacean continuously evaluates your cluster's health across four domains and surfaces actionable recommendations. Checks that only need Docker state (config hygiene, cluster topology) run every 60 seconds. Checks that query Prometheus (resource sizing, operational health) run every 5 minutes.

## Categories

### Resource Sizing

Compares actual Prometheus metrics against configured service limits and reservations. Requires Prometheus with cAdvisor.

| Check | Condition | Severity |
|---|---|---|
| Over-provisioned | p95 usage below 20% of reservation over the lookback window | Info |
| Approaching limit | Current usage above 80% of limit | Warning |
| At limit | Current usage above 95% of limit | Critical |
| No limits | Service has no CPU or memory limit | Warning |
| No reservations | Service has limits but no reservations | Info |

Over-provisioned and limit-based checks include a suggested value and an **Apply suggested value** button that patches the service resources directly.

### Config Hygiene

Inspects service definitions for common misconfigurations. No Prometheus required.

| Check | Condition | Severity |
|---|---|---|
| No health check | Service has no health check or uses `NONE` | Warning |
| No restart policy | Restart policy is missing or set to `none` | Warning |

### Operational Health

Monitors runtime behavior via Prometheus. Requires Prometheus with node-exporter and cAdvisor.

| Check | Condition | Severity |
|---|---|---|
| Flaky service | More than 5 task restarts over the lookback window | Warning |
| Node disk full | Disk usage above 90% | Critical |
| Node memory pressure | Memory usage above 90% | Critical |

### Cluster Topology

Evaluates cluster structure from Docker state. No Prometheus required.

| Check | Condition | Severity |
|---|---|---|
| Single replica | Replicated service with only 1 replica | Info |
| Manager running workloads | Manager node with `active` availability | Warning |
| Uneven distribution | Busiest node has 3× or more tasks than the least busy | Info |

Single-replica and manager-workload checks include an **Apply suggested value** button (scales to 2 replicas or drains the manager node, respectively).

## Configuration

Sizing thresholds are configurable. Other checkers use fixed thresholds.

| Env var | Config file key | Default | Description |
|---|---|---|---|
| `CETACEAN_SIZING_HEADROOM_MULTIPLIER` | `sizing.headroom_multiplier` | `2.0` | Multiplier applied to actual usage when computing suggested values |
| `CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED` | `sizing.thresholds.over_provisioned` | `0.20` | Fraction of reservation below which a service is considered over-provisioned |
| `CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT` | `sizing.thresholds.approaching_limit` | `0.80` | Fraction of limit above which a service is flagged as approaching its limit |
| `CETACEAN_SIZING_THRESHOLD_AT_LIMIT` | `sizing.thresholds.at_limit` | `0.95` | Fraction of limit above which a service is flagged as at its limit |
| `CETACEAN_SIZING_LOOKBACK` | `sizing.thresholds.lookback` | `168h` | Time window for the p95 query used by over-provisioned checks (default 7 days) |

Example TOML configuration:

```toml
[sizing]
  headroom_multiplier = 2.0

  [sizing.thresholds]
    over_provisioned = 0.20
    approaching_limit = 0.80
    at_limit = 0.95
    lookback = "168h"
```

## Where Recommendations Appear

### Recommendations Page

The `/recommendations` page (keyboard shortcut `g r`) shows all active recommendations sorted by severity (critical first). Use the filter tabs to focus on a specific category:

- **All** — every recommendation
- **Sizing** — resource sizing checks only
- **Config** — config hygiene checks only
- **Operational** — runtime health checks only
- **Cluster** — topology checks only

### Dashboard Summary

The dashboard shows a compact summary card below the capacity bars with severity counts (e.g., "2 critical · 3 warnings"). Click **View all →** to open the full page. The card is hidden when there are no recommendations.

### Service Detail Banner

Each service's detail page shows a banner with all recommendations for that service — both sizing and config hygiene. The banner appears below the page header and includes **Apply suggested value** buttons where a safe fix is available.

### Service List Column

The service list includes a **Sizing** column (visible when recommendation data is available) showing the highest-severity sizing hint per service with a compact label like "CPU 85%" or "No limits".

## Graceful Degradation

| State | Behavior |
|---|---|
| Prometheus not configured | Only config hygiene and cluster topology checks run. Sizing and operational checks are skipped. |
| Prometheus unreachable | Sizing and operational checks return empty. Config and cluster results still shown. |
| No cAdvisor targets | Sizing checks emit config-only hints (no limits, no reservations). No metrics-based sizing. |
| All healthy | Recommendations page shows empty state. Dashboard card is hidden. Service list column shows green checkmarks. |

## API

```
GET /recommendations
```

Returns a JSON-LD response with all recommendations, a severity summary, and a timestamp:

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/recommendations",
  "@type": "RecommendationCollection",
  "computedAt": "2026-03-29T14:30:00Z",
  "items": [
    {
      "category": "at-limit",
      "severity": "critical",
      "scope": "service",
      "targetId": "abc123",
      "targetName": "nginx",
      "resource": "memory",
      "message": "Memory usage is at 96% of limit",
      "current": 1020000000,
      "configured": 1073741824,
      "suggested": 2147483648,
      "fixAction": "PATCH /services/{id}/resources"
    }
  ],
  "summary": {
    "critical": 1,
    "warning": 3,
    "info": 2
  },
  "total": 6
}
```

The response includes an `ETag` header for conditional caching.
