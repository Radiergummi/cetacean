# Task State Timeline — Design

## Problem

When debugging Docker Swarm deployment issues, operators need to answer two questions:

1. **Per-task**: Where did this specific task attempt get stuck? (e.g., "spent 2 minutes in `preparing` before failing with an image pull error")
2. **Per-slot**: Why did this slot retry multiple times? (e.g., "slot 3 failed 4 times before succeeding")

Docker only stores the current state per task — no transition history. And when a task is rescheduled, the old task object remains with a terminal state, but there's no explicit grouping by slot.

## Design

### 1. Per-task state history (backend)

New type in `internal/cache/`:

```go
type StateTransition struct {
    State     string    `json:"state"`
    Timestamp time.Time `json:"timestamp"`
    Message   string    `json:"message,omitempty"`
    Err       string    `json:"err,omitempty"`
}
```

Storage: `map[string][]StateTransition` in the cache, keyed by task ID, alongside the existing task map. Protected by the same `sync.RWMutex`.

In `SetTask`: compare incoming task's `Status.State` against the last recorded transition for that task ID. If different (or no history exists), append a new entry. Cap at 20 entries per task. When a task is deleted from the cache, its transitions are also removed.

This is in-memory only — lost on restart. Acceptable because in-flight tasks resolve quickly and historical tasks are in terminal states (no transitions to record).

### 2. Version chain — slot grouping (backend)

New cache method:

```go
func (c *Cache) TasksBySlot(serviceID string, slot int) []swarm.Task
```

Filters the task map by ServiceID + Slot, sorted by `Version.Index` descending. For global-mode services (where Slot is 0), groups by ServiceID + NodeID instead.

No new storage — this queries the existing task map.

### 3. API surface

No new endpoints. Extend the existing task detail response:

```go
type EnrichedTask struct {
    swarm.Task
    ServiceName      string            `json:"ServiceName,omitempty"`
    NodeHostname     string            `json:"NodeHostname,omitempty"`
    StateHistory     []StateTransition `json:"StateHistory,omitempty"`
    PreviousAttempts []EnrichedTask    `json:"PreviousAttempts,omitempty"`
}
```

- `StateHistory`: populated from the per-task transition map. Only present on detail endpoint, not list.
- `PreviousAttempts`: populated via `TasksBySlot`, excluding the current task, capped at 10, enriched with ServiceName/NodeHostname. Only present on detail endpoint.

### 4. Task detail page (frontend)

Two new sections added to `TaskDetail.tsx`:

**State Timeline** — vertical timeline reusing the visual pattern from `ActivityFeed` (left border line, colored dots, rows). Each row shows:
- Colored dot: green for running/complete, red for failed/rejected/shutdown, yellow for intermediate states
- State name (bold)
- Timestamp (absolute + relative via `TimeAgo`)
- Duration since previous state (e.g., "after 2m 13s")
- Message and/or error text, if present

Most recent state at top. Wrapped in a `CollapsibleSection` titled "State Timeline". If `StateHistory` is empty or has only one entry, falls back to showing the current `Status` fields (same as today).

**Previous Attempts** — a `SimpleTable` listing sibling tasks for this slot. Columns:
- Task (linked to `/tasks/{id}`, with status dot)
- State
- Error (exit code or error message)
- Timestamp

Only rendered if `PreviousAttempts` is non-empty. Wrapped in a `CollapsibleSection` titled "Previous Attempts".

### 5. Service detail — slot retry indicator

In `TasksTable` (service variant), when a task has sibling tasks for the same slot:
- Show a small badge on the row (e.g., "×3") indicating total attempts
- Clicking the badge (or a disclosure chevron) expands an inline sub-table showing the retry history for that slot
- Sub-table columns match the main table

This requires the service tasks endpoint to include slot grouping info. Add an `AttemptCount` field to `EnrichedTask` on service task list responses — a simple count, not the full previous attempts list.

### 6. Frontend types

```typescript
interface StateTransition {
  state: string;
  timestamp: string;
  message?: string;
  err?: string;
}

// Extend existing Task type
interface Task {
  // ... existing fields ...
  StateHistory?: StateTransition[];
  PreviousAttempts?: Task[];
  AttemptCount?: number;
}
```

### 7. What we're not doing

- No persistent storage for state transitions
- No changes to SSE event format
- No new API endpoints
- No changes to the global history ring buffer
- No timeline visualization for the version chain (simple table is sufficient)
