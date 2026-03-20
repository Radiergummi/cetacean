# Service Config Frontend Editors Design

## Overview

Add frontend editor components for five service sub-resources: placement, ports, update policy, rollback policy, and log driver. Also add operations-level awareness so editors are disabled when the server restricts write access.

## Components

All new components live in `frontend/src/components/service-detail/`.

### PlacementEditor

**View mode:** Constraint pill badges (reuse existing PlacementPanel display), max replicas as labeled value, preferences read-only.

**Edit mode:**
- Constraints: text input list, one per constraint, with delete per row and "Add constraint" button. Placeholder: `node.role==manager`.
- Max replicas: number input.
- Preferences: not editable (complex structure, rarely changed).

**API:** PUT `/services/{id}/placement` with full `Placement` object.

### PortsEditor

**View mode:** Port badges (protocol:published→target, publish mode).

**Edit mode:** Card-based — each port is a card with:
- Protocol: select (`tcp` / `udp` / `sctp`)
- Target port: number input, labeled "Container port"
- Published port: number input, labeled "Host port", helper text "Leave empty for auto-assign"
- Publish mode: select (`ingress` / `host`), helper text explaining the difference
- Delete button (top-right corner)

"Add port" button below the cards.

**API:** PATCH `/services/{id}/ports` with `Content-Type: application/merge-patch+json`, body `{"ports": [...]}` (full array replacement).

### PolicyEditor (shared)

Single component used for both update and rollback policy.

**Props:** `type: "update" | "rollback"`, `serviceId`, `policy`, `onSaved`.

**View mode:** KVTable with human-friendly formatting — durations as "5s" (not nanoseconds), ratio as percentage.

**Edit mode:**
- Parallelism: number input
- Delay: duration input (seconds, converted to/from nanoseconds)
- Monitor: duration input (seconds, converted to/from nanoseconds)
- Max failure ratio: number input (0-1, displayed as percentage)
- Failure action: select (`pause` / `continue` for rollback; `pause` / `continue` / `rollback` for update)
- Order: select (`stop-first` / `start-first`)

**API:** PATCH `/services/{id}/update-policy` or `/services/{id}/rollback-policy` with `Content-Type: application/merge-patch+json`.

### LogDriverEditor

**View mode:** Driver name as labeled value, options as KV pills or "No options".

**Edit mode:**
- Driver name: text input, placeholder `json-file`
- Options: reuse `KeyValueEditor` for the options map

**API:** PATCH `/services/{id}/log-driver` with `Content-Type: application/merge-patch+json`.

## Operations Level Awareness

### Hook: useOperationsLevel

Fetches `operationsLevel` from `GET /-/health` once on app mount and caches in context. The value is static (config-based, doesn't change at runtime).

### Editor behavior

Each editor compares the configured level against its required tier. All five new editors require tier 1 (`OpsOperational`). Existing editors (env, labels, resources, healthcheck, scale, image, endpoint mode) also gain this check.

When the level is insufficient:
- Edit button renders as **disabled** (grayed out)
- Tooltip on hover: "Editing disabled by server configuration"
- No edit mode is accessible

When the level is sufficient: no change to current behavior.

## State Management Pattern

Each editor follows the existing pattern:

```
const [editing, setEditing] = useState(false);
const [saving, setSaving] = useState(false);
const [saveError, setSaveError] = useState<string | null>(null);
```

Draft state cloned from props on edit open. Save calls the API, on success calls `onSaved` callback and exits edit mode. Errors displayed inline.

## ServiceDetail Integration

Each sub-resource is fetched separately on mount (matching existing pattern for env, healthcheck, resources):

```typescript
const [placement, setPlacement] = useState<Placement | null>(null);
const [ports, setPorts] = useState<PortConfig[] | null>(null);
const [updatePolicy, setUpdatePolicy] = useState<UpdateConfig | null>(null);
const [rollbackPolicy, setRollbackPolicy] = useState<UpdateConfig | null>(null);
const [logDriver, setLogDriver] = useState<Driver | null | undefined>(undefined);
```

Fetched via new API client methods. SSE change events trigger refetch (existing `useResourceStream` pattern).

## API Client Methods

Add to `frontend/src/api/client.ts`:

```typescript
servicePlacement: (id: string) => get<PlacementResponse>(`/services/${id}/placement`),
putServicePlacement: (id: string, placement: Placement) => put<PlacementResponse>(`/services/${id}/placement`, placement),
servicePorts: (id: string) => get<PortsResponse>(`/services/${id}/ports`),
patchServicePorts: (id: string, ports: PortConfig[]) => patch<PortsResponse>(`/services/${id}/ports`, { ports }, "application/merge-patch+json"),
serviceUpdatePolicy: (id: string) => get<UpdatePolicyResponse>(`/services/${id}/update-policy`),
patchServiceUpdatePolicy: (id: string, partial: Partial<UpdateConfig>) => patch<UpdatePolicyResponse>(`/services/${id}/update-policy`, partial, "application/merge-patch+json"),
serviceRollbackPolicy: (id: string) => get<RollbackPolicyResponse>(`/services/${id}/rollback-policy`),
patchServiceRollbackPolicy: (id: string, partial: Partial<UpdateConfig>) => patch<RollbackPolicyResponse>(`/services/${id}/rollback-policy`, partial, "application/merge-patch+json"),
serviceLogDriver: (id: string) => get<LogDriverResponse>(`/services/${id}/log-driver`),
patchServiceLogDriver: (id: string, partial: Partial<Driver>) => patch<LogDriverResponse>(`/services/${id}/log-driver`, partial, "application/merge-patch+json"),
```

## TypeScript Types

Add to `frontend/src/api/types.ts`:

```typescript
interface Placement {
  Constraints?: string[];
  Preferences?: PlacementPreference[];
  MaxReplicas?: number;
  Platforms?: Platform[];
}

interface PlacementPreference {
  Spread?: { SpreadDescriptor: string };
}

interface PortConfig {
  Name?: string;
  Protocol: string;
  TargetPort: number;
  PublishedPort: number;
  PublishMode: string;
}

interface UpdateConfig {
  Parallelism: number;
  Delay: number;
  FailureAction: string;
  Monitor: number;
  MaxFailureRatio: number;
  Order: string;
}

interface Driver {
  Name: string;
  Options?: Record<string, string>;
}
```

Response wrappers follow the existing JSON-LD pattern (with `@context`, `@id`, `@type` + the sub-resource field).

## Duration Display

Update/rollback policy durations are stored as nanoseconds (Go `time.Duration`). The editor converts:
- Display: nanoseconds → human-readable string (e.g., "5s", "30s", "5m")
- Edit: seconds input → nanoseconds on save

This matches the existing pattern in `HealthcheckEditor` (`nanosToSeconds` / `secondsToNanos`).

## Out of Scope

- Placement preferences editing (complex structure, rarely used)
- Placement platforms editing (rarely used)
- Port name editing (optional field, low value)
- Frontend tests (follow-up)
