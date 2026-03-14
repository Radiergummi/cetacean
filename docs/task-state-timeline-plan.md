# Task State Timeline Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-task state transition tracking and slot-based version chain display to help operators debug task scheduling failures and service convergence issues.

**Architecture:** Per-task state transitions are stored in a `map[string][]StateTransition` in the cache, appended in `SetTask` when the state changes. Slot grouping queries the existing task map by (ServiceID, Slot). Both are exposed on the existing task detail endpoint as `StateHistory` and `PreviousAttempts` fields. Frontend adds two new sections to TaskDetail and an expandable retry indicator to TasksTable.

**Tech Stack:** Go (cache + handlers), React/TypeScript (components), existing test patterns (table-driven Go tests, httptest)

**Spec:** `docs/task-state-timeline-design.md`

---

## Chunk 1: Backend — State Transitions + Slot Grouping

### Task 1: StateTransition type and cache storage

**Files:**
- Modify: `internal/cache/cache.go:12-17` (add type), `internal/cache/cache.go:73-94` (add field to Cache), `internal/cache/cache.go:96-115` (init in New)
- Test: `internal/cache/cache_test.go`

- [ ] **Step 1: Write failing test for state transition recording**

In `internal/cache/cache_test.go`, add:

```go
func TestCache_SetTask_StateTransitions(t *testing.T) {
	c := New(nil)

	// First set — creates initial transition
	t1 := swarm.Task{ID: "task1"}
	t1.ServiceID = "svc1"
	t1.Status.State = swarm.TaskStateNew
	t1.Status.Timestamp = time.Now()
	c.SetTask(t1)

	transitions := c.TaskStateHistory("task1")
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].State != string(swarm.TaskStateNew) {
		t.Errorf("expected state 'new', got %q", transitions[0].State)
	}

	// Second set with different state — appends
	t1.Status.State = swarm.TaskStatePreparing
	t1.Status.Timestamp = time.Now()
	c.SetTask(t1)

	transitions = c.TaskStateHistory("task1")
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[1].State != string(swarm.TaskStatePreparing) {
		t.Errorf("expected state 'preparing', got %q", transitions[1].State)
	}

	// Third set with same state — does not append
	c.SetTask(t1)
	transitions = c.TaskStateHistory("task1")
	if len(transitions) != 2 {
		t.Fatalf("expected still 2 transitions, got %d", len(transitions))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -run TestCache_SetTask_StateTransitions -v`
Expected: compile error — `StateTransition` type and `TaskStateHistory` method don't exist.

- [ ] **Step 3: Add StateTransition type, storage map, and recording logic**

In `internal/cache/cache.go`, add the type after the `Event` struct (after line 17):

```go
// StateTransition records a task's state at a point in time.
type StateTransition struct {
	State     string    `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
	Err       string    `json:"err,omitempty"`
}

const maxStateTransitions = 20
```

Add field to the `Cache` struct (after `tasksByNode` on line 79):

```go
	taskStateHistory map[string][]StateTransition // taskID -> state transitions
```

Initialize in `New` (alongside other map inits around line 100):

```go
	taskStateHistory: make(map[string][]StateTransition),
```

In `SetTask` (line 293), add transition recording inside the lock, after `c.tasks[t.ID] = t` and before `c.addTaskIndex(t)`:

```go
func (c *Cache) SetTask(t swarm.Task) {
	c.mu.Lock()
	changed := true
	if old, ok := c.tasks[t.ID]; ok {
		changed = old.Status.State != t.Status.State ||
			old.DesiredState != t.DesiredState ||
			old.Status.Err != t.Status.Err ||
			old.NodeID != t.NodeID ||
			old.Version != t.Version
		c.removeTaskIndex(old)
	}
	c.tasks[t.ID] = t
	c.addTaskIndex(t)
	c.recordStateTransition(t)
	c.mu.Unlock()
	if changed {
		c.notify(Event{Type: "task", Action: "update", ID: t.ID, Resource: t})
	}
}
```

Add the recording helper and query method:

```go
// recordStateTransition appends a transition if the state changed.
// Must be called with c.mu held for writing.
func (c *Cache) recordStateTransition(t swarm.Task) {
	history := c.taskStateHistory[t.ID]
	state := string(t.Status.State)
	if len(history) > 0 && history[len(history)-1].State == state {
		return
	}
	entry := StateTransition{
		State:     state,
		Timestamp: t.Status.Timestamp,
		Message:   t.Status.Message,
		Err:       t.Status.Err,
	}
	if len(history) >= maxStateTransitions {
		history = history[1:]
	}
	c.taskStateHistory[t.ID] = append(history, entry)
}

// TaskStateHistory returns the recorded state transitions for a task.
func (c *Cache) TaskStateHistory(taskID string) []StateTransition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h := c.taskStateHistory[taskID]
	if len(h) == 0 {
		return nil
	}
	out := make([]StateTransition, len(h))
	copy(out, h)
	return out
}
```

In `DeleteTask` (line 319), clean up history inside the lock:

```go
func (c *Cache) DeleteTask(id string) {
	c.mu.Lock()
	if old, ok := c.tasks[id]; ok {
		c.removeTaskIndex(old)
	}
	delete(c.tasks, id)
	delete(c.taskStateHistory, id)
	c.mu.Unlock()
	c.notify(Event{Type: "task", Action: "remove", ID: id})
}
```

In `replaceTasks` (line 904), reset the history map:

```go
func (c *Cache) replaceTasks(tasks []swarm.Task) {
	// ... existing map building code ...
	c.mu.Lock()
	c.tasks = m
	c.tasksByService = byService
	c.tasksByNode = byNode
	c.taskStateHistory = make(map[string][]StateTransition)
	c.mu.Unlock()
}
```

In `ReplaceAll` (line 1078), also reset when tasks are replaced:

```go
	if data.HasTasks {
		c.tasks = tasks
		c.tasksByService = byService
		c.tasksByNode = byNode
		c.taskStateHistory = make(map[string][]StateTransition)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/ -run TestCache_SetTask_StateTransitions -v`
Expected: PASS

- [ ] **Step 5: Write test for transition cap and delete cleanup**

```go
func TestCache_SetTask_StateTransitionsCap(t *testing.T) {
	c := New(nil)
	task := swarm.Task{ID: "task1"}

	// Push 25 different states (exceeds cap of 20)
	states := []swarm.TaskState{
		swarm.TaskStateNew, swarm.TaskStatePending, swarm.TaskStateAssigned,
		swarm.TaskStateAccepted, swarm.TaskStatePreparing, swarm.TaskStateReady,
		swarm.TaskStateStarting, swarm.TaskStateRunning, swarm.TaskStateComplete,
		swarm.TaskStateFailed, swarm.TaskStateNew, swarm.TaskStatePending,
		swarm.TaskStateAssigned, swarm.TaskStateAccepted, swarm.TaskStatePreparing,
		swarm.TaskStateReady, swarm.TaskStateStarting, swarm.TaskStateRunning,
		swarm.TaskStateComplete, swarm.TaskStateFailed, swarm.TaskStateNew,
		swarm.TaskStatePending, swarm.TaskStateAssigned, swarm.TaskStateAccepted,
		swarm.TaskStatePreparing,
	}
	for _, s := range states {
		task.Status.State = s
		c.SetTask(task)
	}

	transitions := c.TaskStateHistory("task1")
	if len(transitions) > maxStateTransitions {
		t.Errorf("expected at most %d transitions, got %d", maxStateTransitions, len(transitions))
	}
}

func TestCache_DeleteTask_CleansHistory(t *testing.T) {
	c := New(nil)
	task := swarm.Task{ID: "task1"}
	task.Status.State = swarm.TaskStateRunning
	c.SetTask(task)

	c.DeleteTask("task1")

	transitions := c.TaskStateHistory("task1")
	if len(transitions) != 0 {
		t.Errorf("expected no transitions after delete, got %d", len(transitions))
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cache/ -run TestCache_SetTask_StateTransition -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "feat: per-task state transition tracking in cache"
```

---

### Task 2: Slot-based task grouping

**Files:**
- Modify: `internal/cache/cache.go` (add method)
- Test: `internal/cache/cache_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestCache_TasksBySlot(t *testing.T) {
	c := New(nil)

	// Three tasks for the same service/slot (simulating retries)
	for i, state := range []swarm.TaskState{swarm.TaskStateFailed, swarm.TaskStateFailed, swarm.TaskStateRunning} {
		task := swarm.Task{ID: fmt.Sprintf("task%d", i)}
		task.ServiceID = "svc1"
		task.Slot = 1
		task.Status.State = state
		task.Meta.Version.Index = uint64(i + 1)
		c.SetTask(task)
	}

	// Different slot
	other := swarm.Task{ID: "task-other"}
	other.ServiceID = "svc1"
	other.Slot = 2
	other.Status.State = swarm.TaskStateRunning
	c.SetTask(other)

	got := c.TasksBySlot("svc1", 1)
	if len(got) != 3 {
		t.Fatalf("expected 3 tasks for slot 1, got %d", len(got))
	}
	// Should be sorted by Version.Index descending (newest first)
	if got[0].ID != "task2" {
		t.Errorf("expected newest task first, got %s", got[0].ID)
	}
}

func TestCache_TasksBySlot_GlobalService(t *testing.T) {
	c := New(nil)

	// Global tasks have Slot=0, grouped by NodeID instead
	for i, nodeID := range []string{"node1", "node1", "node2"} {
		task := swarm.Task{ID: fmt.Sprintf("gtask%d", i)}
		task.ServiceID = "global-svc"
		task.Slot = 0
		task.NodeID = nodeID
		task.Meta.Version.Index = uint64(i + 1)
		c.SetTask(task)
	}

	got := c.TasksBySlot("global-svc", 0, "node1")
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks for global svc on node1, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -run TestCache_TasksBySlot -v`
Expected: compile error — `TasksBySlot` method doesn't exist.

- [ ] **Step 3: Implement TasksBySlot**

Add to `internal/cache/cache.go` after the `ListTasksByNode` method:

```go
// TasksBySlot returns tasks for a given service and slot, sorted by Version.Index descending.
// For global services (slot=0), pass the nodeID to group by node instead.
func (c *Cache) TasksBySlot(serviceID string, slot int, nodeID ...string) []swarm.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := c.tasksByService[serviceID]
	var out []swarm.Task
	for id := range ids {
		t, ok := c.tasks[id]
		if !ok {
			continue
		}
		if slot == 0 && len(nodeID) > 0 {
			// Global service: match by nodeID
			if t.NodeID == nodeID[0] {
				out = append(out, t)
			}
		} else if t.Slot == slot {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b swarm.Task) int {
		return int(b.Meta.Version.Index) - int(a.Meta.Version.Index)
	})
	return out
}
```

(Add `"slices"` to the import block if not already present.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/ -run TestCache_TasksBySlot -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "feat: slot-based task grouping for version chain"
```

---

### Task 3: Expose StateHistory and PreviousAttempts on task detail API

**Files:**
- Modify: `internal/api/handlers.go:692-766` (EnrichedTask, enrichTask, HandleGetTask)
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing test for StateHistory in task detail response**

In `internal/api/handlers_test.go`, add:

```go
func TestHandleGetTask_StateHistory(t *testing.T) {
	c := cache.New(nil)

	// Create a task that transitions through states
	task := swarm.Task{ID: "task1"}
	task.ServiceID = "svc1"
	task.NodeID = "node1"
	task.Slot = 1
	task.Status.State = swarm.TaskStateNew
	task.Status.Timestamp = time.Now().Add(-10 * time.Second)
	c.SetTask(task)

	task.Status.State = swarm.TaskStatePreparing
	task.Status.Timestamp = time.Now().Add(-5 * time.Second)
	c.SetTask(task)

	task.Status.State = swarm.TaskStateRunning
	task.Status.Timestamp = time.Now()
	c.SetTask(task)

	h := NewHandlers(c, nil, nil, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/tasks/task1", nil)
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("id", "task1")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	json.NewDecoder(w.Body).Decode(&body)

	var taskData map[string]json.RawMessage
	json.Unmarshal(body["task"], &taskData)

	var history []map[string]any
	if err := json.Unmarshal(taskData["StateHistory"], &history); err != nil {
		t.Fatalf("failed to decode StateHistory: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 state transitions, got %d", len(history))
	}
}

func TestHandleGetTask_PreviousAttempts(t *testing.T) {
	c := cache.New(nil)

	// Older failed task for same slot
	old := swarm.Task{ID: "task-old"}
	old.ServiceID = "svc1"
	old.Slot = 1
	old.Status.State = swarm.TaskStateFailed
	old.Meta.Version.Index = 1
	c.SetTask(old)

	// Current running task for same slot
	current := swarm.Task{ID: "task-current"}
	current.ServiceID = "svc1"
	current.Slot = 1
	current.Status.State = swarm.TaskStateRunning
	current.Meta.Version.Index = 2
	c.SetTask(current)

	h := NewHandlers(c, nil, nil, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/tasks/task-current", nil)
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("id", "task-current")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	json.NewDecoder(w.Body).Decode(&body)

	var taskData map[string]json.RawMessage
	json.Unmarshal(body["task"], &taskData)

	var attempts []map[string]any
	if err := json.Unmarshal(taskData["PreviousAttempts"], &attempts); err != nil {
		t.Fatalf("failed to decode PreviousAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Errorf("expected 1 previous attempt, got %d", len(attempts))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleGetTask_State -v`
Expected: FAIL — response doesn't contain `StateHistory` or `PreviousAttempts`.

- [ ] **Step 3: Add fields to EnrichedTask and populate in enrichTask/HandleGetTask**

In `internal/api/handlers.go`, update `EnrichedTask`:

```go
type EnrichedTask struct {
	swarm.Task
	ServiceName      string                 `json:"ServiceName,omitempty"`
	NodeHostname     string                 `json:"NodeHostname,omitempty"`
	StateHistory     []cache.StateTransition `json:"StateHistory,omitempty"`
	PreviousAttempts []EnrichedTask         `json:"PreviousAttempts,omitempty"`
	AttemptCount     int                    `json:"AttemptCount,omitempty"`
}
```

Add a new method for detail-level enrichment (the existing `enrichTask` stays lean for list endpoints):

```go
func (h *Handlers) enrichTaskDetail(t swarm.Task) EnrichedTask {
	et := h.enrichTask(t)
	et.StateHistory = h.cache.TaskStateHistory(t.ID)

	// Slot siblings (version chain)
	siblings := h.cache.TasksBySlot(t.ServiceID, t.Slot)
	for _, s := range siblings {
		if s.ID != t.ID {
			et.PreviousAttempts = append(et.PreviousAttempts, h.enrichTask(s))
		}
	}
	if len(et.PreviousAttempts) > 10 {
		et.PreviousAttempts = et.PreviousAttempts[:10]
	}
	return et
}
```

Update `HandleGetTask` to use `enrichTaskDetail`:

```go
func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := h.cache.GetTask(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
		return
	}
	et := h.enrichTaskDetail(task)
	writeJSONWithETag(w, r, NewDetailResponse("/tasks/"+id, "Task", map[string]any{
		"task":    et,
		"service": map[string]any{"@id": "/services/" + et.ServiceID, "name": et.ServiceName},
		"node":    map[string]any{"@id": "/nodes/" + et.NodeID, "hostname": et.NodeHostname},
	}))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestHandleGetTask -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: expose state history and previous attempts on task detail"
```

---

### Task 4: Add AttemptCount to service task list

**Files:**
- Modify: `internal/api/handlers.go:667-676` (HandleServiceTasks)
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestHandleServiceTasks_AttemptCount(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	c.SetService(svc)

	// Two tasks for slot 1 (one failed, one running)
	t1 := swarm.Task{ID: "old"}
	t1.ServiceID = "svc1"
	t1.Slot = 1
	t1.Status.State = swarm.TaskStateFailed
	t1.Meta.Version.Index = 1
	c.SetTask(t1)

	t2 := swarm.Task{ID: "current"}
	t2.ServiceID = "svc1"
	t2.Slot = 1
	t2.Status.State = swarm.TaskStateRunning
	t2.Meta.Version.Index = 2
	c.SetTask(t2)

	h := NewHandlers(c, nil, nil, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/services/svc1/tasks", nil)
	req.Header.Set("Accept", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	var body struct{ Items []EnrichedTask }
	json.NewDecoder(w.Body).Decode(&body)

	for _, task := range body.Items {
		if task.Slot == 1 && task.AttemptCount != 2 {
			t.Errorf("task %s: expected AttemptCount=2, got %d", task.ID, task.AttemptCount)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHandleServiceTasks_AttemptCount -v`
Expected: FAIL — AttemptCount is 0.

- [ ] **Step 3: Add AttemptCount to service task enrichment**

Update `HandleServiceTasks` in `internal/api/handlers.go`:

```go
func (h *Handlers) HandleServiceTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, fmt.Sprintf("service %q not found", id))
		return
	}
	tasks := h.enrichTasks(h.cache.ListTasksByService(id))
	// Add attempt counts per slot
	slotCounts := make(map[int]int)
	for _, t := range tasks {
		slotCounts[t.Slot]++
	}
	for i := range tasks {
		if c := slotCounts[tasks[i].Slot]; c > 1 {
			tasks[i].AttemptCount = c
		}
	}
	writeJSONWithETag(w, r, NewCollectionResponse(tasks, len(tasks), len(tasks), 0))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestHandleServiceTasks_AttemptCount -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: add AttemptCount to service task list response"
```

---

## Chunk 2: Frontend — Types, Components, and Integration

### Task 5: Update frontend types and API client

**Files:**
- Modify: `frontend/src/api/types.ts:143-167`
- Modify: `frontend/src/api/client.ts:160`

- [ ] **Step 1: Add StateTransition interface and extend Task type**

In `frontend/src/api/types.ts`, add before the `Task` interface:

```typescript
export interface StateTransition {
  state: string;
  timestamp: string;
  message?: string;
  err?: string;
}
```

Extend the `Task` interface — add after the `Spec` field:

```typescript
  StateHistory?: StateTransition[];
  PreviousAttempts?: Task[];
  AttemptCount?: number;
```

- [ ] **Step 2: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/types.ts
git commit -m "feat: add StateTransition type and extend Task type"
```

---

### Task 6: TaskStateTimeline component

**Files:**
- Create: `frontend/src/components/TaskStateTimeline.tsx`

- [ ] **Step 1: Create the component**

```tsx
import type { StateTransition } from "../api/types";
import { statusColor } from "../lib/statusColor";
import CollapsibleSection from "./CollapsibleSection";
import TimeAgo from "./TimeAgo";

function transitionDuration(current: string, previous?: string): string | null {
  if (!previous) return null;
  const ms = new Date(current).getTime() - new Date(previous).getTime();
  if (ms < 0 || !Number.isFinite(ms)) return null;
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remaining = seconds % 60;
  return remaining > 0 ? `${minutes}m ${remaining}s` : `${minutes}m`;
}

export default function TaskStateTimeline({ history }: { history: StateTransition[] }) {
  if (history.length <= 1) return null;

  // Most recent first
  const reversed = [...history].reverse();

  return (
    <CollapsibleSection title="State Timeline">
      <div className="relative pl-5">
        <div className="absolute left-[11.5px] top-2.5 bottom-2.5 w-px bg-border" />

        {reversed.map((entry, i) => {
          const prevTimestamp = i < reversed.length - 1 ? reversed[i + 1].timestamp : undefined;
          const duration = transitionDuration(entry.timestamp, prevTimestamp);

          return (
            <div key={`${entry.state}-${entry.timestamp}`} className="relative flex items-start gap-3 py-1.5 ps-3 min-h-8">
              <div
                className={`absolute -left-3.25 top-3 w-2.5 h-2.5 rounded-full ring-2 ring-background ${statusColor(entry.state)}`}
              />

              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{entry.state}</span>
                  <span className="text-xs text-muted-foreground">
                    <TimeAgo date={entry.timestamp} />
                  </span>
                  {duration && (
                    <span className="text-xs text-muted-foreground">
                      (after {duration})
                    </span>
                  )}
                </div>
                {entry.err && (
                  <p className="text-xs text-red-600 dark:text-red-400 mt-0.5">{entry.err}</p>
                )}
                {entry.message && !entry.err && (
                  <p className="text-xs text-muted-foreground mt-0.5">{entry.message}</p>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </CollapsibleSection>
  );
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/TaskStateTimeline.tsx
git commit -m "feat: add TaskStateTimeline component"
```

---

### Task 7: PreviousAttempts component

**Files:**
- Create: `frontend/src/components/TaskPreviousAttempts.tsx`

- [ ] **Step 1: Create the component**

```tsx
import { Link } from "react-router-dom";
import type { Task } from "../api/types";
import { statusColor } from "../lib/statusColor";
import CollapsibleSection from "./CollapsibleSection";
import TaskStatusBadge from "./TaskStatusBadge";
import TimeAgo from "./TimeAgo";

export default function TaskPreviousAttempts({ attempts }: { attempts: Task[] }) {
  if (attempts.length === 0) return null;

  return (
    <CollapsibleSection title="Previous Attempts">
      <div className="overflow-auto rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Task</th>
              <th className="text-left p-3 text-sm font-medium">State</th>
              <th className="text-left p-3 text-sm font-medium">Error</th>
              <th className="text-left p-3 text-sm font-medium">Timestamp</th>
            </tr>
          </thead>
          <tbody>
            {attempts.map((task) => {
              const exitCode = task.Status.ContainerStatus?.ExitCode;
              const errorMessage =
                task.Status.Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : "");

              return (
                <tr key={task.ID} className="border-b last:border-b-0">
                  <td className="p-3 text-sm">
                    <span className="inline-flex items-center gap-2">
                      <span className={`shrink-0 size-2 rounded-full ${statusColor(task.Status.State)}`} />
                      <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                        {task.ID.slice(0, 12)}
                      </Link>
                    </span>
                  </td>
                  <td className="p-3 text-sm">
                    <TaskStatusBadge state={task.Status.State} />
                  </td>
                  <td className="p-3 text-sm text-red-600 dark:text-red-400">{errorMessage}</td>
                  <td className="p-3 text-sm text-muted-foreground">
                    {task.Status.Timestamp ? <TimeAgo date={task.Status.Timestamp} /> : "\u2014"}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </CollapsibleSection>
  );
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/TaskPreviousAttempts.tsx
git commit -m "feat: add TaskPreviousAttempts component"
```

---

### Task 8: Integrate into TaskDetail page

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Add imports and sections to TaskDetail**

Add imports at top of `TaskDetail.tsx`:

```typescript
import TaskStateTimeline from "../components/TaskStateTimeline";
import TaskPreviousAttempts from "../components/TaskPreviousAttempts";
```

Add the two sections after the Status Message block (after line 95) and before the LogViewer block:

```tsx
      <TaskStateTimeline history={task.StateHistory || []} />

      <TaskPreviousAttempts attempts={task.PreviousAttempts || []} />
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/TaskDetail.tsx
git commit -m "feat: integrate state timeline and previous attempts into task detail"
```

---

### Task 9: Slot retry indicator in TasksTable

**Files:**
- Modify: `frontend/src/components/TasksTable.tsx`

- [ ] **Step 1: Add attempt count badge to task rows**

In `TasksTable.tsx`, find the task link cell (line 87-93). After the existing `Link` inside the task cell, add the attempt count badge:

Replace the task cell content (lines 87-94):

```tsx
                  <td className="p-3 text-sm">
                    <span className="inline-flex items-center gap-2">
                      <span className={`shrink-0 size-2 rounded-full ${statusColor(State)}`} />
                      <Link to={`/tasks/${ID}`} className="text-link hover:underline">
                        {variant === "node" && Slot ? `Replica #${Slot}` : ID.slice(0, 12)}
                      </Link>
                      {task.AttemptCount && task.AttemptCount > 1 && (
                        <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground" title={`${task.AttemptCount} attempts for this slot`}>
                          ×{task.AttemptCount}
                        </span>
                      )}
                    </span>
                  </td>
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Run frontend lint**

Run: `cd frontend && npm run lint`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/TasksTable.tsx
git commit -m "feat: show retry attempt count badge in task tables"
```

---

### Task 10: Final verification

- [ ] **Step 1: Run full backend tests**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 2: Run full frontend checks**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`
Expected: No errors.

- [ ] **Step 3: Run make check**

Run: `make check`
Expected: All pass.

- [ ] **Step 4: Final commit if any formatting changes**

```bash
git add -A
git diff --cached --stat  # verify only formatting changes
git commit -m "chore: formatting cleanup"
```
