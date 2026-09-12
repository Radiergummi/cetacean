# Swarm Management

## Summary

Make the /swarm page editable. Per-section edit controls for swarm configuration (orchestration, raft, dispatcher, CA, encryption). Token rotation as action buttons in the page header. Unlock key display and rotation in the encryption section.

## Decisions

- **Scope**: Config editing + token rotation + unlock key (full `docker swarm update` parity)
- **Edit UI**: Per-section edit buttons (matching existing app pattern, not a global edit mode)
- **Token rotation**: Action buttons in page header (cluster-wide actions)
- **Unlock key**: In encryption section (coupled to autolock setting)
- **External CAs**: Read-only for V1 (complex list of URL+protocol pairs; cert expiry is editable)
- **Non-editable raft fields**: ElectionTick and HeartbeatTick are set at swarm init and cannot be changed via the API — they stay read-only.

## Backend

### Docker Client (`internal/docker/client.go`)

Two new methods on `Client`:

**`UpdateSwarm(ctx, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error`**
- Calls `c.docker.SwarmUpdate(ctx, version, spec, flags)`.
- Thin wrapper — no inspect/re-inspect needed since the handlers do inspect→merge→update themselves.

**`GetUnlockKey(ctx) (string, error)`**
- Calls `c.docker.SwarmGetUnlockKey(ctx)`.
- Returns `response.UnlockKey`.

### DockerSystemClient Interface (`internal/api/handlers.go`)

The existing `DockerSystemClient` interface (used for swarm/cluster operations) needs two new methods:

```go
UpdateSwarm(ctx context.Context, spec swarm.Spec, version swarm.Version, flags swarm.UpdateFlags) error
GetUnlockKey(ctx context.Context) (string, error)
```

### Handlers

Each PATCH handler follows the same pattern:
1. Validate `Content-Type: application/merge-patch+json` (415 otherwise).
2. Call `h.systemClient.SwarmInspect(ctx)` to get the current swarm (includes `Version` for optimistic concurrency).
3. Decode the request body.
4. Merge the decoded fields into the relevant section of the existing `swarm.Spec`.
5. Call `h.systemClient.UpdateSwarm(ctx, mergedSpec, currentVersion, swarm.UpdateFlags{})`.
6. Return the updated section via `writeJSON`.

**Wire format note:** All PATCH bodies use **PascalCase** keys matching the Docker SDK's JSON serialization (e.g., `TaskHistoryRetentionLimit`, not `taskHistoryRetentionLimit`). Handlers decode directly into Docker SDK structs — no custom DTO layer needed.

**`HandlePatchSwarmOrchestration`** — `PATCH /swarm/orchestration`
- Body: `{"TaskHistoryRetentionLimit": 5}`
- Merges into `Spec.Orchestration`.

**`HandlePatchSwarmRaft`** — `PATCH /swarm/raft`
- Body: `{"SnapshotInterval": 10000, "LogEntriesForSlowFollowers": 500, "KeepOldSnapshots": 0}`
- Merges into `Spec.Raft`. Only these three fields are mutable — `ElectionTick` and `HeartbeatTick` are ignored.

**`HandlePatchSwarmDispatcher`** — `PATCH /swarm/dispatcher`
- Body: `{"HeartbeatPeriod": 5000000000}` (nanoseconds)
- Merges into `Spec.Dispatcher`.

**`HandlePatchSwarmCAConfig`** — `PATCH /swarm/ca`
- Body: `{"NodeCertExpiry": 7776000000000000}` (nanoseconds)
- Merges into `Spec.CAConfig`. Only `NodeCertExpiry` is editable in V1; `ExternalCAs` and `ForceRotate` are not accepted.

**`HandlePatchSwarmEncryption`** — `PATCH /swarm/encryption`
- Body: `{"AutoLockManagers": true}`
- Merges into `Spec.EncryptionConfig`.

**`HandlePostRotateToken`** — `POST /swarm/rotate-token`
- Body: `{"target": "worker"|"manager"}`
- Calls `UpdateSwarm` with the existing spec (unchanged) and `UpdateFlags{RotateWorkerToken: true}` or `RotateManagerToken: true`.
- Returns 204 No Content.

**`HandlePostRotateUnlockKey`** — `POST /swarm/rotate-unlock-key`
- No body.
- Calls `UpdateSwarm` with the existing spec and `UpdateFlags{RotateManagerUnlockKey: true}`.
- Returns 204 No Content.

**`HandleGetUnlockKey`** — `GET /swarm/unlock-key`
- Calls `h.systemClient.GetUnlockKey(ctx)`.
- Returns `{"unlockKey": "SWMKEY-..."}`.
- Returns empty string if autolock is not enabled (Docker API returns empty key in this case).

### Router (`internal/api/router.go`)

```
PATCH /swarm/orchestration      → tier2
PATCH /swarm/raft               → tier2
PATCH /swarm/dispatcher         → tier2
PATCH /swarm/ca                 → tier3
PATCH /swarm/encryption         → tier3
POST  /swarm/rotate-token       → tier3
POST  /swarm/rotate-unlock-key  → tier3
GET   /swarm/unlock-key         → contentNegotiated (no tier, read-only)
```

### Mock + Tests

Create a new `mockSystemClient` struct in the test file (no existing mock exists for the system client). It needs stubs for `SwarmInspect`, `UpdateSwarm`, and `GetUnlockKey`. Follow the same pattern as `mockWriteClient` (optional fn fields with fallback to `fmt.Errorf("not implemented")`).

Tests for each handler: success, SwarmInspect failure (503), invalid content-type (415 for PATCH), invalid body (400), Docker error mapping.

## Frontend

### API Client (`frontend/src/api/client.ts`)

```ts
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
unlockKey: () => fetchJSON<{ unlockKey: string }>("/swarm/unlock-key"),
```

### SwarmPage Editors

Each collapsible section becomes editable using `EditablePanel` (the existing shared component used by PolicyEditor, CommandEditor, etc.). Note: `EditablePanel` currently hardcodes `opsLevel.configuration` for the edit gate. CA and Encryption sections require `opsLevel.impactful` (tier 3), so `EditablePanel` needs a new optional `requiredLevel` prop (defaulting to `opsLevel.configuration` for backwards compatibility).

**Orchestration section:**
- Task history retention limit: number input.
- Save calls `api.patchSwarmOrchestration({ TaskHistoryRetentionLimit: value })`.

**Raft section:**
- Snapshot interval: number input.
- Log entries for slow followers: number input.
- Keep old snapshots: number input.
- ElectionTick and HeartbeatTick remain read-only (not editable via API).
- Save calls `api.patchSwarmRaft({ SnapshotInterval, LogEntriesForSlowFollowers, KeepOldSnapshots })`.

**Dispatcher section:**
- Heartbeat period: duration input (see below).
- Save calls `api.patchSwarmDispatcher({ HeartbeatPeriod: nanoseconds })`.

**CA section:**
- Cert expiry: duration input.
- External CAs: read-only list (V1).
- Save calls `api.patchSwarmCAConfig({ NodeCertExpiry: nanoseconds })`.

**Encryption section:**
- Autolock: checkbox.
- "Show Unlock Key" button: fetches `api.unlockKey()`, shows in a monospace code block with copy-to-clipboard. Disabled when autolock is off.
- "Rotate Unlock Key" button: confirmation dialog, calls `api.rotateUnlockKey()`. Disabled when autolock is off.
- Save (for autolock toggle) calls `api.patchSwarmEncryption({ AutoLockManagers: value })`.

### Duration Input

Several swarm fields are durations stored as nanoseconds (heartbeat period, cert expiry). The UI needs a human-friendly input:

- Number input + unit dropdown (`seconds`, `minutes`, `hours`, `days`).
- Converts to/from nanoseconds for the API.
- Display shows the current value in the most readable unit (e.g., `720h` → "30 days", `5000000000ns` → "5 seconds").
- This is a reusable component (`DurationInput`) since it appears in multiple sections.

### SwarmActions (Page Header)

Action buttons in the page header, following the ServiceActions/NodeActions pattern:

- "Rotate Worker Token" button → confirmation dialog ("This will invalidate the current worker join token. Existing workers are not affected.") → `api.rotateToken("worker")` → refetch swarm data.
- "Rotate Manager Token" button → confirmation dialog ("This will invalidate the current manager join token. Existing managers are not affected.") → `api.rotateToken("manager")` → refetch swarm data.

Both gated behind `opsLevel.impactful`.

### Operations Tiers

- Orchestration, Raft, Dispatcher editors: `opsLevel.configuration` (tier 2)
- CA, Encryption editors: `opsLevel.impactful` (tier 3)
- Token rotation buttons: `opsLevel.impactful` (tier 3)
- Unlock key display: no tier (read-only)
