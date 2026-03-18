# Rich Mutation UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all 9 mutation UI points with shadcn/ui and base-ui components, consolidate duplicated editors, and add a cluster capacity endpoint for resource slider bounds.

**Architecture:** Backend adds two fields to `ClusterSnapshot` and one new handler. Frontend installs 6 shadcn components, creates 2 shared components (`KeyValueEditor`, `SliderNumberField`), and upgrades all 9 mutation points in-place.

**Tech Stack:** Go 1.22+ stdlib, React 19, TypeScript, shadcn/ui (base-nova style), @base-ui/react NumberField, Tailwind CSS v4

**Spec:** `docs/superpowers/specs/2026-03-18-shadcn-mutation-ui-design.md`

---

### Task 1: Install shadcn/ui Components

**Files:**
- Create: `frontend/src/components/ui/input.tsx`
- Create: `frontend/src/components/ui/select.tsx`
- Create: `frontend/src/components/ui/popover.tsx`
- Create: `frontend/src/components/ui/alert-dialog.tsx`
- Create: `frontend/src/components/ui/slider.tsx`
- Create: `frontend/src/components/ui/label.tsx`

- [ ] **Step 1: Install all 6 components**

```bash
cd frontend && npx shadcn@latest add input select popover alert-dialog slider label -y
```

- [ ] **Step 2: Verify installation**

```bash
ls frontend/src/components/ui/
```

Expected: `alert-dialog.tsx`, `badge.tsx`, `button.tsx`, `card.tsx`, `dialog.tsx`, `input.tsx`, `label.tsx`, `popover.tsx`, `select.tsx`, `slider.tsx`, `table.tsx`

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ui/ frontend/package.json frontend/package-lock.json
git commit -m "feat(frontend): install shadcn input, select, popover, alert-dialog, slider, label"
```

---

### Task 2: Backend — Cluster Capacity Endpoint

**Files:**
- Modify: `internal/cache/cache.go:53-69` (ClusterSnapshot struct)
- Modify: `internal/cache/cache.go:829-843` (Snapshot node loop)
- Modify: `internal/api/handlers.go` (add HandleClusterCapacity)
- Modify: `internal/api/router.go:36` (add route after /cluster/metrics)
- Test: `internal/cache/cache_test.go` (extend TestSnapshot_ResourceTotals)
- Test: `internal/api/handlers_test.go` (add TestHandleClusterCapacity)

- [ ] **Step 1: Write failing test for ClusterSnapshot max fields**

Add to `internal/cache/cache_test.go` after the existing `TestSnapshot_ResourceTotals` (line ~779):

```go
func TestSnapshot_MaxNodeResources(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{
		ID:     "n1",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    4000000000, // 4 cores
				MemoryBytes: 8589934592, // 8 GB
			},
		},
	})
	c.SetNode(swarm.Node{
		ID:     "n2",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    8000000000, // 8 cores
				MemoryBytes: 4294967296, // 4 GB
			},
		},
	})

	snap := c.Snapshot()
	if snap.MaxNodeCPU != 8 {
		t.Errorf("MaxNodeCPU=%d, want 8", snap.MaxNodeCPU)
	}
	if snap.MaxNodeMemory != 8589934592 {
		t.Errorf("MaxNodeMemory=%d, want 8589934592", snap.MaxNodeMemory)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cache/ -run TestSnapshot_MaxNodeResources -v
```

Expected: FAIL — `snap.MaxNodeCPU undefined`

- [ ] **Step 3: Add MaxNodeCPU and MaxNodeMemory to ClusterSnapshot**

In `internal/cache/cache.go`, add two fields to the `ClusterSnapshot` struct:

```go
type ClusterSnapshot struct {
	// ... existing fields ...
	ReservedMemory    int64          `json:"reservedMemory"`
	MaxNodeCPU        int            `json:"maxNodeCPU"`
	MaxNodeMemory     int64          `json:"maxNodeMemory"`
	LastSync          time.Time      `json:"lastSync"`
}
```

In the `Snapshot()` method, inside the node loop (around line 829-843), add max tracking:

```go
var nodesReady, nodesDown, nodesDraining int
var totalNanoCPUs int64
var totalMemory int64
var maxNanoCPUs int64
var maxMemory int64
for _, n := range c.nodes {
	// ... existing switch ...
	totalNanoCPUs += n.Description.Resources.NanoCPUs
	totalMemory += n.Description.Resources.MemoryBytes
	if n.Description.Resources.NanoCPUs > maxNanoCPUs {
		maxNanoCPUs = n.Description.Resources.NanoCPUs
	}
	if n.Description.Resources.MemoryBytes > maxMemory {
		maxMemory = n.Description.Resources.MemoryBytes
	}
}
```

In the return statement, add:

```go
MaxNodeCPU:    int(maxNanoCPUs / 1e9),
MaxNodeMemory: maxMemory,
```

- [ ] **Step 4: Run cache test to verify it passes**

```bash
go test ./internal/cache/ -run TestSnapshot_MaxNodeResources -v
```

Expected: PASS

- [ ] **Step 5: Write failing test for HandleClusterCapacity**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleClusterCapacity(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "n1",
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    4000000000,
				MemoryBytes: 8589934592,
			},
		},
	})
	c.SetNode(swarm.Node{
		ID: "n2",
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    8000000000,
				MemoryBytes: 4294967296,
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil)

	req := httptest.NewRequest("GET", "/cluster/capacity", nil)
	w := httptest.NewRecorder()
	h.HandleClusterCapacity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["@type"] != "ClusterCapacity" {
		t.Errorf("@type=%v, want ClusterCapacity", body["@type"])
	}
	if body["maxNodeCPU"].(float64) != 8 {
		t.Errorf("maxNodeCPU=%v, want 8", body["maxNodeCPU"])
	}
	if body["maxNodeMemory"].(float64) != 8589934592 {
		t.Errorf("maxNodeMemory=%v, want 8589934592", body["maxNodeMemory"])
	}
	if body["nodeCount"].(float64) != 2 {
		t.Errorf("nodeCount=%v, want 2", body["nodeCount"])
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

```bash
go test ./internal/api/ -run TestHandleClusterCapacity -v
```

Expected: FAIL — `h.HandleClusterCapacity undefined`

- [ ] **Step 7: Implement HandleClusterCapacity**

Add to `internal/api/handlers.go`:

```go
func (h *Handlers) HandleClusterCapacity(w http.ResponseWriter, r *http.Request) {
	snap := h.cache.Snapshot()
	extra := map[string]any{
		"maxNodeCPU":    snap.MaxNodeCPU,
		"maxNodeMemory": snap.MaxNodeMemory,
		"totalCPU":      snap.TotalCPU,
		"totalMemory":   snap.TotalMemory,
		"nodeCount":     snap.NodeCount,
	}
	writeJSONWithETag(w, r, NewDetailResponse("/cluster/capacity", "ClusterCapacity", extra))
}
```

- [ ] **Step 8: Register the route**

In `internal/api/router.go`, add after the `/cluster/metrics` line (line 36):

```go
mux.HandleFunc("GET /cluster/capacity", contentNegotiated(h.HandleClusterCapacity, spa))
```

- [ ] **Step 9: Run all backend tests**

```bash
go test ./internal/...
```

Expected: all pass

- [ ] **Step 10: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat(api): add GET /cluster/capacity endpoint for resource slider bounds"
```

---

### Task 3: Frontend — API Client and Types for Cluster Capacity

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add ClusterCapacity type**

Add to `frontend/src/api/types.ts`:

```ts
export interface ClusterCapacity {
  maxNodeCPU: number;
  maxNodeMemory: number;
  totalCPU: number;
  totalMemory: number;
  nodeCount: number;
}
```

- [ ] **Step 2: Add PatchOp type**

Also in `frontend/src/api/types.ts`:

```ts
export interface PatchOp {
  op: string;
  path: string;
  value?: string;
}
```

- [ ] **Step 3: Add api.clusterCapacity() method and update patch signatures to use PatchOp**

Add to `frontend/src/api/client.ts`, in the `api` object (e.g., after `diskUsage`):

```ts
clusterCapacity: () =>
  fetchJSON<ClusterCapacity>("/cluster/capacity"),
```

Update the import to include `ClusterCapacity` and `PatchOp`.

Also update `patchServiceEnv` and `patchNodeLabels` signatures to use the new `PatchOp` type instead of the inline `Array<{ op: string; path: string; value?: string }>`:

```ts
patchServiceEnv: (id: string, ops: PatchOp[]) =>
  patch<Record<string, string>>(`/services/${id}/env`, ops, "application/json-patch+json"),
patchNodeLabels: (id: string, ops: PatchOp[]) =>
  patch<Record<string, string>>(`/nodes/${id}/labels`, ops, "application/json-patch+json"),
```

- [ ] **Step 4: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat(frontend): add ClusterCapacity type and API method"
```

---

### Task 4: SliderNumberField Component

**Files:**
- Create: `frontend/src/components/ui/slider-number-field.tsx`

- [ ] **Step 1: Create SliderNumberField**

Create `frontend/src/components/ui/slider-number-field.tsx`:

```tsx
import { NumberField } from "@base-ui/react/number-field";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { Minus, Plus } from "lucide-react";

interface SliderNumberFieldProps {
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  min?: number;
  max?: number;
  step?: number;
  label: string;
  formatDisplay?: (value: number) => string;
}

export function SliderNumberField({
  value,
  onChange,
  min = 0,
  max,
  step,
  label,
}: SliderNumberFieldProps) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <div className="flex items-center gap-3">
        <NumberField.Root
          value={value ?? null}
          onValueChange={(val) => onChange(val ?? undefined)}
          min={min}
          max={max}
          step={step}
        >
          <NumberField.Group className="flex items-center rounded-md border">
            <NumberField.Decrement className="flex size-8 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
              <Minus className="size-3" />
            </NumberField.Decrement>
            <NumberField.Input className="w-20 bg-transparent px-2 py-1 text-center font-mono text-sm focus:outline-none" />
            <NumberField.Increment className="flex size-8 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
              <Plus className="size-3" />
            </NumberField.Increment>
          </NumberField.Group>
        </NumberField.Root>
        {max !== undefined && (
          <Slider
            value={[value ?? min]}
            onValueChange={([val]) => onChange(val)}
            min={min}
            max={max}
            step={step}
            className="flex-1"
          />
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: no errors. If `NumberField` imports fail, check that `@base-ui/react` exports match — the import path may be `@base-ui/react/number-field` or `@base-ui-components/react/number-field`. Adjust accordingly.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/slider-number-field.tsx
git commit -m "feat(frontend): add SliderNumberField component (slider + number input combo)"
```

---

### Task 5: KeyValueEditor Component

**Files:**
- Create: `frontend/src/components/KeyValueEditor.tsx`

- [ ] **Step 1: Create KeyValueEditor**

Create `frontend/src/components/KeyValueEditor.tsx`. This extracts the shared pattern from `EnvEditor` and `LabelsEditor`:

```tsx
import type { PatchOp } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import SimpleTable from "@/components/SimpleTable";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Pencil, Plus, Trash2, X } from "lucide-react";
import { useState } from "react";

interface KeyValueEditorProps {
  title: string;
  entries: Record<string, string>;
  keyLabel?: string;
  valueLabel?: string;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onSave: (ops: PatchOp[]) => Promise<Record<string, string>>;
  defaultOpen?: boolean;
}

export function KeyValueEditor({
  title,
  entries,
  keyLabel = "Key",
  valueLabel = "Value",
  keyPlaceholder = "key",
  valuePlaceholder = "value",
  onSave,
  defaultOpen = false,
}: KeyValueEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({ ...entries });
    setNewKey("");
    setNewValue("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const key = newKey.trim();
    if (!key) return;
    setDraft((previous) => ({ ...previous, [key]: newValue }));
    setNewKey("");
    setNewValue("");
  }

  // Intentional: no confirmation on row removal. Removals are draft-only
  // and not persisted until Save. The user can always Cancel to undo.
  // This replaces the previous window.confirm() per spec decision.
  function removeRow(key: string) {
    setDraft((previous) => {
      const next = { ...previous };
      delete next[key];
      return next;
    });
  }

  async function save() {
    const ops: PatchOp[] = [];
    const effectiveDraft = newKey.trim()
      ? { ...draft, [newKey.trim()]: newValue }
      : draft;

    for (const key of Object.keys(entries)) {
      if (!(key in effectiveDraft)) {
        ops.push({ op: "remove", path: `/${key}` });
      }
    }
    for (const [key, value] of Object.entries(effectiveDraft)) {
      if (!(key in entries)) {
        ops.push({ op: "add", path: `/${key}`, value });
      } else if (entries[key] !== value) {
        ops.push({ op: "replace", path: `/${key}`, value });
      }
    }
    if (ops.length === 0) {
      setEditing(false);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      await onSave(ops);
      setEditing(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const sortedEntries = Object.entries(entries).sort(([a], [b]) =>
    a.localeCompare(b),
  );
  const draftEntries = Object.entries(draft).sort(([a], [b]) =>
    a.localeCompare(b),
  );

  const controls = !editing ? (
    <Button variant="outline" size="xs" onClick={openEdit}>
      <Pencil className="size-3" />
      Edit
    </Button>
  ) : null;

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={controls}
    >
      {!editing ? (
        sortedEntries.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No {title.toLowerCase()}.
          </p>
        ) : (
          <SimpleTable
            columns={[keyLabel, valueLabel]}
            items={sortedEntries}
            keyFn={([key]) => key}
            renderRow={([key, value]) => (
              <>
                <td className="p-3 font-mono text-xs">{key}</td>
                <td className="p-3 font-mono text-xs break-all">{value}</td>
              </>
            )}
          />
        )
      ) : (
        <div className="space-y-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="p-3 text-left text-sm font-medium">
                    {keyLabel}
                  </th>
                  <th className="p-3 text-left text-sm font-medium">
                    {valueLabel}
                  </th>
                  <th className="p-3" />
                </tr>
              </thead>
              <tbody>
                {draftEntries.map(([key, value]) => (
                  <tr key={key} className="border-b last:border-b-0">
                    <td className="p-3 font-mono text-xs">{key}</td>
                    <td className="p-2">
                      <Input
                        value={value}
                        onChange={(event) =>
                          setDraft((previous) => ({
                            ...previous,
                            [key]: event.target.value,
                          }))
                        }
                        className="font-mono text-xs"
                      />
                    </td>
                    <td className="p-2">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeRow(key)}
                        title="Remove"
                        className="text-muted-foreground hover:text-red-600"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))}
                <tr>
                  <td className="p-2">
                    <Input
                      value={newKey}
                      onChange={(event) => setNewKey(event.target.value)}
                      placeholder={keyPlaceholder}
                      className="font-mono text-xs"
                    />
                  </td>
                  <td className="p-2">
                    <Input
                      value={newValue}
                      onChange={(event) => setNewValue(event.target.value)}
                      placeholder={valuePlaceholder}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") addRow();
                      }}
                      className="font-mono text-xs"
                    />
                  </td>
                  <td className="p-2">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={addRow}
                      disabled={!newKey.trim()}
                      title="Add"
                      className="text-muted-foreground hover:text-foreground"
                    >
                      <Plus className="size-3.5" />
                    </Button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          {saveError && (
            <p className="text-xs text-red-600 dark:text-red-400">
              {saveError}
            </p>
          )}
          <div className="flex gap-2">
            <Button
              size="sm"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving && <Spinner className="size-3" />}
              Save
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={cancelEdit}
              disabled={saving}
            >
              <X className="size-3" />
              Cancel
            </Button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: no errors. Note: verify that `Button` has `size="xs"` and `size="icon-xs"` variants. If not, use the closest available sizes (`"sm"`, `"icon-sm"`).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/KeyValueEditor.tsx
git commit -m "feat(frontend): add KeyValueEditor shared component"
```

---

### Task 6: Upgrade EnvEditor to Use KeyValueEditor

**Files:**
- Modify: `frontend/src/components/service-detail/EnvEditor.tsx`

- [ ] **Step 1: Replace EnvEditor with thin wrapper**

Replace the entire contents of `frontend/src/components/service-detail/EnvEditor.tsx`:

```tsx
import { api } from "@/api/client";
import type { PatchOp } from "@/api/types";
import { KeyValueEditor } from "@/components/KeyValueEditor";

export function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  async function handleSave(ops: PatchOp[]) {
    const updated = await api.patchServiceEnv(serviceId, ops);
    onSaved(updated);
    return updated;
  }

  return (
    <KeyValueEditor
      title="Environment Variables"
      entries={envVars}
      keyLabel="Variable"
      valueLabel="Value"
      keyPlaceholder="NEW_VAR"
      valuePlaceholder="value"
      onSave={handleSave}
    />
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/EnvEditor.tsx
git commit -m "refactor(frontend): replace EnvEditor with KeyValueEditor wrapper"
```

---

### Task 7: Upgrade LabelsEditor to Use KeyValueEditor

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx` (LabelsEditor function, lines 26-241)

- [ ] **Step 1: Replace LabelsEditor with thin wrapper**

Replace the `LabelsEditor` function in `frontend/src/pages/NodeDetail.tsx` (lines 26-241) with:

```tsx
function LabelsEditor({
  nodeId,
  labels,
  onSaved,
}: {
  nodeId: string;
  labels: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  async function handleSave(ops: PatchOp[]) {
    const updated = await api.patchNodeLabels(nodeId, ops);
    onSaved(updated);
    return updated;
  }

  return (
    <KeyValueEditor
      title="Labels"
      entries={labels}
      keyPlaceholder="key"
      valuePlaceholder="value"
      onSave={handleSave}
    />
  );
}
```

Add imports at the top of the file:

```ts
import type { PatchOp } from "../api/types";
import { KeyValueEditor } from "../components/KeyValueEditor";
```

Remove now-unused imports: `CollapsibleSection`, `SimpleTable`, `Pencil`, `Plus`, `Trash2`, `X` — check each one individually. **Do NOT remove `Spinner`** — it is still used by `NodeAvailabilityControl` later in the file.

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx
git commit -m "refactor(frontend): replace LabelsEditor with KeyValueEditor wrapper"
```

---

### Task 8: Upgrade Scale Service (ReplicaCard)

**Files:**
- Modify: `frontend/src/components/service-detail/ReplicaCard.tsx`

- [ ] **Step 1: Replace hand-rolled popover and number input**

Rewrite `ReplicaCard.tsx` to use shadcn Popover, base-ui NumberField, and shadcn Button. Key changes:

1. Replace the hand-rolled `<div className="absolute ...">` with `<Popover>` / `<PopoverTrigger>` / `<PopoverContent>`.
2. Replace `<input type="number">` with `<NumberField.Root>` / `<NumberField.Group>` / `<NumberField.Input>` / `<NumberField.Increment>` / `<NumberField.Decrement>`.
3. Replace bare `<button>` elements with shadcn `<Button>`.
4. Use `modal` prop on PopoverContent to prevent accidental dismiss.
5. Keep: `ReplicaDoughnut`, `InfoCard`, loading/error patterns, keyboard shortcuts (Enter/Escape — Popover handles Escape natively).

Imports to add:
```ts
import { NumberField } from "@base-ui/react/number-field";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Minus, Plus } from "lucide-react";
```

The `scaleOpen` state is replaced by Popover's controlled `open`/`onOpenChange`.

The NumberField manages the numeric value directly — use `onValueChange` instead of parsing string state.

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/ReplicaCard.tsx
git commit -m "feat(frontend): upgrade ReplicaCard to shadcn Popover + base-ui NumberField"
```

---

### Task 9: Upgrade ServiceActions (Image Popover + AlertDialogs)

**Files:**
- Modify: `frontend/src/components/service-detail/ServiceActions.tsx`

- [ ] **Step 1: Upgrade Update Image to use Popover and Input**

Replace the hand-rolled absolute div for image update with shadcn `Popover`/`PopoverTrigger`/`PopoverContent` (with `modal` prop). Replace `<input type="text">` with shadcn `Input`. Replace bare buttons with shadcn `Button`.

- [ ] **Step 2: Upgrade Rollback to use AlertDialog**

Replace `window.confirm("Are you sure you want to rollback this service?")` with:

```tsx
<AlertDialog>
  <AlertDialogTrigger asChild>
    <Button variant="outline" size="sm" disabled={!canRollback || rollbackLoading}>
      {rollbackLoading ? <Spinner className="size-3" /> : <RotateCcw className="size-3.5" />}
      Rollback
    </Button>
  </AlertDialogTrigger>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>Rollback service?</AlertDialogTitle>
      <AlertDialogDescription>
        This will rollback the service to its previous specification.
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>Cancel</AlertDialogCancel>
      <AlertDialogAction onClick={() => void executeRollback()}>
        Rollback
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

Extract the confirm-guarded logic into `executeRollback()` (same body as current `handleRollback` minus the `window.confirm` check).

- [ ] **Step 3: Upgrade Restart to use AlertDialog**

Same pattern as Rollback. Replace `window.confirm(...)` with AlertDialog. Description: "This triggers a rolling restart of all tasks."

- [ ] **Step 4: Add imports**

```ts
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
```

Remove bare button CSS classes.

- [ ] **Step 5: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/service-detail/ServiceActions.tsx
git commit -m "feat(frontend): upgrade ServiceActions to shadcn Popover, AlertDialog, Button"
```

---

### Task 10: Upgrade ResourcesEditor with SliderNumberField

**Files:**
- Modify: `frontend/src/components/service-detail/ResourcesEditor.tsx`

- [ ] **Step 1: Rename state variables and add capacity fetch**

The current state variables use abbreviations (`limitCpu`, `limitMem`, `resCpu`, `resMem`). Rename them to full words per project conventions: `limitCpuCores`, `limitMemoryMegabytes`, `reservedCpuCores`, `reservedMemoryMegabytes`. Also add capacity state.

```ts
import type { ClusterCapacity } from "@/api/types";
import { SliderNumberField } from "@/components/ui/slider-number-field";

const [limitCpuCores, setLimitCpuCores] = useState("");
const [limitMemoryMegabytes, setLimitMemoryMegabytes] = useState("");
const [reservedCpuCores, setReservedCpuCores] = useState("");
const [reservedMemoryMegabytes, setReservedMemoryMegabytes] = useState("");
const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);
```

Update `openEdit()` to convert bytes→MB for memory:

```ts
function openEdit() {
  setLimitCpuCores(typed.limits?.nanoCPUs != null ? String(typed.limits.nanoCPUs / 1e9) : "");
  setLimitMemoryMegabytes(typed.limits?.memoryBytes != null ? String(typed.limits.memoryBytes / (1024 * 1024)) : "");
  setReservedCpuCores(typed.reservations?.nanoCPUs != null ? String(typed.reservations.nanoCPUs / 1e9) : "");
  setReservedMemoryMegabytes(typed.reservations?.memoryBytes != null ? String(typed.reservations.memoryBytes / (1024 * 1024)) : "");
  api.clusterCapacity().then(setCapacity).catch(() => {});
  setSaveError(null);
  setEditing(true);
}
```

- [ ] **Step 2: Replace all 4 number inputs with SliderNumberField**

Replace each `<input type="number">` with `<SliderNumberField>`. All 4 fields:

CPU limit:
```tsx
<SliderNumberField
  label="CPU (cores)"
  value={limitCpuCores ? parseFloat(limitCpuCores) : undefined}
  onChange={(value) => setLimitCpuCores(value !== undefined ? String(value) : "")}
  min={0}
  max={capacity?.maxNodeCPU}
  step={0.25}
/>
```

Memory limit (in MB):
```tsx
<SliderNumberField
  label="Memory (MB)"
  value={limitMemoryMegabytes ? parseFloat(limitMemoryMegabytes) : undefined}
  onChange={(value) => setLimitMemoryMegabytes(value !== undefined ? String(value) : "")}
  min={0}
  max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
  step={16}
/>
```

CPU reserved:
```tsx
<SliderNumberField
  label="CPU (cores)"
  value={reservedCpuCores ? parseFloat(reservedCpuCores) : undefined}
  onChange={(value) => setReservedCpuCores(value !== undefined ? String(value) : "")}
  min={0}
  max={capacity?.maxNodeCPU}
  step={0.25}
/>
```

Memory reserved (in MB):
```tsx
<SliderNumberField
  label="Memory (MB)"
  value={reservedMemoryMegabytes ? parseFloat(reservedMemoryMegabytes) : undefined}
  onChange={(value) => setReservedMemoryMegabytes(value !== undefined ? String(value) : "")}
  min={0}
  max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
  step={16}
/>
```

- [ ] **Step 3: Convert MB back to bytes on save**

In the `save()` function, update all 4 fields. CPU conversion is unchanged (cores → nanoCPUs). Memory now converts MB → bytes:

```ts
async function save() {
  const patch: ServiceResourceShape = {};
  if (limitCpuCores || limitMemoryMegabytes) {
    patch.limits = {};
    if (limitCpuCores) {
      patch.limits.nanoCPUs = Math.round(parseFloat(limitCpuCores) * 1e9);
    }
    if (limitMemoryMegabytes) {
      patch.limits.memoryBytes = Math.round(parseFloat(limitMemoryMegabytes) * 1024 * 1024);
    }
  }
  if (reservedCpuCores || reservedMemoryMegabytes) {
    patch.reservations = {};
    if (reservedCpuCores) {
      patch.reservations.nanoCPUs = Math.round(parseFloat(reservedCpuCores) * 1e9);
    }
    if (reservedMemoryMegabytes) {
      patch.reservations.memoryBytes = Math.round(parseFloat(reservedMemoryMegabytes) * 1024 * 1024);
    }
  }
  // ... rest of save logic unchanged
}
```

- [ ] **Step 4: Replace bare buttons with shadcn Button**

Replace the Save/Cancel/Edit buttons with shadcn `Button` components.

- [ ] **Step 5: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/service-detail/ResourcesEditor.tsx
git commit -m "feat(frontend): upgrade ResourcesEditor with SliderNumberField and cluster capacity"
```

---

### Task 11: Upgrade Node Availability Select and Drain AlertDialog

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx` (NodeAvailabilityControl function — originally at lines 243-279, but line numbers will have shifted after Task 7 reduced LabelsEditor; search for `function NodeAvailabilityControl`)

- [ ] **Step 1: Replace native select with shadcn Select**

Replace the `<select>` in `NodeAvailabilityControl` with shadcn `Select`:

```tsx
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
```

```tsx
<Select value={current} onValueChange={(value) => void handleChange(value as "active" | "drain" | "pause")}>
  <SelectTrigger className="w-32" disabled={loading}>
    <SelectValue />
  </SelectTrigger>
  <SelectContent>
    <SelectItem value="active">Active</SelectItem>
    <SelectItem value="drain">Drain</SelectItem>
    <SelectItem value="pause">Pause</SelectItem>
  </SelectContent>
</Select>
```

- [ ] **Step 2: Replace window.confirm for drain with AlertDialog**

Instead of confirming inline in `handleChange`, use an AlertDialog that opens when "drain" is selected. Add state for pending drain action:

```ts
const [drainPending, setDrainPending] = useState(false);

function handleValueChange(value: string) {
  if (value === "drain" && current !== "drain") {
    setDrainPending(true);
  } else {
    void handleChange(value as "active" | "drain" | "pause");
  }
}

function confirmDrain() {
  setDrainPending(false);
  void handleChange("drain");
}
```

Add AlertDialog for drain confirmation:

```tsx
<AlertDialog open={drainPending} onOpenChange={setDrainPending}>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>Drain this node?</AlertDialogTitle>
      <AlertDialogDescription>
        Draining this node will reschedule all running tasks to other nodes.
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>Cancel</AlertDialogCancel>
      <AlertDialogAction onClick={confirmDrain}>Drain</AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx
git commit -m "feat(frontend): upgrade NodeAvailabilityControl to shadcn Select + AlertDialog"
```

---

### Task 12: Upgrade Remove Task AlertDialog

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Replace window.confirm with AlertDialog**

Replace the `handleRemove` function's `window.confirm(...)` with a controlled AlertDialog. Add state:

```ts
const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
```

Replace the bare Trash2 button with:

```tsx
<AlertDialog open={removeDialogOpen} onOpenChange={setRemoveDialogOpen}>
  <AlertDialogTrigger asChild>
    <Button variant="destructive" size="sm">
      {removeLoading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
      Remove
    </Button>
  </AlertDialogTrigger>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>Force-remove this task?</AlertDialogTitle>
      <AlertDialogDescription>
        This will kill the backing container. The service scheduler will start a replacement.
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>Cancel</AlertDialogCancel>
      <AlertDialogAction
        onClick={() => void executeRemove()}
        className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
      >
        Remove
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

Extract `executeRemove()` from `handleRemove()` (same body minus the `window.confirm` check).

- [ ] **Step 2: Add imports**

```ts
import { Button } from "@/components/ui/button";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
```

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/TaskDetail.tsx
git commit -m "feat(frontend): upgrade task remove to shadcn AlertDialog + Button"
```

---

### Task 13: Lint, Format, and Final Verification

**Files:** All modified files

- [ ] **Step 1: Run frontend lint**

```bash
cd frontend && npm run lint
```

Fix any issues.

- [ ] **Step 2: Run frontend format**

```bash
cd frontend && npm run fmt
```

- [ ] **Step 3: Run frontend type-check**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Run backend tests**

```bash
go test ./internal/...
```

- [ ] **Step 5: Run full check**

```bash
make check
```

Expected: all lint, format, and tests pass.

- [ ] **Step 6: Update OpenAPI spec**

Add the `GET /cluster/capacity` endpoint to `api/openapi.yaml` following the existing patterns for `/cluster` and `/cluster/metrics`.

- [ ] **Step 7: Update CHANGELOG.md**

Add entries under `[Unreleased]`:
- Added: Cluster capacity endpoint (`GET /cluster/capacity`)
- Improved: All mutation forms upgraded to rich shadcn/ui components (popovers, number inputs with increment/decrement, styled dropdowns, confirmation dialogs)
- Improved: Resource limits editor now shows slider with cluster-aware bounds
- Improved: Environment variables and node labels editors consolidated with consistent design

- [ ] **Step 8: Commit any fixes**

```bash
git add -u && git commit -m "chore: fix lint, formatting, and update docs"
```

Only if there were changes to commit.
