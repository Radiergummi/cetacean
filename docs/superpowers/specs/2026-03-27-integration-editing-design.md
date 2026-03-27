# Integration Settings Editing Design

## Problem

Integration panels currently display parsed label configuration read-only. Users need to edit integration settings through the same structured panels, with changes written back as Docker service label mutations.

## Decisions

- **Inline editing** via `EditablePanel`, matching all other service detail editors.
- **Operations level 2 (configuration)** — same tier as labels, env vars, ports.
- **No backend changes** — edits go through the existing `PATCH /services/{id}/labels` endpoint.
- **Simple integrations show all known fields** from documentation, not just currently-set ones.
- **Traefik: edit existing objects only** — no add/remove of routers, services, or middlewares.
- **Disable = set `enable=false`** — preserving other configuration.
- **Dual editor mode** — the existing structured/raw toggle extends to edit mode. In structured edit, users fill a purpose-built form. In raw edit, users get a `KeyValueEditor` for the integration's labels.

## Edit flow

1. User clicks Edit (gated by `operationsLevel >= opsLevel.configuration`).
2. Panel content swaps to form fields (structured) or `KeyValueEditor` (raw), based on the current toggle state. Users can switch between structured and raw while editing.
3. On save:
   - **Structured editor**: serializes form state to a flat `Record<string, string>` of labels. Compares against the original labels to compute changed/removed keys. Sends a merge patch to `PATCH /services/{id}/labels`.
   - **Raw editor**: `KeyValueEditor` generates JSON Patch operations directly (existing behavior).
4. Response updates local state. `EditablePanel` handles errors via `showErrorToast`.

## IntegrationSection changes

`IntegrationSection` currently wraps each panel with a `CollapsibleSection`, a raw/structured toggle, and a docs link. It gains:

- An `editable` prop (boolean, driven by operations level check in ServiceDetail)
- An `onSave` callback for the structured editor path
- Integration with `EditablePanel` for edit/cancel/save controls
- The raw/structured toggle works in both display and edit modes

In display mode: structured view or `KeyValuePills` (current behavior).
In edit mode: structured form or `KeyValueEditor` with the integration's raw labels.

## Panel forms

### Shepherd (2 fields)

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `shepherd.enable` | Toggle | true |
| Auth config | `shepherd.auth.config` | Text input | — |

**Parser fix required**: Remove `schedule`, `imageFilter`, `latest`, `updateOpts` from `ShepherdIntegration` and its parser — these are daemon-level environment variables, not service labels.

### Swarm Cronjob (6 fields)

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `swarm.cronjob.enable` | Toggle | true |
| Schedule | `swarm.cronjob.schedule` | Text input + cron tooltip | — |
| Skip running | `swarm.cronjob.skip-running` | Toggle | false |
| Replicas | `swarm.cronjob.replicas` | Number input | 1 |
| Registry auth | `swarm.cronjob.registry-auth` | Toggle | false |
| Query registry | `swarm.cronjob.query-registry` | Toggle | false |

**Parser addition required**: Add `registryAuth` and `queryRegistry` fields.

### Diun (10 fields + metadata)

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `diun.enable` | Toggle | true |
| Registry options | `diun.regopt` | Text input | — |
| Watch repo | `diun.watch_repo` | Toggle | false |
| Notify on | `diun.notify_on` | Multi-toggle (new, update) | new;update |
| Sort tags | `diun.sort_tags` | Select | reverse |
| Max tags | `diun.max_tags` | Number input | 0 |
| Include tags | `diun.include_tags` | Text input | — |
| Exclude tags | `diun.exclude_tags` | Text input | — |
| Hub link | `diun.hub_link` | Text input | — |
| Platform | `diun.platform` | Text input | — |
| Metadata | `diun.metadata.*` | Key-value editor | — |

**Parser addition required**: Add `regopt`, `hubLink`, `platform` fields.

### Traefik (edit existing only)

Each router, service, and middleware card becomes editable in place:

**Router fields** (per existing router):
- Rule: text input
- Entrypoints: comma-separated text input
- Middlewares: comma-separated text input
- Service: text input
- Priority: number input
- TLS cert resolver: text input

**Service fields** (per existing service):
- Port: number input
- Scheme: text input

**Middleware fields** (per existing middleware):
- Config values: text input per existing key (no add/remove keys)

Names (router name, service name, middleware name) are read-only.

No adding or removing routers, services, or middlewares. Structural changes use the raw labels toggle.

## Label serialization

Each integration panel implements a `serializeToLabels(state) → Record<string, string>` function that converts form state to flat label key-value pairs. The diff against original labels determines which keys to set/remove.

For Traefik, serialization reconstructs the dot-notation keys:
- `traefik.http.routers.<name>.rule` = value
- `traefik.http.services.<name>.loadbalancer.server.port` = value
- etc.

For simple integrations, it's a direct field → label mapping.

Labels with empty/cleared values are removed from the service (sent as `null` in merge patch).

## Validation

- Cron expressions validated client-side using `cron-parser` (already installed) — show inline error if invalid.
- Port numbers: validate as positive integers.
- No other validation — free-form strings are passed through to Docker.

## Backend parser updates

### Shepherd — trim to actual service labels

Remove from `ShepherdIntegration`:
- `Schedule`, `ImageFilter`, `Latest`, `UpdateOpts` (these are daemon env vars)

Add to `ShepherdIntegration`:
- `AuthConfig string` (`shepherd.auth.config`)

### Swarm Cronjob — add missing fields

Add to `CronjobIntegration`:
- `RegistryAuth bool` (`swarm.cronjob.registry-auth`)
- `QueryRegistry bool` (`swarm.cronjob.query-registry`)

### Diun — add missing fields

Add to `DiunIntegration`:
- `RegOpt string` (`diun.regopt`)
- `HubLink string` (`diun.hub_link`)
- `Platform string` (`diun.platform`)

## Testing

- **Backend**: Update existing parser tests to reflect field changes (remove shepherd phantom fields, add new fields for cronjob/diun).
- **Frontend**: No new tests per existing convention. Manual verification of edit → save → label mutation flow.
- **Handler tests**: No changes — edits go through the existing labels endpoint.
