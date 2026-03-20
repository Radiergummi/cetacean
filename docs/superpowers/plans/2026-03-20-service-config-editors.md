# Service Config Frontend Editors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add frontend editor components for placement, ports, update/rollback policy, and log driver, plus operations-level awareness to disable editors when the server restricts writes.

**Architecture:** Each editor follows the existing view/edit toggle pattern (HealthcheckEditor, ResourcesEditor). A new `useOperationsLevel` context provides the server's configured level to all editors. Editors read initial data from the service object (except ports, which need a separate fetch for spec ports). Each editor component is self-contained with its own state management.

**Tech Stack:** React 19, TypeScript, Tailwind CSS v4, shadcn/ui, existing project patterns

**Spec:** `docs/superpowers/specs/2026-03-20-service-config-editors-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `frontend/src/hooks/useOperationsLevel.tsx` | Fetch + cache operations level from health endpoint |
| Modify | `frontend/src/App.tsx` | Wrap app in `OperationsLevelProvider` |
| Modify | `frontend/src/api/client.ts` | Add 6 new API methods (placement PUT, ports GET/PATCH, policy PATCH x2, log driver PATCH) |
| Modify | `frontend/src/api/types.ts` | Add/update types (Placement, PortConfig, UpdateConfig, Driver) |
| Create | `frontend/src/components/service-detail/PlacementEditor.tsx` | Placement constraints + max replicas editor |
| Create | `frontend/src/components/service-detail/PortsEditor.tsx` | Published ports card-based editor |
| Create | `frontend/src/components/service-detail/PolicyEditor.tsx` | Shared update/rollback policy editor |
| Create | `frontend/src/components/service-detail/LogDriverEditor.tsx` | Log driver name + options editor |
| Modify | `frontend/src/components/service-detail/index.ts` | Export new components |
| Modify | `frontend/src/pages/ServiceDetail.tsx` | Replace read-only blocks with editors |

---

### Task 1: Operations level hook + provider

**Files:**
- Create: `frontend/src/hooks/useOperationsLevel.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create `useOperationsLevel.tsx`**

Follow the same pattern as `useAuth.tsx`:

```tsx
import type React from "react";
import { createContext, useContext, useEffect, useState } from "react";

interface OperationsLevelState {
  level: number;
  loading: boolean;
}

const OperationsLevelContext = createContext<OperationsLevelState>({
  level: 0,
  loading: true,
});

export function useOperationsLevel() {
  return useContext(OperationsLevelContext);
}

export function OperationsLevelProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<OperationsLevelState>({
    level: 0,
    loading: true,
  });

  useEffect(() => {
    fetch("/-/health")
      .then((response) => response.json())
      .then((data) =>
        setState({
          level: data.operationsLevel ?? 0,
          loading: false,
        }),
      )
      .catch(() =>
        setState({
          level: 0,
          loading: false,
        }),
      );
  }, []);

  return <OperationsLevelContext value={state}>{children}</OperationsLevelContext>;
}
```

Note: defaults to `level: 0` (read-only) while loading to prevent flash of edit buttons. Uses direct `fetch` to `/-/health` (no `api.get` wrapper needed — this is a meta endpoint).

- [ ] **Step 2: Add provider to `App.tsx`**

Import `OperationsLevelProvider` from `@/hooks/useOperationsLevel`. Wrap inside `AuthProvider`, outside `ConnectionTracker`:

```tsx
<AuthProvider>
  <OperationsLevelProvider>
    <ConnectionTracker>
      ...
    </ConnectionTracker>
  </OperationsLevelProvider>
</AuthProvider>
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep -i operationsLevel`
Expected: no errors related to operations level

- [ ] **Step 4: Commit**

```bash
git add frontend/src/hooks/useOperationsLevel.tsx frontend/src/App.tsx
git commit -m "feat(frontend): add useOperationsLevel hook and provider"
```

---

### Task 2: API client methods + types

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/api/types.ts`

- [ ] **Step 1: Add type aliases to `types.ts`**

These types already exist inline in the `Service` interface. Extract them as named type aliases at the bottom of the file (same pattern as the existing `export type Healthcheck`):

```typescript
export type Placement = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;
export type PlacementPreference = NonNullable<Placement["Preferences"]>[number];
export type PortConfig = NonNullable<NonNullable<Service["Spec"]["EndpointSpec"]>["Ports"]>[number];
export type UpdateConfig = NonNullable<Service["Spec"]["UpdateConfig"]>;
export type LogDriver = NonNullable<Service["Spec"]["TaskTemplate"]["LogDriver"]>;
```

This avoids duplicating/conflicting with the inline types. Note: `UpdateConfig.Parallelism` is `number` (required) in the existing inline type — this is correct for Docker's wire format.

- [ ] **Step 2: Add API methods to `client.ts`**

Add before the closing `}` of the `api` object (follow the existing `fetchJSON` + `.then()` pattern for GETs):

```typescript
  servicePorts: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ ports: PortConfig[] }>(`/services/${id}/ports`, signal).then(
      (r) => r.ports,
    ),

  putServicePlacement: (id: string, placement: Placement) =>
    put<{ placement: Placement }>(`/services/${id}/placement`, placement),

  patchServicePorts: (id: string, ports: PortConfig[]) =>
    patch<{ ports: PortConfig[] }>(
      `/services/${id}/ports`,
      { ports },
      "application/merge-patch+json",
    ),

  patchServiceUpdatePolicy: (id: string, partial: Record<string, unknown>) =>
    patch<{ updatePolicy: UpdateConfig }>(
      `/services/${id}/update-policy`,
      partial,
      "application/merge-patch+json",
    ),

  patchServiceRollbackPolicy: (id: string, partial: Record<string, unknown>) =>
    patch<{ rollbackPolicy: UpdateConfig }>(
      `/services/${id}/rollback-policy`,
      partial,
      "application/merge-patch+json",
    ),

  patchServiceLogDriver: (id: string, partial: Record<string, unknown>) =>
    patch<{ logDriver: LogDriver }>(
      `/services/${id}/log-driver`,
      partial,
      "application/merge-patch+json",
    ),
```

Add the necessary imports at the top of `client.ts`:

```typescript
import type { Placement, PortConfig, UpdateConfig, LogDriver } from "./types";
```

Note: `servicePorts` uses `fetchJSON` (not `get` — there is no `get` function). The `.then(r => r.ports)` unwraps the JSON-LD wrapper, matching the pattern of `serviceEnv`, `serviceLabels`, etc.

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep -c error` (ignore pre-existing ServiceList errors)
Expected: 0 new errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/api/types.ts
git commit -m "feat(frontend): add API client methods and types for service config endpoints"
```

---

### Task 3: PlacementEditor

**Files:**
- Create: `frontend/src/components/service-detail/PlacementEditor.tsx`

- [ ] **Step 1: Create `PlacementEditor.tsx`**

Follow `HealthcheckEditor` as the template for view/edit toggle, error handling, and button layout:

```tsx
import { api } from "@/api/client";
import type { Placement } from "@/api/types";
import { PlacementPanel } from "@/components/service-detail/PlacementPanel";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PlacementEditorProps {
  serviceId: string;
  placement: Placement | null;
  onSaved: () => void;
}

export function PlacementEditor({ serviceId, placement, onSaved }: PlacementEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [constraints, setConstraints] = useState<string[]>([]);
  const [maxReplicas, setMaxReplicas] = useState<number>(0);

  function openEdit() {
    setConstraints([...(placement?.Constraints ?? [])]);
    setMaxReplicas(placement?.MaxReplicas ?? 0);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addConstraint() {
    setConstraints([...constraints, ""]);
  }

  function removeConstraint(index: number) {
    setConstraints(constraints.filter((_, i) => i !== index));
  }

  function updateConstraint(index: number, value: string) {
    const updated = [...constraints];
    updated[index] = value;
    setConstraints(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const nonEmpty = constraints.filter((c) => c.trim() !== "");

      await api.putServicePlacement(serviceId, {
        Constraints: nonEmpty.length > 0 ? nonEmpty : undefined,
        Preferences: placement?.Preferences,
        MaxReplicas: maxReplicas || undefined,
      });

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update placement"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Placement
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        <PlacementPanel placement={placement ?? { Constraints: [], Preferences: [] }} />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Placement
      </h3>

      <div className="space-y-2">
        <label className="text-sm font-medium">Constraints</label>

        {constraints.map((constraint, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={constraint}
              onChange={(event) => updateConstraint(index, event.target.value)}
              placeholder="node.role==manager"
              className="font-mono text-sm"
            />

            <Button
              variant="outline"
              size="xs"
              onClick={() => removeConstraint(index)}
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}

        <Button variant="outline" size="sm" onClick={addConstraint}>
          <Plus className="size-3" />
          Add constraint
        </Button>
      </div>

      <div className="space-y-2">
        <label className="text-sm font-medium">Max replicas per node</label>

        <Input
          type="number"
          min={0}
          value={maxReplicas || ""}
          onChange={(event) => setMaxReplicas(Number(event.target.value) || 0)}
          placeholder="0 (unlimited)"
          className="w-32"
        />
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
```

Note: `onSaved` takes no arguments — after a successful PUT, the SSE change event will trigger a service refetch in ServiceDetail. Preferences are passed through (preserved but not editable).

- [ ] **Step 2: Export from `index.ts`**

Add to `frontend/src/components/service-detail/index.ts`:

```typescript
export { PlacementEditor } from "./PlacementEditor";
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep PlacementEditor`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/PlacementEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat(frontend): add PlacementEditor component"
```

---

### Task 4: PortsEditor

**Files:**
- Create: `frontend/src/components/service-detail/PortsEditor.tsx`

- [ ] **Step 1: Create `PortsEditor.tsx`**

Card-based editor — each port is a card with select/input fields:

```tsx
import { api } from "@/api/client";
import type { PortConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PortsEditorProps {
  serviceId: string;
  ports: PortConfig[];
  onSaved: (ports: PortConfig[]) => void;
}

const defaultPort: PortConfig = {
  Protocol: "tcp",
  TargetPort: 0,
  PublishedPort: 0,
  PublishMode: "ingress",
};

export function PortsEditor({ serviceId, ports, onSaved }: PortsEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<PortConfig[]>([]);

  function openEdit() {
    setDraft(ports.map((port) => ({ ...port })));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addPort() {
    setDraft([...draft, { ...defaultPort }]);
  }

  function removePort(index: number) {
    setDraft(draft.filter((_, i) => i !== index));
  }

  function updatePort(index: number, field: keyof PortConfig, value: string | number) {
    const updated = [...draft];
    updated[index] = { ...updated[index], [field]: value };
    setDraft(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServicePorts(serviceId, draft);
      setEditing(false);
      onSaved(result.ports);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update ports"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Published Ports
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {ports.length === 0 ? (
          <p className="text-sm text-muted-foreground">No published ports.</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {ports.map(({ Protocol, PublishMode, PublishedPort, TargetPort }, index) => (
              <span
                key={index}
                className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 font-mono text-sm"
              >
                <span className="font-semibold">{PublishedPort || "auto"}</span>
                <span className="text-muted-foreground">{"\u2192"}</span>
                <span>
                  {TargetPort}/{Protocol}
                </span>
                <span className="text-xs text-muted-foreground">({PublishMode})</span>
              </span>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Published Ports
      </h3>

      <div className="space-y-3">
        {draft.map((port, index) => (
          <div key={index} className="relative rounded-lg border p-3">
            <Button
              variant="outline"
              size="xs"
              className="absolute top-2 right-2"
              onClick={() => removePort(index)}
            >
              <Trash2 className="size-3" />
            </Button>

            <div className="grid grid-cols-2 gap-3 pr-10">
              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Protocol</label>

                <select
                  value={port.Protocol}
                  onChange={(event) => updatePort(index, "Protocol", event.target.value)}
                  className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                >
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="sctp">SCTP</option>
                </select>
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Publish mode</label>

                <select
                  value={port.PublishMode}
                  onChange={(event) => updatePort(index, "PublishMode", event.target.value)}
                  className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                >
                  <option value="ingress">Ingress (load-balanced)</option>
                  <option value="host">Host (direct)</option>
                </select>
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Container port</label>

                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={port.TargetPort || ""}
                  onChange={(event) => updatePort(index, "TargetPort", Number(event.target.value) || 0)}
                  placeholder="80"
                />
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Host port</label>

                <Input
                  type="number"
                  min={0}
                  max={65535}
                  value={port.PublishedPort || ""}
                  onChange={(event) => updatePort(index, "PublishedPort", Number(event.target.value) || 0)}
                  placeholder="Auto-assign"
                />
              </div>
            </div>
          </div>
        ))}
      </div>

      <Button variant="outline" size="sm" onClick={addPort}>
        <Plus className="size-3" />
        Add port
      </Button>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Export from `index.ts`**

Add: `export { PortsEditor } from "./PortsEditor";`

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep PortsEditor`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/PortsEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat(frontend): add PortsEditor component"
```

---

### Task 5: PolicyEditor (shared for update + rollback)

**Files:**
- Create: `frontend/src/components/service-detail/PolicyEditor.tsx`

- [ ] **Step 1: Create `PolicyEditor.tsx`**

```tsx
import { api } from "@/api/client";
import type { UpdateConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { formatDuration } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

interface PolicyEditorProps {
  type: "update" | "rollback";
  serviceId: string;
  policy: UpdateConfig | null;
  onSaved: () => void;
}

interface FormState {
  parallelism: number;
  delaySeconds: number;
  monitorSeconds: number;
  maxFailureRatio: number;
  failureAction: string;
  order: string;
}

function nanosToSeconds(nanos: number | undefined): number {
  return nanos ? nanos / 1e9 : 0;
}

function policyToForm(policy: UpdateConfig | null): FormState {
  return {
    parallelism: policy?.Parallelism ?? 1,
    delaySeconds: nanosToSeconds(policy?.Delay),
    monitorSeconds: nanosToSeconds(policy?.Monitor),
    maxFailureRatio: policy?.MaxFailureRatio ?? 0,
    failureAction: policy?.FailureAction ?? "pause",
    order: policy?.Order ?? "stop-first",
  };
}

function formatRatio(ratio: number | undefined): string {
  if (ratio == null || ratio === 0) {
    return "0%";
  }

  return `${(ratio * 100).toFixed(0)}%`;
}

const titles: Record<string, string> = {
  update: "Update Policy",
  rollback: "Rollback Policy",
};

export function PolicyEditor({ type, serviceId, policy, onSaved }: PolicyEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(policyToForm(null));

  const patchFunction = type === "update"
    ? api.patchServiceUpdatePolicy
    : api.patchServiceRollbackPolicy;

  const failureActions = type === "update"
    ? ["pause", "continue", "rollback"]
    : ["pause", "continue"];

  function openEdit() {
    setForm(policyToForm(policy));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      await patchFunction(serviceId, {
        Parallelism: form.parallelism,
        Delay: Math.round(form.delaySeconds * 1e9),
        Monitor: Math.round(form.monitorSeconds * 1e9),
        MaxFailureRatio: form.maxFailureRatio,
        FailureAction: form.failureAction,
        Order: form.order,
      });

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, `Failed to update ${type} policy`));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    const rows = [
      ["Parallelism", String(policy?.Parallelism ?? 1)],
      policy?.Delay != null && ["Delay", formatDuration(policy.Delay)],
      policy?.FailureAction && ["Failure Action", policy.FailureAction],
      policy?.Monitor != null && ["Monitor", formatDuration(policy.Monitor)],
      policy?.MaxFailureRatio != null && ["Max Failure Ratio", formatRatio(policy.MaxFailureRatio)],
      policy?.Order && ["Order", policy.Order],
    ].filter(Boolean) as [string, string][];

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            {titles[type]}
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {rows.length > 0 ? (
          <div className="space-y-1">
            {rows.map(([label, value]) => (
              <div key={label} className="flex justify-between text-sm">
                <span className="text-muted-foreground">{label}</span>
                <span className="font-mono">{value}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No {type} policy configured.</p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {titles[type]}
      </h3>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Parallelism</label>

          <Input
            type="number"
            min={0}
            value={form.parallelism}
            onChange={(event) => setForm({ ...form, parallelism: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Delay (seconds)</label>

          <Input
            type="number"
            min={0}
            step={0.1}
            value={form.delaySeconds || ""}
            onChange={(event) => setForm({ ...form, delaySeconds: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Monitor (seconds)</label>

          <Input
            type="number"
            min={0}
            step={0.1}
            value={form.monitorSeconds || ""}
            onChange={(event) => setForm({ ...form, monitorSeconds: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Max failure ratio</label>

          <Input
            type="number"
            min={0}
            max={1}
            step={0.1}
            value={form.maxFailureRatio}
            onChange={(event) => setForm({ ...form, maxFailureRatio: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Failure action</label>

          <select
            value={form.failureAction}
            onChange={(event) => setForm({ ...form, failureAction: event.target.value })}
            className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
          >
            {failureActions.map((action) => (
              <option key={action} value={action}>
                {action}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Order</label>

          <select
            value={form.order}
            onChange={(event) => setForm({ ...form, order: event.target.value })}
            className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
          >
            <option value="stop-first">stop-first</option>
            <option value="start-first">start-first</option>
          </select>
        </div>
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Export from `index.ts`**

Add: `export { PolicyEditor } from "./PolicyEditor";`

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep PolicyEditor`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/PolicyEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat(frontend): add PolicyEditor component (shared for update + rollback)"
```

---

### Task 6: LogDriverEditor

**Files:**
- Create: `frontend/src/components/service-detail/LogDriverEditor.tsx`

- [ ] **Step 1: Create `LogDriverEditor.tsx`**

Options table is embedded in the editor's draft state (not using KeyValueEditor as a standalone component):

```tsx
import { api } from "@/api/client";
import type { LogDriver } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface LogDriverEditorProps {
  serviceId: string;
  logDriver: LogDriver | null;
  onSaved: () => void;
}

export function LogDriverEditor({ serviceId, logDriver, onSaved }: LogDriverEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [driverName, setDriverName] = useState("");
  const [options, setOptions] = useState<[string, string][]>([]);

  function openEdit() {
    setDriverName(logDriver?.Name ?? "");
    setOptions(
      logDriver?.Options
        ? Object.entries(logDriver.Options)
        : [],
    );
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addOption() {
    setOptions([...options, ["", ""]]);
  }

  function removeOption(index: number) {
    setOptions(options.filter((_, i) => i !== index));
  }

  function updateOption(index: number, part: 0 | 1, value: string) {
    const updated = [...options];
    updated[index] = [...updated[index]] as [string, string];
    updated[index][part] = value;
    setOptions(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const optionsMap: Record<string, string> = {};

      for (const [key, value] of options) {
        if (key.trim()) {
          optionsMap[key.trim()] = value;
        }
      }

      await api.patchServiceLogDriver(serviceId, {
        Name: driverName,
        Options: Object.keys(optionsMap).length > 0 ? optionsMap : undefined,
      });

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update log driver"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Log Driver
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {logDriver ? (
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span className="text-muted-foreground">Driver</span>
              <span className="font-mono">{logDriver.Name}</span>
            </div>

            {logDriver.Options && Object.keys(logDriver.Options).length > 0 && (
              <div className="flex flex-wrap gap-2">
                {Object.entries(logDriver.Options).map(([key, value]) => (
                  <span
                    key={key}
                    className="inline-flex items-center rounded-md border px-2 py-0.5 font-mono text-xs"
                  >
                    <span className="text-muted-foreground">{key}=</span>
                    {value}
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No log driver configured.</p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Log Driver
      </h3>

      <div className="space-y-1">
        <label className="text-xs font-medium text-muted-foreground">Driver name</label>

        <Input
          value={driverName}
          onChange={(event) => setDriverName(event.target.value)}
          placeholder="json-file"
          className="w-64"
        />
      </div>

      <div className="space-y-2">
        <label className="text-xs font-medium text-muted-foreground">Options</label>

        {options.map(([key, value], index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={key}
              onChange={(event) => updateOption(index, 0, event.target.value)}
              placeholder="key"
              className="font-mono text-sm"
            />

            <Input
              value={value}
              onChange={(event) => updateOption(index, 1, event.target.value)}
              placeholder="value"
              className="font-mono text-sm"
            />

            <Button variant="outline" size="xs" onClick={() => removeOption(index)}>
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}

        <Button variant="outline" size="sm" onClick={addOption}>
          <Plus className="size-3" />
          Add option
        </Button>
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Export from `index.ts`**

Add: `export { LogDriverEditor } from "./LogDriverEditor";`

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep LogDriverEditor`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/LogDriverEditor.tsx frontend/src/components/service-detail/index.ts
git commit -m "feat(frontend): add LogDriverEditor component"
```

---

### Task 7: ServiceDetail integration

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add imports and state**

Add to imports:

```typescript
import { PlacementEditor } from "@/components/service-detail/PlacementEditor";
import { PortsEditor } from "@/components/service-detail/PortsEditor";
import { PolicyEditor } from "@/components/service-detail/PolicyEditor";
import { LogDriverEditor } from "@/components/service-detail/LogDriverEditor";
import type { PortConfig } from "@/api/types";
```

Add state for spec ports (the only sub-resource that needs a separate fetch):

```typescript
const [specPorts, setSpecPorts] = useState<PortConfig[] | null>(null);
```

Add the fetch in the existing `fetchData` function (near where `serviceEnv`, `serviceHealthcheck`, etc. are fetched):

```typescript
api.servicePorts(id!, signal).then(setSpecPorts).catch(() => {});
```

Note: `servicePorts` already unwraps to `PortConfig[]` (via `.then(r => r.ports)` in the client), so `setSpecPorts` receives the array directly. The `.catch(() => {})` matches the pattern of all other sub-resource fetches.

- [ ] **Step 2: Replace the read-only Ports block**

Replace the existing ports `CollapsibleSection` (lines ~410-434 that read from `service.Endpoint.Ports`) with:

```tsx
{specPorts !== null && (
  <CollapsibleSection title="Ports" defaultOpen={false}>
    <PortsEditor
      serviceId={id!}
      ports={specPorts}
      onSaved={setSpecPorts}
    />
  </CollapsibleSection>
)}
```

Remove the `CollapsibleSection` wrapper — the `PortsEditor` manages its own header with the edit button. Actually, keep it consistent: the placement, log driver, and policy sections are currently inside bordered `<div>`s, not `CollapsibleSection`s. Ports currently uses a `CollapsibleSection`. For consistency, replace the `CollapsibleSection` for ports with a bordered div matching the other editors, or embed the editor directly. Follow whatever pattern the existing section at that location uses.

Look at the actual current structure:
- Ports: `CollapsibleSection` → badge list
- Placement: bordered `<div>` → `PlacementPanel`
- Log Driver: bordered `<div>` → `KVTable`
- Update/Rollback Config: bordered `<div>` → `KVTable`

Replace each:

**Ports** (~lines 410-434): Replace the `CollapsibleSection` + badge list. Keep the PortsEditor in the same location (between healthcheck and mounts, outside the deploy config grid — this is where users expect to see ports):
```tsx
{specPorts !== null && (
  <div className="flex flex-col gap-3 rounded-lg border p-3">
    <PortsEditor serviceId={id!} ports={specPorts} onSaved={setSpecPorts} />
  </div>
)}
```

**Placement** (~lines 579-587): Replace the bordered div + PlacementPanel with:
```tsx
<div className="flex flex-col gap-3 rounded-lg border p-3">
  <PlacementEditor
    serviceId={id!}
    placement={taskTemplate.Placement ?? null}
    onSaved={fetchData}
  />
</div>
```

Note: show PlacementEditor even when placement is null (allows adding constraints to a service that has none). Remove the `{taskTemplate.Placement && (...)}` conditional.

**Log Driver** (~lines 617-633): Replace with:
```tsx
<div className="flex flex-col gap-3 rounded-lg border p-3">
  <LogDriverEditor
    serviceId={id!}
    logDriver={taskTemplate.LogDriver ?? null}
    onSaved={fetchData}
  />
</div>
```

Same: show even when null.

**Update Config** (~lines 635-641): Replace with:
```tsx
<div className="flex flex-col gap-3 rounded-lg border p-3">
  <PolicyEditor
    type="update"
    serviceId={id!}
    policy={service.Spec.UpdateConfig ?? null}
    onSaved={fetchData}
  />
</div>
```

**Rollback Config** (~lines 644-651): Replace with:
```tsx
<div className="flex flex-col gap-3 rounded-lg border p-3">
  <PolicyEditor
    type="rollback"
    serviceId={id!}
    policy={service.Spec.RollbackConfig ?? null}
    onSaved={fetchData}
  />
</div>
```

- [ ] **Step 3: Remove the `updateConfigRows` function**

The `updateConfigRows` function (near line 736) is now unused — the `PolicyEditor` handles its own view mode. Remove it and the `UpdateConfigShape` type alias.

- [ ] **Step 4: Remove `PlacementPanel` import if no longer used directly**

Check if `PlacementPanel` is still used directly in `ServiceDetail.tsx`. If the only usage was the one we replaced, remove it from the imports. (It's still used by `PlacementEditor` internally, so don't delete the component itself.)

- [ ] **Step 5: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | grep ServiceDetail`
Expected: no errors

- [ ] **Step 6: Build**

Run: `cd frontend && npm run build 2>&1 | tail -5`
Expected: build succeeds

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat(frontend): integrate service config editors into ServiceDetail page"
```

---

### Task 8: Add operations level checks to existing editors

**Files:**
- Modify: `frontend/src/components/service-detail/EndpointModeEditor.tsx`
- Modify: `frontend/src/components/service-detail/HealthcheckEditor.tsx`
- Modify: `frontend/src/components/service-detail/ResourcesEditor.tsx`
- Modify: `frontend/src/components/service-detail/EnvEditor.tsx`
- Modify: `frontend/src/components/KeyValueEditor.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx` (action buttons: scale, image, rollback, restart)

- [ ] **Step 1: Add `useOperationsLevel` to `EndpointModeEditor`**

Import `useOperationsLevel` from `@/hooks/useOperationsLevel`. At the top of the component, add:

```tsx
const { level } = useOperationsLevel();
const canEdit = level >= 2; // tier 2 — impactful
```

Find the edit button and add `disabled={!canEdit}` and `title={canEdit ? undefined : "Editing disabled by server configuration"}`.

- [ ] **Step 2: Add `useOperationsLevel` to `HealthcheckEditor`**

Same pattern, but `canEdit = level >= 1` (tier 1).

- [ ] **Step 3: Add `useOperationsLevel` to `ResourcesEditor`**

Same pattern, `canEdit = level >= 1`.

- [ ] **Step 4: Add `useOperationsLevel` to `EnvEditor`**

Same pattern, `canEdit = level >= 1`. The edit button is passed through to `KeyValueEditor` — either add the check in `EnvEditor` (controlling whether it renders KeyValueEditor in editable mode) or pass a `disabled` prop.

- [ ] **Step 5: Add `useOperationsLevel` to `KeyValueEditor` (labels)**

`KeyValueEditor` is used for both service labels (tier 1) and node labels (tier 2). The simplest approach: add an optional `disabled` prop to `KeyValueEditor` that disables the edit button. The parent (`ServiceDetail` for service labels, `NodeDetail` for node labels) passes the appropriate check.

- [ ] **Step 6: Disable action buttons in `ServiceDetail.tsx`**

The service action buttons (scale, image update, rollback, restart) are in `ServiceActions.tsx`. Import `useOperationsLevel` there and disable buttons when `level < 1`. For node availability and task removal (tier 2), check `level < 2`.

- [ ] **Step 7: Type-check**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | tail -10`
Expected: no new errors

- [ ] **Step 8: Commit**

```bash
git add -u frontend/src/
git commit -m "feat(frontend): add operations level checks to existing editors and actions"
```

---

### Task 9: Final verification

- [ ] **Step 1: Type-check entire frontend**

Run: `cd frontend && npx tsc -b --noEmit 2>&1 | tail -10`
Expected: no new errors (only pre-existing ServiceList errors)

- [ ] **Step 2: Build frontend**

Run: `cd frontend && npm run build`
Expected: build succeeds

- [ ] **Step 3: Lint**

Run: `cd frontend && npm run lint 2>&1 | tail -10`
Expected: no new errors
