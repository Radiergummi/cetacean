# Plugin Management Design

## Overview

Full Docker plugin management for Cetacean: enable, disable, install, remove, upgrade, and configure. Plugins are node-local resources (not swarm-orchestrated), so they use live Docker API calls with no cache or SSE streaming.

## Backend

### Interface

New `DockerPluginClient` interface in `handlers.go`, separate from `DockerWriteClient` (plugin ops are node-local, not swarm mutations):

- `PluginList(ctx) ([]*types.Plugin, error)` — moved out of `DockerSystemClient`
- `PluginInspect(ctx, name) (*types.Plugin, error)`
- `PluginEnable(ctx, name) error`
- `PluginDisable(ctx, name) error`
- `PluginRemove(ctx, name, force bool) error`
- `PluginInstall(ctx, name string, privileges types.PluginPrivileges) error`
- `PluginUpgrade(ctx, name string, privileges types.PluginPrivileges) error`
- `PluginPrivileges(ctx, name) (types.PluginPrivileges, error)`
- `PluginConfigure(ctx, name string, args []string) error`

### Routes

| Method | Path | Handler | Tier |
|---|---|---|---|
| `GET` | `/plugins` | `HandlePlugins` | — |
| `GET` | `/plugins/{name}` | `HandlePlugin` | — |
| `POST` | `/plugins/privileges` | `HandlePluginPrivileges` | — (read-only registry query) |
| `POST` | `/plugins` | `HandleInstallPlugin` | Tier 3 |
| `POST` | `/plugins/{name}/enable` | `HandleEnablePlugin` | Tier 2 |
| `POST` | `/plugins/{name}/disable` | `HandleDisablePlugin` | Tier 2 |
| `DELETE` | `/plugins/{name}` | `HandleRemovePlugin` | Tier 3 |
| `POST` | `/plugins/{name}/upgrade` | `HandleUpgradePlugin` | Tier 3 |
| `PATCH` | `/plugins/{name}/settings` | `HandleConfigurePlugin` | Tier 2 |

`GET /swarm/plugins` exists as a content-negotiated alias (JSON serves the same handler, HTML serves the SPA).

### Docker Client Implementation

New methods on `docker.Client` implementing `DockerPluginClient`:

- `PluginInspect` — `docker.PluginInspectWithRaw`
- `PluginEnable` — `docker.PluginEnable` with 30s timeout
- `PluginDisable` — `docker.PluginDisable` (non-force)
- `PluginRemove` — `docker.PluginRemove` with optional force
- `PluginInstall` — `docker.PluginInstall` with accepted privileges; drain and close the pull progress reader
- `PluginUpgrade` — `docker.PluginUpgrade` with accepted privileges; same drain pattern
- `PluginPrivileges` — `docker.PluginGetPrivileges`
- `PluginConfigure` — `docker.PluginSet` with `key=value` args

All methods use 30-second context timeouts. No caching.

### Handler Patterns

- **Read handlers** (`HandlePlugins`, `HandlePlugin`): 5-second timeout, `writeJSONWithETag`, JSON-LD wrapping (`DetailResponse` with `@context`, `@id`, `@type`). No cross-references.
- **Write handlers**: decode JSON body, validate required fields, call client method, re-inspect and return updated resource with `writeJSON` (no ETag on mutations). RFC 9457 problem details on errors.
- **Install flow**: `HandlePluginPrivileges` takes `{"remote": "..."}`, returns privilege list. `HandleInstallPlugin` takes `{"remote": "...", "privileges": [...]}`, installs, inspects, returns the new plugin.
- **Upgrade flow**: Same two-step — caller first hits `POST /plugins/privileges`, then `POST /plugins/{name}/upgrade` with accepted privileges.
- **Remove handler**: Accepts `?force=true` query param. Returns 204 No Content.
- **Error mapping**: Docker "not found" → 404, "already enabled/disabled" → 409, permission denied → 403.

### Operation Tiers

- **Tier 2 (configuration):** enable, disable, configure
- **Tier 3 (impactful):** install, remove, upgrade

Rationale: disabling a plugin (e.g., Loki, CSI) can bring services down or cause disk overflow. Install/remove/upgrade alter node software and are hard to reverse.

## Frontend

### Plugin List Page (`/plugins`)

A simple table matching the existing plugin table on the Swarm page (name, type, status). Exists as a content-negotiated fallback at `/swarm/plugins` — in normal operations users reach plugins through the Swarm page. Each row links to the plugin detail page. "Install Plugin" button in the page header.

### Plugin Detail Page (`/plugins/:name`)

Fetched with `api.plugin(name)`. Sections:

- **Header**: plugin name, enabled/disabled badge, action buttons (enable/disable toggle, remove, upgrade)
- **Overview**: description, version/reference, ID
- **Settings**: current args in an editable key-value editor, saved via PATCH
- **Privileges**: read-only list of granted privileges (network, mount, device access, etc.)
- **Configuration**: read-only display of declared config (entrypoint, workdir, env, mounts, devices, linux capabilities)

### Install Dialog

Triggered from plugin list page header action and Swarm page plugin section action. Two-step modal:

1. Text input for remote reference (e.g., `vieux/sshfs`), "Check Privileges" button → `POST /plugins/privileges` → displays privilege list
2. User reviews privileges, clicks "Install" → `POST /plugins`

### Upgrade Dialog

Same two-step pattern as install, triggered from detail page upgrade button. Pre-fills current `PluginReference`.

### Swarm Page Changes

Existing plugin section stays. Plugin table rows link to detail pages. "Install Plugin" added as a section action above the table. "View All" link to `/plugins`.

### API Client (`client.ts`)

- `api.plugin(name)` — GET detail
- `api.pluginPrivileges(remote)` — POST privileges check
- `api.installPlugin(remote, privileges)` — POST install
- `api.enablePlugin(name)` / `api.disablePlugin(name)` — POST
- `api.removePlugin(name)` — DELETE
- `api.upgradePlugin(name, remote, privileges)` — POST upgrade
- `api.configurePlugin(name, args)` — PATCH settings

### Routing

`/plugins` and `/plugins/:name` in React router. No main nav entry. Swarm page plugin section links to `/plugins`.

## Not In Scope

- Plugin cache or SSE streaming (live calls only)
- Global search inclusion
- Install/upgrade progress streaming (drain pull progress server-side)
- Plugin push/create (authoring tools)

## Testing

- Mock `DockerPluginClient` interface for handler tests
- Test each handler: success path, not found, conflict (already enabled/disabled), validation errors, tier gating
- Test privilege + install two-step flow
- Test `?force=true` on remove
- Test error mapping (Docker API errors → correct HTTP status codes)
