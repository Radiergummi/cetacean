# Recommendation Feed

A unified recommendation engine that aggregates cluster health checks across four domains — resource sizing, config hygiene, operational health, and cluster topology — into a single feed with a dedicated page and dashboard summary.

## Goals

- Centralize all proactive health recommendations into one place
- Replace the standalone `internal/sizing` package with a checker within a unified engine
- Provide a `/recommendations` page with severity-ordered list and category filter tabs
- Show a severity-breakdown summary card on the dashboard
- Support one-click fixes for safely auto-fixable recommendations
- Degrade gracefully: config and cluster checks run without Prometheus; sizing and operational checks are skipped when Prometheus is unavailable

## Non-Goals

- Auto-remediation beyond one-click "Apply suggested value" buttons
- Notification/alerting (email, Slack, webhooks)
- Historical recommendation tracking

## Recommendation Categories

### Sizing (Prometheus + cache)

| Check | Condition | Severity | Auto-fixable |
|---|---|---|---|
| Over-provisioned | p95 usage < 20% of reservation over lookback | info | Yes (PATCH reservation) |
| Approaching limit | Current usage > 80% of limit | warning | Yes (PATCH limit) |
| At limit | Current usage > 95% of limit | critical | Yes (PATCH limit) |
| No limits set | Service has no CPU or memory limit | warning | No |
| No reservations set | Has limits but no reservations | info | No |

### Config Hygiene (cache-only)

| Check | Condition | Severity | Auto-fixable |
|---|---|---|---|
| No health check | Healthcheck nil or Test[0] == "NONE" | warning | No |
| No restart policy | RestartPolicy nil or Condition == "none" | warning | No |

### Operational (Prometheus + cache)

| Check | Condition | Severity | Auto-fixable |
|---|---|---|---|
| Flaky service | > 5 task restarts over lookback window (default 7d) | warning | No |
| Node disk full | Disk usage > 90% | critical | No |
| Node memory pressure | Memory usage > 90% | critical | No |

### Cluster (cache-only)

| Check | Condition | Severity | Auto-fixable |
|---|---|---|---|
| Single replica | Replicated service with 1 replica | info | Yes (PUT scale) |
| Manager running workloads | Manager node with active availability | warning | Yes (PUT availability=drain) |
| Uneven task distribution | Max/min tasks-per-node ratio > 3x | info | No |

All sizing thresholds are configurable via the existing `[sizing]` config section (renamed to `[recommendations.sizing]`). The engine interval is configured via `CETACEAN_RECOMMENDATIONS_INTERVAL` (default `60s`), replacing `CETACEAN_SIZING_INTERVAL`. A top-level `CETACEAN_RECOMMENDATIONS_ENABLED` (default `true`) disables the entire engine. Other checkers use fixed thresholds in v1.

## Data Model

```go
type Scope string

const (
    ScopeService Scope = "service"
    ScopeNode    Scope = "node"
    ScopeCluster Scope = "cluster"
)

type Recommendation struct {
    Category   Category `json:"category"`
    Severity   Severity `json:"severity"`
    Scope      Scope    `json:"scope"`
    TargetID   string   `json:"targetId"`
    TargetName string   `json:"targetName"`
    Resource   string   `json:"resource"`
    Message    string   `json:"message"`
    Current    float64  `json:"current"`
    Configured float64  `json:"configured"`
    Suggested  *float64 `json:"suggested,omitempty"`
    FixAction  *string  `json:"fixAction,omitempty"`
}
```

New fields vs. the current sizing `Recommendation`:
- `Scope`: service, node, or cluster
- `TargetID` / `TargetName`: what the recommendation applies to (service ID, node ID, or empty for cluster)
- `FixAction`: nullable API action string — the frontend substitutes `targetId` into the path to build the request. Nil means the recommendation is informational only.

`Category` and `Severity` types remain unchanged. New category constants:

```go
// Sizing (existing)
CategoryOverProvisioned, CategoryApproachingLimit, CategoryAtLimit, CategoryNoLimits, CategoryNoReservations

// Config hygiene
CategoryNoHealthcheck  Category = "no-healthcheck"
CategoryNoRestartPolicy Category = "no-restart-policy"

// Operational
CategoryFlakyService    Category = "flaky-service"
CategoryNodeDiskFull    Category = "node-disk-full"
CategoryNodeMemPressure Category = "node-memory-pressure"

// Cluster
CategorySingleReplica       Category = "single-replica"
CategoryManagerHasWorkloads  Category = "manager-has-workloads"
CategoryUnevenDistribution   Category = "uneven-distribution"
```

## Architecture

### Checker Interface

```go
type Checker interface {
    Name() string
    Check(ctx context.Context) []Recommendation
}
```

Four implementations:
- `SizingChecker` — refactored from current `internal/sizing/monitor.go`. Takes `QueryFunc`, `*cache.Cache`, `*config.SizingConfig`.
- `ConfigChecker` — cache-only. Takes `*cache.Cache`.
- `OperationalChecker` — Prometheus-dependent. Takes `QueryFunc`, `*cache.Cache`, lookback duration.
- `ClusterChecker` — cache-only. Takes `*cache.Cache`.

Each checker is independently testable — pure function of its inputs.

### Engine

```go
type Engine struct {
    checkers []Checker
    interval time.Duration
    mu       sync.RWMutex
    results  []Recommendation
}
```

- Created in `main.go` with all applicable checkers (sizing/operational only registered when Prometheus is configured)
- `Run(ctx)` starts a tick loop at the configured interval
- Each tick runs all checkers (in parallel — cache-only checkers don't block on Prometheus), merges results
- `Results()` returns the full list, nil-safe
- `Summary()` returns severity counts, nil-safe

### Migration from `internal/sizing`

The `internal/sizing` package is **removed entirely**:
- Types (`Recommendation`, `Category`, `Severity`, `ServiceSizing`) move to `internal/recommendations/`
- Evaluate logic (`evaluate.go`) moves to `internal/recommendations/sizing_checker.go` (or stays as an internal helper)
- Monitor lifecycle (`Run` loop) is handled by the engine — the checker only implements `Check(ctx)`
- `GET /services/sizing` endpoint is removed
- Frontend `useSizingHints` hook is removed

### API

**`GET /recommendations`** — returns JSON-LD collection:

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/recommendations",
  "@type": "RecommendationCollection",
  "items": [...],
  "total": 7,
  "summary": { "critical": 2, "warning": 3, "info": 2 },
  "computedAt": "2026-03-29T..."
}
```

Content-negotiated (JSON for API, SPA for browser). No auth tier gating (read-only). Returns empty collection before first engine tick.

The dashboard reads `summary` and `total`. The full page reads `items`. The service detail page and service list filter `items` client-side by `targetId`.

## Frontend

### Recommendations Page (`/recommendations`)

- New route, added to nav sidebar
- PageHeader: "Recommendations" with severity summary as subtitle
- Filter tabs: All / Sizing / Config / Operational / Cluster — URL-persisted via `?filter=`
- List ordered by severity (critical first)
- Each recommendation shows: severity dot, icon, target link (→ service/node detail), message, "Apply suggested value" button where `fixAction` is present
- Empty state: green checkmark, "No recommendations — your cluster looks healthy"
- Applied fixes dismiss the recommendation locally (same pattern as current SizingBanner)

### Dashboard Summary Card (`RecommendationSummary`)

- Rendered on dashboard below CapacitySection
- Hidden when total is 0
- Colored border matching highest severity
- "Recommendations" title with "View all →" link
- Severity counts inline, zero counts omitted

### Service List Column

- Reads from `GET /recommendations`, filtered by `scope == "service"` and sizing categories
- `useSizingHints` replaced with filtering from `useRecommendations`
- `SizingBadge` component updated to accept `Recommendation[]` (new type with `Scope`/`TargetID` fields)

### Service Detail Banner

- `SizingBanner` reads from `GET /recommendations`, filtered by `targetId == serviceId`
- Shows ALL recommendation categories for this service (sizing + config hygiene), not just sizing
- "Apply suggested value" button where `fixAction` is present

## Graceful Degradation

| State | Behavior |
|---|---|
| Prometheus not configured | Engine runs config + cluster checkers only. Sizing + operational checkers not registered. |
| Prometheus unreachable | Sizing + operational checkers return empty on failed queries. Config + cluster results still shown. |
| No cAdvisor | Sizing checker returns config-only hints (no-limits, no-reservations). Operational checker may have partial results. |
| Engine disabled (`CETACEAN_RECOMMENDATIONS_ENABLED=false`) | Endpoint returns empty collection. Dashboard card hidden. Service list column hidden. |

## Testing

**Backend:**
- `recommendations/engine_test.go` — mock checkers, verify merge, nil-safety, parallel execution
- `recommendations/sizing_checker_test.go` — existing evaluate tests migrated, plus new tests for `Scope`/`TargetID`/`FixAction` population
- `recommendations/config_checker_test.go` — table-driven with mock cache: services with/without health checks, restart policies
- `recommendations/operational_checker_test.go` — mock QueryFunc, node disk/memory thresholds, flaky service detection
- `recommendations/cluster_checker_test.go` — single-replica services, manager availability, task distribution
- `api/handlers_test.go` — `GET /recommendations` returns JSON-LD envelope with summary

**Frontend:**
- `useRecommendations` hook — mock API, verify items + summary
- `RecommendationSummary` — renders counts, hides at zero
- Recommendations page — filter tabs, severity ordering, fix buttons, local dismiss
- Service list/detail — verify they work with the new data source
