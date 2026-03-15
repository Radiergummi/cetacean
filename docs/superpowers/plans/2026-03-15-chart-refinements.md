# Chart Refinements Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refine all charts for production/demo quality with CVD-safe colors, interactive features (linked crosshairs, click-to-isolate, brush-to-zoom), stack drill-down, and polish.

**Architecture:** Theme-integrated color palette via CSS custom properties. Chart interactions built on Chart.js plugins and a shared React sync context. Stack drill-down as a new wrapper component around TimeSeriesChart. Custom range picker as a standalone component composed into MetricsPanel.

**Tech Stack:** Chart.js 4.x, react-chartjs-2, chartjs-plugin-zoom, Tailwind CSS v4, React 19

**Spec:** `docs/superpowers/specs/2026-03-15-chart-refinements-design.md`

---

## Chunk 1: Foundation (Color Palette + Cleanup)

### Task 1: Remove uplot-react and update CLAUDE.md

**Files:**
- Modify: `frontend/package.json` (remove `uplot-react` dependency)
- Modify: `CLAUDE.md` (update chart library references)

- [ ] **Step 1: Remove uplot-react**

```bash
cd frontend && npm uninstall uplot-react
```

- [ ] **Step 2: Verify no remaining uplot imports**

```bash
cd frontend && grep -r "uplot" src/ --include="*.ts" --include="*.tsx"
```

Expected: No matches (or only the comment in `MetricsPanel.test.tsx` which was already updated).

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`, replace all references to "uPlot" with "Chart.js":
- Line mentioning "uPlot for time-series charts" → "Chart.js for time-series and doughnut charts"
- Any other uPlot mentions in the architecture section

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json CLAUDE.md
git commit -m "chore: remove uplot-react, update CLAUDE.md chart references"
```

---

### Task 2: Replace color palette in CSS theme

**Files:**
- Modify: `frontend/src/index.css:28-32` (light theme chart vars)
- Modify: `frontend/src/index.css:64-68` (dark theme chart vars)
- Modify: `frontend/src/index.css:100-104` (theme inline chart color mappings)

- [ ] **Step 1: Replace light theme chart variables (lines 28-32)**

Replace the 5 OKLCH chart variables with 10 hex IBM Carbon/Wong values:

```css
  --chart-1: #648FFF;
  --chart-2: #FFB000;
  --chart-3: #DC267F;
  --chart-4: #785EF0;
  --chart-5: #FE6100;
  --chart-6: #02D4F5;
  --chart-7: #FFD966;
  --chart-8: #CF9FFF;
  --chart-9: #FF85B3;
  --chart-10: #47C1BF;
```

- [ ] **Step 2: Replace dark theme chart variables (lines 64-68)**

Same 10 values as light theme (identical in both modes).

- [ ] **Step 3: Expand theme inline mappings (lines 100-104)**

Replace the 5 `--color-chart-*` lines with 10:

```css
  --color-chart-1: var(--chart-1);
  --color-chart-2: var(--chart-2);
  --color-chart-3: var(--chart-3);
  --color-chart-4: var(--chart-4);
  --color-chart-5: var(--chart-5);
  --color-chart-6: var(--chart-6);
  --color-chart-7: var(--chart-7);
  --color-chart-8: var(--chart-8);
  --color-chart-9: var(--chart-9);
  --color-chart-10: var(--chart-10);
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/index.css
git commit -m "feat: replace chart palette with CVD-safe IBM Carbon/Wong colors"
```

---

### Task 3: Create shared chart color utility

**Files:**
- Create: `frontend/src/lib/chartColors.ts`
- Test: `frontend/src/lib/chartColors.test.ts`

- [ ] **Step 1: Write the test**

```typescript
// frontend/src/lib/chartColors.test.ts
import { describe, it, expect } from "vitest";
import { CHART_COLORS, getChartColor } from "./chartColors";

describe("chartColors", () => {
  it("exports 10 fallback colors", () => {
    expect(CHART_COLORS).toHaveLength(10);
  });

  it("getChartColor returns color by index with modulo wrapping", () => {
    expect(getChartColor(0)).toBe(CHART_COLORS[0]);
    expect(getChartColor(10)).toBe(CHART_COLORS[0]); // wraps
    expect(getChartColor(3)).toBe(CHART_COLORS[3]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npx vitest run src/lib/chartColors.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Write implementation**

```typescript
// frontend/src/lib/chartColors.ts

/** IBM Carbon / Bang Wong CVD-safe palette — fallback values matching CSS --chart-* vars. */
export const CHART_COLORS = [
  "#648FFF", // Blue
  "#FFB000", // Gold
  "#DC267F", // Magenta
  "#785EF0", // Purple
  "#FE6100", // Orange
  "#02D4F5", // Cyan
  "#FFD966", // Amber
  "#CF9FFF", // Lavender
  "#FF85B3", // Pink
  "#47C1BF", // Teal
];

/** Cached resolved colors from CSS custom properties. */
let resolvedColors: string[] | null = null;

function resolveColors(): string[] {
  if (resolvedColors) return resolvedColors;
  if (typeof document === "undefined") return CHART_COLORS;
  const style = getComputedStyle(document.documentElement);
  resolvedColors = CHART_COLORS.map((fallback, i) => {
    const val = style.getPropertyValue(`--chart-${i + 1}`).trim();
    return val || fallback;
  });
  return resolvedColors;
}

/**
 * Get a chart color by index (wraps around).
 * Reads from CSS custom properties (cached after first call), falls back to hex constants.
 */
export function getChartColor(index: number): string {
  const colors = resolveColors();
  return colors[index % colors.length];
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd frontend && npx vitest run src/lib/chartColors.test.ts
```

Expected: PASS (jsdom won't have CSS vars, so it falls back to constants — which is what the test checks).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/chartColors.ts frontend/src/lib/chartColors.test.ts
git commit -m "feat: add shared chart color utility with CVD-safe palette"
```

---

### Task 4: Wire theme colors into TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Replace hardcoded CHART_COLORS import**

Replace the `CHART_COLORS` array (lines 58-67) and add the import:

```typescript
import { getChartColor, CHART_COLORS } from "../../lib/chartColors";
```

Remove the local `CHART_COLORS` constant entirely.

- [ ] **Step 2: Update color references**

In the `fetchData` callback, replace:
```typescript
const color = colorOverride ?? CHART_COLORS[i % CHART_COLORS.length];
```
with:
```typescript
const color = colorOverride ?? getChartColor(i);
```

- [ ] **Step 3: Verify build and tests**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "refactor: use shared chart colors in TimeSeriesChart"
```

---

### Task 5: Wire theme colors into DiskUsageSection

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

- [ ] **Step 1: Replace hardcoded CHART_COLORS**

Replace the local `CHART_COLORS` (line 12) with import:

```typescript
import { getChartColor, CHART_COLORS } from "../lib/chartColors";
```

Remove the local constant. Update all `CHART_COLORS[i % CHART_COLORS.length]` references to use `getChartColor(i)`.

- [ ] **Step 2: Verify build and tests**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DiskUsageSection.tsx
git commit -m "refactor: use shared chart colors in DiskUsageSection"
```

---

## Chunk 2: Tooltip Transitions + Doughnut Polish

### Task 6: Add tooltip transitions to TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

The current tooltip is conditionally rendered via `{tooltip && state === "data" && <div ...>}`. To support CSS opacity transitions, the element must always be mounted and its visibility toggled via opacity.

- [ ] **Step 1: Refactor tooltip to always-mounted with opacity**

Replace the conditional tooltip rendering (currently around the `<div className="relative">` block at the end of the component) with:

```tsx
<div
  ref={tooltipElRef}
  className="absolute pointer-events-none z-20 rounded-md ring-1 ring-border/50 bg-popover/80 backdrop-blur-sm backdrop-saturate-200 px-3 py-2.5 text-xs leading-snug shadow-lg"
  style={{
    left: tooltip ? tooltipLeft(tooltip, tooltipElRef.current) : 0,
    top: tooltip?.top ?? 0,
    opacity: tooltip && state === "data" ? 1 : 0,
    transition: tooltip ? "opacity 50ms ease" : "opacity 100ms ease",
  }}
>
  {tooltip && (
    <>
      <div className="font-semibold mb-1.5 text-foreground">{tooltip.time}</div>
      {tooltip.series.map((s) => (
        <div key={s.label} className="flex items-center gap-2 whitespace-nowrap">
          {s.dashed ? (
            <span className="w-3 shrink-0 border-t-2 border-dashed" style={{ borderColor: s.color }} />
          ) : (
            <span className="w-1 shrink-0 h-3 rounded-sm" style={{ background: s.color }} />
          )}
          <span className="text-muted-foreground">{s.label}</span>
          <span className="font-semibold ms-auto ps-4 text-foreground">{s.value}</span>
        </div>
      ))}
    </>
  )}
</div>
```

- [ ] **Step 2: Verify visually**

```bash
cd frontend && npm run dev
```

Hover over a chart — tooltip should fade in quickly (50ms) and fade out slightly slower (100ms). Position should update immediately without lag.

- [ ] **Step 3: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: add 50ms/100ms opacity transitions to chart tooltips"
```

---

### Task 7: Add tooltip transitions to DiskUsageSection

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

- [ ] **Step 1: Add transition to external tooltip handler**

In the `externalTooltipHandler` function, update the style assignment. When showing (`opacity: "1"`), set `transition: "opacity 50ms ease"`. When hiding (`opacity: "0"`), set `transition: "opacity 100ms ease"`.

In the `Object.assign(el.style, { ... })` block, add:

```typescript
transition: model.opacity === 0 ? "opacity 100ms ease" : "opacity 50ms ease",
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DiskUsageSection.tsx
git commit -m "feat: add tooltip transitions to doughnut chart"
```

---

### Task 8: Doughnut hover expand

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

- [ ] **Step 1: Add hoverOffset to dataset config**

In the `chartData` useMemo (the dataset object), add:

```typescript
hoverOffset: 3,
```

alongside the existing `borderWidth`, `borderRadius`, `spacing` properties.

- [ ] **Step 2: Verify visually**

```bash
cd frontend && npm run dev
```

Hover over a doughnut slice — it should expand outward by 3px.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DiskUsageSection.tsx
git commit -m "feat: add hover expand effect to doughnut chart"
```

---

## Chunk 3: Linked Crosshairs

### Task 9: Create ChartSyncProvider

**Files:**
- Create: `frontend/src/components/metrics/ChartSyncProvider.tsx`
- Test: `frontend/src/components/metrics/ChartSyncProvider.test.tsx`

- [ ] **Step 1: Write the test**

```typescript
// frontend/src/components/metrics/ChartSyncProvider.test.tsx
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { ChartSyncProvider, useChartSync } from "./ChartSyncProvider";

function wrapper({ children }: { children: ReactNode }) {
  return <ChartSyncProvider syncKey="test">{children}</ChartSyncProvider>;
}

describe("ChartSyncProvider", () => {
  it("broadcasts timestamp to subscribers", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });

    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.publish("chart2", 1710000000));

    expect(listener).toHaveBeenCalledWith(1710000000);
  });

  it("does not echo timestamp back to publisher", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });

    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.publish("chart1", 1710000000));

    expect(listener).not.toHaveBeenCalled();
  });

  it("clears all listeners on clear()", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });

    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.clear());
    act(() => result.current.publish("chart2", 1710000000));

    expect(listener).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd frontend && npx vitest run src/components/metrics/ChartSyncProvider.test.tsx
```

Expected: FAIL — module not found.

- [ ] **Step 3: Write implementation**

```typescript
// frontend/src/components/metrics/ChartSyncProvider.tsx
import { createContext, useContext, useRef, useCallback, type ReactNode } from "react";

type Listener = (timestamp: number) => void;

interface ChartSyncApi {
  subscribe: (chartId: string, listener: Listener) => () => void;
  publish: (chartId: string, timestamp: number) => void;
  clear: () => void;
}

const ChartSyncContext = createContext<ChartSyncApi | null>(null);

export function ChartSyncProvider({
  syncKey,
  children,
}: {
  syncKey: string;
  children: ReactNode;
}) {
  const listenersRef = useRef<Map<string, Listener>>(new Map());

  const subscribe = useCallback((chartId: string, listener: Listener) => {
    listenersRef.current.set(chartId, listener);
    return () => {
      listenersRef.current.delete(chartId);
    };
  }, []);

  const publish = useCallback((chartId: string, timestamp: number) => {
    for (const [id, listener] of listenersRef.current) {
      if (id !== chartId) listener(timestamp);
    }
  }, []);

  const clear = useCallback(() => {
    listenersRef.current.clear();
  }, []);

  return (
    <ChartSyncContext.Provider value={{ subscribe, publish, clear }}>
      {children}
    </ChartSyncContext.Provider>
  );
}

export function useChartSync(): ChartSyncApi {
  const ctx = useContext(ChartSyncContext);
  if (!ctx) {
    // Return a no-op API when used outside a provider (standalone charts)
    return {
      subscribe: () => () => {},
      publish: () => {},
      clear: () => {},
    };
  }
  return ctx;
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd frontend && npx vitest run src/components/metrics/ChartSyncProvider.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/metrics/ChartSyncProvider.tsx frontend/src/components/metrics/ChartSyncProvider.test.tsx
git commit -m "feat: add ChartSyncProvider for linked crosshairs"
```

---

### Task 10: Wrap MetricsPanel grid with ChartSyncProvider

**Files:**
- Modify: `frontend/src/components/metrics/MetricsPanel.tsx`
- Modify: `frontend/src/components/metrics/index.ts`

- [ ] **Step 1: Import and wrap**

In `MetricsPanel.tsx`, import `ChartSyncProvider`:

```typescript
import { ChartSyncProvider } from "./ChartSyncProvider";
```

Wrap the grid div:

```tsx
<ChartSyncProvider syncKey="metrics">
  <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
    {charts.map((chart) => (
      <TimeSeriesChart key={chart.query} {...chart} range={range} refreshKey={refreshKey} syncKey="metrics" />
    ))}
  </div>
</ChartSyncProvider>
```

- [ ] **Step 2: Export from barrel**

Add to `frontend/src/components/metrics/index.ts`:

```typescript
export { ChartSyncProvider, useChartSync } from "./ChartSyncProvider";
```

- [ ] **Step 3: Verify build and tests**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/MetricsPanel.tsx frontend/src/components/metrics/index.ts
git commit -m "feat: wrap MetricsPanel with ChartSyncProvider"
```

---

### Task 11: Wire crosshair sync into TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Import useChartSync and add subscription**

Add import:

```typescript
import { useChartSync } from "./ChartSyncProvider";
```

Inside the component, generate a stable chart ID and subscribe:

```typescript
const chartId = useRef(`tsc-${Math.random().toString(36).slice(2, 8)}`).current;
const sync = useChartSync();
const syncTimestampRef = useRef<number | null>(null);
```

Add an effect that subscribes to sync events and draws a crosshair + dots:

```typescript
useEffect(() => {
  return sync.subscribe(chartId, (timestamp) => {
    syncTimestampRef.current = timestamp;
    const chart = chartRef.current;
    if (!chart) return;
    chart.draw(); // triggers afterDraw which reads syncTimestampRef
  });
}, [chartId, sync]);
```

- [ ] **Step 2: Update crosshairPlugin to publish on hover**

In the `afterEvent` hook of `crosshairPlugin`, after setting the tooltip, add:

```typescript
const ts = fetchedData?.timestamps[idx];
if (ts != null) sync.publish(chartId, ts);
```

On mouseout, publish a clear signal:

```typescript
if (args.event.type === "mouseout") {
  sync.publish(chartId, -1); // sentinel for "clear"
  // ... existing tooltip clear
}
```

- [ ] **Step 3: Update afterDraw to render synced crosshair + dots**

In the `crosshairPlugin` `afterDraw` hook, after the existing crosshair drawing, add synced crosshair rendering:

```typescript
// Synced crosshair from sibling charts
const syncTs = syncTimestampRef.current;
if (syncTs != null && syncTs > 0) {
  const xPixel = chart.scales.x?.getPixelForValue(
    fetchedData?.timestamps.findIndex((t) => t >= syncTs) ?? -1
  );
  if (xPixel != null && xPixel >= chartArea.left && xPixel <= chartArea.right) {
    ctx.save();
    ctx.beginPath();
    ctx.moveTo(xPixel, chartArea.top);
    ctx.lineTo(xPixel, chartArea.bottom);
    ctx.lineWidth = 1;
    ctx.strokeStyle = "rgba(136,136,136,0.2)";
    ctx.setLineDash([4, 4]);
    ctx.stroke();

    // Draw dots on each series
    const datasets = chart.data.datasets;
    const idx = fetchedData?.timestamps.findIndex((t) => t >= syncTs) ?? -1;
    if (idx >= 0) {
      for (let si = 0; si < datasets.length; si++) {
        const val = datasets[si].data[idx] as number;
        if (val == null) continue;
        const yPixel = chart.scales.y.getPixelForValue(val);
        ctx.beginPath();
        ctx.arc(xPixel, yPixel, 3, 0, Math.PI * 2);
        ctx.fillStyle = datasets[si].borderColor as string;
        ctx.fill();
      }
    }
    ctx.restore();
  }
}
```

- [ ] **Step 4: Clear sync on mouseout**

In the subscriber callback, handle the sentinel value:

```typescript
return sync.subscribe(chartId, (timestamp) => {
  syncTimestampRef.current = timestamp > 0 ? timestamp : null;
  chartRef.current?.draw();
});
```

- [ ] **Step 5: Verify visually**

```bash
cd frontend && npm run dev
```

Open a page with multiple charts (e.g., Node Detail with 4 charts). Hover one chart — sibling charts should show a dashed crosshair line + colored dots at the same timestamp. Moving away clears them.

- [ ] **Step 6: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: wire linked crosshairs into TimeSeriesChart"
```

---

## Chunk 4: Click-to-Isolate

### Task 12: Add click-to-isolate to TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add isolated state and drag guard**

Add state at the top of the component:

```typescript
const [isolatedIndex, setIsolatedIndex] = useState<number | null>(null);
const justZoomedRef = useRef(false);
```

Reset on data refetch — add to the `fetchData` callback, after setting `fetchedData`:

```typescript
setIsolatedIndex(null);
```

The `justZoomedRef` prevents click-to-isolate from firing after a brush-to-zoom drag release. The zoom plugin's `onZoom` callback sets it to `true`, and the click handler checks/clears it.

- [ ] **Step 2: Add click handler to crosshairPlugin**

In the `afterEvent` hook, handle `click` events. The `justZoomedRef` guard prevents the click that fires on drag release from triggering isolation:

```typescript
if (args.event.type === "click") {
  // Skip if this click is the mouseup from a brush-to-zoom drag
  if (justZoomedRef.current) {
    justZoomedRef.current = false;
    return;
  }
  const { x, y } = args.event;
  if (x == null || y == null) return;
  const elements = chart.getElementsAtEventForMode(
    args.event.native as Event,
    "nearest",
    { intersect: false, axis: "x" },
    false,
  );
  if (elements.length > 0) {
    const clickedIdx = elements[0].datasetIndex;
    setIsolatedIndex((prev) => (prev === clickedIdx ? null : clickedIdx));
  } else {
    setIsolatedIndex(null); // click on background restores all
  }
}
```

- [ ] **Step 3: Apply opacity based on isolated state**

In the `chartData` construction, modify dataset properties based on `isolatedIndex`:

```typescript
datasets: fetchedData.series.map((s, i) => {
  const dimmed = isolatedIndex != null && isolatedIndex !== i;
  return {
    label: s.label,
    data: s.data,
    borderColor: dimmed ? s.color + "4D" : s.color, // 30% opacity hex suffix
    borderWidth: 1.5,
    pointRadius: 0,
    pointHoverRadius: dimmed ? 0 : 3,
    pointHoverBackgroundColor: s.color,
    pointHoverBorderWidth: 0,
    tension: 0.3,
    fill: !dimmed,
    backgroundColor: dimmed
      ? "transparent"
      : (ctx: { chart: ChartJS }) => {
          const chart = ctx.chart;
          if (!chart.chartArea) return s.color + "18";
          return makeGradient(chart.ctx, chart.chartArea, s.color);
        },
  };
}),
```

The `chartData` must be recomputed when `isolatedIndex` changes — ensure `isolatedIndex` is in the dependency array of the useMemo/construction.

- [ ] **Step 4: Verify visually**

```bash
cd frontend && npm run dev
```

On a multi-series chart (top 10 CPU), click a series line — others should dim to 30% opacity. Click again to restore. Click a different series to switch.

- [ ] **Step 5: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: add click-to-isolate for chart series"
```

---

## Chunk 5: Time Range Selection

### Task 13: Install chartjs-plugin-zoom

**Files:**
- Modify: `frontend/package.json`

- [ ] **Step 1: Install**

```bash
cd frontend && npm install chartjs-plugin-zoom
```

- [ ] **Step 2: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore: add chartjs-plugin-zoom dependency"
```

---

### Task 14: Create RangePicker component

**Files:**
- Create: `frontend/src/components/metrics/RangePicker.tsx`

- [ ] **Step 1: Write the component**

```typescript
// frontend/src/components/metrics/RangePicker.tsx
import { useState, useRef, useEffect } from "react";
import { Calendar, X } from "lucide-react";

interface Props {
  from: number | null;
  to: number | null;
  onApply: (from: number, to: number) => void;
  onClear: () => void;
}

const QUICK_PRESETS = [
  { label: "Last 2h", seconds: 7200 },
  { label: "Last 12h", seconds: 43200 },
  { label: "Last 48h", seconds: 172800 },
  { label: "Last 3d", seconds: 259200 },
];

function formatRange(from: number, to: number): string {
  const f = new Date(from * 1000);
  const t = new Date(to * 1000);
  const sameDay = f.toDateString() === t.toDateString();
  const dateFmt: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
  const timeFmt: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
  if (sameDay) {
    return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(undefined, timeFmt)} – ${t.toLocaleTimeString(undefined, timeFmt)}`;
  }
  return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(undefined, timeFmt)} – ${t.toLocaleDateString(undefined, dateFmt)} ${t.toLocaleTimeString(undefined, timeFmt)}`;
}

export default function RangePicker({ from, to, onApply, onClear }: Props) {
  const [open, setOpen] = useState(false);
  const [startInput, setStartInput] = useState("");
  const [endInput, setEndInput] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const handleApply = () => {
    const s = new Date(startInput).getTime() / 1000;
    const e = new Date(endInput).getTime() / 1000;
    if (!isNaN(s) && !isNaN(e) && s < e) {
      onApply(s, e);
      setOpen(false);
    }
  };

  const handlePreset = (seconds: number) => {
    const now = Math.floor(Date.now() / 1000);
    onApply(now - seconds, now);
    setOpen(false);
  };

  const isActive = from != null && to != null;

  return (
    <div ref={ref} className="relative">
      {isActive ? (
        <button
          onClick={onClear}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md border bg-primary/10 border-primary/30 text-foreground hover:bg-primary/20"
        >
          <Calendar className="size-3" />
          <span>{formatRange(from!, to!)}</span>
          <X className="size-3 opacity-60 hover:opacity-100" />
        </button>
      ) : (
        <button
          onClick={() => setOpen(!open)}
          className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-card hover:bg-muted"
          title="Custom range"
        >
          <Calendar className="size-3.5" />
        </button>
      )}

      {open && (
        <div className="absolute right-0 top-full mt-1 z-30 w-72 rounded-lg border bg-popover shadow-lg p-3 text-sm">
          <div className="grid grid-cols-2 gap-1.5 mb-3">
            {QUICK_PRESETS.map((p) => (
              <button
                key={p.label}
                onClick={() => handlePreset(p.seconds)}
                className="px-2 py-1.5 rounded-md text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                {p.label}
              </button>
            ))}
          </div>
          <div className="space-y-2">
            <label className="block">
              <span className="text-xs text-muted-foreground">From</span>
              <input
                type="datetime-local"
                value={startInput}
                onChange={(e) => setStartInput(e.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <label className="block">
              <span className="text-xs text-muted-foreground">To</span>
              <input
                type="datetime-local"
                value={endInput}
                onChange={(e) => setEndInput(e.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <button
              onClick={handleApply}
              className="w-full rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
            >
              Apply
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/metrics/RangePicker.tsx
git commit -m "feat: add custom date-time range picker component"
```

---

### Task 15: Integrate RangePicker and custom range into MetricsPanel

**Files:**
- Modify: `frontend/src/components/metrics/MetricsPanel.tsx`

- [ ] **Step 1: Add custom range state and URL persistence**

Import `RangePicker` and add state:

```typescript
import RangePicker from "./RangePicker";
```

Read `from`/`to` from URL params. If both present, they take precedence over `range`:

```typescript
const fromParam = params.get("from");
const toParam = params.get("to");
const customFrom = fromParam ? Number(fromParam) : null;
const customTo = toParam ? Number(toParam) : null;
const isCustomRange = customFrom != null && customTo != null;
```

Add handlers:

```typescript
const setCustomRange = (from: number, to: number) => {
  setParams((prev) => {
    const next = new URLSearchParams(prev);
    next.set("from", String(Math.floor(from)));
    next.set("to", String(Math.floor(to)));
    next.delete("range");
    return next;
  }, { replace: true });
};

const clearCustomRange = () => {
  setParams((prev) => {
    const next = new URLSearchParams(prev);
    next.delete("from");
    next.delete("to");
    return next;
  }, { replace: true });
};
```

- [ ] **Step 2: Add RangePicker to controls**

In the `controls` JSX, add after the segmented control:

```tsx
<RangePicker from={customFrom} to={customTo} onApply={setCustomRange} onClear={clearCustomRange} />
```

When custom range is active, deselect the segmented control by passing `value={isCustomRange ? "" : range}`.

- [ ] **Step 3: Pass from/to props to TimeSeriesChart**

Update the chart rendering:

```tsx
<TimeSeriesChart
  key={chart.query}
  {...chart}
  range={range}
  from={customFrom ?? undefined}
  to={customTo ?? undefined}
  refreshKey={refreshKey}
  syncKey="metrics"
/>
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/metrics/MetricsPanel.tsx
git commit -m "feat: integrate custom range picker into MetricsPanel"
```

---

### Task 16: Add from/to props and brush-to-zoom to TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add from/to to Props and register zoom plugin**

Add to Props interface:

```typescript
from?: number;
to?: number;
onRangeSelect?: (from: number, to: number) => void;
```

Add import and registration:

```typescript
import zoomPlugin from "chartjs-plugin-zoom";
ChartJS.register(/* existing */, zoomPlugin);
```

Update `fetchData` to use `from`/`to` when provided. **Add `from, to` to the `useCallback` dependency array** (alongside `query, range, title, colorOverride`):

```typescript
const rangeSec = RANGE_SECONDS[range] || 3600;
const now = Math.floor(Date.now() / 1000);
const start = from ?? now - rangeSec;
const end = to ?? now;
const step = Math.max(Math.floor((end - start) / 300), 15);
```

Use `start`/`end` instead of the locally computed values in the API call.

- [ ] **Step 2: Add zoom plugin config to options**

Add to the `options` object:

```typescript
plugins: {
  // ... existing legend, tooltip
  zoom: {
    zoom: {
      drag: {
        enabled: true,
        backgroundColor: "rgba(100, 143, 255, 0.1)", // chart-1 with low alpha
        borderColor: "rgba(100, 143, 255, 0.3)",
        borderWidth: 1,
        threshold: 5,
      },
      mode: "x" as const,
      onZoom: ({ chart }: { chart: ChartJS }) => {
        justZoomedRef.current = true; // prevent click-to-isolate on drag release
        if (!onRangeSelect || !fetchedData) return;
        const xScale = chart.scales.x;
        const minIdx = Math.max(0, Math.floor(xScale.min));
        const maxIdx = Math.min(fetchedData.timestamps.length - 1, Math.ceil(xScale.max));
        const fromTs = fetchedData.timestamps[minIdx];
        const toTs = fetchedData.timestamps[maxIdx];
        if (fromTs && toTs) onRangeSelect(fromTs, toTs);
        chart.resetZoom(); // reset chart zoom, let parent handle the range change
      },
    },
  },
},
```

- [ ] **Step 3: Wire onRangeSelect from MetricsPanel**

Back in `MetricsPanel.tsx`, pass `onRangeSelect={setCustomRange}` to each `TimeSeriesChart`.

- [ ] **Step 4: Verify visually**

```bash
cd frontend && npm run dev
```

Click-drag on a chart to select a time window. On release, the custom range chip should appear and all charts should refetch for the selected window.

- [ ] **Step 5: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx frontend/src/components/metrics/MetricsPanel.tsx
git commit -m "feat: add brush-to-zoom and custom time range support"
```

---

## Chunk 6: Stack Drill-Down

### Task 17: Create StackDrillDownChart component

**Files:**
- Create: `frontend/src/components/metrics/StackDrillDownChart.tsx`

- [ ] **Step 1: Write the component**

This component wraps `TimeSeriesChart` with drill-down state management. It swaps the query between the stacks-aggregated view and the per-service view.

```typescript
// frontend/src/components/metrics/StackDrillDownChart.tsx
import { useState, useCallback } from "react";
import { ChevronLeft } from "lucide-react";
import TimeSeriesChart from "./TimeSeriesChart";
import type { Threshold } from "./TimeSeriesChart";

interface Props {
  title: string;
  stackQuery: string;
  serviceQueryTemplate: string; // contains <STACK> placeholder
  unit?: string;
  yMin?: number;
  range: string;
  from?: number;
  to?: number;
  refreshKey?: number;
  syncKey?: string;
  onRangeSelect?: (from: number, to: number) => void;
  thresholds?: Threshold[];
}

export default function StackDrillDownChart({
  title,
  stackQuery,
  serviceQueryTemplate,
  unit,
  yMin,
  range,
  from,
  to,
  refreshKey,
  syncKey,
  onRangeSelect,
  thresholds,
}: Props) {
  const [drillStack, setDrillStack] = useState<string | null>(null);

  const handleSeriesDoubleClick = useCallback((seriesLabel: string) => {
    setDrillStack(seriesLabel);
  }, []);

  const handleBack = useCallback(() => {
    setDrillStack(null);
  }, []);

  const query = drillStack
    ? serviceQueryTemplate.replace("<STACK>", drillStack)
    : stackQuery;

  const chartTitle = drillStack ? `${title} (${drillStack})` : title;

  return (
    <div>
      {drillStack && (
        <button
          onClick={handleBack}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-1"
        >
          <ChevronLeft className="size-3" />
          All Stacks
        </button>
      )}
      <TimeSeriesChart
        title={chartTitle}
        query={query}
        unit={unit}
        yMin={yMin}
        range={range}
        from={from}
        to={to}
        refreshKey={refreshKey}
        syncKey={syncKey}
        onRangeSelect={onRangeSelect}
        thresholds={thresholds}
        onSeriesDoubleClick={handleSeriesDoubleClick}
      />
    </div>
  );
}
```

- [ ] **Step 2: Add onSeriesDoubleClick prop to TimeSeriesChart**

In `TimeSeriesChart.tsx`, add to Props:

```typescript
onSeriesDoubleClick?: (seriesLabel: string) => void;
```

**Important:** Chart.js only fires plugin events for types listed in `options.events`. Add `'dblclick'` to the chart's events array in the `options` object:

```typescript
events: ['mousemove', 'mouseout', 'click', 'dblclick', 'touchstart', 'touchmove'] as any,
```

Then in the `crosshairPlugin` `afterEvent` hook, add `dblclick` handling:

```typescript
if (args.event.type === "dblclick") {
  const elements = chart.getElementsAtEventForMode(
    args.event.native as Event,
    "nearest",
    { intersect: false, axis: "x" },
    false,
  );
  if (elements.length > 0 && onSeriesDoubleClick) {
    const label = chart.data.datasets[elements[0].datasetIndex]?.label;
    if (label) onSeriesDoubleClick(label);
  }
}
```

- [ ] **Step 3: Export from barrel**

Add to `frontend/src/components/metrics/index.ts`:

```typescript
export { default as StackDrillDownChart } from "./StackDrillDownChart";
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/metrics/StackDrillDownChart.tsx frontend/src/components/metrics/TimeSeriesChart.tsx frontend/src/components/metrics/index.ts
git commit -m "feat: add StackDrillDownChart component with drill-down state"
```

---

### Task 18: Wire stack drill-down into ClusterOverview

**Files:**
- Modify: `frontend/src/pages/ClusterOverview.tsx`

- [ ] **Step 1: Replace MetricsPanel charts with StackDrillDownChart**

Import the new component:

```typescript
import { StackDrillDownChart } from "../components/metrics";
```

Replace the current MetricsPanel `charts` prop for the "Resource Usage by Service" section. Instead of passing chart definitions to MetricsPanel, render two `StackDrillDownChart` instances inside the MetricsPanel grid directly.

The MetricsPanel still wraps them for range controls and `ChartSyncProvider`. Change the `charts` prop approach to a `children` pattern, or render `StackDrillDownChart` directly within a MetricsPanel that accepts `children`.

**Approach:** Use a React context (`MetricsPanelContext`) to provide panel-level props (range, from, to, refreshKey, syncKey, onRangeSelect) to children. This avoids fragile `cloneElement` prop injection.

In `MetricsPanel.tsx`, create and export the context:

```typescript
import { createContext, useContext } from "react";

interface MetricsPanelContextValue {
  range: string;
  from?: number;
  to?: number;
  refreshKey: number;
  syncKey: string;
  onRangeSelect: (from: number, to: number) => void;
}

const MetricsPanelContext = createContext<MetricsPanelContextValue | null>(null);

export function useMetricsPanelContext(): MetricsPanelContextValue | null {
  return useContext(MetricsPanelContext);
}
```

Add `children` to Props:

```typescript
interface Props {
  charts?: ChartDef[];
  children?: React.ReactNode;
  header?: React.ReactNode;
}
```

Wrap the grid in the context provider:

```tsx
<MetricsPanelContext.Provider value={{ range, from: customFrom ?? undefined, to: customTo ?? undefined, refreshKey, syncKey: "metrics", onRangeSelect: setCustomRange }}>
  <ChartSyncProvider syncKey="metrics">
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {children ?? charts?.map((chart) => (
        <TimeSeriesChart key={chart.query} {...chart} range={range} from={customFrom ?? undefined} to={customTo ?? undefined} refreshKey={refreshKey} syncKey="metrics" onRangeSelect={setCustomRange} />
      ))}
    </div>
  </ChartSyncProvider>
</MetricsPanelContext.Provider>
```

- [ ] **Step 2: Update StackDrillDownChart to consume context**

In `StackDrillDownChart.tsx`, import and use the context. Remove `range`, `from`, `to`, `refreshKey`, `syncKey`, `onRangeSelect` from the Props interface — these come from context:

```typescript
import { useMetricsPanelContext } from "./MetricsPanel";

// Inside the component:
const panel = useMetricsPanelContext();
// Use panel?.range, panel?.from, panel?.to, etc. when rendering TimeSeriesChart
// Fall back to direct props for standalone usage
```

- [ ] **Step 3: Update ClusterOverview**

Replace the charts array with children:

```tsx
<MetricsPanel header="Resource Usage by Stack">
  <StackDrillDownChart
    title="CPU Usage (by Stack)"
    stackQuery={`topk(10, sum by (container_label_com_docker_stack_namespace)(rate(container_cpu_usage_seconds_total{container_label_com_docker_stack_namespace!=""}[5m])) * 100)`}
    serviceQueryTemplate={`sum by (container_label_com_docker_swarm_service_name)(rate(container_cpu_usage_seconds_total{container_label_com_docker_stack_namespace="<STACK>", container_label_com_docker_swarm_service_name!=""}[5m])) * 100`}
    unit="%"
    yMin={0}
  />
  <StackDrillDownChart
    title="Memory Usage (by Stack)"
    stackQuery={`topk(10, sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes{container_label_com_docker_stack_namespace!=""}))`}
    serviceQueryTemplate={`sum by (container_label_com_docker_swarm_service_name)(container_memory_usage_bytes{container_label_com_docker_stack_namespace="<STACK>", container_label_com_docker_swarm_service_name!=""})`}
    unit="bytes"
    yMin={0}
  />
</MetricsPanel>
```

- [ ] **Step 3: Verify build and visual**

```bash
cd frontend && npx tsc -b --noEmit && npm run dev
```

On the cluster overview, charts should now show "by Stack" titles. Double-clicking a stack series should drill into that stack's services with a back button.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ClusterOverview.tsx frontend/src/components/metrics/MetricsPanel.tsx
git commit -m "feat: wire stack drill-down into ClusterOverview"
```

---

### Task 19: Add toggleable legend and loading overlay to StackDrillDownChart

**Files:**
- Modify: `frontend/src/components/metrics/StackDrillDownChart.tsx`
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add legend toggle state**

In `StackDrillDownChart.tsx`, add:

```typescript
const [showLegend, setShowLegend] = useState(false);
const [showAll, setShowAll] = useState(false);
```

When `showAll` is true, replace `topk(10, ...)` with `topk(30, ...)` in the query:

```typescript
const effectiveStackQuery = showAll
  ? stackQuery.replace("topk(10,", "topk(30,")
  : stackQuery;
```

- [ ] **Step 2: Render legend below chart**

Below the `TimeSeriesChart`, add a collapsible legend section. The legend entries come from the chart's series data. To get this data, add a new `onSeriesInfo` callback prop to `TimeSeriesChart` that fires after data fetch with the series labels and colors:

```typescript
onSeriesInfo?: (series: { label: string; color: string }[]) => void;
```

In `StackDrillDownChart`, store the series info in state and render:

```tsx
<div className="mt-1">
  <button
    onClick={() => setShowLegend((v) => !v)}
    className="text-[11px] text-muted-foreground hover:text-foreground"
  >
    {showLegend ? "Hide legend" : "Show legend"}
  </button>
  {showLegend && seriesInfo.length > 0 && (
    <div className="flex flex-wrap gap-x-3 gap-y-1 mt-1">
      {seriesInfo.map((s, i) => (
        <button
          key={s.label}
          onClick={() => handleIsolateSeries(i)}
          className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
          style={{ opacity: i >= 10 && !showAll ? 0.3 : 1 }}
        >
          <span className="w-2 h-2 rounded-full shrink-0" style={{ background: s.color }} />
          {s.label}
        </button>
      ))}
      {!drillStack && (
        <button
          onClick={() => setShowAll((v) => !v)}
          className="text-[11px] text-primary hover:underline"
        >
          {showAll ? "Top 10 only" : "Show all"}
        </button>
      )}
    </div>
  )}
</div>
```

- [ ] **Step 3: Add loading overlay for drill-down transitions**

In `TimeSeriesChart.tsx`, when `state === "loading"` and there is already `fetchedData` from a previous load, show an overlay spinner instead of replacing the chart:

```tsx
{state === "loading" && fetchedData && (
  <div className="absolute inset-0 flex items-start justify-end p-2 z-10">
    <RefreshCw className="size-3.5 animate-spin text-muted-foreground" />
  </div>
)}
```

This keeps the previous chart visible underneath while the new data loads, avoiding layout shift.

Change the existing loading state check to only show the skeleton when there's no previous data:

```tsx
{state === "loading" && !fetchedData && <div className="h-[200px] rounded bg-muted/50" />}
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/metrics/StackDrillDownChart.tsx frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: add toggleable legend and loading overlay for drill-down"
```

---

### Task 20: Final verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

```bash
cd frontend && npx vitest run
```

Expected: All tests pass.

- [ ] **Step 2: Type check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: Clean.

- [ ] **Step 3: Lint**

```bash
cd frontend && npm run lint
```

Expected: Clean.

- [ ] **Step 4: Full build**

```bash
cd frontend && npm run build
```

Expected: Build succeeds.

- [ ] **Step 5: Visual smoke test**

```bash
cd frontend && npm run dev
```

Verify on each page type:
- ClusterOverview: stack drill-down charts with double-click drill-in/back
- Node Detail: 4 charts with linked crosshairs
- Service Detail: charts with thresholds
- Disk Usage: doughnut with hover expand and themed colors
- All: custom range picker, brush-to-zoom, click-to-isolate, tooltip transitions
