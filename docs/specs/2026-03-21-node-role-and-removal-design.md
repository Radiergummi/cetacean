# Node Role Change & Node Removal

## Summary

Add two new write operations to the node detail page: role change (promote/demote) and node removal. Role change is an editable InfoCard with radio cards in a popover. Node removal is an action button that opens a type-to-confirm alert dialog.

## Decisions

- **Placement**: Role editor replaces the static Role InfoCard in the metadata grid. Remove button lives in a NodeActions bar after the PageHeader.
- **Force removal**: Not supported. Remove button disabled when node status ≠ `down`.
- **Leader demotion**: Allowed with an extra warning about Raft leader re-election.
- **Quorum math**: Shown when demoting a manager — displays current manager count, remaining count after demotion, and quorum threshold.
- **Operations tier**: Both operations are tier 3 (impactful).

## Backend

### Docker Client (`internal/docker/client.go`)

Two new methods on `Client`:

**`UpdateNodeRole(ctx, id, role string) (*swarm.Node, error)`**
- Calls `NodeInspectWithRaw` to get current version.
- Sets `node.Spec.Role = swarm.NodeRole(role)`.
- Calls `NodeUpdate` with the current version.
- Returns the updated node via a final `NodeInspectWithRaw`.
- Same inspect→mutate→update→inspect pattern as `UpdateNodeAvailability`.

**`RemoveNode(ctx, id string) error`**
- Calls `NodeRemove(ctx, id, swarm.NodeRemoveOptions{Force: false})`.

### DockerWriteClient Interface (`internal/api/write_handlers.go`)

Add two methods:
```go
UpdateNodeRole(ctx context.Context, id string, role string) (*swarm.Node, error)
RemoveNode(ctx context.Context, id string) error
```

### Handlers (`internal/api/write_handlers.go`)

**`HandleUpdateNodeRole`** — `PUT /nodes/{id}/role`
- Decodes body: `{"role": "worker"|"manager"}`.
- Validates role with a switch (400 for invalid values).
- Checks cache for node existence (404).
- Logs the operation.
- Calls `writeClient.UpdateNodeRole`.
- Maps Docker errors via `writeDockerError` (conflict → 409, not-found → 404).
- Returns updated node wrapped in `NewDetailResponse`.

**`HandleRemoveNode`** — `DELETE /nodes/{id}`
- Checks cache for node existence (404).
- Logs the operation.
- Calls `writeClient.RemoveNode`.
- Maps Docker errors via `writeDockerError`. Docker itself rejects removing non-down nodes, so no server-side state check needed.
- Returns 204 No Content.

### Read Companion (`internal/api/write_handlers.go`)

**`HandleGetNodeRole`** — `GET /nodes/{id}/role`
- Returns `{"role": "manager", "isLeader": true, "managerCount": 3}`.
- Reads from cache: node's `Spec.Role` and `ManagerStatus.Leader` for the target node, iterates `cache.ListNodes()` to count nodes with `Spec.Role == "manager"`.
- No operations tier gating (read-only).

### Router (`internal/api/router.go`)

```
GET  /nodes/{id}/role  → contentNegotiated(HandleGetNodeRole, spa) (no tier)
PUT  /nodes/{id}/role  → tier3(HandleUpdateNodeRole)
DELETE /nodes/{id}     → tier3(HandleRemoveNode)
```

## Frontend

### API Client (`frontend/src/api/client.ts`)

```ts
nodeRole(id: string, signal?: AbortSignal): Promise<{ role: string; isLeader: boolean; managerCount: number }>
updateNodeRole(id: string, role: string): Promise<Node>
removeNode(id: string): Promise<void>
```

### RoleEditor Component (`frontend/src/components/node-detail/RoleEditor.tsx`)

Follows the `AvailabilityEditor` pattern: InfoCard with pencil button → Popover with radio cards.

**Props**: `nodeId: string`, `currentRole: string`, `isLeader: boolean`, `managerCount: number`

**Closed state**: InfoCard showing current role. If manager and leader, shows "Leader" badge (preserving current display). Pencil edit button triggers popover.

**Open state (popover)**:
- Title: "Change Role"
- Two `RadioCard` components (uses the existing `RadioCard` from `components/ui/radio-card.tsx`):
  - **Worker**: "Runs tasks. Cannot participate in Raft consensus or manage the cluster."
  - **Manager**: "Participates in Raft consensus. Can manage nodes, services, and other cluster resources."
- Current role card shown with "(current)" suffix, visually dimmed, not selectable.
- Cancel and Apply buttons. Apply calls `api.updateNodeRole`.

**Warning box** (amber, appears only when selecting worker while current role is manager):
- If leader: "This node is the Raft leader. Demoting it will trigger a leader re-election."
- Always when demoting: "This cluster has N managers. Demoting this node leaves N-1 managers (quorum requires ⌈N/2⌉)." If N-1 equals quorum exactly: "Losing one more manager will make the cluster unrecoverable."

**State management**: `useAsyncAction` for loading/error. Popover closes on success.

**Gating**: Hidden when `operationsLevel < opsLevel.impactful`. Pencil button not rendered.

### NodeActions Component (`frontend/src/components/node-detail/NodeActions.tsx`)

Uses a controlled `AlertDialog` (unlike `ServiceActions` which is uncontrolled) because the type-to-confirm input requires tracking internal state to gate the action button.

**Props**: `node: Node`, `nodeId: string`

**Remove button**:
- `Button` with `variant="outline"`, destructive styling, `Trash2` icon.
- Disabled with tooltip ("Node must be in down state to remove") when `node.Status.State !== "down"`.
- Opens a controlled `AlertDialog` (`open`/`onOpenChange` state, no `AlertDialogTrigger`).

**AlertDialog content**:
- Title: "Remove node?"
- Description: "This will permanently remove **{hostname}** from the swarm. The node will no longer appear in the cluster and cannot rejoin without being re-initialized. Any node-specific labels and configuration will be lost."
- Text input: label "Type **{hostname}** to confirm", monospace font.
- Remove button (`AlertDialogAction`, `variant="destructive"`): disabled until input matches hostname exactly (case-sensitive).
- On success: `navigate("/nodes", { replace: true })`.

**Gating**: Entire component hidden when `operationsLevel < opsLevel.impactful`.

### NodeDetail.tsx Changes

- Import `RoleEditor` and `NodeActions`.
- Add state for `nodeRole` data (`api.nodeRole(id)` in `fetchData`).
- Replace static `<InfoCard label="Role" ...>` with `<RoleEditor nodeId={node.ID} currentRole={role} isLeader={isLeader} managerCount={managerCount} />`.
- Add `<NodeActions node={node} nodeId={node.ID} />` between `<PageHeader>` and `<MetadataGrid>`.

### Index Export

Add `RoleEditor` to `frontend/src/components/node-detail/index.ts` barrel export.

## Testing

### Backend
- Extend `mockWriteClient` in `write_handlers_test.go` with `updateNodeRoleFn` and `removeNodeFn` fields + stub methods to satisfy the updated `DockerWriteClient` interface.
- Unit tests for `HandleUpdateNodeRole`: valid role, invalid role (400), not found (404), conflict (409).
- Unit tests for `HandleRemoveNode`: success (204), not found (404), Docker rejection of non-down node (mapped to appropriate error).
- Unit tests for `HandleGetNodeRole`: returns role, leader status, and manager count.

### Frontend
- `RoleEditor`: renders current role, opens popover, shows warning when demoting manager, shows leader warning, disables current role card.
- `NodeActions`: remove button disabled when node not down, enabled when down, dialog requires matching hostname, fires API call on confirm.
