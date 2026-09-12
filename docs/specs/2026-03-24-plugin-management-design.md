# Plugin Management Design

## Overview

Full Docker plugin management for Cetacean: enable, disable, install, remove, upgrade, and configure. Plugins are node-local resources (not swarm-orchestrated), so they use live Docker API calls with no cache or SSE streaming.

## Backend

### Interface

New `DockerPluginClient` interface in `handlers.go`, separate from `DockerWriteClient` (plugin ops are node-local, not swarm mutations):

- `PluginList(ctx context.Context) (types.PluginsListResponse, error)` — moved out of `DockerSystemClient`
- `PluginInspect(ctx context.Context, name string) (*types.Plugin, error)`
- `PluginEnable(ctx context.Context, name string) error`
- `PluginDisable(ctx context.Context, name string) error`
- `PluginRemove(ctx context.Context, name string, force bool) error`
- `PluginInstall(ctx context.Context, name string, privileges types.PluginPrivileges) (*types.Plugin, error)`
- `PluginUpgrade(ctx context.Context, name string, remote string, privileges types.PluginPrivileges) error`
- `PluginPrivileges(ctx context.Context, remote string) (types.PluginPrivileges, error)`
- `PluginConfigure(ctx context.Context, name string, args []string) error`

When `PluginList` moves to `DockerPluginClient`, the existing `HandlePlugins` handler switches from `h.systemClient` to `h.pluginClient`. The `PluginList` method is removed from `DockerSystemClient`.

### Routes

| Method | Path | Handler | Tier | Dispatch |
|---|---|---|---|---|
| `GET` | `/plugins` | `HandlePlugins` | — | `contentNegotiated` |
| `GET` | `/plugins/{name}` | `HandlePlugin` | — | `contentNegotiated` |
| `GET` | `/swarm/plugins` | `HandlePlugins` | — | `contentNegotiated` (alias) |
| `POST` | `/plugins/privileges` | `HandlePluginPrivileges` | — (read-only registry query) | — |
| `POST` | `/plugins` | `HandleInstallPlugin` | Tier 3 | — |
| `POST` | `/plugins/{name}/enable` | `HandleEnablePlugin` | Tier 2 | — |
| `POST` | `/plugins/{name}/disable` | `HandleDisablePlugin` | Tier 2 | — |
| `DELETE` | `/plugins/{name}` | `HandleRemovePlugin` | Tier 3 | — |
| `POST` | `/plugins/{name}/upgrade` | `HandleUpgradePlugin` | Tier 3 | — |
| `PATCH` | `/plugins/{name}/settings` | `HandleConfigurePlugin` | Tier 2 | — |

Note: `POST /plugins/privileges` is registered before `GET /plugins/{name}` to avoid ambiguity. These are distinct patterns in Go 1.22's ServeMux (different methods), so there is no actual conflict — but `GET /plugins/privileges` would match the `{name}` wildcard. Since we only define `POST` for privileges, this is fine.

### Docker Client Implementation

New methods on `docker.Client` implementing `DockerPluginClient`:

- `PluginList` — already exists, no changes
- `PluginInspect` — calls `c.docker.PluginInspectWithRaw(ctx, name)`, returns `*types.Plugin` (discards raw bytes)
- `PluginEnable` — calls `c.docker.PluginEnable(ctx, name, types.PluginEnableOptions{Timeout: 30})`
- `PluginDisable` — calls `c.docker.PluginDisable(ctx, name, types.PluginDisableOptions{})`
- `PluginRemove` — calls `c.docker.PluginRemove(ctx, name, types.PluginRemoveOptions{Force: force})`
- `PluginInstall` — calls `c.docker.PluginInstall(ctx, name, types.PluginInstallOptions{RemoteRef: name, AcceptAllPermissions: true})`. Drains and closes the returned `io.ReadCloser`. Then inspects and returns the new plugin. Privileges are already accepted by the user via the two-step UI flow; `AcceptAllPermissions: true` skips the SDK's internal permission callback.
- `PluginUpgrade` — calls `c.docker.PluginUpgrade(ctx, name, types.PluginInstallOptions{RemoteRef: remote, AcceptAllPermissions: true})`. Same drain pattern.
- `PluginPrivileges` — the Docker SDK has no public `PluginGetPrivileges` method. Instead, we hit the Docker Engine REST API directly at `GET /plugins/privileges?remote=<ref>` using `c.docker.(*client.Client)` (the underlying HTTP client). Decodes the JSON response into `types.PluginPrivileges`.
- `PluginConfigure` — calls `c.docker.PluginSet(ctx, name, args)` where args are `key=value` strings

All methods use 30-second context timeouts. No caching.

### Handler Patterns

- **Read handlers** (`HandlePlugins`, `HandlePlugin`): 5-second timeout, `writeJSONWithETag`, JSON-LD wrapping. `HandlePlugin` returns `DetailResponse` with `@context`, `@id`, `@type`. No cross-references since plugins don't relate to cached resources.
- **Write handlers**: decode JSON body, validate required fields, call client method, re-inspect and return updated resource with `writeJSON` (no ETag on mutations). RFC 9457 problem details on errors.
- **Install flow**: `HandlePluginPrivileges` takes `{"remote": "..."}`, returns privilege list as JSON. `HandleInstallPlugin` takes `{"remote": "..."}` (privileges already accepted in the UI), installs via `PluginInstall` (which uses `AcceptAllPermissions: true`), and returns the new plugin.
- **Upgrade flow**: Same two-step — caller first hits `POST /plugins/privileges` with the new remote ref, then `POST /plugins/{name}/upgrade` with `{"remote": "..."}`.
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
- `api.installPlugin(remote)` — POST install
- `api.enablePlugin(name)` / `api.disablePlugin(name)` — POST
- `api.removePlugin(name, force?)` — DELETE
- `api.upgradePlugin(name, remote)` — POST upgrade
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
