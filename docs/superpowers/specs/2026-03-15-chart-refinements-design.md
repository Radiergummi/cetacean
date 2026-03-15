# Chart Refinements Design

**Date:** 2026-03-15
**Status:** Draft

## Overview

Refine all charts across the Cetacean dashboard for production and demo quality. Migrate color palette to a CVD-safe theme-integrated system, add interactive features (linked crosshairs, click-to-isolate, brush-to-zoom), redesign the cluster overview charts around stack-based drill-down, and polish tooltips and doughnut hover effects.

## 1. Color Palette

Replace hardcoded `CHART_COLORS` arrays in `TimeSeriesChart.tsx` and `DiskUsageSection.tsx` with CSS custom properties defined in the Tailwind theme.

**Palette:** IBM Carbon / Bang Wong (research-backed CVD-safe, proven for deuteranopia, protanopia, and tritanopia). These are sRGB hex values — the existing shadcn `--chart-1` through `--chart-5` OKLCH tokens are replaced with 10 new hex-based tokens.

| Token | Name | Hex |
|-------|------|-----|
| `--chart-1` | Blue | `#648FFF` |
| `--chart-2` | Gold | `#FFB000` |
| `--chart-3` | Magenta | `#DC267F` |
| `--chart-4` | Purple | `#785EF0` |
| `--chart-5` | Orange | `#FE6100` |
| `--chart-6` | Cyan | `#02D4F5` |
| `--chart-7` | Amber | `#FFD966` |
| `--chart-8` | Lavender | `#CF9FFF` |
| `--chart-9` | Pink | `#FF85B3` |
| `--chart-10` | Teal | `#47C1BF` |

**Integration:**
- Define `--chart-1` through `--chart-10` in both light and dark `:root` blocks in `index.css`. Same values for both modes — these hex values have sufficient contrast on both light and dark backgrounds.
- Expose as `--color-chart-1` etc. in the `@theme inline` block so Tailwind classes like `text-chart-1`, `bg-chart-1` are available. This replaces the existing 5 shadcn-default chart tokens — any components using those classes will get the new colors.
- Both `TimeSeriesChart` and `DiskUsageSection` read colors at runtime via `getComputedStyle` from CSS custom properties, falling back to hex constants.

## 2. Time Range Selection

Current state: `MetricsPanel` has a `SegmentedControl` with 4 fixed ranges (1h, 6h, 24h, 7d), URL-persisted via `?range=`.

### 2.1 Custom Range Picker

Add a calendar-icon button to the right of the segmented control. Clicking it opens a dropdown with:
- Two date-time inputs (start, end)
- Quick presets within the dropdown (last 2h, last 12h, yesterday, last 3d)
- "Apply" button

When a custom range is active:
- The segmented control deselects all presets
- A chip/badge appears showing the selected timeframe (e.g. "Mar 14 09:00 – 11:30") with an X button to clear
- Clearing reverts to the previous preset (default: 1h)

**URL params and precedence:**
- Preset ranges use `?range=6h` (current behavior)
- Custom ranges use `?from=<unix>&to=<unix>` and remove `?range=`
- If both `from`/`to` and `range` are present in the URL, `from`/`to` take precedence
- Clearing custom range removes `from`/`to` and restores `?range=` (or removes it for the default 1h)

### 2.2 TimeSeriesChart Prop Changes

`TimeSeriesChart` gains optional `from?: number` and `to?: number` props (unix timestamps). When provided, these override the `range` string prop for data fetching. The `range` prop remains for backward compatibility — if `from`/`to` are absent, the component computes the time window from `range` as before.

### 2.3 Brush-to-Zoom

On any `TimeSeriesChart`, click-drag horizontally to select a time window:
- Visual: semi-transparent overlay on the selected region during drag
- On release: activates custom range mode with the selected window
- All charts in the same `MetricsPanel` update to the new range
- The custom range chip appears in the controls
- A "Reset zoom" action is available via the X on the chip

**Implementation:** Chart.js `zoom` plugin (`chartjs-plugin-zoom`) with `drag` mode on the x-axis only. Set `drag.threshold: 5` (5px minimum drag distance) — clicks shorter than this threshold are treated as click-to-isolate, not brush-to-zoom.

**Touch:** Out of scope for this iteration. `chartjs-plugin-zoom` supports pinch-to-zoom which can be enabled later.

## 3. Linked Crosshairs

Charts within the same `MetricsPanel` share `syncKey="metrics"` (already passed, currently unused).

**Behavior:**
- Hovering one chart broadcasts the cursor's x-axis timestamp to all sibling charts via a shared React context.
- **Hovered chart:** shows full tooltip + crosshair line (current behavior).
- **Sibling charts:** show crosshair line + small filled dots (radius 3px) on each visible series at the matched timestamp. No tooltip.
- **Mouse leaves all charts:** all crosshairs and dots clear.

**Implementation:** Use a shared React context (`ChartSyncProvider`) wrapping the `MetricsPanel` grid. Each chart registers with the provider. On cursor move, the hovered chart publishes `{ timestamp: number }` to the context. Sibling charts resolve the pixel position via `chart.scales.x.getPixelForValue(timestamp)` — this is robust regardless of chart padding or pixel width differences.

## 4. Click-to-Isolate

On any time-series chart with multiple series:

- **Click a series line or its tooltip entry:** solo that series. All other series dim to 30% opacity. The clicked series renders at full opacity with its gradient fill.
- **Click the same series again (or click chart background):** restore all series to full opacity.
- **Click a different series while one is isolated:** switch focus to the new series.
- **Reset on data refetch:** isolated state clears when the chart fetches new data (range change, refresh).

**Disambiguation from brush-to-zoom:** The `chartjs-plugin-zoom` `drag.threshold: 5` setting ensures that clicks (< 5px movement) trigger click-to-isolate, while drags (>= 5px) trigger brush-to-zoom.

**Implementation:** Manipulate Chart.js dataset `borderColor`/`backgroundColor` alpha to dim non-isolated series. Track isolated series index in component state.

## 5. Stack Drill-Down (Cluster Overview)

Replace the current "Top 10 services" CPU/Memory charts on `ClusterOverview` with stack-based drill-down charts.

### 5.1 Default View: Top 10 Stacks

- Prometheus queries aggregate metrics by `container_label_com_docker_stack_namespace`
- Chart title: "CPU Usage (by Stack)" / "Memory Usage (by Stack)"
- Shows top 10 stacks by aggregate resource consumption
- Toggleable legend at bottom (collapsed by default)

### 5.2 Legend Behavior

- **Collapsed (default):** no legend visible, chart uses full height
- **Expanded:** shows all stacks (capped at 30 to limit query size), not just top 10. Stacks beyond top 10 render at 30% opacity until hovered or clicked.
- "Show all" toggle at the bottom of the legend
- Clicking a legend item isolates that stack (same as click-to-isolate)

### 5.3 Drill-Down into Stack

- **Double-click** a stack series to drill down into its individual services. Single-click isolates (dims others) as per section 4.
- Chart title updates: "CPU Usage (webapp-production)"
- Breadcrumb appears: "← All Stacks"
- All services within the stack are shown (no limit)
- Legend shows service names
- Click-to-isolate works the same way within the drilled-down view
- Clicking the breadcrumb returns to the stacks overview
- **Loading transition:** chart shows a subtle loading overlay (spinner icon in top-right corner) while fetching the drill-down query. Previous data remains visible underneath to avoid layout shift.

### 5.4 Query Strategy

**Stacks view (CPU):**
```promql
topk(10, sum by (container_label_com_docker_stack_namespace)(
  rate(container_cpu_usage_seconds_total{container_label_com_docker_stack_namespace!=""}[5m])
) * 100)
```

**Stacks view (Memory):**
```promql
topk(10, sum by (container_label_com_docker_stack_namespace)(
  container_memory_usage_bytes{container_label_com_docker_stack_namespace!=""}
))
```

**Services within stack (CPU):**
```promql
sum by (container_label_com_docker_swarm_service_name)(
  rate(container_cpu_usage_seconds_total{
    container_label_com_docker_stack_namespace="<stack>",
    container_label_com_docker_swarm_service_name!=""
  }[5m])
) * 100
```

**Services within stack (Memory):**
```promql
sum by (container_label_com_docker_swarm_service_name)(
  container_memory_usage_bytes{
    container_label_com_docker_stack_namespace="<stack>",
    container_label_com_docker_swarm_service_name!=""
  }
)
```

**Legend "Show all" view:** Replace `topk(10, ...)` with `topk(30, ...)`.

Both views respect the active time range (preset or custom/brush-to-zoom).

### 5.5 Other Pages Unchanged

`ServiceList`, `NodeList`, `NodeDetail`, `ServiceDetail`, `TaskDetail` keep their current chart configurations. Only `ClusterOverview` gets the stack drill-down.

## 6. Doughnut Chart Polish

### 6.1 Hover Expand

- Hovered slice expands outward by 3px (`hoverOffset: 3` in Chart.js config)
- Non-hovered slices remain in place
- Transition: Chart.js default animation (~200ms ease)

### 6.2 Colors

- Uses `--chart-1` through `--chart-4` from the theme (matching the 4 disk usage types)

## 7. Tooltip Transitions

### 7.1 TimeSeriesChart Tooltips (React-rendered)

The tooltip is a React element toggled via `tooltip` state. Add a CSS class with `transition: opacity 50ms ease` and manage a `data-visible` attribute:
- Mount the tooltip element always (not conditionally rendered), toggle `opacity: 0`/`opacity: 1` via the attribute
- Use `50ms` for appear, `100ms` for disappear (switch transition-duration based on state)
- Position updates remain immediate (no transition on `left`/`top`)

### 7.2 DiskUsageSection Tooltips (DOM-rendered via Chart.js external handler)

The tooltip is an imperatively managed DOM element. Same approach — `transition: opacity` on the element, set `opacity` to `"0"` or `"1"` via `el.style`. The 50ms/100ms timing applies the same way.

## 8. Cleanup

- Remove `uplot-react` from `package.json` (still present despite `uplot` being uninstalled)
- Update `CLAUDE.md` to reference Chart.js instead of uPlot

## 9. Files Affected

| File | Changes |
|------|---------|
| `frontend/src/index.css` | Replace `--chart-1`–`--chart-5` with `--chart-1`–`--chart-10` in both color schemes and `@theme inline` |
| `frontend/src/components/metrics/TimeSeriesChart.tsx` | Read theme colors, `from`/`to` props, linked crosshairs, click-to-isolate, brush-to-zoom, tooltip transitions |
| `frontend/src/components/metrics/MetricsPanel.tsx` | `ChartSyncProvider`, custom range picker, range state management |
| `frontend/src/components/DiskUsageSection.tsx` | Theme colors, hover expand, tooltip transitions |
| `frontend/src/pages/ClusterOverview.tsx` | Stack drill-down chart configuration, new queries |
| `frontend/src/components/metrics/ChartSyncProvider.tsx` | New: shared crosshair sync context |
| `frontend/src/components/metrics/RangePicker.tsx` | New: custom date-time range picker dropdown |
| `frontend/src/components/metrics/StackDrillDownChart.tsx` | New: stack drill-down wrapper around TimeSeriesChart |
| `frontend/package.json` | Remove `uplot-react`, add `chartjs-plugin-zoom` |
| `CLAUDE.md` | Update chart library reference |

## 10. Dependencies

- `chartjs-plugin-zoom` — for brush-to-zoom drag selection
- Remove `uplot-react` — no longer used
- No other new dependencies (Chart.js and react-chartjs-2 already installed)
