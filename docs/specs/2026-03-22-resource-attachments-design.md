# Service Resource Attachments — Configs, Secrets, Networks

## Summary

Add write operations for managing config, secret, and network attachments on services. Each resource type gets a GET + PATCH endpoint pair (full array replacement) and a frontend editor component following the PortsEditor pattern. Mount attachments are out of scope — they will be a separate spec.

## Decisions

- **API shape**: Full array replacement via PATCH (like ports), not individual add/remove endpoints.
- **Editable fields**: Config/secret: resource picker + target path. Network: resource picker + aliases. UID/GID/Mode omitted (Docker defaults).
- **Resource picker**: Combobox with search. Networks filter out already-attached; configs/secrets allow duplicates (different target paths).
- **Default target path**: `/<name>` for configs (Docker's default), `/run/secrets/<name>` for secrets (Docker's default). Auto-filled on selection.
- **Network aliases**: Multi-combobox (tag-style, free-form input).
- **Operations tier**: Tier 2 (configuration), same as env vars, ports, and other service config editors.

## Backend

### Docker Client (`internal/docker/client.go`)

All three methods follow the standard inspect→mutate→update→re-inspect pattern (like `UpdateServicePorts`, `UpdateServicePlacement`, etc.). They do NOT use `UpdateServiceContainerConfig` — that functional pattern has zero existing call sites and lacks a nil guard for `ContainerSpec`.

**`UpdateServiceConfigs(ctx, id string, configs []*swarm.ConfigReference) (swarm.Service, error)`**
- Inspects the service, guards `ContainerSpec != nil`, sets `svc.Spec.TaskTemplate.ContainerSpec.Configs = configs`, calls `ServiceUpdate`, re-inspects.

**`UpdateServiceSecrets(ctx, id string, secrets []*swarm.SecretReference) (swarm.Service, error)`**
- Same pattern: sets `svc.Spec.TaskTemplate.ContainerSpec.Secrets = secrets`.

**`UpdateServiceNetworks(ctx, id string, networks []swarm.NetworkAttachmentConfig) (swarm.Service, error)`**
- Same pattern but at `TaskTemplate` level: sets `svc.Spec.TaskTemplate.Networks = networks`.

### DockerWriteClient Interface (`internal/api/handlers.go`)

Add three methods:
```go
UpdateServiceConfigs(ctx context.Context, id string, configs []*swarm.ConfigReference) (swarm.Service, error)
UpdateServiceSecrets(ctx context.Context, id string, secrets []*swarm.SecretReference) (swarm.Service, error)
UpdateServiceNetworks(ctx context.Context, id string, networks []swarm.NetworkAttachmentConfig) (swarm.Service, error)
```

### Handlers (`internal/api/write_handlers.go`)

Six new handlers following established patterns:

**`HandleGetServiceConfigs`** — `GET /services/{id}/configs`
- Returns `{ "configs": [{ "configID": "...", "configName": "...", "fileName": "..." }] }`.
- Reads from cache: `service.Spec.TaskTemplate.ContainerSpec.Configs`.
- Maps each `*swarm.ConfigReference` to a simplified response object with `configID`, `configName`, and `fileName` (from `File.Name`).

**`HandlePatchServiceConfigs`** — `PATCH /services/{id}/configs` (`application/merge-patch+json`)
- Decodes body: `{ "configs": [{ "configName": "...", "configID": "...", "fileName": "..." }] }`.
- Validates: each entry must have `configID` and `configName`. `fileName` defaults to `/<configName>` if empty (Docker's default for configs).
- Builds `[]*swarm.ConfigReference` with `File` target (Name=fileName, UID="0", GID="0", Mode=0444).
- Calls `writeClient.UpdateServiceConfigs`.
- Returns the updated config list (same shape as GET).

**`HandleGetServiceSecrets`** — `GET /services/{id}/secrets`
- Same pattern as configs: returns `{ "secrets": [{ "secretID": "...", "secretName": "...", "fileName": "..." }] }`.

**`HandlePatchServiceSecrets`** — `PATCH /services/{id}/secrets` (`application/merge-patch+json`)
- Same pattern as configs. Builds `[]*swarm.SecretReference` with `File` target.
- `fileName` defaults to `/run/secrets/<secretName>` if empty.

**`HandleGetServiceNetworks`** — `GET /services/{id}/networks`
- Returns `{ "networks": [{ "target": "network-id", "aliases": ["alias1"] }] }`.
- Reads from `service.Spec.TaskTemplate.Networks`.

**`HandlePatchServiceNetworks`** — `PATCH /services/{id}/networks` (`application/merge-patch+json`)
- Decodes body: `{ "networks": [{ "target": "...", "aliases": [...] }] }`.
- Validates: each entry must have `target`.
- Builds `[]swarm.NetworkAttachmentConfig`.
- Calls `writeClient.UpdateServiceNetworks`.
- Returns the updated network list.

### Router (`internal/api/router.go`)

All at tier 2 (configuration):
```
GET   /services/{id}/configs   → contentNegotiated(HandleGetServiceConfigs, spa)
PATCH /services/{id}/configs   → tier2(HandlePatchServiceConfigs)
GET   /services/{id}/secrets   → contentNegotiated(HandleGetServiceSecrets, spa)
PATCH /services/{id}/secrets   → tier2(HandlePatchServiceSecrets)
GET   /services/{id}/networks  → contentNegotiated(HandleGetServiceNetworks, spa)
PATCH /services/{id}/networks  → tier2(HandlePatchServiceNetworks)
```

## Frontend

### API Client (`frontend/src/api/client.ts`)

Six new methods:

```ts
// GET sub-resource endpoints
serviceConfigs(id, signal?): Promise<{ configs: ServiceConfigRef[] }>
serviceSecrets(id, signal?): Promise<{ secrets: ServiceSecretRef[] }>
serviceNetworks(id, signal?): Promise<{ networks: ServiceNetworkRef[] }>

// PATCH mutations
patchServiceConfigs(id, configs): Promise<{ configs: ServiceConfigRef[] }>
patchServiceSecrets(id, secrets): Promise<{ secrets: ServiceSecretRef[] }>
patchServiceNetworks(id, networks): Promise<{ networks: ServiceNetworkRef[] }>
```

Response types (in `types.ts`):
```ts
export interface ServiceConfigRef {
  configID: string;
  configName: string;
  fileName: string;
}

export interface ServiceSecretRef {
  secretID: string;
  secretName: string;
  fileName: string;
}

export interface ServiceNetworkRef {
  target: string;
  aliases?: string[];
}
```

**Go response structs must use explicit `json` tags** to match the camelCase frontend types:
```go
type serviceConfigRef struct {
    ConfigID   string `json:"configID"`
    ConfigName string `json:"configName"`
    FileName   string `json:"fileName"`
}

type serviceSecretRef struct {
    SecretID   string `json:"secretID"`
    SecretName string `json:"secretName"`
    FileName   string `json:"fileName"`
}

type serviceNetworkRef struct {
    Target  string   `json:"target"`
    Aliases []string `json:"aliases,omitempty"`
}
```

### ConfigsEditor Component (`frontend/src/components/service-detail/ConfigsEditor.tsx`)

Follows the `PortsEditor` pattern: `CollapsibleSection` with edit toggle.

**Props**: `serviceId: string`, `configs: ServiceConfigRef[]`

**Read-only mode**: Table with linked config name + monospace target path. Edit button (hidden below tier 2).

**Edit mode**:
- Draft array copied from props on edit open.
- Fetches available configs via `api.configs({ limit: 0 })` on edit open.
- Each row: config name (read-only) + target path (read-only) + remove button.
- Add row: Combobox picker (all configs, duplicates allowed) + target path input (auto-filled with `/<name>` on selection) + Add button.
- Save: calls `api.patchServiceConfigs(serviceId, draft)`.
- Cancel: resets draft to props.

### SecretsEditor Component (`frontend/src/components/service-detail/SecretsEditor.tsx`)

Near-identical to `ConfigsEditor`. Same structure, same behavior. Differences:
- Uses `api.secrets()` for the picker, `api.patchServiceSecrets()` for save.
- Props use `ServiceSecretRef[]`.
- Default target path: `/run/secrets/<name>` (not `/<name>` like configs).

### NetworksEditor Component (`frontend/src/components/service-detail/NetworksEditor.tsx`)

Same `PortsEditor` pattern with differences:

**Props**: `serviceId: string`, `networks: ServiceNetworkRef[]`, `networkNames: Record<string, string>` (ID→name map, already fetched in ServiceDetail)

**Read-only mode**: Table with linked network name + comma-separated aliases.

**Edit mode**:
- Fetches available networks via `api.networks({ limit: 0 })` on edit open.
- Combobox filters out already-attached network IDs.
- Aliases field: multi-combobox (tag-style, free-form input — aliases are arbitrary strings).
- Each row: network name (read-only) + aliases tags (read-only) + remove button.
- Add row: Network combobox + aliases multi-combobox + Add button.

### ServiceDetail.tsx Changes

- Import `ConfigsEditor`, `SecretsEditor`, `NetworksEditor`.
- Replace the three read-only `CollapsibleSection` blocks (configs at ~lines 499-521, secrets at ~lines 523-545, networks at ~lines 469-497) with the new editor components.
- Pass current attachment data from the service object + service ID as props.
- `networkNames` map is already built in ServiceDetail — pass it to `NetworksEditor`.

### Barrel Export

Add `ConfigsEditor`, `SecretsEditor`, `NetworksEditor` to `frontend/src/components/service-detail/index.ts`.

## Testing

### Backend
- Extend `mockWriteClient` with `updateServiceConfigsFn`, `updateServiceSecretsFn`, `updateServiceNetworksFn` fields + stub methods.
- Per handler: OK (200 with correct response shape), not found (404), conflict (409), invalid body (400).
- GET handlers: return correct shape from cache, empty list when none attached, 404 for missing service.

### Frontend
- Each editor: renders read-only table, opens edit mode, shows combobox picker, adds/removes entries, saves draft.
- ConfigsEditor: auto-fills target path on selection.
- NetworksEditor: filters already-attached networks from picker.
