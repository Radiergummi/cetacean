# Rich Mutation UI with shadcn Components

**Date:** 2026-03-18
**Status:** Approved

## Summary

Replace all plain HTML form elements in the 9 mutation UI points with shadcn/ui and base-ui components. Consolidate duplicated patterns (key-value editors, slider+number combos) into shared components. Add a new `/cluster/capacity` backend endpoint to provide slider bounds for resource limits.

## Goals

- Consistent, polished form inputs across all write actions
- Replace native `window.confirm()` with styled AlertDialog
- Replace hand-rolled popovers with shadcn Popover
- Use base-ui NumberField with increment/decrement for numeric inputs
- Add slider+number combo for CPU/memory resource editing with cluster-aware bounds
- Reduce code duplication between EnvEditor and LabelsEditor

## New shadcn/ui Components to Install

- `input` — styled text input
- `select` — dropdown with keyboard nav
- `popover` — positioned floating panel
- `alert-dialog` — confirmation modal
- `slider` — range slider
- `label` — form labels

Already available (no install needed):
- `button`, `dialog`, `card`, `table`, `badge` — already installed
- `@base-ui/react` NumberField — already a dependency

## New Backend Endpoint

### `GET /cluster/capacity`

Returns max single-node resources (for slider bounds) and cluster totals.

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/cluster/capacity",
  "@type": "ClusterCapacity",
  "maxNodeCPU": 8,
  "maxNodeMemory": 17179869184,
  "totalCPU": 24,
  "totalMemory": 51539607552,
  "nodeCount": 3
}
```

- `maxNodeCPU` — cores (int) of the largest single node, used as slider max for CPU
- `maxNodeMemory` — bytes (int64) of the largest single node, used as slider max for memory
- `totalCPU` / `totalMemory` — cluster-wide totals (already computed in Snapshot)
- `nodeCount` — included for frontend context

Implementation:
- Extend `ClusterSnapshot` to include `MaxNodeCPU` and `MaxNodeMemory` fields, computed in the existing `Snapshot()` node iteration loop (avoids a separate cache method and redundant locking)
- Handler registered with `contentNegotiated` (JSON handler + SPA fallback)
- Route: `GET /cluster/capacity` in router.go

## New Shared Components

### `KeyValueEditor` (`components/KeyValueEditor.tsx`)

Extracts the duplicated pattern from EnvEditor and LabelsEditor.

**Props:**
```ts
{
  title: string;              // "Environment Variables" | "Labels"
  entries: Record<string, string>;
  keyLabel?: string;          // column header, default "Key"
  valueLabel?: string;        // column header, default "Value"
  keyPlaceholder?: string;    // "NEW_VAR" | "key"
  valuePlaceholder?: string;  // "value"
  onSave: (ops: PatchOp[]) => Promise<Record<string, string>>;
  defaultOpen?: boolean;
}
```

- Owns all editing state (draft, newKey/newVal, saving, error)
- Computes JSON Patch ops internally, then passes them to `onSave` (the caller just forwards to the API). Data flow: KeyValueEditor diffs draft vs entries → produces `PatchOp[]` → calls `onSave(ops)` → caller sends to API
- If a new row is partially filled (newKey non-empty) when Save is clicked, it is auto-included in the patch (preserves current EnvEditor behavior)
- Uses shadcn Input for text fields, Button for actions
- Row removal does not require confirmation — removals are only applied on Save, so the user can always Cancel the whole edit to undo
- EnvEditor and LabelsEditor become thin wrappers passing the API call as `onSave`
- Define a shared `PatchOp` type: `{ op: string; path: string; value?: string }`

### `SliderNumberField` (`components/ui/slider-number-field.tsx`)

Combines shadcn Slider with base-ui NumberField for spatial + precise numeric input.

**Props:**
```ts
{
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  min?: number;               // default 0
  max?: number;               // from cluster capacity
  step?: number;              // 0.25 for CPU, 16 for memory (MB)
  label: string;              // "CPU (cores)" | "Memory (MB)"
  formatDisplay?: (v: number) => string;
}
```

- Slider and NumberField bidirectionally synced
- When max is undefined (capacity not yet loaded), falls back to NumberField only
- Used 4 times in ResourcesEditor: CPU limit, CPU reserved, memory limit, memory reserved

## Per-Mutation Upgrades

### 1. Scale Service (`ReplicaCard.tsx`)
- Hand-rolled absolute div → shadcn Popover (use `modal` prop to prevent accidental dismiss during edits)
- `<input type="number">` → base-ui NumberField with min=0, step=1, increment/decrement buttons
- Bare buttons → shadcn Button

### 2. Update Image (`ServiceActions.tsx`)
- Hand-rolled absolute div → shadcn Popover (use `modal` prop to prevent accidental dismiss during edits)
- `<input type="text">` → shadcn Input (monospace)
- Bare buttons → shadcn Button

### 3. Rollback Service (`ServiceActions.tsx`)
- `window.confirm()` → shadcn AlertDialog
- Bare button → shadcn Button variant="outline"

### 4. Restart Service (`ServiceActions.tsx`)
- `window.confirm()` → shadcn AlertDialog
- Bare button → shadcn Button variant="outline"

### 5. Env Variables (`EnvEditor.tsx`)
- Replaced by thin wrapper around KeyValueEditor
- `onSave` calls `api.patchServiceEnv`

### 6. Node Labels (`NodeDetail.tsx` LabelsEditor)
- Replaced by thin wrapper around KeyValueEditor
- `onSave` calls `api.patchNodeLabels`

### 7. Resources (`ResourcesEditor.tsx`)
- Plain `<input type="number">` → SliderNumberField for all 4 fields
- CPU: step=0.25, max from capacity.maxNodeCPU. Values are in cores (no conversion needed — current code already converts nanoCPUs to cores on edit open and back on save)
- Memory: step=16 (in MB), max from capacity.maxNodeMemory / (1024*1024). **Unit conversion**: on edit open, divide existing `memoryBytes` by `1024 * 1024` to get MB; on save, multiply MB back to bytes. The slider and number field display/accept MB values. The PATCH body still sends bytes.
- Fetches `/cluster/capacity` on edit open
- Bare buttons → shadcn Button

### 8. Node Availability (`NodeDetail.tsx`)
- `<select>` → shadcn Select with three items (Active, Drain, Pause)
- `window.confirm()` for drain → shadcn AlertDialog

### 9. Remove Task (`TaskDetail.tsx`)
- `window.confirm()` → shadcn AlertDialog (destructive variant, red confirm button)
- Bare button → shadcn Button variant="destructive"

## What Stays the Same

- CollapsibleSection wrapper for env, labels, resources editors
- Edit-in-place pattern (pencil button toggles view/edit)
- API calls and patch formats (same endpoints, same JSON Patch / merge patch)
- Error display pattern (inline error text below actions)
- Loading states (Spinner on buttons)
- Keyboard shortcuts (Enter to submit, Escape to cancel)
- ReplicaDoughnut SVG
- InfoCard layout for replica display
- No new pages, no new routes (except /cluster/capacity)

## Files Modified

### Backend
- `internal/cache/cache.go` — add `MaxNodeCPU` and `MaxNodeMemory` fields to `ClusterSnapshot`, computed in existing `Snapshot()` loop
- `internal/api/handlers.go` — add `HandleClusterCapacity` handler
- `internal/api/router.go` — register `GET /cluster/capacity`

### Frontend — New Files
- `frontend/src/components/KeyValueEditor.tsx`
- `frontend/src/components/ui/slider-number-field.tsx`
- `frontend/src/components/ui/input.tsx` (shadcn add)
- `frontend/src/components/ui/select.tsx` (shadcn add)
- `frontend/src/components/ui/popover.tsx` (shadcn add)
- `frontend/src/components/ui/alert-dialog.tsx` (shadcn add)
- `frontend/src/components/ui/slider.tsx` (shadcn add)
- `frontend/src/components/ui/label.tsx` (shadcn add)

### Frontend — Modified Files
- `frontend/src/components/service-detail/ReplicaCard.tsx` — Popover + NumberField
- `frontend/src/components/service-detail/ServiceActions.tsx` — Popover, AlertDialog, Button
- `frontend/src/components/service-detail/EnvEditor.tsx` — thin wrapper around KeyValueEditor
- `frontend/src/components/service-detail/ResourcesEditor.tsx` — SliderNumberField + capacity fetch
- `frontend/src/pages/NodeDetail.tsx` — KeyValueEditor, Select, AlertDialog
- `frontend/src/pages/TaskDetail.tsx` — AlertDialog, Button
- `frontend/src/api/client.ts` — add `api.clusterCapacity()` method
- `frontend/src/api/types.ts` — add `ClusterCapacity` type
