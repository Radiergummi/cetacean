# New Chart Types Design

**Date:** 2026-03-15
**Status:** Draft

## Overview

Add three new chart visualizations to the Cetacean dashboard: a stacked area toggle on the cluster overview, horizontal bar charts for resource allocation on service detail, and a multi-ring doughnut for disk usage.

## 1. Stacked Area Toggle (ClusterOverview)

The existing stack drill-down line charts on ClusterOverview gain a view toggle. Two icon buttons (line chart icon / stacked area icon) appear in the chart header, right-aligned next to the unit label. Default view remains line charts.

### Stacked Area Mode

- Same Prometheus query and data as the line chart — no new API calls
- Chart.js `fill: 'stack'` mode with chart palette colors at ~40% opacity
- Y-axis remains hidden; the stacked total is visible as the top edge of the area
- Tooltip shows all series values at the hovered timestamp plus a **Total** row. The Total always sums all datasets regardless of isolation state (it represents the real aggregate).
- The toggle state is per chart instance, stored in component state (not URL-persisted)
- The toggle applies individually — CPU and Memory charts can be in different modes

### Click-to-Isolate in Stacked Mode

When a series is isolated in stacked mode, dimmed datasets have their `data` replaced with all zeros (not just opacity change). This genuinely removes them from the stack so the isolated series fills from the baseline. When isolation is cleared, the original data is restored. This differs from line mode (which uses opacity dimming) because stacked fill regions depend on dataset values, not just visual styling.

### Toggle UI

Two small icon buttons in the chart header. The header layout changes from `justify-between` (title, unit) to three groups: left (title), center (toggle icons when `stackable`), right (unit). Icons:
- Line chart icon (lucide `LineChart`)
- Stacked area icon (lucide `AreaChart`)
- Active icon gets `bg-muted` highlight, inactive is ghost-style
- Icons are ~14px, same visual weight as existing chart header elements

### Implementation

`TimeSeriesChart` gains a `stackable?: boolean` prop. When true, the header renders the toggle icons and the component maintains a `stacked` state. In stacked mode:
- Each dataset uses `fill: 'stack'` instead of `fill: true`
- `backgroundColor` uses solid color at 40% opacity (not gradient)
- `borderWidth` reduces to `1` for cleaner stacking
- Click-to-isolate zeros out dimmed datasets' data instead of using opacity
- The tooltip Total row sums `dataset.data[idx]` across all datasets (using original data, not zeroed)

The component stores the original fetched series data separately so zeroed-out datasets can be restored when isolation is cleared.

`StackDrillDownChart` passes `stackable` through to `TimeSeriesChart`. The `ClusterOverview` charts get `stackable` by default.

## 2. Horizontal Bar Chart (ServiceDetail)

A new "Resource Allocation" section on the service detail page, below the existing metrics panel. Renders two horizontal bar charts side by side in a 2-column grid (CPU left, Memory right).

### Data Model

Each chart shows up to three elements per metric:
- **Reserved bar** — per-replica reservation from `service.Spec.TaskTemplate.Resources.Reservations` (NanoCPUs / MemoryBytes), **multiplied by the number of running replicas** to match the actual usage sum
- **Actual bar** — from Prometheus instant query: sum of usage across all running tasks
- **Limit marker** — per-replica limit from `service.Spec.TaskTemplate.Resources.Limits`, **multiplied by the number of running replicas**

Both Reserved and Limit are scaled by replica count so they're directly comparable to the Actual sum. The replica count comes from the task list (count of tasks in `running` state) which is already available on the service detail page.

### Visual Design

- Reserved bar: chart palette color at 30% opacity
- Actual bar: chart palette color at full opacity
- Limit marker: `destructive` color, dashed vertical line drawn via a Chart.js plugin
- Labels on the left axis: "Reserved", "Actual"
- Values displayed at the end of each bar (formatted with `formatValue`)
- If no reservation is configured, the "Reserved" bar is absent
- If no limit is configured, the limit marker is absent
- If neither limit nor reservation is configured for either CPU or memory, the entire section is hidden

### Prometheus Unavailable

If Prometheus is not configured or unreachable (`hasCadvisor` is false on ServiceDetail):
- The "Actual" bar is not shown
- The section still renders if the service has limits/reservations (showing just the static Docker data)
- This provides value even without monitoring: "what did we configure?"

If Prometheus is available but the query returns no data (e.g., service has no running tasks), the "Actual" bar shows as empty/zero.

### Prometheus Queries

CPU actual (instant):
```promql
sum(rate(container_cpu_usage_seconds_total{
  container_label_com_docker_swarm_service_name="<SERVICE>"
}[5m])) * 100
```

Memory actual (instant):
```promql
sum(container_memory_usage_bytes{
  container_label_com_docker_swarm_service_name="<SERVICE>"
})
```

These use `api.metricsQuery()` (instant query), not `metricsQueryRange`. The queries depend on cAdvisor being configured with Docker label passthrough (the default in the monitoring stack from `compose.monitoring.yaml`).

### Component

New component `ResourceAllocationChart` in `frontend/src/components/metrics/`. Uses Chart.js `bar` chart type with `indexAxis: 'y'` for horizontal orientation. The limit marker is drawn via a small Chart.js plugin (`afterDatasetsDraw`), same pattern as the existing threshold plugin in `TimeSeriesChart`.

Props: `cpuReserved`, `cpuLimit`, `cpuActual`, `memReserved`, `memLimit`, `memActual` (all `number | undefined`). The component handles layout (2-column grid) and conditional rendering internally.

## 3. Multi-Ring Doughnut (DiskUsageSection)

Replace the current single-ring doughnut with a two-ring chart.

### Ring Layout

- **Outer ring** — total size per type (Images, Build Cache, Volumes, Containers). Same 4 segments as current.
- **Inner ring** — each type split into non-reclaimable and reclaimable. 8 data points total, interleaved: `[images_nonreclaim, images_reclaim, buildcache_nonreclaim, buildcache_reclaim, ...]`.

### Colors

Color indexing maps inner ring data points to their parent type: `getChartColor(Math.floor(i / 2))`.

- Outer ring: `getChartColor(i)` at full opacity (same as current)
- Inner ring non-reclaimable (even indices): `getChartColor(Math.floor(i / 2))` at full opacity
- Inner ring reclaimable (odd indices): `getChartColor(Math.floor(i / 2))` at 40% opacity (lighter shade indicating "can be freed")

### Chart.js Configuration

Two datasets on the same Doughnut chart:
- Dataset 0 (outer): `data: [totalImages, totalBuildCache, totalVolumes, totalContainers]`
- Dataset 1 (inner): `data: [imagesNonReclaim, imagesReclaim, bcNonReclaim, bcReclaim, volNonReclaim, volReclaim, contNonReclaim, contReclaim]`

Ring sizing:
- Outer: default radius (current `cutout: "62%"` stays)
- Inner: smaller radius via per-dataset `weight` (e.g., outer `weight: 2`, inner `weight: 1`)

### Tooltip

On outer ring hover: shows type name + total size + percentage of total (current behavior).

On inner ring hover: shows type name + "reclaimable" or "in use" + size + percentage of that type's total. For example: "Images: 9.1 GB reclaimable (53%)" or "Images: 8.1 GB in use (47%)".

The external tooltip handler differentiates by checking `tooltip.dataPoints[0].datasetIndex` (0 = outer, 1 = inner) and for inner ring, `dataIndex % 2` determines reclaimable (odd) vs. non-reclaimable (even).

### Minimum Slice Handling

`withMinSlice` is generalized to accept a `number[]` (currently it takes `DiskUsageSummary[]`). The outer ring uses the existing logic (4% of total). The inner ring applies the same 4% floor relative to the outer ring total, ensuring tiny reclaimable slices remain visible and hoverable.

### Other Properties

- Center text: unchanged (total size + "Total")
- Hover expand: `hoverOffset: 3` on both datasets
- Gap/spacing: `spacing: 3`, `borderRadius: 4` on both datasets

## 4. Files Affected

| File | Changes |
|------|---------|
| `frontend/src/components/metrics/TimeSeriesChart.tsx` | Add `stackable` prop, stacked area mode, toggle UI, Total row in tooltip, zero-data isolation for stacked mode |
| `frontend/src/components/metrics/StackDrillDownChart.tsx` | Pass `stackable` through |
| `frontend/src/pages/ClusterOverview.tsx` | Set `stackable` on StackDrillDownChart instances |
| `frontend/src/components/metrics/ResourceAllocationChart.tsx` | New: horizontal bar chart with limit markers |
| `frontend/src/components/metrics/index.ts` | Export ResourceAllocationChart |
| `frontend/src/pages/ServiceDetail.tsx` | Add Resource Allocation section |
| `frontend/src/components/DiskUsageSection.tsx` | Refactor DoughnutChart to two-ring layout, generalize `withMinSlice` |

## 5. Dependencies

No new dependencies. Chart.js already supports stacked area (`fill: 'stack'`), horizontal bar (`indexAxis: 'y'`), and multi-dataset doughnut natively.
