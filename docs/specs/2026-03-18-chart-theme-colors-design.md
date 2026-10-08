# Chart Theme Colors Design

**Date:** 2026-03-18
**Status:** Approved

## Problem

Chart-related colors are hardcoded as hex literals across ~10 files. This makes theming impossible and scatters visual decisions throughout the codebase.

## Goal

Move all chart/visualization colors into CSS custom properties in `index.css` so they're theme-controlled. No component should contain a hardcoded hex color for chart purposes.

## New CSS Variables

Added to both `:root` and `.dark` blocks in `index.css` (same values for both themes — these are theme-invariant, but having them in both blocks makes future per-theme overrides trivial):

```css
/* Semantic chart colors */
--chart-cpu: var(--chart-1);
--chart-memory: #34d399;
--chart-ok: #10b981;
--chart-warning: #f59e0b;
--chart-critical: #ef4444;
--chart-reserved: #3b82f6;
--chart-crosshair: rgba(136, 136, 136, 0.3);
--chart-zoom: rgba(100, 143, 255, 0.15);
```

Plus corresponding `--color-chart-*` mappings for Tailwind utility generation (e.g., `text-chart-cpu`, `stroke-chart-memory`).

Note: `--chart-cpu` uses `var(--chart-1)` because it's semantically "the first chart series color." The others are standalone status/threshold colors unrelated to the palette series, so they use direct hex values. All are overridable per theme.

## Color Resolution Strategy

There are two contexts for consuming chart colors:

1. **SVG components** (Sparkline, ResourceGauge) — can use `var(--chart-*)` directly in `stroke`/`fill` attributes, or `currentColor` with Tailwind text classes.

2. **Chart.js components** (TimeSeriesChart, ResourceAllocationChart, and any component passing colors as props to Chart.js) — must use resolved hex values since Chart.js cannot parse CSS `var()` references. These use `getSemanticChartColor(name)` from `chartColors.ts`.

## chartColors.ts Changes

Add `getSemanticChartColor(name)` that reads `--chart-{name}` from computed styles and caches the result. Example: `getSemanticChartColor("cpu")` resolves `--chart-cpu` to its hex value at runtime.

## Component Changes

### Sparkline.tsx (SVG — uses currentColor)
- Remove `color` prop (hex string)
- Use `stroke="currentColor"` on the polyline
- Accept `className` for color control (caller passes e.g. `className="text-chart-1"`)
- Default color via `text-chart-1` class

### TaskSparkline.tsx (SVG — uses Tailwind classes)
- Remove hardcoded `COLORS` map (`#4f8cf6`, `#34d399`)
- Pass `className="text-chart-cpu"` / `className="text-chart-memory"` to Sparkline

### ResourceGauge.tsx (SVG — uses var() in stroke attribute)
- Replace `colorForValue()` hex returns with `var(--chart-ok)`, `var(--chart-warning)`, `var(--chart-critical)`
- Note: uses `--chart-*` not `--color-chart-*` — the `--color-` prefix is Tailwind's utility layer, not needed for direct CSS `var()` usage

### ServiceDetail.tsx (Chart.js — uses getSemanticChartColor)
- Memory chart `color: "#34d399"` → `color: getSemanticChartColor("memory")`
- Threshold colors in `cpuThresholds()`: `"#3b82f6"` → `getSemanticChartColor("reserved")`, `"#ef4444"` → `getSemanticChartColor("critical")`
- Same pattern for `memoryThresholds()`

### TaskDetail.tsx (Chart.js — uses getSemanticChartColor)
- Memory chart `color: "#34d399"` → `color: getSemanticChartColor("memory")`

### TimeSeriesChart.tsx (Chart.js — uses getSemanticChartColor)
- Crosshair colors → `getSemanticChartColor("crosshair")` (with appropriate alpha handling)
- Zoom drag area → `getSemanticChartColor("zoom")`

### ResourceAllocationChart.tsx (Chart.js — uses getSemanticChartColor)
- Limit line fallback `#ef4444` → `getSemanticChartColor("critical")`

### topologyTransform.ts (runtime — uses getChartColor)
- Replace hardcoded 12-color `COLORS` array with `getChartColor()` calls (already reads from `--chart-*` CSS variables, wraps around the 10-color palette)

## Out of Scope

- No changes to the 10 chart palette colors themselves (`--chart-1` through `--chart-10`)
- No changes to non-chart colors (UI elements, badges, status indicators)
