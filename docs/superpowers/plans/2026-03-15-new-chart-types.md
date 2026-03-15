# New Chart Types Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add stacked area toggle, horizontal bar resource allocation chart, and multi-ring doughnut to the Cetacean dashboard.

**Architecture:** Three independent features: (1) stacked area as a mode toggle on existing TimeSeriesChart, (2) new ResourceAllocationChart component for ServiceDetail, (3) refactored DoughnutChart with two-ring layout. Each feature is self-contained and can be built/tested independently.

**Tech Stack:** Chart.js 4.x, react-chartjs-2, Tailwind CSS v4, React 19

**Spec:** `docs/superpowers/specs/2026-03-15-new-chart-types-design.md`

---

## Chunk 1: Stacked Area Toggle

### Task 1: Add stackable prop and toggle UI to TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add stackable prop and stacked state**

Add to the Props interface (after `onSeriesInfo`):

```typescript
stackable?: boolean;
```

Destructure it in the component. Add state:

```typescript
const [stacked, setStacked] = useState(false);
```

- [ ] **Step 2: Add toggle icons to chart header**

Import icons:
```typescript
import { RefreshCw, BarChart3, LineChart, AreaChart } from "lucide-react";
```

Replace the chart header JSX (currently `flex items-center justify-between`):

```tsx
<div className="flex items-center gap-2 px-4 pt-4 pb-2">
  <span className="text-sm font-medium">{title}</span>
  {stackable && (
    <div className="flex items-center gap-0.5 ml-1">
      <button
        onClick={() => setStacked(false)}
        className={`p-0.5 rounded ${!stacked ? "bg-muted" : "hover:bg-muted/50"}`}
        title="Line chart"
      >
        <LineChart className="size-3.5" />
      </button>
      <button
        onClick={() => setStacked(true)}
        className={`p-0.5 rounded ${stacked ? "bg-muted" : "hover:bg-muted/50"}`}
        title="Stacked area"
      >
        <AreaChart className="size-3.5" />
      </button>
    </div>
  )}
  {unit && <span className="text-xs text-muted-foreground ml-auto">{unit}</span>}
</div>
```

- [ ] **Step 3: Modify chartData for stacked mode**

The `chartData` construction currently builds datasets with `fill: !dimmed` and gradient backgrounds. In stacked mode, the behavior differs:

- `fill: 'stack'` instead of `fill: true`
- Solid color at 40% opacity instead of gradient
- When isolated (click-to-isolate), dimmed datasets get `data` replaced with zeros instead of opacity dimming

Store a reference to the original series data for restoring after isolation:

```typescript
const originalSeriesRef = useRef(fetchedData?.series);
if (fetchedData) originalSeriesRef.current = fetchedData.series;
```

Replace the `chartData` construction:

```typescript
const chartData: ChartData<"line"> | null = fetchedData
  ? {
      labels: fetchedData.labels,
      datasets: fetchedData.series.map((s, i) => {
        const dimmed = isolatedIndex != null && isolatedIndex !== i;
        if (stacked) {
          return {
            label: s.label,
            data: dimmed ? s.data.map(() => 0) : s.data,
            borderColor: s.color,
            borderWidth: 1,
            pointRadius: 0,
            pointHoverRadius: dimmed ? 0 : 3,
            pointHoverBackgroundColor: s.color,
            pointHoverBorderWidth: 0,
            tension: 0.3,
            fill: "stack" as const,
            backgroundColor: s.color + "66", // 40% opacity
          };
        }
        return {
          label: s.label,
          data: s.data,
          borderColor: dimmed ? s.color + "4D" : s.color,
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
    }
  : null;
```

- [ ] **Step 3b: Update options for stacked y-axis**

The `options` useMemo must conditionally enable `scales.y.stacked` for Chart.js to compute stacked baselines. Add `stacked` to the useMemo dependency array. In the `scales.y` config:

```typescript
y: {
  display: false,
  stacked: stacked || undefined,
  min: yMin,
  suggestedMax,
},
```

The `stacked` variable is the component's toggle state. Update the useMemo deps from `[yMin, suggestedMax]` to `[yMin, suggestedMax, stacked]`.

Without `scales.y.stacked: true`, Chart.js renders `fill: 'stack'` as `fill: 'origin'` — each dataset fills from zero instead of stacking.

- [ ] **Step 4: Add Total row to tooltip in stacked mode**

In the crosshairPlugin's `afterEvent` mousemove handler, after `items.sort(...)`, add:

```typescript
// In stacked mode, add a Total row using original (non-zeroed) data
if (stacked && originalSeriesRef.current) {
  const total = originalSeriesRef.current.reduce((sum, ser) => {
    const v = ser.data[idx];
    return sum + (v ?? 0);
  }, 0);
  items.push({
    label: "Total",
    color: "transparent",
    value: formatValue(total, unitRef.current),
    raw: total,
  });
}
```

Note: the Total row is pushed after the sort, so it always appears at the bottom of the tooltip regardless of its value.

- [ ] **Step 5: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: add stacked area toggle to TimeSeriesChart"
```

---

### Task 2: Pass stackable through StackDrillDownChart to ClusterOverview

**Files:**
- Modify: `frontend/src/components/metrics/StackDrillDownChart.tsx`
- Modify: `frontend/src/pages/ClusterOverview.tsx`

- [ ] **Step 1: Add stackable to StackDrillDownChart Props**

Add `stackable?: boolean` to the Props interface. Destructure it. Pass it through to `TimeSeriesChart`:

```tsx
<TimeSeriesChart
  {...existingProps}
  stackable={stackable}
/>
```

- [ ] **Step 2: Set stackable on ClusterOverview StackDrillDownCharts**

In `ClusterOverview.tsx`, add `stackable` to both StackDrillDownChart instances:

```tsx
<StackDrillDownChart
  title="CPU Usage (by Stack)"
  stackable
  {...otherProps}
/>
<StackDrillDownChart
  title="Memory Usage (by Stack)"
  stackable
  {...otherProps}
/>
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/StackDrillDownChart.tsx frontend/src/pages/ClusterOverview.tsx
git commit -m "feat: enable stacked area toggle on ClusterOverview charts"
```

---

## Chunk 2: Horizontal Bar Resource Allocation Chart

### Task 3: Create ResourceAllocationChart component

**Files:**
- Create: `frontend/src/components/metrics/ResourceAllocationChart.tsx`
- Modify: `frontend/src/components/metrics/index.ts`

- [ ] **Step 1: Write the component**

```typescript
// frontend/src/components/metrics/ResourceAllocationChart.tsx
import { useMemo } from "react";
import {
  Chart as ChartJS,
  BarElement,
  CategoryScale,
  LinearScale,
  type ChartOptions,
  type Plugin,
} from "chart.js";
import { Bar } from "react-chartjs-2";
import { getChartColor } from "../../lib/chartColors";

ChartJS.register(BarElement, CategoryScale, LinearScale);

interface BarChartProps {
  title: string;
  reserved?: number;
  actual?: number;
  limit?: number;
  unit: "%" | "bytes";
}

function formatBarValue(v: number, unit: "%" | "bytes"): string {
  if (unit === "%") return `${v.toFixed(1)}%`;
  if (v >= 1e9) return `${(v / 1e9).toFixed(1)} GB`;
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)} MB`;
  if (v >= 1e3) return `${(v / 1e3).toFixed(1)} KB`;
  return `${v.toFixed(0)} B`;
}

function AllocationBar({ title, reserved, actual, limit, unit }: BarChartProps) {
  const color = getChartColor(0);
  const hasData = reserved != null || actual != null;
  if (!hasData) return null;

  const labels: string[] = [];
  const values: number[] = [];
  const colors: string[] = [];

  if (reserved != null) {
    labels.push("Reserved");
    values.push(reserved);
    colors.push(color + "4D"); // 30% opacity
  }
  if (actual != null) {
    labels.push("Actual");
    values.push(actual);
    colors.push(color);
  }

  const chartData = {
    labels,
    datasets: [{
      data: values,
      backgroundColor: colors,
      borderRadius: 4,
      barThickness: 20,
    }],
  };

  const limitPlugin = useMemo<Plugin<"bar">>(() => ({
    id: "limitMarker",
    afterDatasetsDraw(chart) {
      if (limit == null) return;
      const { ctx, chartArea, scales } = chart;
      if (!chartArea || !scales.x) return;
      const xPixel = scales.x.getPixelForValue(limit);
      if (xPixel < chartArea.left || xPixel > chartArea.right) return;
      ctx.save();
      ctx.strokeStyle = getComputedStyle(chart.canvas).getPropertyValue("--destructive").trim() || "#ef4444";
      ctx.lineWidth = 2;
      ctx.setLineDash([6, 4]);
      ctx.beginPath();
      ctx.moveTo(xPixel, chartArea.top);
      ctx.lineTo(xPixel, chartArea.bottom);
      ctx.stroke();
      ctx.restore();
    },
  }), [limit]);

  const maxVal = Math.max(...values, limit ?? 0) * 1.15;

  const options = useMemo<ChartOptions<"bar">>(() => ({
    indexAxis: "y" as const,
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        callbacks: {
          label: (ctx) => ` ${formatBarValue(ctx.parsed.x, unit)}`,
        },
      },
    },
    scales: {
      x: {
        display: false,
        min: 0,
        max: maxVal,
      },
      y: {
        grid: { display: false },
        ticks: {
          font: { size: 11 },
        },
      },
    },
  }), [unit, maxVal]);

  const plugins = useMemo(() => [limitPlugin], [limitPlugin]);

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-medium">{title}</span>
        {limit != null && (
          <span className="text-[11px] text-muted-foreground">
            Limit: {formatBarValue(limit, unit)}
          </span>
        )}
      </div>
      <div className="h-[80px]">
        <Bar data={chartData} options={options} plugins={plugins} />
      </div>
    </div>
  );
}

interface Props {
  cpuReserved?: number;
  cpuLimit?: number;
  cpuActual?: number;
  memReserved?: number;
  memLimit?: number;
  memActual?: number;
}

export default function ResourceAllocationChart({
  cpuReserved,
  cpuLimit,
  cpuActual,
  memReserved,
  memLimit,
  memActual,
}: Props) {
  const hasCpu = cpuReserved != null || cpuActual != null || cpuLimit != null;
  const hasMem = memReserved != null || memActual != null || memLimit != null;

  if (!hasCpu && !hasMem) return null;

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {hasCpu && (
        <div className="rounded-lg border bg-card p-4">
          <AllocationBar title="CPU" reserved={cpuReserved} actual={cpuActual} limit={cpuLimit} unit="%" />
        </div>
      )}
      {hasMem && (
        <div className="rounded-lg border bg-card p-4">
          <AllocationBar title="Memory" reserved={memReserved} actual={memActual} limit={memLimit} unit="bytes" />
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Export from barrel**

Add to `frontend/src/components/metrics/index.ts`:

```typescript
export { default as ResourceAllocationChart } from "./ResourceAllocationChart";
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/ResourceAllocationChart.tsx frontend/src/components/metrics/index.ts
git commit -m "feat: add ResourceAllocationChart for service resource allocation"
```

---

### Task 4: Wire ResourceAllocationChart into ServiceDetail

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Import the component**

```typescript
import { MetricsPanel, ResourceAllocationChart, type Threshold } from "../components/metrics";
```

- [ ] **Step 2: Add state and fetch for actual usage**

Inside the `ServiceDetail` component, after the existing state declarations, add:

```typescript
const [cpuActual, setCpuActual] = useState<number | undefined>();
const [memActual, setMemActual] = useState<number | undefined>();
```

Add an effect that fetches actual usage when the service loads and cAdvisor is available. Use `serviceName` (not the `service` object) as the dependency to avoid refiring on every SSE update:

```typescript
const serviceName = service?.Spec.Name;

useEffect(() => {
  if (!serviceName || !hasCadvisor) return;
  let cancelled = false;

  api.metricsQuery(
    `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"}[5m])) * 100`
  ).then((resp) => {
    if (cancelled) return;
    const val = resp.data?.result?.[0]?.value?.[1];
    if (val != null) setCpuActual(Number(val));
  }).catch(() => {});

  api.metricsQuery(
    `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"})`
  ).then((resp) => {
    if (cancelled) return;
    const val = resp.data?.result?.[0]?.value?.[1];
    if (val != null) setMemActual(Number(val));
  }).catch(() => {});

  return () => { cancelled = true; };
}, [serviceName, hasCadvisor]);
```

- [ ] **Step 3: Compute scaled reservation/limit values**

The reservations and limits are per-task. Scale by running replica count:

```typescript
const runningTasks = tasks?.filter((t) => t.Status?.State === "running").length ?? 0;
const resources = service?.Spec?.TaskTemplate?.Resources;
const cpuReserved = resources?.Reservations?.NanoCPUs
  ? (resources.Reservations.NanoCPUs / 1e9) * 100 * runningTasks
  : undefined;
const cpuLimit = resources?.Limits?.NanoCPUs
  ? (resources.Limits.NanoCPUs / 1e9) * 100 * runningTasks
  : undefined;
const memReserved = resources?.Reservations?.MemoryBytes
  ? resources.Reservations.MemoryBytes * runningTasks
  : undefined;
const memLimit = resources?.Limits?.MemoryBytes
  ? resources.Limits.MemoryBytes * runningTasks
  : undefined;
```

Where `tasks` is the task list already fetched on the page. Check where the current tasks data is — it's likely already in component state.

- [ ] **Step 4: Render the chart section**

After the existing MetricsPanel section, add:

```tsx
{(cpuReserved != null || cpuLimit != null || memReserved != null || memLimit != null) && (
  <div className="mb-6">
    <CollapsibleSection title="Resource Allocation">
      <ResourceAllocationChart
        cpuReserved={cpuReserved}
        cpuLimit={cpuLimit}
        cpuActual={cpuActual}
        memReserved={memReserved}
        memLimit={memLimit}
        memActual={memActual}
      />
    </CollapsibleSection>
  </div>
)}
```

- [ ] **Step 5: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add resource allocation bars to service detail"
```

---

## Chunk 3: Multi-Ring Doughnut

### Task 5: Generalize withMinSlice

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

- [ ] **Step 1: Refactor withMinSlice to accept number[]**

Replace the current `withMinSlice` function:

```typescript
/** Ensure very small slices are still visible by enforcing a minimum display value. */
function withMinSlice(values: number[], total?: number): number[] {
  const sum = total ?? values.reduce((s, v) => s + v, 0);
  if (sum === 0) return values.map(() => 0);
  const minFraction = 0.04;
  return values.map((v) => Math.max(v, sum * minFraction));
}
```

Update the existing call site in `DoughnutChart` — it currently passes `DiskUsageSummary[]`:

```typescript
// Before:  data: withMinSlice(data),
// After:
data: withMinSlice(data.map((d) => d.totalSize)),
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/DiskUsageSection.tsx
git commit -m "refactor: generalize withMinSlice to accept number[]"
```

---

### Task 6: Refactor DoughnutChart to two-ring layout

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

- [ ] **Step 1: Build two-dataset chart data**

In the `DoughnutChart` component, replace the `chartData` useMemo:

```typescript
const chartData = useMemo(() => {
  const outerData = data.map((d) => d.totalSize);
  const outerColors = data.map((_, i) => getChartColor(i));

  // Inner ring: interleaved [nonReclaim, reclaim, nonReclaim, reclaim, ...]
  const innerData: number[] = [];
  const innerColors: string[] = [];
  for (let i = 0; i < data.length; i++) {
    const d = data[i];
    const color = getChartColor(i);
    const nonReclaim = d.totalSize - d.reclaimable;
    innerData.push(nonReclaim, d.reclaimable);
    innerColors.push(color, color + "66"); // full, 40% opacity
  }

  const total = data.reduce((s, d) => s + d.totalSize, 0);

  return {
    labels: data.map((d) => typeMeta[d.type]?.label ?? d.type),
    datasets: [
      {
        data: withMinSlice(outerData),
        backgroundColor: outerColors,
        borderWidth: 0,
        borderRadius: 4,
        spacing: 3,
        hoverOffset: 3,
        weight: 2,
        _rawData: data,
        _ring: "outer" as const,
      },
      {
        data: withMinSlice(innerData, total),
        backgroundColor: innerColors,
        borderWidth: 0,
        borderRadius: 3,
        spacing: 2,
        hoverOffset: 3,
        weight: 1,
        _rawData: data,
        _ring: "inner" as const,
      },
    ],
  };
}, [data]);
```

- [ ] **Step 2: Update the external tooltip handler**

The `externalTooltipHandler` needs to handle both rings. The tooltip differentiates by `datasetIndex`:

Update the handler's content-building section. After getting `idx` and checking `rawData`:

```typescript
const datasetIndex = model.dataPoints[0].datasetIndex;
const idx = model.dataPoints[0].dataIndex;
const dataset = chart.data.datasets[datasetIndex];
const rawData = (dataset as unknown as { _rawData: DiskUsageSummary[] })._rawData;
const ring = (dataset as unknown as { _ring: string })._ring;

if (!rawData) return;

let label: string;
let color: string;
let size: string;
let pctText: string;

if (ring === "outer") {
  // Outer ring: same as before
  const d = rawData[idx];
  if (!d) return;
  const total = rawData.reduce((sum, r) => sum + r.totalSize, 0);
  color = getChartColor(idx);
  label = typeMeta[d.type]?.label ?? d.type;
  size = formatBytes(d.totalSize);
  const pct = total > 0 ? Math.round((d.totalSize / total) * 100) : 0;
  pctText = `${pct}% of total`;
} else {
  // Inner ring: idx is interleaved (0=img_nonreclaim, 1=img_reclaim, 2=bc_nonreclaim, ...)
  const typeIdx = Math.floor(idx / 2);
  const isReclaimable = idx % 2 === 1;
  const d = rawData[typeIdx];
  if (!d) return;
  color = getChartColor(typeIdx);
  label = typeMeta[d.type]?.label ?? d.type;
  const value = isReclaimable ? d.reclaimable : d.totalSize - d.reclaimable;
  size = formatBytes(value);
  const pct = d.totalSize > 0 ? Math.round((value / d.totalSize) * 100) : 0;
  pctText = isReclaimable
    ? `${pct}% reclaimable`
    : `${pct}% in use`;
  if (isReclaimable) color = color + "66";
}
```

Then build the tooltip DOM using these variables (replace the existing `buildTooltipEl` call or inline the construction).

- [ ] **Step 3: Verify ring sizing**

The spec says to keep `cutout: "62%"` and use per-dataset `weight` for ring proportions. The plan's Step 1 already sets `weight: 2` on the outer dataset and `weight: 1` on the inner dataset. Chart.js uses `weight` to allocate proportional ring widths within the cutout — no cutout change needed. Verify the two rings are both visible; if the inner ring is too thin, adjust the weight ratio (e.g., `weight: 3` outer, `weight: 2` inner).

- [ ] **Step 4: Verify build**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/DiskUsageSection.tsx
git commit -m "feat: refactor doughnut to two-ring layout with reclaimable inner ring"
```

---

### Task 7: Final verification

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

- [ ] **Step 4: Build**

```bash
cd frontend && npm run build
```

Expected: Build succeeds.

- [ ] **Step 5: Visual smoke test**

```bash
cd frontend && npm run dev
```

Verify:
- ClusterOverview: line/stacked toggle icons visible, switching works, stacked area fills correctly, Total row in tooltip
- ServiceDetail (for a service with resource limits): Resource Allocation section shows horizontal bars with limit marker
- Disk Usage: two-ring doughnut visible, outer ring shows totals, inner ring shows reclaimable split, tooltips differentiate
