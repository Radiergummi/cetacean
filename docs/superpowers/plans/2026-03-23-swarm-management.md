# Swarm Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the /swarm page editable with per-section config editors, token rotation actions, and unlock key display.

**Architecture:** Backend adds `UpdateSwarm` and `GetUnlockKey` to the Docker client + system client interface, plus 8 new handlers (5 PATCH for spec sections, 2 POST for rotation, 1 GET for unlock key). Frontend adds per-section editors using EditablePanel (with a new `requiredLevel` prop), SwarmActions for token rotation, DurationInput for nanosecond fields, and unlock key display.

**Tech Stack:** Go (Docker Engine API), React 19, TypeScript, EditablePanel component

**Spec:** `docs/superpowers/specs/2026-03-23-swarm-management-design.md`

---

### Task 1: Docker Client — UpdateSwarm & GetUnlockKey

**Files:**
- Modify: `internal/docker/client.go`

- [ ] **Step 1: Add methods**

After `SwarmInspect` (around line 140), add:

```go
func (c *Client) UpdateSwarm(
	ctx context.Context,
	spec swarm.Spec,
	version swarm.Version,
	flags swarm.UpdateFlags,
) error {
	return c.docker.SwarmUpdate(ctx, version, spec, flags)
}

func (c *Client) GetUnlockKey(ctx context.Context) (string, error) {
	resp, err := c.docker.SwarmGetUnlockKey(ctx)
	if err != nil {
		return "", err
	}
	return resp.UnlockKey, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateSwarm and GetUnlockKey to Docker client"
```

---

### Task 2: DockerSystemClient Interface & Mock

**Files:**
- Modify: `internal/api/handlers.go`
- Create: `internal/api/swarm_handlers_test.go` (new test file for swarm handlers — keeps tests focused)

- [ ] **Step 1: Extend DockerSystemClient interface**

In `handlers.go`, add to the `DockerSystemClient` interface (after `LocalNodeID`):

```go
UpdateSwarm(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error
GetUnlockKey(ctx context.Context) (string, error)
```

- [ ] **Step 2: Create mockSystemClient**

Create `internal/api/swarm_handlers_test.go` with the mock and package declaration:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/config"
)

type mockSystemClient struct {
	swarmInspectFn func(ctx context.Context) (swarm.Swarm, error)
	diskUsageFn    func(ctx context.Context) (types.DiskUsage, error)
	pluginListFn   func(ctx context.Context) (types.PluginsListResponse, error)
	localNodeIDFn  func(ctx context.Context) (string, error)
	updateSwarmFn  func(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error
	getUnlockKeyFn func(ctx context.Context) (string, error)
}

func (m *mockSystemClient) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	if m.swarmInspectFn != nil {
		return m.swarmInspectFn(ctx)
	}
	return swarm.Swarm{}, fmt.Errorf("not implemented")
}

func (m *mockSystemClient) DiskUsage(ctx context.Context) (types.DiskUsage, error) {
	if m.diskUsageFn != nil {
		return m.diskUsageFn(ctx)
	}
	return types.DiskUsage{}, fmt.Errorf("not implemented")
}

func (m *mockSystemClient) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	if m.pluginListFn != nil {
		return m.pluginListFn(ctx)
	}
	return types.PluginsListResponse{}, fmt.Errorf("not implemented")
}

func (m *mockSystemClient) LocalNodeID(ctx context.Context) (string, error) {
	if m.localNodeIDFn != nil {
		return m.localNodeIDFn(ctx)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockSystemClient) UpdateSwarm(
	ctx context.Context,
	spec swarm.Spec,
	version swarm.Version,
	flags swarm.UpdateFlags,
) error {
	if m.updateSwarmFn != nil {
		return m.updateSwarmFn(ctx, spec, version, flags)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockSystemClient) GetUnlockKey(ctx context.Context) (string, error) {
	if m.getUnlockKeyFn != nil {
		return m.getUnlockKeyFn(ctx)
	}
	return "", fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/api/`

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers.go internal/api/swarm_handlers_test.go
git commit -m "feat: extend DockerSystemClient with UpdateSwarm and GetUnlockKey"
```

---

### Task 3: Swarm PATCH Handlers + Tests

**Files:**
- Modify: `internal/api/write_handlers.go` (or create `internal/api/swarm_handlers.go` — implementer's choice based on file size)
- Modify: `internal/api/swarm_handlers_test.go`

This task adds the 5 PATCH handlers plus 2 POST handlers plus 1 GET handler. All PATCH handlers follow the same pattern: SwarmInspect → merge patch into relevant spec section → SwarmUpdate. The implementer should read `HandlePatchSwarmOrchestration` as the reference pattern and replicate it for the others.

**IMPORTANT pattern for all PATCH handlers:**

```go
func (h *Handlers) HandlePatchSwarmOrchestration(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType,
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeProblem(w, r, http.StatusNotImplemented, "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "failed to inspect swarm")
		return
	}

	var patch swarm.OrchestrationConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	// Merge: only overwrite fields that were sent
	spec := current.Spec
	if patch.TaskHistoryRetentionLimit != nil {
		spec.Orchestration.TaskHistoryRetentionLimit = patch.TaskHistoryRetentionLimit
	}

	slog.Info("updating swarm orchestration config")

	if err := h.systemClient.UpdateSwarm(ctx, spec, current.Version, swarm.UpdateFlags{}); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"orchestration": spec.Orchestration})
}
```

Each PATCH handler follows this same structure — only the decode type and merge logic differ.

**Handlers to implement:**

1. `HandlePatchSwarmOrchestration` — merges `swarm.OrchestrationConfig` into `spec.Orchestration`
2. `HandlePatchSwarmRaft` — **CRITICAL: do NOT assign `spec.Raft = patch`**. `ElectionTick` and `HeartbeatTick` are non-pointer `int` fields — decoding zeroes them, and assigning would corrupt the raft cluster. Merge only the three mutable fields individually:
   ```go
   var patch swarm.RaftConfig
   // ... decode ...
   if patch.SnapshotInterval != 0 {
       spec.Raft.SnapshotInterval = patch.SnapshotInterval
   }
   if patch.KeepOldSnapshots != nil {
       spec.Raft.KeepOldSnapshots = patch.KeepOldSnapshots
   }
   if patch.LogEntriesForSlowFollowers != 0 {
       spec.Raft.LogEntriesForSlowFollowers = patch.LogEntriesForSlowFollowers
   }
   ```
3. `HandlePatchSwarmDispatcher` — merges `swarm.DispatcherConfig` into `spec.Dispatcher`
4. `HandlePatchSwarmCAConfig` — merges only `NodeCertExpiry` into `spec.CAConfig`. Ignores `ExternalCAs`/`ForceRotate`.
5. `HandlePatchSwarmEncryption` — merges `swarm.EncryptionConfig` into `spec.EncryptionConfig`
6. `HandlePostRotateToken` — decodes `{"target":"worker"|"manager"}`, calls `UpdateSwarm` with existing spec + appropriate `UpdateFlags` field set to `true`. Returns 204.
7. `HandlePostRotateUnlockKey` — calls `UpdateSwarm` with `UpdateFlags{RotateManagerUnlockKey: true}`. Returns 204.
8. `HandleGetUnlockKey` — calls `h.systemClient.GetUnlockKey(ctx)`, returns `{"unlockKey": "..."}`.

**Tests:** For each handler, write at minimum: success case, nil system client (501), and invalid content-type for PATCH (415). Follow the pattern from the mock in Task 2. Use `NewHandlers(nil, nil, nil, sc, nil, closedReady(), nil, config.OpsImpactful)` where `sc` is the mock.

- [ ] **Step 1: Write tests for all handlers**
- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleSwarm" -v`

- [ ] **Step 3: Implement all handlers**
- [ ] **Step 4: Run tests to verify they pass**
- [ ] **Step 5: Commit**

```bash
git add internal/api/write_handlers.go internal/api/swarm_handlers_test.go
git commit -m "feat: add swarm config PATCH, token rotation, and unlock key handlers"
```

---

### Task 4: Router Registration

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add routes**

After the `GET /swarm` line (around line 55), add:

```go
// Swarm write operations
mux.Handle("PATCH /swarm/orchestration", tier2(h.HandlePatchSwarmOrchestration))
mux.Handle("PATCH /swarm/raft", tier2(h.HandlePatchSwarmRaft))
mux.Handle("PATCH /swarm/dispatcher", tier2(h.HandlePatchSwarmDispatcher))
mux.Handle("PATCH /swarm/ca", tier3(h.HandlePatchSwarmCAConfig))
mux.Handle("PATCH /swarm/encryption", tier3(h.HandlePatchSwarmEncryption))
mux.Handle("POST /swarm/rotate-token", tier3(h.HandlePostRotateToken))
mux.Handle("POST /swarm/rotate-unlock-key", tier3(h.HandlePostRotateUnlockKey))
mux.HandleFunc("GET /swarm/unlock-key", h.HandleGetUnlockKey)
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -count=1`

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register swarm management routes"
```

---

### Task 5: Frontend API Client Methods

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add methods**

In the API object, add a swarm management section:

```ts
// Swarm management
patchSwarmOrchestration: (data: Record<string, unknown>) =>
  patch("/swarm/orchestration", data, "application/merge-patch+json"),
patchSwarmRaft: (data: Record<string, unknown>) =>
  patch("/swarm/raft", data, "application/merge-patch+json"),
patchSwarmDispatcher: (data: Record<string, unknown>) =>
  patch("/swarm/dispatcher", data, "application/merge-patch+json"),
patchSwarmCAConfig: (data: Record<string, unknown>) =>
  patch("/swarm/ca", data, "application/merge-patch+json"),
patchSwarmEncryption: (data: Record<string, unknown>) =>
  patch("/swarm/encryption", data, "application/merge-patch+json"),
rotateToken: (target: "worker" | "manager") =>
  mutationFetch<void>("/swarm/rotate-token", "POST", { target }, "application/json"),
rotateUnlockKey: () =>
  mutationFetch<void>("/swarm/rotate-unlock-key", "POST"),
unlockKey: () =>
  fetchJSON<{ unlockKey: string }>("/swarm/unlock-key"),
```

Note: `mutationFetch` is not currently exported. Either export it, or create inline wrappers. Check the current file — if `mutationFetch` is module-private, add `export` to its declaration.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add swarm management API client methods"
```

---

### Task 6: EditablePanel `requiredLevel` Prop

**Files:**
- Modify: `frontend/src/components/service-detail/EditablePanel.tsx`

- [ ] **Step 1: Add `requiredLevel` prop**

Add to the `EditablePanelProps` interface:

```ts
requiredLevel?: number;
```

Change the `canEdit` computation from:

```ts
const canEdit = !levelLoading && level >= opsLevel.configuration;
```

To:

```ts
const canEdit = !levelLoading && level >= (requiredLevel ?? opsLevel.configuration);
```

Destructure `requiredLevel` from props.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/EditablePanel.tsx
git commit -m "feat: add requiredLevel prop to EditablePanel"
```

---

### Task 7: DurationInput Component

**Files:**
- Create: `frontend/src/components/ui/duration-input.tsx`

A reusable component for editing durations stored as nanoseconds. Used by Dispatcher (heartbeat period) and CA (cert expiry) sections.

- [ ] **Step 1: Create component**

```tsx
import { Input } from "@/components/ui/input";
import { useState } from "react";

const units = [
  { label: "seconds", factor: 1_000_000_000 },
  { label: "minutes", factor: 60_000_000_000 },
  { label: "hours", factor: 3_600_000_000_000 },
  { label: "days", factor: 86_400_000_000_000 },
] as const;

interface DurationInputProps {
  value: number;
  onChange: (nanoseconds: number) => void;
  disabled?: boolean;
}

function bestUnit(nanoseconds: number): (typeof units)[number] {
  for (let index = units.length - 1; index > 0; index--) {
    if (nanoseconds >= units[index].factor && nanoseconds % units[index].factor === 0) {
      return units[index];
    }
  }

  return units[0];
}

export function DurationInput({ value, onChange, disabled }: DurationInputProps) {
  const initial = bestUnit(value);
  const [unit, setUnit] = useState(initial);
  const displayValue = value === 0 ? 0 : value / unit.factor;

  return (
    <div className="flex gap-2">
      <Input
        type="number"
        min={0}
        value={displayValue}
        onChange={(event) => {
          const number = Number(event.target.value) || 0;
          onChange(number * unit.factor);
        }}
        disabled={disabled}
        className="flex-1"
      />
      <select
        value={unit.label}
        onChange={(event) => {
          const next = units.find(({ label }) => label === event.target.value) ?? units[0];
          setUnit(next);
          const currentNumber = value / unit.factor;
          onChange(currentNumber ? currentNumber * next.factor : 0);
        }}
        disabled={disabled}
        className="flex h-8 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {units.map(({ label }) => (
          <option key={label} value={label}>
            {label}
          </option>
        ))}
      </select>
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/duration-input.tsx
git commit -m "feat: add DurationInput component for nanosecond duration editing"
```

---

### Task 8: SwarmActions Component (Token Rotation)

**Files:**
- Create: `frontend/src/components/swarm-detail/SwarmActions.tsx`

Follows ServiceActions/NodeActions pattern. Two confirmation dialogs for token rotation.

- [ ] **Step 1: Create component**

The component should:
- Import `api`, `useAsyncAction`, `useOperationsLevel`, `opsLevel`, `Button`, `Spinner`, `AlertDialog` parts
- Render two `ConfirmAction`-style buttons: "Rotate Worker Token" and "Rotate Manager Token"
- Each opens a confirmation dialog with a description of consequences
- On confirm: calls `api.rotateToken(target)` then `onRotated()` callback to trigger swarm data refetch
- Gated behind `opsLevel.impactful`
- Props: `onRotated: () => void`

- [ ] **Step 2: Verify TypeScript compiles**
- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/swarm-detail/SwarmActions.tsx
git commit -m "feat: add SwarmActions component for token rotation"
```

---

### Task 9: Refactor SwarmPage — Per-Section Editors

**Files:**
- Modify: `frontend/src/pages/SwarmPage.tsx`

This is the largest frontend task. Each of the 5 collapsible sections (Orchestration, Raft, Dispatcher, CA, Encryption) becomes editable using `EditablePanel`. The encryption section also gets unlock key display and rotation.

- [ ] **Step 1: Add imports**

Import `EditablePanel`, `DurationInput`, `SwarmActions`, `Input`, `useOperationsLevel`, `opsLevel`, `useAsyncAction`, `AlertDialog` parts, copy icon utilities.

- [ ] **Step 2: Add SwarmActions to page header**

Add `SwarmActions` alongside the existing JoinTokenDialog buttons in the `actions` prop of `PageHeader`. Pass `onRotated={fetchData}` so tokens refresh after rotation.

- [ ] **Step 3: Convert Orchestration section**

Replace the `CollapsibleSection` + `KVTable` for Orchestration with an `EditablePanel`. The `display` prop shows the current KVTable (read-only). The `edit` prop shows a number input for `TaskHistoryRetentionLimit`. `onSave` calls `api.patchSwarmOrchestration({ TaskHistoryRetentionLimit: value })` then `fetchData()`.

- [ ] **Step 4: Convert Raft section**

Same pattern. `edit` shows 3 number inputs (SnapshotInterval, LogEntriesForSlowFollowers, KeepOldSnapshots). ElectionTick and HeartbeatTick stay read-only in both display and edit modes.

- [ ] **Step 5: Convert Dispatcher section**

`edit` shows a `DurationInput` for HeartbeatPeriod.

- [ ] **Step 6: Convert CA section**

`edit` shows a `DurationInput` for NodeCertExpiry. External CAs stay read-only. Uses `requiredLevel={opsLevel.impactful}` on `EditablePanel`.

- [ ] **Step 7: Convert Encryption section**

`edit` shows a checkbox for AutoLockManagers. Uses `requiredLevel={opsLevel.impactful}`.

Add below the EditablePanel:
- "Show Unlock Key" button: fetches `api.unlockKey()`, displays in a monospace code block with copy-to-clipboard. Disabled when autolock is off.
- "Rotate Unlock Key" button: confirmation dialog, calls `api.rotateUnlockKey()`. Disabled when autolock is off. Gated behind `opsLevel.impactful`.

- [ ] **Step 8: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 9: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`

- [ ] **Step 10: Fix formatting**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run fmt`

- [ ] **Step 11: Commit**

```bash
git add frontend/src/pages/SwarmPage.tsx
git commit -m "feat: add per-section swarm config editors with token rotation and unlock key"
```

---

### Task 10: Full Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`

- [ ] **Step 2: Run frontend checks**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npm run lint && npm run fmt:check`

- [ ] **Step 3: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass
