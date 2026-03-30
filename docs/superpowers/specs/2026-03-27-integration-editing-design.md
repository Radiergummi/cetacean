# Integration Settings Editing Design

## Problem

Integration panels currently display parsed label configuration read-only. Users need to edit integration settings through the same structured panels, with changes written back as Docker service label mutations.

## Decisions

- **Inline editing** via `EditablePanel`, matching all other service detail editors.
- **Operations level 2 (configuration)** — same tier as labels, env vars, ports.
- **No backend changes** — edits go through the existing `PATCH /services/{id}/labels` endpoint using JSON Patch (`application/json-patch+json`), matching the existing `patchServiceLabels` client.
- **Simple integrations show all known fields** from documentation, not just currently-set ones.
- **Traefik: edit existing objects only** — no add/remove of routers, services, or middlewares. Unrecognized `traefik.*` labels (TCP, UDP, docker) are preserved — the structured editor only overwrites keys it knows about.
- **Disable = set `enable=false`** — preserving other configuration.
- **Dual editor mode** — the existing structured/raw toggle extends to edit mode, but the toggle is locked for the duration of an edit session (whichever mode was active when Edit was clicked stays active until save/cancel).

## Edit flow

1. User clicks Edit (gated by `operationsLevel >= opsLevel.configuration`).
2. Panel content swaps to form fields (structured) or `KeyValueEditor` (raw), based on the current toggle state. The toggle is disabled while editing.
3. On save:
   - **Structured editor**: serializes form state to a flat `Record<string, string>` of labels. Diffs against the original raw labels to generate `PatchOp[]` (add/replace/remove operations). Sends via `api.patchServiceLabels(id, ops)`.
   - **Raw editor**: `KeyValueEditor` generates `PatchOp[]` directly (existing behavior), sent via the same `api.patchServiceLabels`.
4. Response updates local state. `EditablePanel` handles errors via `showErrorToast`. SSE events trigger a service detail re-fetch, which re-runs `integrations.Detect` and updates the panel.

## Component composition

```
IntegrationSection
├── CollapsibleSection (title, controls: [DocsLink, StructuredRawToggle, EditButton])
│   ├── [display + structured] → panel's structured view (current behavior)
│   ├── [display + raw] → KeyValuePills (current behavior)
│   ├── [edit + structured] → panel's form (rendered via children prop)
│   └── [edit + raw] → KeyValueEditor with integration's raw labels
```

`IntegrationSection` manages the `showRaw` and `editing` state. It renders the Edit/Save/Cancel controls itself (not using `EditablePanel` directly, since the dual-mode switching doesn't fit `EditablePanel`'s single display/edit pair). It follows the same button styling and save/cancel patterns.

Each panel component receives an `editing` boolean and renders either its display or form content accordingly. The panel also provides:
- `serializeToLabels(formState) → Record<string, string>` — for the structured save path
- `onSave` callback wired by `IntegrationSection`

## Panel forms

### Shepherd (2 fields)

Shepherd's only service-level labels are `shepherd.enable` and `shepherd.auth.config` ([source](https://github.com/djmaze/shepherd#usage)). The fields `schedule`, `imageFilter`, `latest`, and `updateOpts` in the current parser are Shepherd daemon environment variables, not service labels.

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `shepherd.enable` | Toggle | true |
| Auth config | `shepherd.auth.config` | Text input | — |

**Parser fix required**: Remove `Schedule`, `ImageFilter`, `Latest`, `UpdateOpts` from `ShepherdIntegration`. Add `AuthConfig`.

### Swarm Cronjob (6 fields)

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `swarm.cronjob.enable` | Toggle | true |
| Schedule | `swarm.cronjob.schedule` | Text input + cron tooltip | — |
| Skip running | `swarm.cronjob.skip-running` | Toggle | false |
| Replicas | `swarm.cronjob.replicas` | Number input | 1 |
| Registry auth | `swarm.cronjob.registry-auth` | Toggle | false |
| Query registry | `swarm.cronjob.query-registry` | Toggle | false |

**Parser addition required**: Add `RegistryAuth` and `QueryRegistry` bool fields.

### Diun (10 fields + metadata)

| Field | Label key | Control | Default |
|-------|-----------|---------|---------|
| Enable | `diun.enable` | Toggle | true |
| Registry options | `diun.regopt` | Text input | — |
| Watch repo | `diun.watch_repo` | Toggle | false |
| Notify on | `diun.notify_on` | Multi-toggle (new, update) | new;update |
| Sort tags | `diun.sort_tags` | Select (default, reverse, semver, lexicographical) | reverse |
| Max tags | `diun.max_tags` | Number input | 0 |
| Include tags | `diun.include_tags` | Text input | — |
| Exclude tags | `diun.exclude_tags` | Text input | — |
| Hub link | `diun.hub_link` | Text input | — |
| Platform | `diun.platform` | Text input | — |
| Metadata | `diun.metadata.*` | Key-value editor | — |

`notify_on` serializes as semicolon-separated values (e.g., `"new;update"`).

**Parser addition required**: Add `RegOpt`, `HubLink`, `Platform` string fields.

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

Names (router name, service name, middleware name) are read-only. TLS domains are not editable in structured mode (pass through unmodified).

No adding or removing routers, services, or middlewares. Structural changes use the raw labels editor.

## Label serialization

Each panel implements a `serializeToLabels(formState) → Record<string, string>` function that converts form state to flat label key-value pairs. The structured save path then diffs this against the original raw labels for the integration to produce `PatchOp[]`:

- Key present in new but not original → `add` operation
- Key present in both but value changed → `replace` operation
- Key present in original but not in new (field cleared) → `remove` operation
- Keys not in the serialization output (unrecognized labels) → **preserved unchanged**

This last point is critical for Traefik: `traefik.tcp.*`, `traefik.udp.*`, and any unrecognized `traefik.*` labels are not included in serialization output and thus not touched by the diff. They survive edits.

For simple integrations, serialization is a direct field → label key mapping. Empty string fields are omitted (resulting in removal if they previously existed). Boolean `false` fields are omitted for optional booleans but explicitly set for `enable`.

For Traefik, serialization reconstructs dot-notation keys:
- `traefik.http.routers.<name>.rule` = value
- `traefik.http.services.<name>.loadbalancer.server.port` = String(value)
- `traefik.http.middlewares.<name>.<type>.<configKey>` = value
- `traefik.enable` = String(enabled)

## Validation

- Cron expressions validated client-side using `cron-parser` (already installed) — show inline error if invalid.
- Port numbers: validate as positive integers.
- No other validation — free-form strings are passed through to Docker.

## Backend parser updates

### Shepherd — trim to actual service labels

Remove from `ShepherdIntegration`:
- `Schedule`, `ImageFilter`, `Latest`, `UpdateOpts` (daemon env vars, not service labels — [confirmed](https://github.com/djmaze/shepherd#usage))

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
