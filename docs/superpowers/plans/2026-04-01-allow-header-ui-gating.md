# Allow Header UI Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the global operations-level context with per-resource permission gating driven by the `Allow` response header, so write UI is hidden when the user lacks permission.

**Architecture:** `fetchJSON` returns `{ data, allowedMethods }` parsed from the `Allow` header (empty set = default-deny). Detail pages pass `allowedMethods` to child components. `useOperationsLevel` and its provider are deleted entirely.

**Tech Stack:** React, TypeScript, Go (backend `setAllowList` extension)

**Spec:** `docs/superpowers/specs/2026-04-01-allow-header-ui-gating-design.md`

---

### Task 1: Change `fetchJSON` to return `FetchResult<T>`

**Files:**
- Modify: `frontend/src/api/client.ts:111-126` (`fetchJSON` function)

- [ ] **Step 1: Add `FetchResult` type and `parseAllowHeader` helper**

In `frontend/src/api/client.ts`, add above `fetchJSON`:

```ts
export interface FetchResult<T> {
  data: T;
  allowedMethods: Set<string>;
}

function parseAllowHeader(response: Response): Set<string> {
  const header = response.headers.get("Allow");
  if (!header) {
    return new Set();
  }

  return new Set(
    header.split(",").map((method) => method.trim().toUpperCase()),
  );
}
```

- [ ] **Step 2: Update `fetchJSON` return type**

Change `fetchJSON` to return `FetchResult<T>`:

```ts
async function fetchJSON<T>(path: string, signal?: AbortSignal): Promise<FetchResult<T>> {
  const res = await fetch(apiPath(path), {
    headers,
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!res.ok) {
    if (res.status === 401 && res.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLogin();
    }

    await throwResponseError(res);
  }

  const data: T = await res.json();
  return { data, allowedMethods: parseAllowHeader(res) };
}
```

- [ ] **Step 3: Update `get` export**

```ts
export function get<T>(path: string, signal?: AbortSignal): Promise<FetchResult<T>> {
  return fetchJSON(path, signal);
}
```

- [ ] **Step 4: Update `fetchRange` to also return `allowedMethods`**

Change `fetchRange` to return `FetchResult<CollectionResponse<T>>`:

```ts
async function fetchRange<T>(
  path: string,
  params?: ListParams,
  signal?: AbortSignal,
): Promise<FetchResult<CollectionResponse<T>>> {
  const offset = params?.offset ?? 0;
  const end = offset + pageSize - 1;
  const url = `${path}${buildListQueryString(params)}`;

  const res = await fetch(apiPath(url), {
    headers: {
      Accept: "application/json",
      Range: `items ${offset}-${end}`,
    },
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!res.ok) {
    if (res.status === 401 && res.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLogin();
    }

    await throwResponseError(res);
  }

  const data: CollectionResponse<T> = await res.json();
  return { data, allowedMethods: parseAllowHeader(res) };
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(frontend): return FetchResult with allowedMethods from fetchJSON and fetchRange"
```

---

### Task 2: Update all `api` object methods to propagate `FetchResult`

**Files:**
- Modify: `frontend/src/api/client.ts:288+` (the `api` object)

Every method that calls `fetchJSON` or `fetchRange` now gets a `FetchResult` back. Methods that previously did `.then((r) => r.field)` to unwrap the JSON-LD envelope must propagate `allowedMethods` through the unwrap.

- [ ] **Step 1: Update detail methods that unwrap envelopes**

Change the methods that `.then()` unwrap to preserve `allowedMethods`:

```ts
  node: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ node: Node }>(`/nodes/${id}`, signal).then(({ data, allowedMethods }) => ({
      data: data.node,
      allowedMethods,
    })),
  task: (id: string) =>
    fetchJSON<{ task: Task }>(`/tasks/${id}`).then(({ data, allowedMethods }) => ({
      data: data.task,
      allowedMethods,
    })),
  stack: (name: string) =>
    fetchJSON<{ stack: StackDetail }>(`/stacks/${name}`).then(({ data, allowedMethods }) => ({
      data: data.stack,
      allowedMethods,
    })),
  plugin: (name: string, signal?: AbortSignal) =>
    fetchJSON<{ plugin: Plugin }>(`/plugins/${encodeURIComponent(name)}`, signal).then(
      ({ data, allowedMethods }) => ({
        data: data.plugin,
        allowedMethods,
      }),
    ),
```

- [ ] **Step 2: Update detail methods that return directly**

These already return `FetchResult` from `fetchJSON` — no change needed for their return shape, but their consumers will need updating. Confirm these are correct:

```ts
  service: (id: string, signal?: AbortSignal) =>
    fetchJSON<ServiceDetail>(`/services/${id}`, signal),
  config: (id: string, signal?: AbortSignal) => fetchJSON<ConfigDetail>(`/configs/${id}`, signal),
  secret: (id: string, signal?: AbortSignal) => fetchJSON<SecretDetail>(`/secrets/${id}`, signal),
  network: (id: string, signal?: AbortSignal) =>
    fetchJSON<NetworkDetail>(`/networks/${id}`, signal),
  volume: (name: string, signal?: AbortSignal) =>
    fetchJSON<VolumeDetail>(`/volumes/${name}`, signal),
  swarm: () => fetchJSON<SwarmInfo>("/swarm"),
```

These now return `FetchResult<T>` — correct, no further change.

- [ ] **Step 3: Update list methods that use `fetchRange`**

`fetchRange` now returns `FetchResult<CollectionResponse<T>>`. The list methods (`nodes`, `services`, `tasks`, `stacks`, `configs`, `secrets`, `networks`, `volumes`) just pass through `fetchRange` so they automatically return the new type. No change needed.

- [ ] **Step 4: Update methods that unwrap and discard `allowedMethods`**

These methods call `fetchJSON` but don't need `allowedMethods` — unwrap to just the data:

```ts
  stacksSummary: () =>
    fetchJSON<CollectionResponse<StackSummary>>("/stacks/summary").then(({ data }) => data.items),
  serviceTasks: (id: string, signal?: AbortSignal) =>
    fetchJSON<CollectionResponse<Task>>(`/services/${id}/tasks`, signal).then(({ data }) => data.items),
  serviceLogs: (id: string, opts?: LogOpts) =>
    fetchJSON<LogResponse>(`/services/${id}/logs?${buildLogParams(opts)}`, opts?.signal).then(({ data }) => data),
  taskLogs: (id: string, opts?: LogOpts) =>
    fetchJSON<LogResponse>(`/tasks/${id}/logs?${buildLogParams(opts)}`, opts?.signal).then(({ data }) => data),
```

Do the same for all other methods that call `fetchJSON` but don't need `allowedMethods`: `whoami`, `cluster`, `clusterMetrics`, `monitoringStatus`, `recommendations`, `unlockKey`, `history`, `search`, `diskUsage`, `clusterCapacity`, `pluginPrivileges`. Each gets `.then(({ data }) => data)` appended.

- [ ] **Step 5: Add `headAllowedMethods` helper for SearchPalette**

Add a new function for firing HEAD requests:

```ts
export async function headAllowedMethods(path: string): Promise<Set<string>> {
  const res = await fetch(apiPath(path), {
    method: "HEAD",
    headers,
    signal: AbortSignal.timeout(defaultTimeoutMilliseconds),
  });

  return parseAllowHeader(res);
}
```

- [ ] **Step 6: Verify the build compiles**

Run: `cd frontend && npx tsc -b --noEmit`

This will show all type errors from consumers that need updating. That's expected — we fix them in subsequent tasks.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(frontend): propagate allowedMethods through all api methods"
```

---

### Task 3: Update `useDetailResource` to expose `allowedMethods`

**Files:**
- Modify: `frontend/src/hooks/useDetailResource.ts`

- [ ] **Step 1: Update the hook**

```ts
import type { FetchResult } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { api } from "../api/client";
import { useCallback, useEffect, useRef, useState } from "react";

const emptyMethods: Set<string> = new Set();

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  ssePath: string,
) {
  const [data, setData] = useState<T | null>(null);
  const [allowedMethods, setAllowedMethods] = useState<Set<string>>(emptyMethods);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState<Error | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const fetchData = useCallback(() => {
    if (!key) {
      return;
    }

    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    setError(null);

    fetchFn(key, controller.signal)
      .then(({ data: d, allowedMethods: methods }) => {
        if (!controller.signal.aborted) {
          setData(d);
          setAllowedMethods(methods);
        }
      })
      .catch((thrown) => {
        if (!controller.signal.aborted) {
          setError(thrown instanceof Error ? thrown : new Error(String(thrown)));
        }
      });

    api
      .history({ resourceId: key, limit: 10 }, controller.signal)
      .then((entry) => {
        if (!controller.signal.aborted) {
          setHistory(entry);
        }
      })
      .catch(console.warn);
  }, [key, fetchFn]);

  useEffect(() => {
    fetchData();

    return () => {
      abortRef.current?.abort();
    };
  }, [fetchData]);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fetchDataRef = useRef(fetchData);
  fetchDataRef.current = fetchData;

  useResourceStream(
    ssePath,
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        fetchDataRef.current();
      }, 500);
    }, []),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

  return { data, allowedMethods, history, error, retry: fetchData };
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/hooks/useDetailResource.ts
git commit -m "feat(frontend): expose allowedMethods from useDetailResource"
```

---

### Task 4: Update `useSwarmResource` to expose `allowedMethods`

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts`

- [ ] **Step 1: Update the hook to capture `allowedMethods` from the first page fetch**

Add state and capture from the `fetchFn` result. The `fetchFn` now returns `FetchResult<CollectionResponse<T>>`, so destructure it:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());
```

In the `loadPage` callback, where the current code does:

```ts
fetchFn(pageNumber * pageSize, controller.signal)
  .then((response) => {
```

Change to:

```ts
fetchFn(pageNumber * pageSize, controller.signal)
  .then(({ data: response, allowedMethods: methods }) => {
```

And after `setServerTotal(response.total);`, add:

```ts
if (isFirstPage) {
  setAllowedMethods(methods);
}
```

Update the `fetchFn` parameter type from `(offset: number, signal: AbortSignal) => Promise<CollectionResponse<T>>` to:

```ts
import type { FetchResult } from "../api/client";

fetchFn: (offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>
```

Add `allowedMethods` to the return value of the hook.

- [ ] **Step 2: Update all `useSwarmResource` callers to pass the updated `fetchFn` type**

The list pages pass functions like `(offset, signal) => api.nodes({ offset }, signal)`. Since `api.nodes()` now returns `FetchResult<CollectionResponse<Node>>`, these already match the new type — no caller changes needed.

But the callers that destructure the return value need to add `allowedMethods`. Find all usages:

```
grep -rn "useSwarmResource" frontend/src/pages/
```

Each list page does `const { items, total, ... } = useSwarmResource(...)`. Add `allowedMethods` to the destructuring where needed (config list, secret list, plugin list for create buttons). Other list pages can ignore it.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSwarmResource.ts
git commit -m "feat(frontend): expose allowedMethods from useSwarmResource"
```

---

### Task 5: Update `EditablePanel` to accept `canEdit` prop

**Files:**
- Modify: `frontend/src/components/service-detail/EditablePanel.tsx`

- [ ] **Step 1: Replace `useOperationsLevel` with a `canEdit` prop**

Remove the `useOperationsLevel` import and internal level check. Add `canEdit` to props:

```ts
interface EditablePanelProps {
  /** Read-only display content */
  display: ReactNode;
  /** Form content shown in edit mode */
  edit: ReactNode;
  /** Called when the user clicks Edit — use this to reset form state from current props */
  onOpen: () => void;
  /** Called when the user clicks Save — throw to show an error */
  onSave: () => Promise<void>;
  /** Optional title shown above content in both modes */
  title?: string;
  /** Extra buttons rendered on the left side of the edit footer (e.g. "Add option") */
  actions?: ReactNode;
  /** When true, shows the empty state instead of display content */
  empty?: boolean;
  /** Description shown in the empty state when canEdit is true */
  emptyDescription?: string;
  /** Whether to wrap in a bordered div (default true) */
  bordered?: boolean;
  /** Whether the user has write permission for this resource (default false) */
  canEdit?: boolean;
  /** Extra buttons rendered next to Edit in the title row (only shown when not editing) */
  headerActions?: ReactNode;
}
```

Remove the `requiredLevel` prop. Remove these lines:

```ts
const { level, loading: levelLoading } = useOperationsLevel();
const canEdit = !levelLoading && level >= (requiredLevel ?? opsLevel.configuration);
```

Use the `canEdit` prop directly (defaults to `false`):

```ts
export function EditablePanel({
  // ... other props
  canEdit = false,
  // ...
}: EditablePanelProps) {
```

The rest of the component stays the same — it already uses the local `canEdit` variable.

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/service-detail/EditablePanel.tsx
git commit -m "refactor(frontend): EditablePanel accepts canEdit prop instead of reading ops level"
```

---

### Task 6: Update detail pages using `useDetailResource` (Config, Secret, Network, Volume)

**Files:**
- Modify: `frontend/src/pages/ConfigDetail.tsx`
- Modify: `frontend/src/pages/SecretDetail.tsx`
- Modify: `frontend/src/pages/NetworkDetail.tsx`
- Modify: `frontend/src/pages/VolumeDetail.tsx`

These four pages use `useDetailResource` and can now access `allowedMethods` directly.

- [ ] **Step 1: Update ConfigDetail**

Replace:
```ts
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
// ...
const { level, loading: levelLoading } = useOperationsLevel();
```

With:
```ts
const { data, allowedMethods, history, error, retry } = useDetailResource(id, api.config, `/configs/${id}`);
```

Replace `level < opsLevel.configuration` with `!allowedMethods.has("PATCH")` where it's passed as `editDisabled`. Pass `canEdit={allowedMethods.has("PATCH")}` to `EditablePanel`. Pass `canDelete={allowedMethods.has("DELETE")}` to `RemoveResourceAction`.

- [ ] **Step 2: Update SecretDetail**

Same pattern as ConfigDetail. Replace ops level check with `allowedMethods.has("PATCH")` and `allowedMethods.has("DELETE")`.

- [ ] **Step 3: Update NetworkDetail and VolumeDetail**

These pages only have the remove action. Pass `canDelete={allowedMethods.has("DELETE")}` to `RemoveResourceAction`. Remove `useOperationsLevel` import if present.

- [ ] **Step 4: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ConfigDetail.tsx frontend/src/pages/SecretDetail.tsx frontend/src/pages/NetworkDetail.tsx frontend/src/pages/VolumeDetail.tsx
git commit -m "refactor(frontend): use allowedMethods for write gating on config/secret/network/volume detail"
```

---

### Task 7: Update ServiceDetail and its child components

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/components/service-detail/ServiceActions.tsx`
- Modify: `frontend/src/components/service-detail/ReplicaCard.tsx`
- Modify: `frontend/src/components/service-detail/EnvEditor.tsx`
- Modify: `frontend/src/components/service-detail/ResourcesEditor.tsx`
- Modify: `frontend/src/components/service-detail/PortsEditor.tsx`
- Modify: `frontend/src/components/service-detail/HealthcheckEditor.tsx`
- Modify: `frontend/src/components/service-detail/SecretsEditor.tsx`
- Modify: `frontend/src/components/service-detail/NetworksEditor.tsx`
- Modify: `frontend/src/components/service-detail/ConfigsEditor.tsx`
- Modify: `frontend/src/components/service-detail/MountsEditor.tsx`
- Modify: `frontend/src/components/service-detail/EndpointModeEditor.tsx`
- Modify: `frontend/src/components/data/ContainerImage.tsx`

ServiceDetail does its own fetching (not `useDetailResource`). It calls `api.service(id, signal)` which now returns `FetchResult<ServiceDetail>`.

- [ ] **Step 1: Capture `allowedMethods` in ServiceDetail**

Add state:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());
```

In `fetchService`, change:

```ts
api
  .service(id, signal)
  .then((response) => {
    setService(response.service);
```

To:

```ts
api
  .service(id, signal)
  .then(({ data: response, allowedMethods: methods }) => {
    setService(response.service);
    setAllowedMethods(methods);
```

Remove `useOperationsLevel` import and usage. Replace `canEditConfig` with:

```ts
const canPatch = allowedMethods.has("PATCH");
const canDelete = allowedMethods.has("DELETE");
```

Pass `allowedMethods` to `ServiceActions`. Pass `canEdit={canPatch}` to `EditablePanel` instances and editors. Pass `editDisabled={!canPatch}` where that prop is used.

- [ ] **Step 2: Update ServiceActions**

Change to accept `allowedMethods: Set<string>` prop instead of reading ops level:

```ts
export function ServiceActions({ service, serviceId, allowedMethods }: {
  service: Service;
  serviceId: string;
  allowedMethods: Set<string>;
}) {
  const canWrite = allowedMethods.has("POST");
  const canDelete = allowedMethods.has("DELETE");
```

Gate Rollback and Restart on `canWrite` (POST). Gate Remove on `canDelete`. Return `null` if neither.

- [ ] **Step 3: Update ReplicaCard**

Change to accept `allowedMethods: Set<string>` prop:

```ts
const canScale = allowedMethods.has("PUT");
const canChangeMode = allowedMethods.has("DELETE");
```

Scale uses PUT. Mode change is impactful — gate on DELETE (same tier).

- [ ] **Step 4: Update ContainerImage**

Change to accept `canEdit: boolean` prop instead of reading ops level:

```ts
export function ContainerImage({ image, serviceId, canEdit = false }: {
  image: string;
  serviceId?: string;
  canEdit?: boolean;
}) {
```

Parent passes `canEdit={allowedMethods.has("PUT")}`.

- [ ] **Step 5: Update editor components**

For `EnvEditor`, `ResourcesEditor`, `PortsEditor`, `HealthcheckEditor`, `SecretsEditor`, `NetworksEditor`, `ConfigsEditor`, `MountsEditor`, `EndpointModeEditor`: each currently reads `useOperationsLevel()` internally. Change each to accept a `canEdit: boolean` prop. Remove the `useOperationsLevel` import.

The pattern for each is the same — replace:

```ts
const { level, loading: levelLoading } = useOperationsLevel();
const canEdit = !levelLoading && level >= opsLevel.configuration;
```

With a prop:

```ts
canEdit = false  // in the destructured props with default
```

- [ ] **Step 6: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx frontend/src/components/service-detail/ frontend/src/components/data/ContainerImage.tsx
git commit -m "refactor(frontend): use allowedMethods for write gating on service detail and editors"
```

---

### Task 8: Update NodeDetail and its child components

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/components/node-detail/NodeActions.tsx`
- Modify: `frontend/src/components/node-detail/AvailabilityEditor.tsx`
- Modify: `frontend/src/components/node-detail/RoleEditor.tsx`

NodeDetail does its own fetching via `api.node(id, signal)`.

- [ ] **Step 1: Capture `allowedMethods` in NodeDetail**

Add state:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());
```

In `fetchNode`, change:

```ts
api
  .node(id, signal)
  .then((node) => {
    setNode(node);
```

To:

```ts
api
  .node(id, signal)
  .then(({ data: node, allowedMethods: methods }) => {
    setNode(node);
    setAllowedMethods(methods);
```

Remove `useOperationsLevel`. Pass `allowedMethods` to child components.

- [ ] **Step 2: Update NodeActions**

Accept `allowedMethods: Set<string>` prop. Gate remove button on `allowedMethods.has("DELETE")`.

- [ ] **Step 3: Update AvailabilityEditor and RoleEditor**

Accept `canEdit: boolean` prop. Parent passes `canEdit={allowedMethods.has("PUT")}`.

- [ ] **Step 4: Pass `editDisabled` for node labels**

Replace `operationsLevel < opsLevel.impactful` with `!allowedMethods.has("PATCH")`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx frontend/src/components/node-detail/
git commit -m "refactor(frontend): use allowedMethods for write gating on node detail"
```

---

### Task 9: Update TaskDetail, StackDetail, SwarmPage, PluginDetail

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`
- Modify: `frontend/src/pages/StackDetail.tsx`
- Modify: `frontend/src/components/stack-detail/StackActions.tsx`
- Modify: `frontend/src/pages/SwarmPage.tsx`
- Modify: `frontend/src/components/swarm-detail/SwarmActions.tsx`
- Modify: `frontend/src/pages/PluginDetail.tsx`

- [ ] **Step 1: Update TaskDetail**

TaskDetail calls `api.task(id)`. Capture `allowedMethods`:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());

// In fetchTask:
api
  .task(id)
  .then(({ data: task, allowedMethods: methods }) => {
    setTask(task);
    setAllowedMethods(methods);
  })
```

Replace `canRemove` with `allowedMethods.has("DELETE")`.

- [ ] **Step 2: Update StackDetail and StackActions**

StackDetail calls `api.stack(name)` via a local import. Capture `allowedMethods`:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());

// In fetchData:
.stack(name)
// becomes:
fetchJSON<{ stack: StackDetail }>(`/stacks/${name}`).then(({ data, allowedMethods: methods }) => {
  setStack(data.stack);
  setAllowedMethods(methods);
})
```

Actually, StackDetail imports from `../api/client` indirectly. It uses the `api.stack()` method which already returns `FetchResult`. Update the `.then()`:

```ts
api
  .stack(name)
  .then(({ data: stack, allowedMethods: methods }) => {
    setStack(stack);
    setAllowedMethods(methods);
  })
```

Pass `allowedMethods` to `StackActions`. `StackActions` gates remove on `allowedMethods.has("DELETE")`.

- [ ] **Step 3: Update SwarmPage and SwarmActions**

SwarmPage calls `api.swarm()`. Capture `allowedMethods`:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());

// In fetchSwarmInfo:
api
  .swarm()
  .then(({ data, allowedMethods: methods }) => {
    setData(data);
    setAllowedMethods(methods);
  })
```

Remove `useOperationsLevel`. Pass `canEdit` booleans to `EditablePanel` instances:
- Raft, Orchestration, Dispatcher sections: `canEdit={allowedMethods.has("PATCH")}`
- CA Config, Encryption sections: `canEdit={allowedMethods.has("POST")}` (these use POST for impactful ops)

Pass `allowedMethods` to `SwarmActions`. SwarmActions gates token rotation on `allowedMethods.has("POST")`.

- [ ] **Step 4: Update PluginDetail**

PluginDetail calls `api.plugin(name)`. Capture `allowedMethods`:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());

// In fetchPlugin:
api
  .plugin(name)
  .then(({ data: plugin, allowedMethods: methods }) => {
    setPlugin(plugin);
    setAllowedMethods(methods);
  })
```

Replace ops level checks:
- Enable/disable: `allowedMethods.has("POST")`
- Remove: `allowedMethods.has("DELETE")`
- Settings: `allowedMethods.has("PATCH")`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/TaskDetail.tsx frontend/src/pages/StackDetail.tsx frontend/src/components/stack-detail/StackActions.tsx frontend/src/pages/SwarmPage.tsx frontend/src/components/swarm-detail/SwarmActions.tsx frontend/src/pages/PluginDetail.tsx
git commit -m "refactor(frontend): use allowedMethods for write gating on task/stack/swarm/plugin detail"
```

---

### Task 10: Update `RemoveResourceAction` and `CreateResourceDialog`

**Files:**
- Modify: `frontend/src/components/RemoveResourceAction.tsx`
- Modify: `frontend/src/components/CreateResourceDialog.tsx`

- [ ] **Step 1: Update RemoveResourceAction**

Replace `useOperationsLevel` with a `canDelete: boolean` prop (default `false`):

```ts
interface RemoveResourceActionProps {
  // ... existing props
  canDelete?: boolean;
}
```

Remove:
```ts
const { level, loading: levelLoading } = useOperationsLevel();
const canImpact = !levelLoading && level >= opsLevel.impactful;
```

Use `canDelete` prop directly. Return `null` if `!canDelete`.

- [ ] **Step 2: Update CreateResourceDialog**

Replace `useOperationsLevel` with a `canCreate: boolean` prop (default `false`):

```ts
interface CreateResourceDialogProps {
  // ... existing props
  canCreate?: boolean;
}
```

Remove:
```ts
const { level, loading: levelLoading } = useOperationsLevel();
const canCreate = !levelLoading && level >= opsLevel.configuration;
```

Use the prop directly.

- [ ] **Step 3: Update list page callers of CreateResourceDialog**

Config list and secret list pages pass the new prop. They get `allowedMethods` from `useSwarmResource`:

```ts
const { items, total, ..., allowedMethods } = useSwarmResource(...);
// ...
<CreateResourceDialog canCreate={allowedMethods.has("POST")} ... />
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/RemoveResourceAction.tsx frontend/src/components/CreateResourceDialog.tsx
git commit -m "refactor(frontend): RemoveResourceAction and CreateResourceDialog accept permission props"
```

---

### Task 11: Update SearchPalette and actions to use HEAD requests

**Files:**
- Modify: `frontend/src/lib/actions.ts`
- Modify: `frontend/src/components/search/SearchPalette.tsx`

- [ ] **Step 1: Replace `requiredLevel` with `requiredMethod` on `PaletteAction`**

In `frontend/src/lib/actions.ts`, change the interface:

```ts
export interface PaletteAction {
  id: string;
  label: string;
  keywords: string[];
  steps: PaletteStep[];
  execute: (...args: any[]) => Promise<void>;
  destructive?: boolean;
  /** HTTP method needed on the target resource (checked via HEAD). */
  requiredMethod?: string;
}
```

Update each action's `requiredLevel` to `requiredMethod`:
- Scale, Update Image, Rollback, Restart: `requiredMethod: "PUT"` (scale/image use PUT) or `"POST"` (rollback/restart use POST)
- Drain, Activate, Pause, Promote, Demote: `requiredMethod: "PUT"`
- Remove actions (service, node, stack, config, secret, network, volume, task): `requiredMethod: "DELETE"`

Specifically:
```ts
// scale, image → PUT
{ id: "scale", requiredMethod: "PUT", ... }
{ id: "image", requiredMethod: "PUT", ... }

// rollback, restart → POST
{ id: "rollback", requiredMethod: "POST", ... }
{ id: "restart", requiredMethod: "POST", ... }

// node actions → PUT
{ id: "drain", requiredMethod: "PUT", ... }
{ id: "activate", requiredMethod: "PUT", ... }
{ id: "pause", requiredMethod: "PUT", ... }
{ id: "promote-node", requiredMethod: "PUT", ... }
{ id: "demote-node", requiredMethod: "PUT", ... }

// remove actions → DELETE
removeTargets.map → requiredMethod: "DELETE"
{ id: "remove-task", requiredMethod: "DELETE", ... }
```

Also add a `resourcePath` helper. Each action's first step has `resourceType` and the selected resource gives an `id`. The path is `/${resourceType}s/${id}` (pluralized). Add a helper:

```ts
import { resourcePath } from "@/lib/searchConstants";
```

(If `resourcePath` from `searchConstants` returns e.g. `/services/abc`, it already works. Check its shape.)

- [ ] **Step 2: Update SearchPalette permission check**

In `frontend/src/components/search/SearchPalette.tsx`:

Remove `useOperationsLevel` import. Remove the `operationsLevel` variable. Remove the `requiredLevel` filter from the `actions` memo — show all actions unconditionally:

```ts
const actions = useMemo(() => getActions(), []);
```

In `executeAction`, add a permission check before executing. The first arg in `args` is the `SearchResult` (the target resource) for actions with a resource step. Construct the detail path and fire HEAD:

```ts
import { headAllowedMethods } from "../../api/client";
import { resourcePath as toResourcePath } from "@/lib/searchConstants";

const executeAction = useCallback(
  async (action: PaletteAction, args: unknown[]) => {
    // Check permission via HEAD if the action requires a method
    if (action.requiredMethod && action.steps[0]?.type === "resource") {
      const resource = args[0] as SearchResult;
      const path = toResourcePath(action.steps[0].resourceType!, resource.id, resource.name);
      if (!path) {
        setActionError("Unknown resource type.");
        return;
      }
      const methods = await headAllowedMethods(path);
      if (!methods.has(action.requiredMethod)) {
        setActionError("You don't have permission to perform this action on this resource.");
        return;
      }
    }

    if (action.destructive) {
      setPendingConfirm({ action, args });
      return;
    }

    void doExecute(action, args);
  },
  [doExecute],
);
```

This fires the HEAD before the destructive confirm dialog too, so the user gets a permission error before being asked to confirm.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/actions.ts frontend/src/components/search/SearchPalette.tsx
git commit -m "refactor(frontend): SearchPalette checks permissions via HEAD instead of ops level"
```

---

### Task 12: Update PluginList

**Files:**
- Modify: `frontend/src/pages/PluginList.tsx`

- [ ] **Step 1: Replace ops level check**

PluginList gates the install button and per-row enable/disable toggles on ops level. It doesn't use `useSwarmResource` (plugins aren't paginated the same way — `api.plugins()` returns all).

Since `api.plugins()` is a list call that returns all items (not using `fetchRange`), and its `allowedMethods` reflects the list endpoint, it can be used to gate the install button. But the per-row toggles need per-plugin permissions — these are gated on the plugin list response's `Allow` header as a heuristic.

Replace `useOperationsLevel` with `allowedMethods` from the plugins fetch:

```ts
const [allowedMethods, setAllowedMethods] = useState<Set<string>>(new Set());

// In the useEffect:
api.plugins().then(({ data, allowedMethods: methods }) => {
  setPlugins(data);
  setAllowedMethods(methods);
});
```

Wait — `api.plugins()` currently does `.then((r) => r.items)` which discards `allowedMethods`. This needs to be updated in Task 2 to preserve it, or PluginList should call `fetchJSON` directly.

Actually, the simpler approach: update `api.plugins()` in client.ts to return `FetchResult` like list endpoints. Change:

```ts
plugins: () => fetchJSON<CollectionResponse<Plugin>>("/plugins").then(({ data }) => data.items),
```

To:

```ts
plugins: () => fetchJSON<CollectionResponse<Plugin>>("/plugins").then(({ data, allowedMethods }) => ({
  data: data.items,
  allowedMethods,
})),
```

Then in PluginList:

```ts
api.plugins().then(({ data, allowedMethods: methods }) => {
  setPlugins(data);
  setAllowedMethods(methods);
});
```

Gate install button on `allowedMethods.has("POST")`. Gate per-row enable/disable on `allowedMethods.has("POST")` (same heuristic — list-level permission).

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/PluginList.tsx frontend/src/api/client.ts
git commit -m "refactor(frontend): use allowedMethods for write gating on plugin list"
```

---

### Task 13: Delete `useOperationsLevel` infrastructure and remove from App.tsx

**Files:**
- Delete: `frontend/src/hooks/useOperationsLevel.ts`
- Delete: `frontend/src/hooks/OperationsLevelProvider.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Verify no remaining imports**

Run: `cd frontend && grep -rn "useOperationsLevel\|OperationsLevelProvider\|opsLevel" src/`

This should return zero results (all consumers were updated in Tasks 5–12). If any remain, fix them first.

- [ ] **Step 2: Delete the files**

```bash
rm frontend/src/hooks/useOperationsLevel.ts frontend/src/hooks/OperationsLevelProvider.tsx
```

- [ ] **Step 3: Remove from App.tsx**

Remove the `OperationsLevelProvider` import and the `<OperationsLevelProvider>` wrapper from the component tree.

- [ ] **Step 4: Verify everything compiles and tests pass**

```bash
cd frontend && npx tsc -b --noEmit && npx vitest run
```

- [ ] **Step 5: Commit**

```bash
git add -u frontend/src/hooks/useOperationsLevel.ts frontend/src/hooks/OperationsLevelProvider.tsx frontend/src/App.tsx
git commit -m "refactor(frontend): delete OperationsLevelProvider and useOperationsLevel"
```

---

### Task 14: Backend — extend `setAllowList` and wire into list handlers

**Files:**
- Modify: `internal/api/allow.go`
- Modify: `internal/api/allow_test.go`
- Modify: `internal/api/config_handlers.go`
- Modify: `internal/api/secret_handlers.go`
- Modify: `internal/api/plugin_handlers.go`

- [ ] **Step 1: Write the test for `setAllowList` with POST**

In `internal/api/allow_test.go`, add:

```go
func TestSetAllowList_POST(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{
		Grants: []acl.Grant{
			{Resources: []string{"config:*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
		},
	})
	h := &Handlers{operationsLevel: config.OpsConfiguration, acl: e}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/configs", nil)
	r = r.WithContext(auth.WithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	h.setAllowList(w, r, "config")

	got := w.Header().Get("Allow")
	if got != "GET, HEAD, POST" {
		t.Errorf("setAllowList(config, write grant) = %q, want %q", got, "GET, HEAD, POST")
	}
}

func TestSetAllowList_NoWrite(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{
		Grants: []acl.Grant{
			{Resources: []string{"config:*"}, Audience: []string{"*"}, Permissions: []string{"read"}},
		},
	})
	h := &Handlers{operationsLevel: config.OpsConfiguration, acl: e}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/configs", nil)
	r = r.WithContext(auth.WithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	h.setAllowList(w, r, "config")

	got := w.Header().Get("Allow")
	if got != "GET, HEAD" {
		t.Errorf("setAllowList(config, read only) = %q, want %q", got, "GET, HEAD")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestSetAllowList_POST -v`

Expected: FAIL (current `setAllowList` doesn't accept parameters)

- [ ] **Step 3: Implement `setAllowList` with POST support**

Update `setAllowList` in `internal/api/allow.go`:

```go
// listCreateMethods maps resource types that support creation via POST to
// the minimum operations tier required.
var listCreateMethods = map[string]config.OperationsLevel{
	"config": config.OpsConfiguration,
	"secret": config.OpsConfiguration,
	"plugin": config.OpsConfiguration,
}

// setAllowList sets the Allow header for list endpoints. For resource types
// that support creation, POST is included when the user's operations level
// and ACL permit it.
func (h *Handlers) setAllowList(w http.ResponseWriter, r *http.Request, resourceType string) {
	methods := []string{"GET", "HEAD"}

	if tier, ok := listCreateMethods[resourceType]; ok && h.operationsLevel >= tier {
		id := auth.IdentityFromContext(r.Context())
		if h.acl.Can(id, "write", resourceType+":*") {
			methods = append(methods, "POST")
		}
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
}
```

- [ ] **Step 4: Update the existing `setAllowList` test**

The existing test calls `h.setAllowList(w)` with no args. Update to `h.setAllowList(w, r, "node")` (nodes don't support create, so it should still return `GET, HEAD`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestSetAllowList -v`

Expected: All PASS

- [ ] **Step 6: Wire `setAllowList` into list handlers**

Add `h.setAllowList(w, r, "config")` calls to `HandleListConfigs`, `HandleListSecrets`, and `HandleListPlugins` — at the top of each handler, before writing the response. For other list handlers (nodes, services, tasks, stacks, networks, volumes), add `h.setAllowList(w, r, "<type>")` for consistency even though they don't have POST — they'll just get `GET, HEAD`.

Example for `HandleListConfigs`:

```go
func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	h.setAllowList(w, r, "config")
	configs := h.cache.ListConfigs()
	// ... rest unchanged
```

- [ ] **Step 7: Run full test suite**

Run: `go test ./internal/api/ -v`

- [ ] **Step 8: Commit**

```bash
git add internal/api/allow.go internal/api/allow_test.go internal/api/config_handlers.go internal/api/secret_handlers.go internal/api/plugin_handlers.go internal/api/node_handlers.go internal/api/service_handlers.go internal/api/task_handlers.go internal/api/stack_handlers.go internal/api/network_handlers.go internal/api/volume_handlers.go
git commit -m "feat(api): include POST in list endpoint Allow header for creatable resources"
```

---

### Task 15: Full integration verification

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `go test ./...`

Expected: All pass

- [ ] **Step 2: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`

Expected: No errors

- [ ] **Step 3: Run frontend tests**

Run: `cd frontend && npx vitest run`

Expected: All pass

- [ ] **Step 4: Run lint**

Run: `make lint`

Expected: No issues

- [ ] **Step 5: Run full build**

Run: `make build`

Expected: Builds successfully

- [ ] **Step 6: Manual smoke test with dev-auth compose**

Start the compose stack and verify:
- Port 9005 (admin): all write buttons visible
- Port 9006 (viewer): no write buttons visible, only read-only views
- Port 9007 (frontend dev): write buttons on frontend-prefixed stacks, read-only elsewhere
- Port 9008 (oncall): write buttons on services/tasks, read-only on nodes/infra

- [ ] **Step 7: Commit any remaining fixes**

```bash
git add -A && git commit -m "fix: address integration issues from Allow header UI gating"
```
