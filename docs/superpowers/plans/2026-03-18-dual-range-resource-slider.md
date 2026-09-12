# Dual-Range Resource Slider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace separate resource limit/reservation controls with dual-thumb range sliders inside the Deploy Configuration section.

**Architecture:** New `ResourceRangeSlider` component built on Base UI's `Slider` primitive with custom dead zones and tick marks. `ResourcesEditor` is refactored to use two range sliders (CPU, memory) and moved from a standalone section into Deploy Configuration.

**Tech Stack:** React 19, Base UI `@base-ui/react/slider`, Tailwind CSS v4, vitest + testing-library

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `frontend/src/components/service-detail/resource-range-slider.tsx` | Dual-thumb slider with dead zones, ticks, number inputs |
| Create | `frontend/src/components/service-detail/ResourceRangeSlider.test.tsx` | Tests for the new slider component |
| Modify | `frontend/src/components/service-detail/ResourcesEditor.tsx` | Replace 4x SliderNumberField with 2x ResourceRangeSlider |
| Modify | `frontend/src/pages/ServiceDetail.tsx:296-303,535-549,745-824` | Move editor into Deploy Config, remove standalone section + ResourcesPanel + ResourceLimitsBar |
| Modify | `frontend/src/components/service-detail/index.ts` | No changes needed (ResourcesEditor already exported) |

---

### Task 1: Build ResourceRangeSlider component

**Files:**
- Create: `frontend/src/components/service-detail/resource-range-slider.tsx`

- [ ] **Step 1: Create the component with dual-thumb slider core**

The component uses Base UI's `Slider.Root` with a two-element value array `[reservationPos, limitPos]`. Internal positions: 0 = no reservation, `step` through `max` = valid values, `max + step` = no limit.

```tsx
// frontend/src/components/service-detail/resource-range-slider.tsx
import { Label } from "@/components/ui/label";
import { NumberField } from "@base-ui/react/number-field";
import { Slider as SliderPrimitive } from "@base-ui/react/slider";
import { Minus, Plus } from "lucide-react";
import { useMemo } from "react";

interface ResourceRangeSliderProps {
  label: string;
  reservation: number | undefined;
  limit: number | undefined;
  onChange: (values: { reservation: number | undefined; limit: number | undefined }) => void;
  max: number;
  step: number;
}

function toPosition(value: number | undefined, max: number, step: number, side: "reservation" | "limit"): number {
  if (value === undefined) return side === "reservation" ? 0 : max + step;
  return value;
}

function fromPosition(position: number, max: number, step: number, side: "reservation" | "limit"): number | undefined {
  if (side === "reservation" && position === 0) return undefined;
  if (side === "limit" && position >= max + step) return undefined;
  return position;
}

export function ResourceRangeSlider({ label, reservation, limit, onChange, max, step }: ResourceRangeSliderProps) {
  const sliderMax = max + step; // extra position for ∞
  const reservationPosition = toPosition(reservation, max, step, "reservation");
  const limitPosition = toPosition(limit, max, step, "limit");

  const ticks = useMemo(() => computeTicks(max, step), [max, step]);

  // Dead zone width as percentage of total slider width
  const deadZonePercent = (step / sliderMax) * 100;

  function handleSliderChange(positions: number[]) {
    onChange({
      reservation: fromPosition(positions[0], max, step, "reservation"),
      limit: fromPosition(positions[1], max, step, "limit"),
    });
  }

  function handleReservationInput(next: number | null) {
    if (next === null || next === undefined) {
      onChange({ reservation: undefined, limit });
      return;
    }
    const clamped = Math.max(step, Math.min(max, next));
    // Push limit if reservation exceeds it
    const newLimit = limit !== undefined && clamped > limit ? clamped : limit;
    onChange({ reservation: clamped, limit: newLimit });
  }

  function handleLimitInput(next: number | null) {
    if (next === null || next === undefined) {
      onChange({ reservation, limit: undefined });
      return;
    }
    const clamped = Math.max(step, Math.min(max, next));
    // Push reservation down if limit is below it
    const newReservation = reservation !== undefined && clamped < reservation ? clamped : reservation;
    onChange({ reservation: newReservation, limit: clamped });
  }

  const isReservationActive = reservation !== undefined;
  const isLimitActive = limit !== undefined;

  return (
    <div className="flex flex-col gap-1 w-full">
      <Label className="text-xs text-muted-foreground">{label}</Label>

      {/* Slider with dead zones */}
      <div className="relative">
        <SliderPrimitive.Root
          value={[reservationPosition, limitPosition]}
          onValueChange={handleSliderChange}
          min={0}
          max={sliderMax}
          step={step}
          className="data-horizontal:w-full"
        >
          <SliderPrimitive.Control className="relative flex w-full touch-none items-center select-none h-6">
            <SliderPrimitive.Track className="relative grow overflow-hidden rounded-full bg-transparent select-none data-horizontal:h-1.5 data-horizontal:w-full">
              {/* Dead zone left */}
              <div
                className="absolute top-1/2 left-0 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/20"
                style={{ width: `${deadZonePercent}%` }}
              />
              {/* Main track */}
              <div
                className="absolute top-0 h-full rounded-full bg-muted"
                style={{ left: `${deadZonePercent}%`, right: `${deadZonePercent}%` }}
              />
              {/* Dead zone right */}
              <div
                className="absolute top-1/2 right-0 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/20"
                style={{ width: `${deadZonePercent}%` }}
              />
              {/* Filled range indicator */}
              <SliderPrimitive.Indicator
                className={`data-horizontal:h-full bg-primary ${!isReservationActive && !isLimitActive ? "opacity-20" : ""}`}
              />
            </SliderPrimitive.Track>
            {/* Reservation thumb */}
            <SliderPrimitive.Thumb
              className={`relative block size-3.5 shrink-0 rounded-full border-2 transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden active:ring-3 ${
                isReservationActive
                  ? "border-primary bg-white ring-primary/50"
                  : "border-muted-foreground/40 bg-muted ring-muted-foreground/20"
              }`}
            />
            {/* Limit thumb */}
            <SliderPrimitive.Thumb
              className={`relative block size-3.5 shrink-0 rounded-full border-2 transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden active:ring-3 ${
                isLimitActive
                  ? "border-primary bg-white ring-primary/50"
                  : "border-muted-foreground/40 bg-muted ring-muted-foreground/20"
              }`}
            />
          </SliderPrimitive.Control>
        </SliderPrimitive.Root>

        {/* Tick marks */}
        <div className="relative h-3" style={{ marginLeft: `${deadZonePercent}%`, marginRight: `${deadZonePercent}%` }}>
          {ticks.map((tick) => (
            <div
              key={tick.value}
              className="absolute top-0 flex flex-col items-center -translate-x-1/2"
              style={{ left: `${((tick.value - step) / (max - step)) * 100}%` }}
            >
              <div className={`w-px bg-muted-foreground/40 ${tick.tall ? "h-2.5" : "h-1.5"}`} />
            </div>
          ))}
        </div>
      </div>

      {/* Number inputs: reservation left, limit right */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Reserved</span>
          {isReservationActive ? (
            <NumberField.Root
              value={reservation}
              onValueChange={handleReservationInput}
              min={step}
              max={max}
              step={step}
            >
              <NumberField.Group className="flex items-center rounded-md border">
                <NumberField.Decrement className="flex size-6 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
                  <Minus className="size-2.5" />
                </NumberField.Decrement>
                <NumberField.Input className="w-12 bg-transparent px-1 py-0.5 text-center font-mono text-xs focus:outline-none" />
                <NumberField.Increment className="flex size-6 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
                  <Plus className="size-2.5" />
                </NumberField.Increment>
              </NumberField.Group>
            </NumberField.Root>
          ) : (
            <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">&mdash;</span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {isLimitActive ? (
            <NumberField.Root
              value={limit}
              onValueChange={handleLimitInput}
              min={step}
              max={max}
              step={step}
            >
              <NumberField.Group className="flex items-center rounded-md border">
                <NumberField.Decrement className="flex size-6 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
                  <Minus className="size-2.5" />
                </NumberField.Decrement>
                <NumberField.Input className="w-12 bg-transparent px-1 py-0.5 text-center font-mono text-xs focus:outline-none" />
                <NumberField.Increment className="flex size-6 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
                  <Plus className="size-2.5" />
                </NumberField.Increment>
              </NumberField.Group>
            </NumberField.Root>
          ) : (
            <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">&mdash;</span>
          )}
          <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Limit</span>
        </div>
      </div>
    </div>
  );
}

/** Generate tick positions. Boundary ticks (step and max) are tall; intermediate ticks are short. */
export function computeTicks(max: number, step: number): Array<{ value: number; tall: boolean }> {
  const ticks: Array<{ value: number; tall: boolean }> = [];

  // Determine intermediate tick interval
  // For CPU (step ≤ 1): every whole core
  // For memory (step > 1): power-of-two boundaries
  let interval: number;
  if (step <= 1) {
    interval = 1;
  } else {
    // Find a nice power-of-two interval that gives ~4-8 ticks
    const range = max - step;
    const targetTicks = 6;
    const raw = range / targetTicks;
    interval = Math.pow(2, Math.round(Math.log2(raw)));
    if (interval < step) interval = step;
  }

  // Boundary tick at min (step)
  ticks.push({ value: step, tall: true });

  // Intermediate ticks
  const firstIntermediate = Math.ceil((step + 0.001) / interval) * interval;
  for (let v = firstIntermediate; v < max; v += interval) {
    if (Math.abs(v - step) > step * 0.01 && Math.abs(v - max) > step * 0.01) {
      ticks.push({ value: v, tall: false });
    }
  }

  // Boundary tick at max
  if (max > step) {
    ticks.push({ value: max, tall: true });
  }

  return ticks;
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/resource-range-slider.tsx
git commit -m "feat(frontend): add ResourceRangeSlider dual-thumb component"
```

---

### Task 2: Test ResourceRangeSlider

**Files:**
- Create: `frontend/src/components/service-detail/ResourceRangeSlider.test.tsx`

- [ ] **Step 1: Write tests for rendering, constraints, and tick generation**

```tsx
// frontend/src/components/service-detail/ResourceRangeSlider.test.tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ResourceRangeSlider, computeTicks } from "./resource-range-slider";

describe("ResourceRangeSlider", () => {
  const defaultProps = {
    label: "CPU (cores)",
    reservation: undefined as number | undefined,
    limit: undefined as number | undefined,
    onChange: vi.fn(),
    max: 4,
    step: 0.25,
  };

  it("renders label", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    expect(screen.getByText("CPU (cores)")).toBeInTheDocument();
  });

  it("shows dashes when both values are undefined", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    const dashes = screen.getAllByText("—");
    expect(dashes).toHaveLength(2);
  });

  it("shows reservation input when reservation is set", () => {
    render(<ResourceRangeSlider {...defaultProps} reservation={0.5} />);
    const input = screen.getAllByRole("textbox")[0];
    expect(input).toHaveValue("0.5");
  });

  it("shows limit input when limit is set", () => {
    render(<ResourceRangeSlider {...defaultProps} limit={2} />);
    const inputs = screen.getAllByRole("textbox");
    expect(inputs[inputs.length - 1]).toHaveValue("2");
  });

  it("renders Reserved and Limit labels", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    expect(screen.getByText("Reserved")).toBeInTheDocument();
    expect(screen.getByText("Limit")).toBeInTheDocument();
  });
});

describe("computeTicks", () => {
  it("produces boundary ticks at step and max for CPU", () => {
    const ticks = computeTicks(4, 0.25);
    expect(ticks[0]).toEqual({ value: 0.25, tall: true });
    expect(ticks[ticks.length - 1]).toEqual({ value: 4, tall: true });
  });

  it("produces intermediate ticks at whole cores", () => {
    const ticks = computeTicks(4, 0.25);
    const intermediates = ticks.filter((t) => !t.tall);
    // Should have ticks at 1, 2, 3
    expect(intermediates.map((t) => t.value)).toEqual([1, 2, 3]);
  });

  it("produces boundary ticks for memory", () => {
    const ticks = computeTicks(4096, 16);
    expect(ticks[0]).toEqual({ value: 16, tall: true });
    expect(ticks[ticks.length - 1]).toEqual({ value: 4096, tall: true });
  });

  it("does not produce ticks when max equals step", () => {
    const ticks = computeTicks(0.25, 0.25);
    expect(ticks).toHaveLength(1);
    expect(ticks[0]).toEqual({ value: 0.25, tall: true });
  });
});
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/components/service-detail/ResourceRangeSlider.test.tsx`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/ResourceRangeSlider.test.tsx
git commit -m "test(frontend): add ResourceRangeSlider tests"
```

---

### Task 3: Refactor ResourcesEditor to use ResourceRangeSlider

**Files:**
- Modify: `frontend/src/components/service-detail/ResourcesEditor.tsx`

- [ ] **Step 1: Replace SliderNumberField with ResourceRangeSlider**

Rewrite `ResourcesEditor.tsx`:
- Remove `SliderNumberField` import, add `ResourceRangeSlider` import
- Replace 4 state variables (`limitCpuCores`, `limitMemoryMegabytes`, `reservedCpuCores`, `reservedMemoryMegabytes`) with 2 state objects:
  ```tsx
  const [cpu, setCpu] = useState<{ reservation: number | undefined; limit: number | undefined }>({ reservation: undefined, limit: undefined });
  const [memory, setMemory] = useState<{ reservation: number | undefined; limit: number | undefined }>({ reservation: undefined, limit: undefined });
  ```
- In `openEdit()`, initialize from `resources`:
  ```tsx
  setCpu({
    reservation: resources.reservations?.nanoCPUs != null ? resources.reservations.nanoCPUs / 1e9 : undefined,
    limit: resources.limits?.nanoCPUs != null ? resources.limits.nanoCPUs / 1e9 : undefined,
  });
  setMemory({
    reservation: resources.reservations?.memoryBytes != null ? resources.reservations.memoryBytes / (1024 * 1024) : undefined,
    limit: resources.limits?.memoryBytes != null ? resources.limits.memoryBytes / (1024 * 1024) : undefined,
  });
  ```
- In `save()`, build the patch from the two state objects:
  ```tsx
  const patch: ServiceResourceShape = {};
  if (cpu.limit !== undefined || memory.limit !== undefined) {
    patch.limits = {};
    if (cpu.limit !== undefined) patch.limits.nanoCPUs = Math.round(cpu.limit * 1e9);
    if (memory.limit !== undefined) patch.limits.memoryBytes = Math.round(memory.limit * 1024 * 1024);
  }
  if (cpu.reservation !== undefined || memory.reservation !== undefined) {
    patch.reservations = {};
    if (cpu.reservation !== undefined) patch.reservations.nanoCPUs = Math.round(cpu.reservation * 1e9);
    if (memory.reservation !== undefined) patch.reservations.memoryBytes = Math.round(memory.reservation * 1024 * 1024);
  }
  ```
- Remove the `CollapsibleSection` wrapper — the component now renders just the editing UI (sliders + save/cancel) or read-only display
- In edit mode, render two `ResourceRangeSlider` instances. Show a loading skeleton if `capacity` is null:
  ```tsx
  {capacity ? (
    <div className="space-y-3">
      <ResourceRangeSlider
        label="CPU (cores)"
        reservation={cpu.reservation}
        limit={cpu.limit}
        onChange={setCpu}
        max={capacity.maxNodeCPU}
        step={0.25}
      />
      <ResourceRangeSlider
        label="Memory (MB)"
        reservation={memory.reservation}
        limit={memory.limit}
        onChange={setMemory}
        max={capacity.maxNodeMemory / (1024 * 1024)}
        step={16}
      />
    </div>
  ) : (
    <div className="h-24 animate-pulse rounded bg-muted" />
  )}
  ```
- The read-only state keeps the existing grid display (CPU limit, memory limit, CPU reserved, memory reserved as text) but is now rendered without the `CollapsibleSection` — just the content.
- Accept a new optional `pids` prop. Render in read-only mode as:
  ```tsx
  {pids != null && (
    <div className="flex items-center justify-between text-sm">
      <span className="font-medium">PID Limit</span>
      <span className="font-mono">{pids}</span>
    </div>
  )}
  ```
  This replaces the PID display that was previously in `ResourcesPanel`.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/ResourcesEditor.tsx
git commit -m "refactor(frontend): use ResourceRangeSlider in ResourcesEditor"
```

---

### Task 4: Integrate into ServiceDetail — move editor into Deploy Configuration

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx:296-303,535-549,745-824`

- [ ] **Step 1: Remove the standalone Resource Limits section**

Delete lines ~296-303 (the `{serviceResources !== null && (<ResourcesEditor .../>)}` block between Resource Allocation and Container Configuration).

- [ ] **Step 2: Replace static ResourcesPanel with ResourcesEditor inside Deploy Configuration**

In the Deploy Configuration section (~lines 541-549), replace:
```tsx
{taskTemplate.Resources && (
  <div className="flex flex-col gap-3 rounded-lg border p-4">
    <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
      Resources
    </h3>
    <ResourcesPanel resources={taskTemplate.Resources} />
  </div>
)}
```

With:
```tsx
{serviceResources !== null && (
  <div className="flex flex-col gap-3 rounded-lg border p-4">
    <div className="flex items-center justify-between">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Resources
      </h3>
      {/* Edit button is rendered by ResourcesEditor internally */}
    </div>
    <ResourcesEditor
      serviceId={id!}
      resources={serviceResources}
      onSaved={setServiceResources}
      pids={taskTemplate.Resources?.Limits?.Pids}
    />
  </div>
)}
```

Note: The `ResourcesEditor` component handles its own edit/read-only toggle, so the panel just wraps it with the heading and border.

- [ ] **Step 3: Delete ResourceLimitsBar, ResourcesPanel, and orphaned code**

Remove from `ServiceDetail.tsx`:
- `ResourceLimitsBar` function (lines ~747-791)
- `ResourcesPanel` function (lines ~793-824)
- `type ResourceShape` alias (line ~745) — only used by the deleted functions
- Any imports that become unused after these deletions (check `formatCores`, `formatBytes` — they may still be used elsewhere in the file, e.g. by the deploy config display; only remove if truly orphaned)

- [ ] **Step 4: Verify it compiles and renders**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

Run: `cd frontend && npm run lint`
Expected: no errors (no unused imports/variables from removed code)

- [ ] **Step 5: Run all frontend tests**

Run: `cd frontend && npx vitest run`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx frontend/src/components/service-detail/ResourcesEditor.tsx
git commit -m "refactor(frontend): move resource editor into Deploy Configuration section"
```

---

### Task 5: Visual polish and manual verification

- [ ] **Step 1: Run the dev server and visually verify**

Run: `cd frontend && npm run dev`

Check on the service detail page:
1. Deploy Configuration section shows Resources panel with read-only values
2. Clicking Edit shows dual-range sliders for CPU and memory
3. Dragging thumbs to dead zones correctly shows/hides number inputs
4. Reservation ≤ limit constraint is enforced on drag and input
5. Save persists changes, Cancel reverts
6. PID limits show as static text when present
7. The standalone "Resource Limits" section is gone

- [ ] **Step 2: Run full check**

Run: `cd frontend && npm run fmt:check && npm run lint && npx tsc -b --noEmit && npx vitest run`
Expected: all pass

- [ ] **Step 3: Fix any formatting issues**

Run: `cd frontend && npm run fmt`

- [ ] **Step 4: Final commit if any formatting changes**

```bash
git add -u frontend/
git commit -m "style(frontend): format resource slider components"
```
