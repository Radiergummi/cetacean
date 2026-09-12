# ServiceDetail Component Extraction Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `frontend/src/pages/ServiceDetail.tsx` (~1950 lines) into focused files under `components/service-detail/`, with no behavior changes.

**Architecture:** Extract 6 substantial components into a new `components/service-detail/` subdirectory with an `index.ts` barrel, following the existing `log/` and `metrics/` patterns. Update `ServiceDetail.tsx` to import from the barrel. Replace four duplicate local `Spinner` definitions across `ServiceDetail.tsx`, `ReplicaCard.tsx`, `NodeDetail.tsx`, and `TaskDetail.tsx` with the shared `components/Spinner.tsx` — this changes the spinner icon from a custom SVG arc to `<Loader2>` from lucide-react, which is an intentional visual normalisation documented in the spec.

**Tech Stack:** React 19, TypeScript, Vite. Verification: `npx tsc -b --noEmit` (type-only, no emit), `npx vitest run`.

---

## File Map

| Action | Path | Contents |
|---|---|---|
| Create | `frontend/src/components/service-detail/PlacementPanel.tsx` | `humanizeConstraint`, `PlacementShape`, `PlacementPanel` |
| Create | `frontend/src/components/service-detail/DeploymentChanges.tsx` | `DeploymentChanges` |
| Create | `frontend/src/components/service-detail/ServiceActions.tsx` | `ServiceActions` |
| Create | `frontend/src/components/service-detail/ReplicaCard.tsx` | `ReplicaDoughnut` (private), `ReplicaCard` |
| Create | `frontend/src/components/service-detail/EnvEditor.tsx` | `EnvEditor` |
| Create | `frontend/src/components/service-detail/ResourcesEditor.tsx` | `ServiceResourceShape`, `ResourcesEditor` |
| Create | `frontend/src/components/service-detail/index.ts` | re-exports all public components |
| Modify | `frontend/src/pages/ServiceDetail.tsx` | remove extracted code + local Spinner, add barrel import |
| Modify | `frontend/src/pages/NodeDetail.tsx` | remove local Spinner, add shared import |
| Modify | `frontend/src/pages/TaskDetail.tsx` | remove local Spinner, add shared import |

> **Note on pre-existing unstaged changes:** `TasksTable.tsx` and `ViewToggle.tsx` already have unrelated modifications. When using `git add`, always add specific files — never `git add -A` or `git add .`.

---

### Task 1: Create PlacementPanel.tsx

**Files:**
- Create: `frontend/src/components/service-detail/PlacementPanel.tsx`

- [ ] **Step 1: Create the file**

```tsx
import type {Service} from "../../api/types";

export type PlacementShape = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;

function humanizeConstraint(raw: string): { label: string; exclude: boolean } | null {
  const match = raw.match(/^(.+?)\s*(==|!=)\s*(.+)$/);
  if (!match) {
    return null;
  }
  const [, field, op, value] = match;
  const exclude = op === "!=";

  if (field === "node.role") {
    if (value === "manager" && !exclude) {
      return {label: "Manager nodes only", exclude};
    }
    if (value === "worker" && !exclude) {
      return {label: "Worker nodes only", exclude};
    }
    if (value === "manager" && exclude) {
      return {label: "Exclude manager nodes", exclude};
    }
    if (value === "worker" && exclude) {
      return {label: "Exclude worker nodes", exclude};
    }
  }
  if (field === "node.hostname") {
    return {label: exclude ? `Exclude node ${value}` : `Node: ${value}`, exclude};
  }
  if (field === "node.id") {
    return {label: exclude ? `Exclude node ID ${value}` : `Node ID: ${value}`, exclude};
  }
  if (field === "node.platform.os") {
    return {label: exclude ? `Exclude OS ${value}` : `OS: ${value}`, exclude};
  }
  if (field === "node.platform.arch") {
    return {label: exclude ? `Exclude arch ${value}` : `Arch: ${value}`, exclude};
  }
  if (field.startsWith("node.labels.")) {
    const key = field.slice("node.labels.".length);
    return {label: exclude ? `${key} \u2260 ${value}` : `${key} = ${value}`, exclude};
  }
  if (field.startsWith("engine.labels.")) {
    const key = field.slice("engine.labels.".length);
    return {
      label: exclude ? `engine ${key} \u2260 ${value}` : `engine ${key} = ${value}`,
      exclude,
    };
  }
  return null;
}

export function PlacementPanel({placement}: { placement: PlacementShape }) {
  const constraints = placement.Constraints ?? [];
  const preferences = placement.Preferences ?? [];
  const hasContent = constraints.length > 0 || placement.MaxReplicas || preferences.length > 0;

  if (!hasContent) {
    return <p className="text-sm text-muted-foreground">No placement constraints.</p>;
  }

  return (
    <div className="space-y-3">
      {constraints.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {constraints.map((c) => {
            const humanized = humanizeConstraint(c);
            return (
              <span
                key={c}
                data-exclude={humanized?.exclude || undefined}
                className="inline-flex items-center rounded-lg border px-3 py-2 text-sm data-exclude:border-red-200 data-exclude:bg-red-50 data-exclude:text-red-800 dark:data-exclude:border-red-800 dark:data-exclude:bg-red-950/30 dark:data-exclude:text-red-300"
                title={c}
              >
                {humanized?.label ?? c}
              </span>
            );
          })}
        </div>
      )}

      {placement.MaxReplicas != null && placement.MaxReplicas > 0 && (
        <div className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
          <span className="text-muted-foreground">Max replicas per node:</span>
          <span className="font-semibold tabular-nums">{placement.MaxReplicas}</span>
        </div>
      )}

      {preferences.length > 0 && (
        <div className="space-y-1">
          <div className="text-xs font-medium text-muted-foreground">Spread preferences</div>
          <div className="flex flex-wrap gap-2">
            {preferences.map((p, i) => (
              <span
                key={i}
                className="inline-flex items-center rounded-md border px-2.5 py-1 font-mono text-xs"
              >
                {p.Spread?.SpreadDescriptor}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: no errors related to `PlacementPanel.tsx` (file is self-contained).

---

### Task 2: Create DeploymentChanges.tsx

**Files:**
- Create: `frontend/src/components/service-detail/DeploymentChanges.tsx`

- [ ] **Step 1: Create the file**

```tsx
import {ArrowRight} from "lucide-react";
import type {Service, SpecChange} from "../../api/types";
import {timeAgo} from "../TimeAgo";

export function DeploymentChanges({
  changes,
  updateStatus,
}: {
  changes: SpecChange[];
  updateStatus?: Service["UpdateStatus"];
}) {
  const ts = updateStatus?.CompletedAt || updateStatus?.StartedAt;
  const deploymentLabels: Record<string, string> = {
    updating: "In progress",
    rollback_started: "Rolling back",
    rollback_paused: "Rollback paused",
    rollback_completed: "Rolled back",
  };
  const stateLabel = deploymentLabels[updateStatus?.State ?? ""] ?? "Completed";

  return (
    <div className="space-y-3">
      {ts && (
        <p className="text-sm text-muted-foreground">
          {stateLabel} {timeAgo(ts)}
        </p>
      )}
      <div className="divide-y rounded-lg border">
        {changes.map(({field, new: change, old}, index) => (
          <div
            key={index}
            className="flex items-center gap-2 px-3 py-2 text-sm"
          >
            <span className="min-w-40 shrink-0 font-medium">{field}</span>
            {old && change ? (
              <>
                <span className="font-mono text-xs text-red-600 line-through dark:text-red-400">
                  {old}
                </span>
                <ArrowRight className="size-3 shrink-0 text-muted-foreground"/>
                <span className="font-mono text-xs text-green-600 dark:text-green-400">
                  {change}
                </span>
              </>
            ) : old ? (
              <span className="font-mono text-xs text-red-600 dark:text-red-400">{old}</span>
            ) : (
              <span className="font-mono text-xs text-green-600 dark:text-green-400">{change}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: no new errors.

> **Note:** If `timeAgo` is not exported from `components/TimeAgo.tsx`, check the actual export name. The function is used in `ServiceDetail.tsx` as `import {timeAgo} from "../components/TimeAgo"`.

---

### Task 3: Create ServiceActions.tsx

**Files:**
- Create: `frontend/src/components/service-detail/ServiceActions.tsx`

- [ ] **Step 1: Create the file**

```tsx
import {ImageIcon, RefreshCw, RotateCcw} from "lucide-react";
import {useState} from "react";
import {api} from "../../api/client";
import type {Service} from "../../api/types";
import {Spinner} from "../Spinner";

export function ServiceActions({service, serviceId}: { service: Service; serviceId: string }) {
  const currentImage = service.Spec.TaskTemplate.ContainerSpec.Image;
  const imageWithoutDigest = currentImage.replace(/@sha256:[a-f0-9]+$/, "");

  const [imageOpen, setImageOpen] = useState(false);
  const [imageValue, setImageValue] = useState("");
  const [imageLoading, setImageLoading] = useState(false);
  const [imageError, setImageError] = useState<string | null>(null);

  const [rollbackLoading, setRollbackLoading] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  const [restartLoading, setRestartLoading] = useState(false);
  const [restartError, setRestartError] = useState<string | null>(null);

  const canRollback = !!service.PreviousSpec;

  function openImage() {
    setImageValue(imageWithoutDigest);
    setImageError(null);
    setImageOpen(true);
  }

  function cancelImage() {
    setImageOpen(false);
    setImageError(null);
  }

  async function submitImage() {
    const trimmed = imageValue.trim();

    if (!trimmed) {
      setImageError("Enter an image name");
      return;
    }

    setImageLoading(true);
    setImageError(null);

    try {
      await api.updateServiceImage(serviceId, trimmed);
      setImageOpen(false);
    } catch (error) {
      setImageError(error instanceof Error ? error.message : "Failed to update image");
    } finally {
      setImageLoading(false);
    }
  }

  async function handleRollback() {
    if (!window.confirm("Are you sure you want to rollback this service?")) {
      return;
    }

    setRollbackLoading(true);
    setRollbackError(null);

    try {
      await api.rollbackService(serviceId);
    } catch (error) {
      setRollbackError(error instanceof Error ? error.message : "Failed to rollback");
    } finally {
      setRollbackLoading(false);
    }
  }

  async function handleRestart() {
    if (!window.confirm("Are you sure you want to restart this service? This triggers a rolling restart.")) {
      return;
    }

    setRestartLoading(true);
    setRestartError(null);

    try {
      await api.restartService(serviceId);
    } catch (error) {
      setRestartError(error instanceof Error ? error.message : "Failed to restart");
    } finally {
      setRestartLoading(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Update Image */}
      <div className="relative">
        <button
          type="button"
          onClick={openImage}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          <ImageIcon className="h-3.5 w-3.5"/>
          Update Image
        </button>
        {imageOpen && (
          <div className="absolute left-0 top-full z-50 mt-1 w-80 rounded-lg border bg-card p-3 shadow-lg">
            <p className="mb-1 text-xs font-medium text-muted-foreground">New image</p>
            <p className="mb-2 truncate font-mono text-xs text-muted-foreground" title={currentImage}>
              Current: {imageWithoutDigest}
            </p>
            <input
              type="text"
              value={imageValue}
              onChange={(e) => setImageValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  void submitImage();
                }
                if (e.key === "Escape") {
                  cancelImage();
                }
              }}
              placeholder="image:tag"
              className="mb-2 w-full rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              autoFocus
            />
            {imageError && (
              <p className="mb-2 text-xs text-red-600 dark:text-red-400">{imageError}</p>
            )}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => void submitImage()}
                disabled={imageLoading}
                className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
              >
                {imageLoading && <Spinner className="size-3"/>}
                Update
              </button>
              <button
                type="button"
                onClick={cancelImage}
                disabled={imageLoading}
                className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Rollback */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRollback()}
          disabled={!canRollback || rollbackLoading}
          title={canRollback ? "Rollback to previous spec" : "No previous spec available"}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          {rollbackLoading ? <Spinner className="size-3"/> : <RotateCcw className="h-3.5 w-3.5"/>}
          Rollback
        </button>
        {rollbackError && (
          <p className="text-xs text-red-600 dark:text-red-400">{rollbackError}</p>
        )}
      </div>

      {/* Restart */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRestart()}
          disabled={restartLoading}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          {restartLoading ? <Spinner className="size-3"/> : <RefreshCw className="h-3.5 w-3.5"/>}
          Restart
        </button>
        {restartError && (
          <p className="text-xs text-red-600 dark:text-red-400">{restartError}</p>
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
Expected: no new errors.

---

### Task 4: Create ReplicaCard.tsx

**Files:**
- Create: `frontend/src/components/service-detail/ReplicaCard.tsx`

The inline `<svg>` spinner block at the scale submit button is replaced with `<Spinner className="size-3" />`. The permanent pencil/edit SVG icon on the scale-trigger button stays as-is.

- [ ] **Step 1: Create the file**

```tsx
import {useState} from "react";
import {api} from "../../api/client";
import type {Service, Task} from "../../api/types";
import InfoCard from "../InfoCard";
import {Spinner} from "../Spinner";

function ReplicaDoughnut({running, desired}: { running: number; desired: number }) {
  const size = 50;
  const stroke = 5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const ratio = desired > 0 ? Math.min(running / desired, 1) : 0;
  const offset = circumference * (1 - ratio);
  const healthy = running >= desired;

  if (healthy) {
    return (
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
      >
        <circle
          cx={size / 2}
          cy={size / 2}
          r={size / 2}
          className="fill-green-500"
        />
        <path
          d="M15 25.5 L21.5 32 L35 19"
          fill="none"
          stroke="white"
          strokeWidth={3}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        className="text-muted"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        className="text-red-500"
      />
    </svg>
  );
}

export function ReplicaCard({service, tasks}: { service: Service; tasks: Task[] }) {
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleValue, setScaleValue] = useState("");
  const [scaleLoading, setScaleLoading] = useState(false);
  const [scaleError, setScaleError] = useState<string | null>(null);

  const replicated = service.Spec.Mode.Replicated;
  if (!replicated) {
    return <InfoCard label="Mode" value="global"/>;
  }

  const desired = replicated.Replicas ?? 0;
  const running = tasks.filter((t) => t.Status.State === "running").length;
  const healthy = running >= desired;

  function openScale() {
    setScaleValue(String(desired));
    setScaleError(null);
    setScaleOpen(true);
  }

  function cancelScale() {
    setScaleOpen(false);
    setScaleError(null);
  }

  async function submitScale() {
    const n = parseInt(scaleValue, 10);
    if (isNaN(n) || n < 0) {
      setScaleError("Enter a valid replica count");
      return;
    }
    setScaleLoading(true);
    setScaleError(null);
    try {
      await api.scaleService(service.ID, n);
      setScaleOpen(false);
    } catch (err) {
      setScaleError(err instanceof Error ? err.message : "Failed to scale");
    } finally {
      setScaleLoading(false);
    }
  }

  const value = (
    <>
      <span className="tabular-nums">
        <span className="text-2xl font-bold">{running}</span>
        <span className="text-lg font-normal text-muted-foreground">/{desired}</span>
      </span>

      {!healthy && (
        <div className="mt-1 text-xs text-red-600 dark:text-red-400">
          {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
        </div>
      )}
    </>
  );

  const scaleControl = (
    <div className="relative flex items-center gap-2">
      {desired > 0 && <ReplicaDoughnut running={running} desired={desired}/>}
      <button
        type="button"
        onClick={openScale}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        title="Scale service"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
        </svg>
      </button>

      {scaleOpen && (
        <div className="absolute right-0 top-full z-50 mt-1 w-52 rounded-lg border bg-card p-3 shadow-lg">
          <p className="mb-2 text-xs font-medium text-muted-foreground">Scale replicas</p>
          <input
            type="number"
            min={0}
            value={scaleValue}
            onChange={(e) => setScaleValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                void submitScale();
              }
              if (e.key === "Escape") {
                cancelScale();
              }
            }}
            className="mb-2 w-full rounded border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
            autoFocus
          />
          {scaleError && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{scaleError}</p>
          )}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void submitScale()}
              disabled={scaleLoading}
              className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {scaleLoading && <Spinner className="size-3"/>}
              Scale
            </button>
            <button
              type="button"
              onClick={cancelScale}
              disabled={scaleLoading}
              className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );

  return (
    <InfoCard
      label="Replicas"
      value={value}
      right={scaleControl}
    />
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: no new errors.

---

### Task 5: Create EnvEditor.tsx

**Files:**
- Create: `frontend/src/components/service-detail/EnvEditor.tsx`

- [ ] **Step 1: Create the file**

```tsx
import {Pencil, Plus, Trash2, X} from "lucide-react";
import {useState} from "react";
import {api} from "../../api/client";
import CollapsibleSection from "../CollapsibleSection";
import SimpleTable from "../SimpleTable";
import {Spinner} from "../Spinner";

export function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({...envVars});
    setNewKey("");
    setNewVal("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const key = newKey.trim();
    if (!key) {
      return;
    }

    setDraft((prev) => ({...prev, [key]: newVal}));
    setNewKey("");
    setNewVal("");
  }

  function removeRow(key: string) {
    if (!window.confirm(`Remove env var "${key}"?`)) {
      return;
    }

    setDraft((prev) => {
      const next = {...prev};
      delete next[key];
      return next;
    });
  }

  async function save() {
    // Build JSON Patch ops
    const ops: Array<{ op: string; path: string; value?: string }> = [];
    const original = envVars;

    // Auto-add pending new row if the key field is non-empty
    const effectiveDraft =
      newKey.trim() ? {...draft, [newKey.trim()]: newVal} : draft;

    // Removed keys
    for (const key of Object.keys(original)) {
      if (!(key in effectiveDraft)) {
        ops.push({op: "remove", path: `/${key}`});
      }
    }

    // Added / replaced keys
    for (const [key, value] of Object.entries(effectiveDraft)) {
      if (!(key in original)) {
        ops.push({op: "add", path: `/${key}`, value: value});
      } else if (original[key] !== value) {
        ops.push({op: "replace", path: `/${key}`, value: value});
      }
    }

    if (ops.length === 0) {
      setEditing(false);
      return;
    }

    setSaving(true);
    setSaveError(null);

    try {
      const updated = await api.patchServiceEnv(serviceId, ops);
      onSaved(updated);
      setEditing(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const entries = Object
    .entries(envVars)
    .sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object
    .entries(draft)
    .sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="size-3"/>
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Environment Variables"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No environment variables.</p>
        ) : (
          <SimpleTable
            columns={["Variable", "Value"]}
            items={entries}
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
                <th className="p-3 text-left text-sm font-medium">Variable</th>
                <th className="p-3 text-left text-sm font-medium">Value</th>
                <th className="p-3"/>
              </tr>
              </thead>
              <tbody>
              {draftEntries.map(([k, v]) => (
                <tr key={k} className="border-b last:border-b-0">
                  <td className="p-3 font-mono text-xs">{k}</td>
                  <td className="p-2">
                    <input
                      type="text"
                      value={v}
                      onChange={(e) => setDraft((prev) => ({...prev, [k]: e.target.value}))}
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </td>
                  <td className="p-2">
                    <button
                      type="button"
                      onClick={() => removeRow(k)}
                      className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-red-600"
                      title="Remove"
                    >
                      <Trash2 className="h-3.5 w-3.5"/>
                    </button>
                  </td>
                </tr>
              ))}
              <tr>
                <td className="p-2">
                  <input
                    type="text"
                    value={newKey}
                    onChange={(e) => setNewKey(e.target.value)}
                    placeholder="NEW_VAR"
                    className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </td>
                <td className="p-2">
                  <input
                    type="text"
                    value={newVal}
                    onChange={(e) => setNewVal(e.target.value)}
                    placeholder="value"
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        addRow();
                      }
                    }}
                    className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </td>
                <td className="p-2">
                  <button
                    type="button"
                    onClick={addRow}
                    disabled={!newKey.trim()}
                    className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
                    title="Add"
                  >
                    <Plus className="h-3.5 w-3.5"/>
                  </button>
                </td>
              </tr>
              </tbody>
            </table>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner className="size-3"/>}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="size-3"/>
              Cancel
            </button>
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
Expected: no new errors.

---

### Task 6: Create ResourcesEditor.tsx

**Files:**
- Create: `frontend/src/components/service-detail/ResourcesEditor.tsx`

- [ ] **Step 1: Create the file**

```tsx
import {Pencil, X} from "lucide-react";
import {useState} from "react";
import {api} from "../../api/client";
import {formatBytes} from "../../lib/formatBytes";
import CollapsibleSection from "../CollapsibleSection";
import {Spinner} from "../Spinner";

interface ServiceResourceShape {
  limits?: { nanoCPUs?: number; memoryBytes?: number; pids?: number };
  reservations?: { nanoCPUs?: number; memoryBytes?: number };
}

export function ResourcesEditor({
  serviceId,
  resources,
  onSaved,
}: {
  serviceId: string;
  resources: Record<string, unknown>;
  onSaved: (updated: Record<string, unknown>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const typed = resources as ServiceResourceShape;

  const [limitCpu, setLimitCpu] = useState("");
  const [limitMem, setLimitMem] = useState("");
  const [resCpu, setResCpu] = useState("");
  const [resMem, setResMem] = useState("");

  function openEdit() {
    setLimitCpu(typed.limits?.nanoCPUs != null ? String(typed.limits.nanoCPUs / 1e9) : "");
    setLimitMem(typed.limits?.memoryBytes != null ? String(typed.limits.memoryBytes) : "");
    setResCpu(typed.reservations?.nanoCPUs != null ? String(typed.reservations.nanoCPUs / 1e9) : "");
    setResMem(typed.reservations?.memoryBytes != null ? String(typed.reservations.memoryBytes) : "");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (limitCpu || limitMem) {
      patch.limits = {};
      if (limitCpu) {
        patch.limits.nanoCPUs = Math.round(parseFloat(limitCpu) * 1e9);
      }
      if (limitMem) {
        patch.limits.memoryBytes = parseInt(limitMem, 10);
      }
    }
    if (resCpu || resMem) {
      patch.reservations = {};
      if (resCpu) {
        patch.reservations.nanoCPUs = Math.round(parseFloat(resCpu) * 1e9);
      }
      if (resMem) {
        patch.reservations.memoryBytes = parseInt(resMem, 10);
      }
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchServiceResources(serviceId, patch);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const hasResources =
    typed.limits?.nanoCPUs ||
    typed.limits?.memoryBytes ||
    typed.reservations?.nanoCPUs ||
    typed.reservations?.memoryBytes;

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="size-3"/>
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Resource Limits"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        !hasResources ? (
          <p className="text-sm text-muted-foreground">No resource limits configured.</p>
        ) : (
          <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            {typed.limits?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Limit</div>
                <div className="font-mono">{(typed.limits.nanoCPUs / 1e9).toFixed(2)} cores
                </div>
              </div>
            )}
            {typed.limits?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Limit</div>
                <div className="font-mono">{formatBytes(typed.limits.memoryBytes)}</div>
              </div>
            )}
            {typed.reservations?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Reserved</div>
                <div className="font-mono">{(typed.reservations.nanoCPUs / 1e9).toFixed(2)} cores
                </div>
              </div>
            )}
            {typed.reservations?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Reserved</div>
                <div className="font-mono">{formatBytes(typed.reservations.memoryBytes)}</div>
              </div>
            )}
          </div>
        )
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Limits</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={limitCpu}
                  onChange={(e) => setLimitCpu(e.target.value)}
                  placeholder="e.g. 0.5"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={limitMem}
                  onChange={(e) => setLimitMem(e.target.value)}
                  placeholder="e.g. 536870912"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Reservations</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={resCpu}
                  onChange={(e) => setResCpu(e.target.value)}
                  placeholder="e.g. 0.25"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={resMem}
                  onChange={(e) => setResMem(e.target.value)}
                  placeholder="e.g. 268435456"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
            </div>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner className="size-3"/>}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="size-3"/>
              Cancel
            </button>
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
Expected: no new errors.

---

### Task 7: Create index.ts

**Files:**
- Create: `frontend/src/components/service-detail/index.ts`

- [ ] **Step 1: Create the file**

```ts
export {DeploymentChanges} from "./DeploymentChanges";
export {EnvEditor} from "./EnvEditor";
export {PlacementPanel} from "./PlacementPanel";
export type {PlacementShape} from "./PlacementPanel";
export {ReplicaCard} from "./ReplicaCard";
export {ResourcesEditor} from "./ResourcesEditor";
export {ServiceActions} from "./ServiceActions";
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: no errors.

- [ ] **Step 3: Commit the new directory**

```bash
git add frontend/src/components/service-detail/
git commit -m "feat: add service-detail component directory"
```

---

### Task 8: Update ServiceDetail.tsx

This is the main surgery step. Replace the extracted code with a barrel import, remove the local `Spinner`, and trim the lucide imports.

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Replace the lucide import line**

Old:
```tsx
import {ArrowRight, Globe, ImageIcon, Pencil, Plus, RefreshCw, RotateCcw, Shuffle, Trash2, X} from "lucide-react";
```

New (`ArrowRight` stays — it is used in the ports section of the page component JSX):
```tsx
import {ArrowRight, Globe, Shuffle} from "lucide-react";
```

- [ ] **Step 2: Add the barrel import**

After the existing component imports (e.g. after the `TasksTable` import line), add:
```tsx
import {DeploymentChanges, EnvEditor, PlacementPanel, ReplicaCard, ResourcesEditor, ServiceActions} from "../components/service-detail";
```

- [ ] **Step 3: Remove the local Spinner function**

Delete the entire `Spinner` function (lines ~748–771 in the original file):
```tsx
function Spinner() {
  return (
    <svg
      className="size-3 animate-spin"
      ...
    >
      ...
    </svg>
  );
}
```

- [ ] **Step 4: Remove ServiceActions (lines ~773–950)**

Delete the entire `ServiceActions` function from the file.

- [ ] **Step 5: Remove ReplicaDoughnut + ReplicaCard (lines ~952–1181)**

Delete both functions.

- [ ] **Step 6: Remove EnvEditor (lines ~1434–1677)**

Delete the entire `EnvEditor` function.

- [ ] **Step 7: Remove ServiceResourceShape + ResourcesEditor (lines ~1679–1892)**

Delete the interface and function.

- [ ] **Step 8: Remove DeploymentChanges (lines ~1382–1432)**

Delete the function.

- [ ] **Step 9: Remove PlacementShape type + PlacementPanel + humanizeConstraint (lines ~1194–1297)**

Delete `humanizeConstraint`, `type PlacementShape`, and `PlacementPanel`.

- [ ] **Step 10: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: zero errors. If there are missing import errors, verify the barrel export in `index.ts` includes all required names.

- [ ] **Step 11: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "refactor: extract components from ServiceDetail into service-detail/"
```

---

### Task 9: Fix Spinner in NodeDetail.tsx

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`

- [ ] **Step 1: Remove the local Spinner function**

Delete lines 25–37:
```tsx
function Spinner() {
  return (
    <svg
      className="h-3 w-3 animate-spin"
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox="0 0 24 24"
    >
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
    </svg>
  );
}
```

- [ ] **Step 2: Add the shared Spinner import**

Add to the imports block (alongside existing component imports):
```tsx
import {Spinner} from "../components/Spinner";
```

- [ ] **Step 3: Fix the Spinner usage**

The `<Spinner />` call at line ~232 (in the LabelsEditor save button) needs `className="size-3"`:
```tsx
{saving && <Spinner className="size-3" />}
```

- [ ] **Step 4: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: zero errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx
git commit -m "refactor: use shared Spinner in NodeDetail"
```

---

### Task 10: Fix Spinner in TaskDetail.tsx

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Remove the local Spinner function**

Delete lines 21–37:
```tsx
function Spinner() {
  return (
    <svg
      className="h-3 w-3 animate-spin"
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox="0 0 24 24"
    >
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"/>
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>
  );
}
```

- [ ] **Step 2: Add the shared Spinner import**

```tsx
import {Spinner} from "../components/Spinner";
```

- [ ] **Step 3: Fix the Spinner usage**

The `<Spinner/>` call in the Force Remove button (line ~129) needs `className="size-3"`:
```tsx
{removeLoading ? <Spinner className="size-3"/> : <Trash2 className="size-3.5"/>}
```

- [ ] **Step 4: Type-check**

```bash
cd frontend && npx tsc -b --noEmit
```
Expected: zero errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/TaskDetail.tsx
git commit -m "refactor: use shared Spinner in TaskDetail"
```

---

### Task 11: Final verification

- [ ] **Step 1: Run all frontend tests**

```bash
cd frontend && npx vitest run
```
Expected: all tests pass. No tests cover the extracted components directly (they're pure moves), but existing integration tests for the pages should still pass.

- [ ] **Step 2: Verify ServiceDetail.tsx line count is reasonable**

```bash
wc -l frontend/src/pages/ServiceDetail.tsx
```
Expected: under 600 lines (was ~1950).

- [ ] **Step 3: Verify the new directory structure**

```bash
ls frontend/src/components/service-detail/
```
Expected:
```
DeploymentChanges.tsx
EnvEditor.tsx
PlacementPanel.tsx
ReplicaCard.tsx
ResourcesEditor.tsx
ResourcesEditor.tsx
ServiceActions.tsx
index.ts
```
