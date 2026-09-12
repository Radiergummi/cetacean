# Service Container Config Editors

## Summary

Add a single `GET/PATCH /services/{id}/container-config` endpoint to read and update ContainerSpec fields that aren't already covered by existing endpoints (image, env, healthcheck). Transform the existing read-only "Container Configuration" section on the service detail page into a container for five inline editors grouped by concern.

## Decisions

- **Single backend endpoint** for all ContainerSpec fields. Merge-patch lets clients send only changed fields.
- **Multiple inline editors** within the existing Container Configuration `CollapsibleSection`, following the Deploy Configuration pattern (multiple editors in one section).
- **Operations tier 2** (configuration) — same as env, resources, healthcheck.
- **camelCase JSON keys** in the endpoint response/request, mapped from PascalCase Docker SDK fields.
- **stopGracePeriod** is nanoseconds (matching Docker SDK). Frontend converts to/from seconds.

## Backend

### Docker Client (`internal/docker/client.go`)

One new method:

**`UpdateServiceContainerConfig(ctx, id string, cfg containerConfigResponse) (swarm.Service, error)`**
- Calls `ServiceInspectWithRaw` to get current version.
- Maps `containerConfigResponse` fields onto `svc.Spec.TaskTemplate.ContainerSpec` (converting `StopGracePeriod` from `*int64` back to `*time.Duration`).
- Calls `ServiceUpdate` with current version.
- Returns the updated service via `InspectService`.
- Same inspect→mutate→update→inspect pattern as all other `UpdateService*` methods. Takes a concrete struct like `UpdateServiceResources`, `UpdateServicePlacement`, etc.

### DockerWriteClient Interface (`internal/api/handlers.go`)

Add:
```go
UpdateServiceContainerConfig(ctx context.Context, id string, cfg containerConfigUpdate) (swarm.Service, error)
```

Where `containerConfigUpdate` is a concrete struct matching `containerConfigResponse` (defined in `write_handlers.go`, shared between handler and client via an internal type or by having the client accept the same struct).

### Handlers (`internal/api/write_handlers.go`)

**`HandleGetServiceContainerConfig`** — `GET /services/{id}/container-config`
- Reads from cache: `svc.Spec.TaskTemplate.ContainerSpec`
- Returns a `containerConfigResponse` struct with camelCase JSON tags:

```go
type containerConfigResponse struct {
    Command        []string         `json:"command"`
    Args           []string         `json:"args"`
    Dir            string           `json:"dir"`
    User           string           `json:"user"`
    Hostname       string           `json:"hostname"`
    Init           *bool            `json:"init"`
    TTY            bool             `json:"tty"`
    ReadOnly       bool             `json:"readOnly"`
    StopSignal     string           `json:"stopSignal"`
    StopGracePeriod *int64          `json:"stopGracePeriod"`
    CapabilityAdd  []string         `json:"capabilityAdd"`
    CapabilityDrop []string         `json:"capabilityDrop"`
    Groups         []string         `json:"groups"`
    Hosts          []string         `json:"hosts"`
    DNSConfig      *dnsConfigJSON   `json:"dnsConfig"`
}

type dnsConfigJSON struct {
    Nameservers []string `json:"nameservers"`
    Search      []string `json:"search"`
    Options     []string `json:"options"`
}
```

`StopGracePeriod` is converted from `*time.Duration` to `*int64` (nanoseconds) for JSON serialization.

**`HandlePatchServiceContainerConfig`** — `PATCH /services/{id}/container-config`
- Validates `Content-Type: application/merge-patch+json` → 415 if wrong.
- Checks cache for service existence → 404.
- Reads current container config from cache, populates a `containerConfigResponse` from the `ContainerSpec` fields, then marshals that struct to `map[string]any` as the base. This ensures camelCase keys in the base match the camelCase keys from the client's patch.
- Reads patch body, unmarshals to `map[string]any`.
- Applies `mergePatch(base, patch)` (existing RFC 7396 helper).
- Unmarshals merged result into `containerConfigResponse`.
- Calls `writeClient.UpdateServiceContainerConfig(ctx, id, mergedConfig)` with the concrete struct.
- Returns the updated container config (re-read from the returned service).

### Router (`internal/api/router.go`)

```
GET   /services/{id}/container-config  → contentNegotiated(HandleGetServiceContainerConfig, spa) (no tier)
PATCH /services/{id}/container-config  → tier2(HandlePatchServiceContainerConfig)
```

## Frontend

### API Client (`frontend/src/api/client.ts`)

```ts
serviceContainerConfig: (id: string, signal?: AbortSignal) =>
  fetchJSON<ContainerConfig>(`/services/${id}/container-config`, signal),

patchServiceContainerConfig: (id: string, partial: Partial<ContainerConfig>) =>
  patch<ContainerConfig>(`/services/${id}/container-config`, partial, "application/merge-patch+json"),
```

### Types (`frontend/src/api/types.ts`)

Add to the `Service` interface's `ContainerSpec`:
```ts
TTY?: boolean;
Groups?: string[];
Hosts?: string[];
DNSConfig?: {
  Nameservers?: string[];
  Search?: string[];
  Options?: string[];
} | null;
CapabilityAdd?: string[];
CapabilityDrop?: string[];
```

New type for the endpoint (uses optional `?` fields for arrays matching the codebase convention — Go nil slices serialize as JSON `null`, which TypeScript treats as absent via optional fields):
```ts
export interface ContainerConfig {
  command?: string[];
  args?: string[];
  dir: string;
  user: string;
  hostname: string;
  init?: boolean;
  tty: boolean;
  readOnly: boolean;
  stopSignal: string;
  stopGracePeriod?: number;
  capabilityAdd?: string[];
  capabilityDrop?: string[];
  groups?: string[];
  hosts?: string[];
  dnsConfig?: {
    nameservers?: string[];
    search?: string[];
    options?: string[];
  };
}
```

Note: when sending a merge-patch to clear a field, the frontend sends `null` explicitly (e.g. `{ dnsConfig: null }`) — the `Partial<ContainerConfig>` type is used loosely, and the actual patch body is built as a plain object with `null` values where needed.

### Editor Components

All five editors share these characteristics:
- Live inside the existing "Container Configuration" `CollapsibleSection` on `ServiceDetail.tsx`
- Follow the `HealthcheckEditor` display/edit toggle pattern (display view + edit button → form with Save/Cancel)
- Gated by `operationsLevel >= opsLevel.configuration` (tier 2)
- Use manual `useState` for `saving`/`saveError` with try/catch/finally (matching `HealthcheckEditor` and `ResourcesEditor` patterns — NOT `useAsyncAction`, which is for action-style components)
- Use `useEscapeCancel` for keyboard cancel
- Call `api.patchServiceContainerConfig(id, partialPatch)` with only their fields
- Accept `config: ContainerConfig` and `onSaved: (updated: ContainerConfig) => void` props
- The section renders all five in a two-column grid (matching Deploy Configuration layout)

**Editor 1: `CommandEditor`** (`frontend/src/components/service-detail/CommandEditor.tsx`)

Fields: `command`, `args`, `dir`, `user`

- Display: KV layout — Command (joined with spaces, monospace), Args (joined with spaces, monospace), Working Dir, User. "—" for unset.
- Edit: Four text inputs. Command and Args accept space-separated tokens. The component splits on spaces to produce `string[]` for the API, and joins for display. Dir and User are plain strings.
- Clearing a field sends `null` (for arrays) or `""` (for strings) in the patch.

**Editor 2: `RuntimeEditor`** (`frontend/src/components/service-detail/RuntimeEditor.tsx`)

Fields: `hostname`, `init`, `tty`, `readOnly`, `stopSignal`, `stopGracePeriod`

- Display: KV layout — Hostname, Init (Yes/No/Default), TTY (Yes/No), Read Only (Yes/No), Stop Signal, Stop Grace Period (human-readable, e.g. "10s").
- Edit: Hostname and Stop Signal are text inputs. Init, TTY, Read Only use checkbox inputs (following the `HealthcheckEditor` toggle pattern — there is no `Switch` component in the codebase). Stop Grace Period is a numeric input in seconds — the component multiplies by 1e9 for nanoseconds on save and divides on load.
- `init: null` means "use Docker default" — rendered as an unchecked checkbox with a "(default)" label. Checking it sets `true`, and a separate "Reset to default" link sets it back to `null`.

**Editor 3: `CapabilitiesEditor`** (`frontend/src/components/service-detail/CapabilitiesEditor.tsx`)

Fields: `capabilityAdd`, `capabilityDrop`

- Display: Two labeled lists of badges (e.g. `NET_ADMIN`, `SYS_PTRACE`), or "None" if empty/null.
- Edit: Two tag-input fields. Type a capability name, press Enter to add as a badge. Click the × on a badge to remove it. Input auto-uppercases. Common capabilities can be typed freely (no autocomplete needed for v1).
- Sends `capabilityAdd` and `capabilityDrop` arrays in the patch.

**Editor 4: `ExtraHostsEditor`** (`frontend/src/components/service-detail/ExtraHostsEditor.tsx`)

Fields: `hosts`

- Display: Table with IP Address and Hostname columns, parsed from Docker's `/etc/hosts` format: `"IP_address hostname"` (space-separated, IP first). Or "None" if empty/null.
- Edit: Editable rows with IP and hostname text inputs, plus add/remove row buttons. Similar to `KeyValueEditor` but for IP/hostname pairs. On save, joins back to `"IP hostname"` strings (space-separated, IP first — matching Docker swarmkit format).
- Sends `hosts` array in the patch.

**Editor 5: `DnsEditor`** (`frontend/src/components/service-detail/DnsEditor.tsx`)

Fields: `dnsConfig`

- Display: Three sub-sections — Nameservers (comma-separated), Search Domains (comma-separated), Options (comma-separated). Or "Default" if `dnsConfig` is null.
- Edit: Three text inputs (comma-separated values). The component splits/joins on commas.
- Sends `dnsConfig: { nameservers, search, options }` in the patch. Sending `dnsConfig: null` clears to Docker default.

### ServiceDetail.tsx Changes

- Fetch `api.serviceContainerConfig(id)` in the data loading section alongside other sub-resource fetches.
- Replace the existing read-only `KVTable` inside "Container Configuration" `CollapsibleSection` with the five editors arranged in a two-column grid.
- Pass `config` and `onSaved` to each editor.
- The `CollapsibleSection` `defaultOpen` changes from `false` to `true` if the user has write access (matching how other editable sections behave).

### Index Export

Add all five editors to `frontend/src/components/service-detail/index.ts`.

## Testing

### Backend
- Extend `mockWriteClient` with `updateServiceContainerConfigFn` field + stub method (takes a concrete `containerConfigUpdate` struct, not a function — matching the updated interface).
- `TestHandleGetServiceContainerConfig`: returns all fields correctly from cache, handles null/empty ContainerSpec fields gracefully, 404 for missing service.
- `TestHandlePatchServiceContainerConfig`: partial patch (only `hostname` and `tty`), null to clear `dnsConfig`, full patch with all fields, invalid content-type (415), 404, conflict (409).

### Frontend
- Each editor: renders display mode with current values, toggles to edit mode, submits correct partial patch with only its fields, handles error display, respects operations level gating.
