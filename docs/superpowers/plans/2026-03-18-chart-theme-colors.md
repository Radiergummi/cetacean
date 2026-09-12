# Chart Theme Colors Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all hardcoded chart hex colors with CSS custom properties defined in the theme.

**Architecture:** Add semantic chart CSS variables to `index.css`, extend `chartColors.ts` with `getSemanticChartColor()` for Chart.js contexts, and update all components to reference theme colors instead of hex literals. SVG components use `currentColor`/`var()`; Chart.js components use runtime-resolved values via `getSemanticChartColor()`.

**Tech Stack:** React 19, Tailwind CSS v4, Chart.js, Vite.

---

## File Map

| Action | Path | Purpose |
|---|---|---|
| Modify | `frontend/src/index.css` | Add semantic chart CSS variables + Tailwind mappings |
| Modify | `frontend/src/lib/chartColors.ts` | Add `getSemanticChartColor()` |
| Modify | `frontend/src/components/metrics/Sparkline.tsx` | Use `currentColor` instead of `color` prop |
| Modify | `frontend/src/components/metrics/Sparkline.test.tsx` | Update test for new API |
| Modify | `frontend/src/components/metrics/TaskSparkline.tsx` | Use Tailwind chart classes |
| Modify | `frontend/src/components/metrics/ResourceGauge.tsx` | Use `var(--chart-*)` |
| Modify | `frontend/src/pages/ServiceDetail.tsx` | Use `getSemanticChartColor()` |
| Modify | `frontend/src/pages/TaskDetail.tsx` | Use `getSemanticChartColor()` |
| Modify | `frontend/src/components/metrics/TimeSeriesChart.tsx` | Use `getSemanticChartColor()` for crosshair/zoom |
| Modify | `frontend/src/components/metrics/ResourceAllocationChart.tsx` | Use `getSemanticChartColor()` for limit line |
| Modify | `frontend/src/lib/topologyTransform.ts` | Use `getChartColor()` instead of hardcoded array |
| Modify | `frontend/src/pages/NodeList.tsx` | Pass chart color class to Sparkline |

---

### Task 1: Add CSS variables and extend chartColors.ts

**Files:**
- Modify: `frontend/src/index.css`
- Modify: `frontend/src/lib/chartColors.ts`

- [ ] **Step 1: Add semantic chart variables to `:root` in index.css**

After the existing `--chart-10` line (line 37), add:

```css
  --chart-cpu: var(--chart-1);
  --chart-memory: #34d399;
  --chart-ok: #10b981;
  --chart-warning: #f59e0b;
  --chart-critical: #ef4444;
  --chart-reserved: #3b82f6;
  --chart-crosshair: rgba(136, 136, 136, 0.3);
  --chart-zoom: rgba(100, 143, 255, 0.15);
```

- [ ] **Step 2: Add the same variables to `.dark` block**

After the dark theme's `--chart-10` line (line 78), add the same block.

- [ ] **Step 3: Add Tailwind `--color-chart-*` mappings**

After the existing `--color-chart-10` line (line 119), add:

```css
  --color-chart-cpu: var(--chart-cpu);
  --color-chart-memory: var(--chart-memory);
  --color-chart-ok: var(--chart-ok);
  --color-chart-warning: var(--chart-warning);
  --color-chart-critical: var(--chart-critical);
  --color-chart-reserved: var(--chart-reserved);
```

(No Tailwind mappings needed for crosshair/zoom — those are only used in Chart.js canvas contexts.)

- [ ] **Step 4: Add `getSemanticChartColor()` to chartColors.ts**

Add after the existing `getChartColor` function:

```ts
/** Cached semantic color resolutions. */
const semanticCache = new Map<string, string>();

/**
 * Get a semantic chart color by name (e.g. "cpu", "memory", "critical").
 * Reads from CSS custom property `--chart-{name}`, caches after first call.
 * For Chart.js contexts that need resolved hex/rgba values.
 */
export function getSemanticChartColor(name: string): string {
  const cached = semanticCache.get(name);
  if (cached) return cached;
  if (typeof document === "undefined") return "";
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--chart-${name}`)
    .trim();
  if (value) semanticCache.set(name, value);
  return value;
}
```

- [ ] **Step 5: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/index.css frontend/src/lib/chartColors.ts
git commit -m "feat: add semantic chart CSS variables and getSemanticChartColor()"
```

---

### Task 2: Update Sparkline and TaskSparkline

**Files:**
- Modify: `frontend/src/components/metrics/Sparkline.tsx`
- Modify: `frontend/src/components/metrics/Sparkline.test.tsx`
- Modify: `frontend/src/components/metrics/TaskSparkline.tsx`
- Modify: `frontend/src/pages/NodeList.tsx`

- [ ] **Step 1: Rewrite Sparkline.tsx to use currentColor**

Replace the entire file with:

```tsx
interface Props {
  data: number[];
  width?: number;
  height?: number;
  className?: string;
}

export default function Sparkline({data, width = 80, height = 24, className = "text-chart-1"}: Props) {
  if (data.length < 2) {
    return <div style={{width, height}} />;
  }

  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;
  const pad = 1;

  const points = data
    .map((value, index) => {
      const x = pad + (index / (data.length - 1)) * (width - pad * 2);
      const y = pad + (1 - (value - min) / range) * (height - pad * 2);
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <svg
      width={width}
      height={height}
      className={`inline-block align-middle ${className}`}
    >
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
```

- [ ] **Step 2: Update Sparkline.test.tsx**

Replace the custom dimensions test (which previously didn't test color) — the test file stays mostly the same, but update the `color` prop test if present. The current tests don't test color, so no test changes needed. Verify the existing tests still pass:

```bash
cd frontend && npx vitest run src/components/metrics/Sparkline.test.tsx
```

- [ ] **Step 3: Update TaskSparkline.tsx**

Replace the `COLORS` map and Sparkline usage:

Old:
```tsx
const COLORS = {
  cpu: "#4f8cf6",
  memory: "#34d399",
};
```

New — remove `COLORS` entirely. Replace:
```tsx
const CHART_CLASSES = {
  cpu: "text-chart-cpu",
  memory: "text-chart-memory",
};
```

And update the Sparkline call:

Old:
```tsx
      <Sparkline
        data={data}
        color={COLORS[type]}
      />
```

New:
```tsx
      <Sparkline
        data={data}
        className={CHART_CLASSES[type]}
      />
```

- [ ] **Step 4: Update NodeList.tsx Sparkline usage**

Find the Sparkline usage at line ~143. It currently uses no color prop (relies on default). After the change, the default is `text-chart-1` which is fine — no change needed.

- [ ] **Step 5: Type-check and test**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/metrics/Sparkline.tsx frontend/src/components/metrics/Sparkline.test.tsx frontend/src/components/metrics/TaskSparkline.tsx
git commit -m "refactor: use theme colors in Sparkline and TaskSparkline"
```

---

### Task 3: Update ResourceGauge

**Files:**
- Modify: `frontend/src/components/metrics/ResourceGauge.tsx`

- [ ] **Step 1: Replace colorForValue() hex returns**

Old:
```tsx
function colorForValue(v: number): string {
  if (v >= 90) return "#ef4444"; // red
  if (v >= 75) return "#f59e0b"; // amber
  return "#10b981"; // emerald
}
```

New:
```tsx
function colorForValue(v: number): string {
  if (v >= 90) return "var(--chart-critical)";
  if (v >= 75) return "var(--chart-warning)";
  return "var(--chart-ok)";
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/metrics/ResourceGauge.tsx
git commit -m "refactor: use theme colors in ResourceGauge"
```

---

### Task 4: Update ServiceDetail and TaskDetail chart colors

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Add import to ServiceDetail.tsx**

Add `getSemanticChartColor` to imports:
```tsx
import {getSemanticChartColor} from "../lib/chartColors";
```

- [ ] **Step 2: Replace memory chart color in ServiceDetail.tsx**

At line ~160, change:
```tsx
              color: "#34d399",
```
to:
```tsx
              color: getSemanticChartColor("memory"),
```

- [ ] **Step 3: Replace threshold colors in cpuThresholds()**

At line ~872, change:
```tsx
      color: "#3b82f6",
```
to:
```tsx
      color: getSemanticChartColor("reserved"),
```

At line ~883, change:
```tsx
      color: "#ef4444",
```
to:
```tsx
      color: getSemanticChartColor("critical"),
```

- [ ] **Step 4: Replace threshold colors in memoryThresholds()**

Same pattern — at line ~870:
```tsx
      color: "#3b82f6",
```
→
```tsx
      color: getSemanticChartColor("reserved"),
```

At line ~879:
```tsx
      color: "#ef4444",
```
→
```tsx
      color: getSemanticChartColor("critical"),
```

- [ ] **Step 5: Update TaskDetail.tsx**

Add import:
```tsx
import {getSemanticChartColor} from "../lib/chartColors";
```

At line ~221, change:
```tsx
                color: "#34d399",
```
to:
```tsx
                color: getSemanticChartColor("memory"),
```

- [ ] **Step 6: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx frontend/src/pages/TaskDetail.tsx
git commit -m "refactor: use theme colors for chart series and thresholds"
```

---

### Task 5: Update TimeSeriesChart crosshair and zoom colors

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add import**

Add to imports:
```tsx
import {getSemanticChartColor} from "@/lib/chartColors.ts";
```

(Note: this file uses `@/` path aliases.)

- [ ] **Step 2: Replace crosshair colors**

At line ~767, change:
```tsx
          ctx.strokeStyle = "rgba(136,136,136,0.3)";
```
to:
```tsx
          ctx.strokeStyle = getSemanticChartColor("crosshair");
```

At line ~785, change:
```tsx
            ctx.strokeStyle = "rgba(136,136,136,0.2)";
```
to:
```tsx
            ctx.strokeStyle = getSemanticChartColor("crosshair");
```

(Both crosshair lines use the same CSS variable — the synced crosshair was previously slightly more transparent, but consolidating to one variable is cleaner. If the visual difference matters, a second variable like `--chart-crosshair-synced` can be added later.)

- [ ] **Step 3: Replace zoom drag colors**

At lines ~843-844, change:
```tsx
              backgroundColor: "rgba(100, 143, 255, 0.1)",
              borderColor: "rgba(100, 143, 255, 0.3)",
```
to:
```tsx
              backgroundColor: getSemanticChartColor("zoom"),
              borderColor: getSemanticChartColor("zoom"),
```

- [ ] **Step 4: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "refactor: use theme colors for chart crosshair and zoom"
```

---

### Task 6: Update ResourceAllocationChart limit line

**Files:**
- Modify: `frontend/src/components/metrics/ResourceAllocationChart.tsx`

- [ ] **Step 1: Add import**

Add to imports:
```tsx
import {getSemanticChartColor} from "../../lib/chartColors";
```

- [ ] **Step 2: Replace limit line color**

At lines ~60-62, change:
```tsx
        ctx.strokeStyle =
          getComputedStyle(document.documentElement).getPropertyValue("--destructive").trim() ||
          "#ef4444";
```
to:
```tsx
        ctx.strokeStyle = getSemanticChartColor("critical");
```

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/ResourceAllocationChart.tsx
git commit -m "refactor: use theme color for resource allocation limit line"
```

---

### Task 7: Update topologyTransform.ts

**Files:**
- Modify: `frontend/src/lib/topologyTransform.ts`

- [ ] **Step 1: Replace hardcoded COLORS array**

Remove the `COLORS` array (lines 4-17) and add import:

```tsx
import {getChartColor} from "./chartColors";
```

- [ ] **Step 2: Update hashColor function**

Old:
```tsx
export function hashColor(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) | 0;
  }
  return COLORS[Math.abs(h) % COLORS.length];
}
```

New:
```tsx
export function hashColor(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) | 0;
  }
  return getChartColor(Math.abs(h));
}
```

(`getChartColor` already wraps around the palette length internally.)

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/topologyTransform.ts
git commit -m "refactor: use chart palette for topology colors"
```

---

### Task 8: Delete CHART_COLORS export and verify no remaining hardcoded colors

**Files:**
- Modify: `frontend/src/lib/chartColors.ts`

- [ ] **Step 1: Check if CHART_COLORS is used externally**

```bash
cd frontend && grep -r "CHART_COLORS" src/ --include="*.ts" --include="*.tsx"
```

If only used internally in `chartColors.ts`, remove the `export` keyword (keep as `const` for the fallback logic).

- [ ] **Step 2: Verify no chart hex colors remain**

```bash
cd frontend && grep -rn '#[0-9a-fA-F]\{6\}\|rgba\?' src/components/metrics/ src/pages/ServiceDetail.tsx src/pages/TaskDetail.tsx src/lib/topologyTransform.ts --include="*.ts" --include="*.tsx"
```

Expected: no matches in chart-related files (only `chartColors.ts` fallback array and `index.css` definitions).

- [ ] **Step 3: Run full test suite**

```bash
cd frontend && npx vitest run
```

Expected: all tests pass.

- [ ] **Step 4: Commit if any cleanup was needed**

```bash
git add frontend/src/lib/chartColors.ts
git commit -m "refactor: make CHART_COLORS internal to chartColors module"
```
