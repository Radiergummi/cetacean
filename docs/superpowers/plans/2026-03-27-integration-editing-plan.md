# Integration Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add inline editing to integration panels, letting users modify integration settings through structured forms that write back as Docker service label mutations.

**Architecture:** Each integration panel gains an edit mode using the existing `EditablePanel` pattern. `IntegrationSection` manages display/edit state and the structured/raw toggle (locked during editing). Structured editors serialize form state to labels and diff against originals to produce `PatchOp[]` sent via the existing `patchServiceLabels` API. No backend API changes needed.

**Tech Stack:** React + TypeScript (forms, state), existing `PatchOp`/`patchServiceLabels` (label mutations), `cron-parser` (validation)

**Spec:** `docs/superpowers/specs/2026-03-27-integration-editing-design.md`

---

## File Structure

### Backend (modified files)
- `internal/integrations/shepherd.go` — Remove phantom fields, add `AuthConfig`
- `internal/integrations/shepherd_test.go` — Update tests
- `internal/integrations/cronjob.go` — Add `RegistryAuth`, `QueryRegistry`
- `internal/integrations/cronjob_test.go` — Update tests
- `internal/integrations/diun.go` — Add `RegOpt`, `HubLink`, `Platform`
- `internal/integrations/diun_test.go` — Update tests

### Frontend (new files)
- `frontend/src/lib/integrationLabels.ts` — Shared label serialization and diff utilities

### Frontend (modified files)
- `frontend/src/api/types.ts` — Update integration type fields
- `frontend/src/components/service-detail/IntegrationSection.tsx` — Add edit mode, lock toggle, save/cancel controls
- `frontend/src/components/service-detail/ShepherdPanel.tsx` — Add edit form
- `frontend/src/components/service-detail/CronjobPanel.tsx` — Add edit form
- `frontend/src/components/service-detail/DiunPanel.tsx` — Add edit form
- `frontend/src/components/service-detail/TraefikPanel.tsx` — Add edit form
- `frontend/src/pages/ServiceDetail.tsx` — Pass `serviceId`, `onSaved` to panels

---

## Task 1: Fix backend parsers

**Files:**
- Modify: `internal/integrations/shepherd.go`
- Modify: `internal/integrations/shepherd_test.go`
- Modify: `internal/integrations/cronjob.go`
- Modify: `internal/integrations/cronjob_test.go`
- Modify: `internal/integrations/diun.go`
- Modify: `internal/integrations/diun_test.go`
- Modify: `frontend/src/api/types.ts`

- [ ] **Step 1: Update Shepherd parser — remove phantom fields, add AuthConfig**

In `internal/integrations/shepherd.go`, replace the struct and parser:

```go
type ShepherdIntegration struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AuthConfig string `json:"authConfig,omitempty"`
}
```

Remove the `schedule`, `image-filter`, `latest`, `update-opts` cases from the switch. Add:

```go
case "auth.config":
    integration.AuthConfig = v
```

- [ ] **Step 2: Update Shepherd tests**

Remove `TestDetectShepherd_Basic`'s checks for schedule/imageFilter/latest/updateOpts. Update the basic test to use `shepherd.auth.config` instead. Remove `TestDetectShepherd_EnableOnly` (was testing schedule being empty — no longer relevant). Add a test for auth config:

```go
func TestDetectShepherd_AuthConfig(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable":      "true",
		"shepherd.auth.config": "my-registry",
	}
	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.AuthConfig != "my-registry" {
		t.Errorf("expected authConfig 'my-registry', got %q", result.AuthConfig)
	}
}
```

- [ ] **Step 3: Update Cronjob parser — add RegistryAuth, QueryRegistry**

In `internal/integrations/cronjob.go`, add fields to the struct:

```go
RegistryAuth  bool `json:"registryAuth,omitempty"`
QueryRegistry bool `json:"queryRegistry,omitempty"`
```

Add cases to the switch:

```go
case "registry-auth":
    integration.RegistryAuth = v == "true"
case "query-registry":
    integration.QueryRegistry = v == "true"
```

- [ ] **Step 4: Update Cronjob tests**

Add a test for the new fields:

```go
func TestDetectCronjob_RegistryFields(t *testing.T) {
	labels := map[string]string{
		"swarm.cronjob.enable":         "true",
		"swarm.cronjob.schedule":       "0 * * * *",
		"swarm.cronjob.registry-auth":  "true",
		"swarm.cronjob.query-registry": "true",
	}
	result := detectCronjob(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.RegistryAuth {
		t.Error("expected registryAuth=true")
	}
	if !result.QueryRegistry {
		t.Error("expected queryRegistry=true")
	}
}
```

- [ ] **Step 5: Update Diun parser — add RegOpt, HubLink, Platform**

In `internal/integrations/diun.go`, add fields to the struct:

```go
RegOpt   string `json:"regopt,omitempty"`
HubLink  string `json:"hubLink,omitempty"`
Platform string `json:"platform,omitempty"`
```

Add cases to the switch:

```go
case "regopt":
    integration.RegOpt = v
case "hub_link":
    integration.HubLink = v
case "platform":
    integration.Platform = v
```

- [ ] **Step 6: Update Diun tests**

Add a test for the new fields:

```go
func TestDetectDiun_ExtraFields(t *testing.T) {
	labels := map[string]string{
		"diun.enable":   "true",
		"diun.regopt":   "my-registry",
		"diun.hub_link": "https://hub.example.com",
		"diun.platform": "linux/amd64",
	}
	result := detectDiun(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.RegOpt != "my-registry" {
		t.Errorf("RegOpt: got %q", result.RegOpt)
	}
	if result.HubLink != "https://hub.example.com" {
		t.Errorf("HubLink: got %q", result.HubLink)
	}
	if result.Platform != "linux/amd64" {
		t.Errorf("Platform: got %q", result.Platform)
	}
}
```

- [ ] **Step 7: Update frontend types**

In `frontend/src/api/types.ts`, update:

```typescript
// Shepherd — remove phantom fields, add authConfig
export interface ShepherdIntegration {
  name: "shepherd";
  enabled: boolean;
  authConfig?: string;
}

// Cronjob — add registryAuth, queryRegistry
export interface CronjobIntegration {
  name: "swarm-cronjob";
  enabled: boolean;
  schedule?: string;
  skipRunning?: boolean;
  replicas?: number;
  registryAuth?: boolean;
  queryRegistry?: boolean;
}

// Diun — add regopt, hubLink, platform
export interface DiunIntegration {
  name: "diun";
  enabled: boolean;
  watchRepo?: boolean;
  notifyOn?: string;
  maxTags?: number;
  includeTags?: string;
  excludeTags?: string;
  sortTags?: string;
  regopt?: string;
  hubLink?: string;
  platform?: string;
  metadata?: Record<string, string>;
}
```

- [ ] **Step 8: Update ShepherdPanel display to match new fields**

Remove schedule/imageFilter/latest/updateOpts rows from `ShepherdPanel.tsx`. Add authConfig row. Remove `CronSchedule` import.

- [ ] **Step 9: Update CronjobPanel and DiunPanel display to show new fields**

Add `registryAuth` and `queryRegistry` rows to `CronjobPanel`. Add `regopt`, `hubLink`, `platform` rows to `DiunPanel`.

- [ ] **Step 10: Run tests and lint**

Run: `go test ./internal/integrations/ -v` — all pass
Run: `cd frontend && npx tsc -b --noEmit && npm run lint` — clean

- [ ] **Step 11: Commit**

```bash
git add internal/integrations/ frontend/src/api/types.ts \
  frontend/src/components/service-detail/ShepherdPanel.tsx \
  frontend/src/components/service-detail/CronjobPanel.tsx \
  frontend/src/components/service-detail/DiunPanel.tsx
git commit -m "fix: correct parser fields for Shepherd, Cronjob, and Diun integrations"
```

---

## Task 2: Label serialization and diff utilities

**Files:**
- Create: `frontend/src/lib/integrationLabels.ts`

- [ ] **Step 1: Create the shared utility module**

Create `frontend/src/lib/integrationLabels.ts` with:

```typescript
import type { PatchOp } from "@/api/types";

/**
 * Compute JSON Patch operations from label changes.
 * Compares new labels against the original raw labels for an integration.
 * Keys not present in newLabels are left untouched (passthrough).
 */
export function diffLabels(
  originalEntries: [string, string][],
  newLabels: Record<string, string>,
): PatchOp[] {
  const ops: PatchOp[] = [];
  const originalMap = Object.fromEntries(originalEntries);

  // Changed or added keys
  for (const [key, value] of Object.entries(newLabels)) {
    if (!(key in originalMap)) {
      ops.push({ op: "add", path: `/${key}`, value });
    } else if (originalMap[key] !== value) {
      ops.push({ op: "replace", path: `/${key}`, value });
    }
  }

  // Removed keys (in original but not in new)
  for (const [key] of originalEntries) {
    if (!(key in newLabels)) {
      ops.push({ op: "remove", path: `/${key}` });
    }
  }

  return ops;
}
```

Note: JSON Patch paths for label keys use `/` prefix. Label keys containing `~` or `/` need escaping per RFC 6901 (`~` → `~0`, `/` → `~1`), but Docker label keys don't use those characters, so we skip escaping.

- [ ] **Step 2: Type check**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/integrationLabels.ts
git commit -m "feat: add label serialization and diff utilities"
```

---

## Task 3: IntegrationSection edit mode

**Files:**
- Modify: `frontend/src/components/service-detail/IntegrationSection.tsx`

- [ ] **Step 1: Rewrite IntegrationSection with edit support**

The component gains:
- `editing` state (boolean)
- `saving` state (boolean)
- `saveError` state (string | null)
- `editable` prop (boolean, driven by operations level)
- `onSave` prop (callback returning `Promise<void>`)
- `onEditStart` prop (callback to reset form state)
- `editContent` prop (ReactNode for the structured edit form)
- `rawLabels` used for both display (KeyValuePills) and edit (KeyValueEditor)
- `serviceId` prop (for the raw KeyValueEditor save path)
- `onRawSave` prop (callback after raw save)

The structured/raw toggle is disabled while `editing` is true. The Edit button follows the same styling as `EditablePanel` (outline, xs, Pencil icon). Save/Cancel footer matches `EditablePanel`'s pattern.

States:
- display + structured → `children` (current behavior)
- display + raw → `KeyValuePills` (current behavior)
- edit + structured → `editContent`
- edit + raw → `KeyValueEditor` with integration's raw labels

The Edit button appears in the controls area next to the toggle and docs link. Save/Cancel appear as a footer below the form content.

Import `KeyValueEditor`, `Button`, `Pencil` icon, `Spinner`, `useEscapeCancel`, `showErrorToast`, `getErrorMessage`, `opsLevel`, `useOperationsLevel`.

- [ ] **Step 2: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/IntegrationSection.tsx
git commit -m "feat: add edit mode to IntegrationSection"
```

---

## Task 4: Shepherd edit form

**Files:**
- Modify: `frontend/src/components/service-detail/ShepherdPanel.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add edit form to ShepherdPanel**

ShepherdPanel gains:
- `serviceId` prop and `onSaved` callback prop
- Form state: `enabled` (boolean), `authConfig` (string)
- `resetForm()` — initializes form from `integration` prop
- `serializeToLabels()` — converts form state to `Record<string, string>`:
  - `"shepherd.enable"` → `String(enabled)`
  - `"shepherd.auth.config"` → `authConfig` (omit if empty)
- `handleSave()` — calls `diffLabels(rawLabels, serializeToLabels())` to get `PatchOp[]`, then `const updated = await api.patchServiceLabels(serviceId, ops)`, then `onSaved(updated)` to update `serviceLabels` state

Pass `editContent`, `onEditStart={resetForm}`, `onSave={handleSave}` to `IntegrationSection`.

Form layout: a simple vertical stack with a toggle for enabled and a text input for auth config. Use `Input` from `@/components/ui/input` and a checkbox/switch for the toggle.

- [ ] **Step 2: Wire serviceId and onSaved for ShepherdPanel in ServiceDetail.tsx**

In `ServiceDetail.tsx`, update only the Shepherd case in the integration dispatcher to pass `serviceId={id!}` and `onSaved` callback. The `onSaved` callback receives the updated labels `Record<string, string>` from `patchServiceLabels` and should call `setServiceLabels(updated)` to keep local state current (matching the existing labels editor pattern at line 528-532). The other panels (Cronjob, Diun, Traefik) will be wired in their respective tasks.

```typescript
case "shepherd":
  return (
    <ShepherdPanel
      key={integration.name}
      integration={integration}
      rawLabels={rawLabels}
      serviceId={id!}
      onSaved={setServiceLabels}
    />
  );
```

Each panel's `handleSave` calls `api.patchServiceLabels(serviceId, ops)` which returns the updated full labels map, then passes it to `onSaved`.

- [ ] **Step 3: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/ShepherdPanel.tsx \
  frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add Shepherd integration editing"
```

---

## Task 5: Cronjob edit form

**Files:**
- Modify: `frontend/src/components/service-detail/CronjobPanel.tsx`

- [ ] **Step 1: Add edit form to CronjobPanel**

Form state: `enabled`, `schedule`, `skipRunning`, `replicas`, `registryAuth`, `queryRegistry`.

`serializeToLabels()`:
- `"swarm.cronjob.enable"` → `String(enabled)`
- `"swarm.cronjob.schedule"` → `schedule` (omit if empty)
- `"swarm.cronjob.skip-running"` → `String(skipRunning)` (omit if false)
- `"swarm.cronjob.replicas"` → `String(replicas)` (omit if 0 or 1)
- `"swarm.cronjob.registry-auth"` → `String(registryAuth)` (omit if false)
- `"swarm.cronjob.query-registry"` → `String(queryRegistry)` (omit if false)

Validate cron expression using `cron-parser` — show inline error if invalid.

Form layout: toggle for enabled, text input for schedule (with cron validation), toggles for booleans, number input for replicas.

- [ ] **Step 2: Wire serviceId and onSaved for CronjobPanel in ServiceDetail.tsx**

Same pattern as Shepherd in Task 4 Step 2: pass `serviceId={id!}` and `onSaved={setServiceLabels}` to the `CronjobPanel` case in the dispatcher.

- [ ] **Step 3: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/CronjobPanel.tsx \
  frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add Swarm Cronjob integration editing"
```

---

## Task 6: Diun edit form

**Files:**
- Modify: `frontend/src/components/service-detail/DiunPanel.tsx`

- [ ] **Step 1: Add edit form to DiunPanel**

Form state: `enabled`, `regopt`, `watchRepo`, `notifyOn` (as `{new: boolean, update: boolean}`), `sortTags`, `maxTags`, `includeTags`, `excludeTags`, `hubLink`, `platform`, `metadata` (as `Record<string, string>`).

`serializeToLabels()`:
- `"diun.enable"` → `String(enabled)`
- `"diun.regopt"` → `regopt` (omit if empty)
- `"diun.watch_repo"` → `String(watchRepo)` (omit if false)
- `"diun.notify_on"` → join selected triggers with `;` (omit if empty)
- `"diun.sort_tags"` → `sortTags` (omit if empty)
- `"diun.max_tags"` → `String(maxTags)` (omit if 0)
- `"diun.include_tags"` → `includeTags` (omit if empty)
- `"diun.exclude_tags"` → `excludeTags` (omit if empty)
- `"diun.hub_link"` → `hubLink` (omit if empty)
- `"diun.platform"` → `platform` (omit if empty)
- `"diun.metadata.<key>"` → value for each metadata entry

Notify on: two toggles ("New image", "Updated tag") that map to semicolon-joined values.
Sort tags: a `<select>` with options: default, reverse, semver, lexicographical.
Metadata: inline `KeyValueEditor`-style add/edit/remove for `diun.metadata.*` keys.

- [ ] **Step 2: Wire serviceId and onSaved for DiunPanel in ServiceDetail.tsx**

Same pattern as Shepherd in Task 4 Step 2: pass `serviceId={id!}` and `onSaved={setServiceLabels}` to the `DiunPanel` case in the dispatcher.

- [ ] **Step 3: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/DiunPanel.tsx \
  frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add Diun integration editing"
```

---

## Task 7: Traefik edit form

**Files:**
- Modify: `frontend/src/components/service-detail/TraefikPanel.tsx`

- [ ] **Step 1: Add edit form to TraefikPanel**

In edit mode, each `RouterCard`, `ServiceRow`, and `MiddlewareRow` renders form inputs instead of display text:

**RouterCard edit:**
- Rule: `<Input>` (text)
- Entrypoints: `<Input>` (comma-separated text)
- Middlewares: `<Input>` (comma-separated text)
- Service: `<Input>` (text)
- Priority: `<Input type="number">`
- TLS cert resolver: `<Input>` (text)
- Name: displayed as read-only bold text

**ServiceRow edit:**
- Port: `<Input type="number">`
- Scheme: `<Input>` (text)
- Name: read-only

**MiddlewareRow edit:**
- Each config key: `<Input>` with key as label
- Name and type: read-only

Form state: deep copy of the integration's routers/services/middlewares arrays + enabled boolean.

`serializeToLabels()`:
- `"traefik.enable"` → `String(enabled)`
- For each router: `"traefik.http.routers.<name>.rule"` → value, etc.
- For each service: `"traefik.http.services.<name>.loadbalancer.server.port"` → String(port), etc.
- For each middleware: `"traefik.http.middlewares.<name>.<type>.<configKey>"` → value

The diff only produces ops for keys that changed relative to `rawLabels`. Unrecognized `traefik.*` labels (TCP/UDP) pass through unchanged because they are never included in the serialization output, so `diffLabels` never generates ops for them.

For fields the user clears (e.g., empties the priority input), the serializer should omit the key, causing `diffLabels` to generate a `remove` op if it previously existed.

- [ ] **Step 2: Wire serviceId and onSaved for TraefikPanel in ServiceDetail.tsx**

Same pattern as Shepherd in Task 4 Step 2: pass `serviceId={id!}` and `onSaved={setServiceLabels}` to the `TraefikPanel` case in the dispatcher.

- [ ] **Step 3: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/TraefikPanel.tsx \
  frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add Traefik integration editing"
```

---

## Task 8: Changelog and OpenAPI

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `api/openapi.yaml`

- [ ] **Step 1: Update changelog**

Add under `[Unreleased]` / `### Added`:

```markdown
- Edit integration settings (Traefik, Shepherd, Swarm Cronjob, Diun) inline on the service detail page
```

- [ ] **Step 2: Update OpenAPI schemas**

Update the Shepherd, Cronjob, and Diun schemas in `api/openapi.yaml` to reflect the added/removed fields.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md api/openapi.yaml
git commit -m "docs: add changelog and OpenAPI updates for integration editing"
```
