# Dual-Range Resource Slider

## Summary

Replace the standalone "Resource Limits" editor and the static "Resources" panel inside "Deploy Configuration" with a single inline editor using dual-thumb range sliders. Each slider (CPU, memory) combines reservation and limit into one control where thumb position encodes both value and enabled/disabled state.

## Interaction Model

Each resource (CPU, memory) gets one slider with two thumbs on a track divided into three zones:

```
───┲━━━━━━━━━━━━━━━━━━━━┱───
   ┃                     ┃
  min                   max
```

- **Left dead zone** (position 0): thin muted track. Left thumb here = no reservation.
- **Main track** (min to max): full-thickness track with tick marks. Min = first valid value (e.g. 0.25 cores, 16 MB). Max = cluster node capacity.
- **Right dead zone** (position max+1): thin muted track, symmetric with left. Right thumb here = no limit.

Tall tick marks at the boundary positions (min and max). Short ticks at round intermediate values. For CPU: ticks at every whole core. For memory: ticks at every power-of-two boundary (256, 512, 1024, 2048, ...) that fits within max.

### Thumb semantics

| Left thumb | Right thumb | Meaning |
|---|---|---|
| 0 (dead zone) | max+1 (dead zone) | No constraints |
| > 0 | max+1 (dead zone) | Reservation only |
| 0 (dead zone) | ≤ max | Limit only |
| > 0 | > left, ≤ max | Both reservation and limit |

Constraint: left thumb ≤ right thumb always. The slider enforces this — dragging one past the other pushes the other along. For number inputs: typing a reservation > current limit pushes the limit up to match; typing a limit < current reservation pushes the reservation down to match.

### Filled range

The primary-colored fill spans from left thumb to right thumb, representing the container's operating range. When both thumbs are in dead zones, a subtle (20% opacity) fill spans the main track to indicate "anything goes."

### Thumb styling

Active thumbs (in the main track) use the primary color border with white fill. Dead-zone thumbs use a muted border and dark fill to visually communicate "inactive."

### Number inputs

Two compact stepper inputs below the slider, justified to opposite ends:
- Left: "Reserved" label + stepper
- Right: stepper + "Limit" label

When a thumb is in its dead zone, the corresponding input shows "—" with no +/- buttons. Typing a value in the input moves the thumb; stepping with +/- moves by `step` increments.

## Internal model

The slider's internal range is `[0, max + step]`:
- Position 0 = no reservation (emits `undefined`)
- Positions `step` through `max` = valid values
- Position `max + step` = no limit (emits `undefined`)

The `min` prop represents the first valid value and equals `step` (e.g. CPU min=0.25, step=0.25; memory min=16, step=16). Internally the slider always starts at 0 (dead zone), with the first real position at `step`.

The component emits: `{ reservation: number | undefined; limit: number | undefined }`.

## New Component: `ResourceRangeSlider`

**Location:** `frontend/src/components/service-detail/resource-range-slider.tsx`

**Props:**
```typescript
interface ResourceRangeSliderProps {
  label: string;                    // e.g. "CPU (cores)", "Memory (MB)"
  reservation: number | undefined;  // current reservation value
  limit: number | undefined;        // current limit value
  onChange: (values: { reservation: number | undefined; limit: number | undefined }) => void;
  max: number;                      // cluster capacity
  step: number;                     // increment size (also the first valid value)
}
```

Built on the existing Base UI `Slider` primitive (already used in `slider.tsx`), configured with two thumbs. The dead zones and tick marks are custom elements rendered around the slider.

## Changes to `ResourcesEditor`

- Replace the four `SliderNumberField` instances (CPU limit, memory limit, CPU reserved, memory reserved) with two `ResourceRangeSlider` instances (one for CPU, one for memory).
- Reduce state from four variables to two objects: `{ reservation, limit }` for CPU and memory.
- The edit/save/cancel flow, PATCH endpoint, and capacity fetch remain unchanged.
- The `CollapsibleSection` wrapper is removed — the editor is now embedded directly in the Deploy Configuration section.
- Show a loading skeleton for the sliders until cluster capacity is fetched (max is required).

### Read-only state

When not editing, the Resources sub-panel shows the same compact read-only grid as today (CPU limit, memory limit, CPU reserved, memory reserved as text values). The dual-range sliders only appear in edit mode. PID limits are always shown as static text in the read-only view.

### Save behavior

When saving, `undefined` reservation/limit values mean "remove that constraint." The existing `save()` logic already handles this — it only includes `limits`/`reservations` objects in the patch when at least one sub-field is defined, which causes the API to clear the omitted fields.

## Changes to `ServiceDetail.tsx`

1. **Remove** the standalone "Resource Limits" `CollapsibleSection` at line ~297 (between Resource Allocation and Container Configuration). This editor is being moved into Deploy Configuration.
2. **Inside "Deploy Configuration"**: replace the static `ResourcesPanel` with the `ResourcesEditor`. The Resources sub-panel gets an edit button in its header. PID limits remain as static text below the sliders.
3. The `ResourcesPanel` helper component is removed — its read-only display is absorbed into `ResourcesEditor`.

## What stays the same

- "Resource Allocation" chart section (actual usage vs reserved/limited) — separate concern, untouched.
- PATCH `/services/{id}/resources` API — no backend changes.
- `SliderNumberField` component — still used by `ReplicaCard` for scaling.

## Visual reference

Mockups in `.superpowers/brainstorm/` — `resource-slider-v3.html` shows the final layout with all four states.
