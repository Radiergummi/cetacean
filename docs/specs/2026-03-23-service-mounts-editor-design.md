# Service Mounts Editor

## Summary

Add a mounts editor to the service detail page, replacing the current read-only SimpleTable. Card-based UI (like PortsEditor) with type-specific fields per mount type. Supports all Docker mount types: bind, volume, tmpfs, npipe, cluster, image.

## Decisions

- **All mount types** supported (bind, volume, tmpfs, npipe, cluster, image)
- **Card-based UI** following PortsEditor pattern — not EditableTable (which is two-column)
- **Compact chips** for read-only view — type badge + source → target + read-only flag
- **Conditional fields** per card — type dropdown controls which fields appear
- **Type-specific options** exposed: bind propagation, volume no-copy/subpath, tmpfs size/mode, image subpath
- **Operations tier**: tier 2 (configuration), matching configs/secrets/networks

## Backend

### Docker Client (`internal/docker/client.go`)

New method:

**`UpdateServiceMounts(ctx, id string, mounts []mount.Mount) (swarm.Service, error)`**
- Calls `ServiceInspectWithRaw` to get current version.
- Sets `svc.Spec.TaskTemplate.ContainerSpec.Mounts = mounts`.
- Calls `ServiceUpdate` with the current version.
- Returns the updated service via `InspectService`.
- Same inspect→mutate→update→re-inspect pattern as `UpdateServiceConfigs`.

Import: `"github.com/docker/docker/api/types/mount"` must be added to `client.go` (not currently imported). Also add to `handlers.go` (for the interface) and `write_handlers.go` (for the handler).

### DockerWriteClient Interface (`internal/api/handlers.go`)

Add method:
```go
UpdateServiceMounts(ctx context.Context, id string, mounts []mount.Mount) (swarm.Service, error)
```

### Handlers (`internal/api/write_handlers.go`)

**`HandleGetServiceMounts`** — `GET /services/{id}/mounts`
- Looks up service in cache (404 if not found).
- Returns `{ "mounts": svc.Spec.TaskTemplate.ContainerSpec.Mounts }` via `writeJSONWithETag` (matching all other GET sub-resource handlers).
- Returns empty array (not null) if no mounts.

**`HandlePatchServiceMounts`** — `PATCH /services/{id}/mounts`
- Validates `Content-Type: application/merge-patch+json` (415 otherwise).
- Looks up service in cache (404 if not found).
- Decodes body: `{ "mounts": []mount.Mount }`.
- Calls `writeClient.UpdateServiceMounts`.
- Returns updated mounts list via `writeJSON` wrapped in a response object.
- No server-side validation beyond content-type — Docker validates mount specs on service update.

### Router (`internal/api/router.go`)

```
GET   /services/{id}/mounts → contentNegotiated(HandleGetServiceMounts, spa) (no tier)
PATCH /services/{id}/mounts → tier2(HandlePatchServiceMounts)
```

### Mock (`internal/api/write_handlers_test.go`)

Add `updateServiceMountsFn` field and `UpdateServiceMounts` stub method to `mockWriteClient`.

### Tests

- `TestHandleGetServiceMounts_OK` — service with mounts returns them.
- `TestHandleGetServiceMounts_Empty` — service without mounts returns empty array.
- `TestHandleGetServiceMounts_NotFound` — missing service returns 404.
- `TestHandlePatchServiceMounts_OK` — valid patch returns updated mounts.
- `TestHandlePatchServiceMounts_NotFound` — missing service returns 404.
- `TestHandlePatchServiceMounts_InvalidContentType` — wrong content-type returns 415.

## Frontend

### Types (`frontend/src/api/types.ts`)

Replace the inline `Mounts` type on `ContainerSpec` with a named `ServiceMount` type:

```ts
export interface ServiceMount {
  Type: string;
  Source: string;
  Target: string;
  ReadOnly?: boolean;
  BindOptions?: {
    Propagation?: string;
    NonRecursive?: boolean;
    CreateMountpoint?: boolean;
  };
  VolumeOptions?: {
    NoCopy?: boolean;
    Labels?: Record<string, string>;
    Subpath?: string;
  };
  TmpfsOptions?: {
    SizeBytes?: number;
    Mode?: number;
  };
  ImageOptions?: {
    Subpath?: string;
  };
  ClusterOptions?: Record<string, unknown>;
}
```

Update `ContainerSpec.Mounts` to use `ServiceMount[]`.

### API Client (`frontend/src/api/client.ts`)

```ts
serviceMounts: (id: string, signal?: AbortSignal) =>
  fetchJSON<{ mounts: ServiceMount[] }>(`/services/${id}/mounts`, signal).then(
    (r) => r.mounts ?? [],
  ),
patchServiceMounts: (id: string, mounts: ServiceMount[]) =>
  patch<{ mounts: ServiceMount[] }>(`/services/${id}/mounts`, { mounts }, "application/merge-patch+json"),
```

### MountsEditor Component (`frontend/src/components/service-detail/MountsEditor.tsx`)

Follows the PortsEditor pattern: `CollapsibleSection` with edit/read-only toggle.

**Props:**
```ts
interface MountsEditorProps {
  serviceId: string;
  mounts: ServiceMount[];
  onSaved: (mounts: ServiceMount[]) => void;
}
```

**State:** `editing`, `saving`, `saveError`, `draft: ServiceMount[]`. Uses `useEscapeCancel` for cancel-on-escape. Uses `useOperationsLevel` gated at `opsLevel.configuration`.

**Read-only view** — compact chips in a flex-wrap container (like PortsEditor read-only):
- Each mount rendered as a badge: `[type] source → target (ro)`.
- Type shown as a colored badge: bind=amber, volume=blue, tmpfs=purple, npipe=slate, cluster=teal, image=indigo. Extend the existing `MountTypeBadge` with the three new types.
- Source links to `/volumes/{name}` for volume type mounts.
- tmpfs mounts show only target (no source).
- Empty state: "No mounts configured" with edit hint.

**Edit mode** — responsive card grid (`grid-cols-1 md:grid-cols-2 lg:grid-cols-3`):
- Each card is a bordered container with a remove button (X) top-right.
- First field: **Type** dropdown (bind, volume, tmpfs, npipe, cluster, image).
- Changing type resets type-specific options and clears source for tmpfs.
- Fields appear/disappear based on type:

| Field | bind | volume | tmpfs | npipe | cluster | image |
|-------|------|--------|-------|-------|---------|-------|
| Source | host path (text) | volume name (text) | — | pipe name (text) | CSI vol (text) | image ref (text) |
| Target | container path (text) | container path (text) | container path (text) | container path (text) | container path (text) | container path (text) |
| Read-only | checkbox | checkbox | checkbox | checkbox | checkbox | checkbox |
| Propagation | dropdown | — | — | — | — | — |
| No-copy | — | checkbox | — | — | — | — |
| Subpath | — | text | — | — | — | text |
| Size | — | — | number (bytes) | — | — | — |
| Mode | — | — | number (octal) | — | — | — |

- Propagation dropdown options: private, rprivate, shared, rshared, slave, rslave.
- Default new mount: type=volume, all other fields empty.
- Add/Save/Cancel footer (same layout as PortsEditor).
- Empty edit state: dashed "Add a mount" button (same as PortsEditor).

**Save:** calls `api.patchServiceMounts(serviceId, draft)`, updates parent via `onSaved`.

### ServiceDetail.tsx Changes

- Import `MountsEditor`.
- Add `serviceMounts` state + fetch in `fetchData`.
- Replace the current read-only mounts SimpleTable (lines 437–470) with `<MountsEditor>`.
- Move `MountTypeBadge` into `MountsEditor.tsx` (it's only used there now) or into a shared file if referenced elsewhere.

### Barrel Export

Add `MountsEditor` to `frontend/src/components/service-detail/index.ts` if a barrel exists, or import directly.
