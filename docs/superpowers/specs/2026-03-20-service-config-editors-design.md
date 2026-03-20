# Service Config Frontend Editors Design

## Overview

Add frontend editor components for five service sub-resources: placement, ports, update policy, rollback policy, and log driver. Also add operations-level awareness so editors are disabled when the server restricts write access.

## Components

All new components live in `frontend/src/components/service-detail/`. Export each from `index.ts`.

### PlacementEditor

**View mode:** Constraint pill badges (reuse existing PlacementPanel display), max replicas as labeled value, preferences read-only.

**Edit mode:**
- Constraints: text input list, one per constraint, with delete per row and "Add constraint" button. Placeholder: `node.role==manager`.
- Max replicas: number input.
- Preferences: not editable (complex structure, rarely changed).

**API:** PUT `/services/{id}/placement` with full `Placement` object.

**Data source:** Read initial values from `service.Spec.TaskTemplate.Placement` (already on the service object). No separate fetch needed. After save, update local state from the PUT response.

### PortsEditor

**View mode:** Port badges (protocol:published→target, publish mode).

**Edit mode:** Card-based — each port is a card with:
- Protocol: select (`tcp` / `udp` / `sctp`)
- Target port: number input, labeled "Container port"
- Published port: number input, labeled "Host port", helper text "Leave empty for auto-assign" (value 0 = auto-assign)
- Publish mode: select (`ingress` / `host`), helper text explaining the difference
- Delete button (top-right corner)

"Add port" button below the cards.

**API:** PATCH `/services/{id}/ports` with `Content-Type: application/merge-patch+json`, body `{"ports": [...]}` (full array replacement).

**Data source:** Fetch from `GET /services/{id}/ports` on mount. This returns `Spec.EndpointSpec.Ports` (the desired ports configuration), which may differ from `service.Endpoint.Ports` (the resolved/actual ports). The editor works with spec ports since that's what gets written back. The existing read-only badge display should also switch to spec ports for consistency.

### PolicyEditor (shared)

Single component used for both update and rollback policy.

**Props:** `type: "update" | "rollback"`, `serviceId`, `policy: UpdateConfig | null`, `onSaved`.

**View mode:** KVTable with human-friendly formatting — durations as "5s" (not nanoseconds), ratio as percentage.

**Edit mode:**
- Parallelism: number input
- Delay: duration input (seconds, converted to/from nanoseconds)
- Monitor: duration input (seconds, converted to/from nanoseconds)
- Max failure ratio: number input (0-1, displayed as percentage)
- Failure action: select (`pause` / `continue` for rollback; `pause` / `continue` / `rollback` for update)
- Order: select (`stop-first` / `start-first`)

**API:** PATCH `/services/{id}/update-policy` or `/services/{id}/rollback-policy` with `Content-Type: application/merge-patch+json`.

**Data source:** Read from `service.Spec.UpdateConfig` or `service.Spec.RollbackConfig` (already on the service object). No separate fetch needed.

### LogDriverEditor

**View mode:** Driver name as labeled value, options as KV pills or "No options".

**Edit mode:**
- Driver name: text input, placeholder `json-file`
- Options: inline key-value table (similar to KeyValueEditor's edit mode, but embedded in the LogDriverEditor's own draft state — not using KeyValueEditor as a standalone component). Add/remove rows, save buffers everything into the draft, single save sends the full merged driver object.

**API:** PATCH `/services/{id}/log-driver` with `Content-Type: application/merge-patch+json`.

**Data source:** Read from `service.Spec.TaskTemplate.LogDriver` (already on the service object). When nil, the backend returns `logDriver: null` — display "No log driver configured" in view mode.

## Operations Level Awareness

### useOperationsLevel hook

New file: `frontend/src/hooks/useOperationsLevel.ts`

Fetches `operationsLevel` from `GET /-/health` once on app mount. The health endpoint returns:
```json
{"status": "ok", "version": "...", "commit": "...", "buildDate": "...", "operationsLevel": 1}
```

The hook provides an `OperationsLevelProvider` context (wraps the app in `App.tsx`, same pattern as `AuthProvider`/`ConnectionProvider`) and a `useOperationsLevel()` hook that returns the numeric level (0, 1, or 2). Defaults to 0 (read-only) while loading to prevent flash of edit buttons.

### Editor behavior

Each editor checks the level against its required tier:
- **Tier 1** (`operationsLevel >= 1`): PlacementEditor, PortsEditor, PolicyEditor, LogDriverEditor, EnvEditor, KeyValueEditor (labels), ResourcesEditor, HealthcheckEditor, scale/image/rollback/restart actions
- **Tier 2** (`operationsLevel >= 2`): EndpointModeEditor, node availability, node labels, service mode, task removal

When insufficient:
- Edit button renders as **disabled** (grayed out)
- `title="Editing disabled by server configuration"` tooltip on hover
- No edit mode is accessible

When sufficient: no change to current behavior.

## ServiceDetail Integration

Placement, update/rollback policy, and log driver are read directly from the service object (no separate fetch). Ports are fetched from the dedicated endpoint (spec ports differ from resolved ports).

The existing read-only display blocks (`PlacementPanel`, KVTable for update/rollback config, log driver KVTable, port badges) are **replaced** by the new editor components, which handle both view and edit modes.

```typescript
// Ports need a separate fetch (spec ports, not resolved)
const [specPorts, setSpecPorts] = useState<PortConfig[] | null>(null);
useEffect(() => { api.servicePorts(id).then(r => setSpecPorts(r.ports)); }, [id]);

// Everything else comes from the service object
const placement = service.Spec.TaskTemplate.Placement;
const updatePolicy = service.Spec.UpdateConfig;
const rollbackPolicy = service.Spec.RollbackConfig;
const logDriver = service.Spec.TaskTemplate.LogDriver;
```

After a successful save, editors call `onSaved` which either:
- Updates local state (ports), or
- Triggers a service refetch via the existing SSE pattern (placement, policies, log driver — the SSE change event from the backend update naturally triggers a refetch)

## API Client Methods

Add to `frontend/src/api/client.ts`:

```typescript
// Placement (PUT — full replace)
putServicePlacement: (id: string, placement: Placement) =>
  put<{ placement: Placement }>(`/services/${id}/placement`, placement),

// Ports (PATCH — array replace via merge patch)
servicePorts: (id: string) =>
  get<{ ports: PortConfig[] }>(`/services/${id}/ports`),
patchServicePorts: (id: string, ports: PortConfig[]) =>
  patch<{ ports: PortConfig[] }>(`/services/${id}/ports`, { ports }, "application/merge-patch+json"),

// Update policy (PATCH — merge patch)
patchServiceUpdatePolicy: (id: string, partial: Record<string, unknown>) =>
  patch<{ updatePolicy: UpdateConfig }>(`/services/${id}/update-policy`, partial, "application/merge-patch+json"),

// Rollback policy (PATCH — merge patch)
patchServiceRollbackPolicy: (id: string, partial: Record<string, unknown>) =>
  patch<{ rollbackPolicy: UpdateConfig }>(`/services/${id}/rollback-policy`, partial, "application/merge-patch+json"),

// Log driver (PATCH — merge patch)
patchServiceLogDriver: (id: string, partial: Record<string, unknown>) =>
  patch<{ logDriver: Driver }>(`/services/${id}/log-driver`, partial, "application/merge-patch+json"),
```

Response types are inlined as the JSON-LD wrapper fields. The actual responses include `@context`, `@id`, `@type` which are ignored by the client (just destructure the named field).

## TypeScript Types

These types already partially exist in `frontend/src/api/types.ts`. Update or add as needed:

```typescript
interface Placement {
  Constraints?: string[];
  Preferences?: PlacementPreference[];
  MaxReplicas?: number;
}

interface PlacementPreference {
  Spread?: { SpreadDescriptor: string };
}

interface PortConfig {
  Name?: string;
  Protocol: string;
  TargetPort: number;
  PublishedPort: number;  // 0 = auto-assign
  PublishMode: string;
}

interface UpdateConfig {
  Parallelism?: number;
  Delay?: number;
  FailureAction?: string;
  Monitor?: number;
  MaxFailureRatio?: number;
  Order?: string;
}

interface Driver {
  Name: string;
  Options?: Record<string, string>;
}
```

Note: `UpdateConfig` fields are all optional (matching Go's zero-value semantics). `Platforms` removed from `Placement` (out of scope, read-only field not exposed by the editor).

## Duration Display

Update/rollback policy durations are stored as nanoseconds (Go `time.Duration`). The editor converts:
- Display: nanoseconds → human-readable string (e.g., "5s", "30s", "5m")
- Edit: seconds input → nanoseconds on save

This matches the existing pattern in `HealthcheckEditor` (`nanosToSeconds` / `secondsToNanos`).

## Out of Scope

- Placement preferences editing (complex structure, rarely used)
- Port name editing (optional field, low value)
- Frontend tests (follow-up)
