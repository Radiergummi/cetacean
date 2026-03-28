# Resource Right-Sizing Hints

Proactive indicators that surface when a service's actual resource usage diverges significantly from its configured limits and reservations.

## Goals

- Surface actionable sizing hints to day-to-day operators as they browse the dashboard
- Compare actual Prometheus metrics against configured limits/reservations
- Provide concrete suggested values that can be applied with one click
- Degrade gracefully when Prometheus or cAdvisor isn't available
- Design the data model to be extensible to a future recommendations feed (health checks, flaky services, disk usage)

## Non-Goals

- Auto-remediation (read-only hints only)
- Dedicated recommendations feed page (future feature; this spec designs for extensibility)
- Dashboard/cluster overview summary (deferred)
- Stack detail page aggregation (may fall out naturally but not required)

## Categories

| Category | Condition | Time Window | Severity |
|---|---|---|---|
| Over-provisioned | Actual < 20% of reservation, sustained | N consecutive ticks (default 3 × 60s) | info |
| Approaching limit | Actual > 80% of limit | Current tick | warning |
| At limit | Actual > 95% of limit | Current tick | critical |
| No limits set | Service has no CPU or memory limit | N/A (config-only) | warning |
| No reservations set | Has limits but no reservations | N/A (config-only) | info |

All thresholds are configurable. CPU and memory are evaluated independently — a service can have multiple hints.

## Data Model

```go
type Category string

const (
    CategoryOverProvisioned  Category = "over-provisioned"
    CategoryApproachingLimit Category = "approaching-limit"
    CategoryAtLimit          Category = "at-limit"
    CategoryNoLimits         Category = "no-limits"
    CategoryNoReservations   Category = "no-reservations"
)

type Severity string

const (
    SeverityInfo     Severity = "info"
    SeverityWarning  Severity = "warning"
    SeverityCritical Severity = "critical"
)

type Recommendation struct {
    Category   Category `json:"category"`
    Severity   Severity `json:"severity"`
    Resource   string   `json:"resource"`              // "cpu" or "memory"
    Message    string   `json:"message"`               // Human-readable description
    Current    float64  `json:"current"`               // Actual usage value
    Configured float64  `json:"configured"`            // Limit or reservation being compared
    Suggested  *float64 `json:"suggested,omitempty"`   // Suggested value (nil for config-only)
}

type ServiceSizing struct {
    ServiceID   string           `json:"serviceId"`
    ServiceName string           `json:"serviceName"`
    Hints       []Recommendation `json:"hints"`
    ComputedAt  time.Time        `json:"computedAt"`
}
```

The `Recommendation` type is intentionally generic — `Category` can be extended with non-sizing categories (e.g., `no-healthcheck`, `flaky-tasks`) in future work without changing the shape.

## Architecture

### Backend: Sizing Monitor

New `internal/sizing` package:

```
sizing/
  monitor.go    — Monitor struct, Run loop, Prometheus queries
  evaluate.go   — Threshold comparison, recommendation generation
  config.go     — SizingConfig struct
```

**Monitor** is a long-lived goroutine (like `Broadcaster` and Docker watcher):
- Created in `main.go` with `PromClient`, `*cache.Cache`, and `SizingConfig`
- Nil when Prometheus is not configured — all consumers are nil-safe
- Runs a ticker (default 60s) that:
  1. Queries Prometheus for all services' CPU rate and memory average (2 queries total)
  2. Reads all services from the cache
  3. Calls `evaluate()` per service, comparing actual vs spec
  4. Stores `map[string]*ServiceSizing` behind `sync.RWMutex`

**Prometheus queries (2 per tick):**

```promql
-- CPU: current rate per service (consistent with useServiceMetrics)
sum by (container_label_com_docker_swarm_service_name)(
  rate(container_cpu_usage_seconds_total{
    container_label_com_docker_swarm_service_id!=""
  }[5m])
) * 100

-- Memory: 1h average per service
avg_over_time(
  sum by (container_label_com_docker_swarm_service_name)(
    container_memory_usage_bytes{
      container_label_com_docker_swarm_service_id!=""
    }
  )[1h:]
)
```

**Evaluate logic** (`evaluate.go`):
- Pure function: takes service spec resources, actual metrics, and previous tick state
- Returns `[]Recommendation` for that service
- Checks in priority order: no-limits → no-reservations → at-limit → approaching-limit → over-provisioned
- Over-provisioned requires N consecutive ticks below threshold (sustained-tick counter tracked in monitor state)
- Suggested values: `actual × headroomMultiplier`, rounded to sensible units (nearest 0.05 CPU cores, nearest 64MB memory)

**API endpoint:**
- `GET /services/sizing` → `[]ServiceSizing` (only services with hints; healthy services omitted)
- Returns `[]` when monitor is nil or no data computed yet
- No auth tier gating (read-only)
- Includes `computedAt` for staleness detection

### Configuration

New `[sizing]` TOML section:

```toml
[sizing]
  enabled = true
  interval = "60s"
  headroom_multiplier = 2.0

  [sizing.thresholds]
    over_provisioned = 0.20
    approaching_limit = 0.80
    at_limit = 0.95
    sustained_ticks = 3
```

Environment variables:
- `CETACEAN_SIZING_ENABLED` (default `true`)
- `CETACEAN_SIZING_INTERVAL` (default `60s`)
- `CETACEAN_SIZING_HEADROOM_MULTIPLIER` (default `2.0`)
- `CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED` (default `0.20`)
- `CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT` (default `0.80`)
- `CETACEAN_SIZING_THRESHOLD_AT_LIMIT` (default `0.95`)
- `CETACEAN_SIZING_SUSTAINED_TICKS` (default `3`)

Loaded via `LoadSizing(flags, fileConfig)` following the existing resolver pattern. New `resolveFloat` helper for threshold and multiplier fields with min/max validation.

### Frontend: Service List

**`useSizingHints` hook:**
- Checks `useMonitoringStatus()` — returns empty when no Prometheus/cAdvisor
- Fetches `GET /services/sizing` on mount
- Returns `Map<serviceId, ServiceSizing>` + `hasData` boolean
- Refetches on navigation; no SSE, no polling

**Sizing column in DataTable:**
- Conditionally rendered when `hasData` is true (auto-shows when monitoring available, hidden otherwise)
- Shows highest-severity hint per service:
  - Critical: red, `▲ MEM 92%`
  - Warning: amber, `▲ CPU 85%`
  - Info (over-provisioned): blue, `▼ CPU 12%`
  - Info (config-only): gray, `☐ No limits`
  - Healthy: green, `✓ OK`
- Tooltip lists all hints when multiple exist
- Sortable by severity
- Card view: same badge in card footer

### Frontend: Service Detail

**PageHeader badge:**
- Small colored pill next to service name showing highest-severity hint
- Clicking scrolls to and expands the Resources section

**ResourcesEditor callout:**
- Colored banner above existing allocation bars
- Lists all hints for the service, one per line with human-readable message and suggested value
- Each suggestion has an "Apply" button that enters edit mode with the value pre-filled in the slider/input
- User must confirm and save — no auto-apply

## Graceful Degradation

| State | Behavior |
|---|---|
| Prometheus not configured | Monitor nil, endpoint returns `[]`, column hidden, no badges — invisible |
| Prometheus unreachable | Stale data served with `computedAt`; frontend shows "last updated X ago" if stale > 5min |
| No cAdvisor targets | Only config-only hints surface (no-limits, no-reservations) |
| Sizing disabled via config | Same as Prometheus not configured |
| No running tasks | Skip metrics-based hints, still surface config-only hints |

## Testing

**Backend:**
- `sizing/evaluate_test.go` — Table-driven tests for each category, boundary conditions, multi-hint services, nil resources
- `sizing/monitor_test.go` — Mock `PromClient`, verify tick loop output, sustained-tick counting, nil-receiver safety
- `api/handlers_test.go` — `GET /services/sizing` endpoint shape, empty when monitor nil
- `config/` — `LoadSizing` with env/TOML/flag combos, threshold range validation

**Frontend:**
- `useSizingHints` — Mock API, verify empty when monitoring unavailable
- Service list — Column renders/hides based on `hasData`, severity sorting
- ResourcesEditor — Banner rendering, "Apply" pre-fills editor
