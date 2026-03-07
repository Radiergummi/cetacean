# Performance Optimizations Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate the three biggest performance bottlenecks: full stack rebuild on every mutation, linear task scans, and slow JSON serialization.

**Architecture:** (1) Replace `rebuildStacks()` with incremental stack maintenance — add/remove individual resources from stacks on Set/Delete. (2) Add `tasksByService` and `tasksByNode` secondary index maps maintained on SetTask/DeleteTask/ReplaceTasks. (3) Swap `encoding/json` for `github.com/goccy/go-json` as a drop-in replacement.

**Tech Stack:** Go, `github.com/goccy/go-json`

---

### Task 1: Add secondary indexes for tasks

The simplest change with no dependencies. Add `tasksByService map[string]map[string]struct{}` and `tasksByNode map[string]map[string]struct{}` to Cache. Maintain on SetTask, DeleteTask, ReplaceTasks.

**Files:**
- Modify: `internal/cache/cache.go` (Cache struct, New, SetTask, DeleteTask, ReplaceTasks, ListTasksByService, ListTasksByNode)
- Test: `internal/cache/cache_test.go` (existing tests cover correctness)
- Bench: `internal/cache/cache_bench_test.go` (existing benchmarks measure improvement)

**Step 1: Add index fields to Cache struct and New()**

In `internal/cache/cache.go`, add two fields to `Cache`:

```go
type Cache struct {
	mu             sync.RWMutex
	nodes          map[string]swarm.Node
	services       map[string]swarm.Service
	tasks          map[string]swarm.Task
	tasksByService map[string]map[string]struct{} // serviceID -> set of taskIDs
	tasksByNode    map[string]map[string]struct{} // nodeID -> set of taskIDs
	configs        map[string]swarm.Config
	secrets        map[string]swarm.Secret
	networks       map[string]network.Summary
	volumes        map[string]volume.Volume
	stacks         map[string]Stack
	onChange       OnChangeFunc
}
```

Initialize both in `New()`:

```go
tasksByService: make(map[string]map[string]struct{}),
tasksByNode:    make(map[string]map[string]struct{}),
```

**Step 2: Update SetTask to maintain indexes**

```go
func (c *Cache) SetTask(t swarm.Task) {
	c.mu.Lock()
	if old, ok := c.tasks[t.ID]; ok {
		c.removeTaskIndex(old)
	}
	c.tasks[t.ID] = t
	c.addTaskIndex(t)
	c.mu.Unlock()
	c.notify(Event{Type: "task", Action: "update", ID: t.ID, Resource: t})
}
```

Add helpers (called with lock held):

```go
func (c *Cache) addTaskIndex(t swarm.Task) {
	if t.ServiceID != "" {
		if c.tasksByService[t.ServiceID] == nil {
			c.tasksByService[t.ServiceID] = make(map[string]struct{})
		}
		c.tasksByService[t.ServiceID][t.ID] = struct{}{}
	}
	if t.NodeID != "" {
		if c.tasksByNode[t.NodeID] == nil {
			c.tasksByNode[t.NodeID] = make(map[string]struct{})
		}
		c.tasksByNode[t.NodeID][t.ID] = struct{}{}
	}
}

func (c *Cache) removeTaskIndex(t swarm.Task) {
	if t.ServiceID != "" {
		if m := c.tasksByService[t.ServiceID]; m != nil {
			delete(m, t.ID)
			if len(m) == 0 {
				delete(c.tasksByService, t.ServiceID)
			}
		}
	}
	if t.NodeID != "" {
		if m := c.tasksByNode[t.NodeID]; m != nil {
			delete(m, t.ID)
			if len(m) == 0 {
				delete(c.tasksByNode, t.NodeID)
			}
		}
	}
}
```

**Step 3: Update DeleteTask to maintain indexes**

```go
func (c *Cache) DeleteTask(id string) {
	c.mu.Lock()
	if old, ok := c.tasks[id]; ok {
		c.removeTaskIndex(old)
	}
	delete(c.tasks, id)
	c.mu.Unlock()
	c.notify(Event{Type: "task", Action: "remove", ID: id})
}
```

**Step 4: Update ReplaceTasks to rebuild indexes**

```go
func (c *Cache) ReplaceTasks(tasks []swarm.Task) {
	m := make(map[string]swarm.Task, len(tasks))
	byService := make(map[string]map[string]struct{})
	byNode := make(map[string]map[string]struct{})
	for _, t := range tasks {
		m[t.ID] = t
		if t.ServiceID != "" {
			if byService[t.ServiceID] == nil {
				byService[t.ServiceID] = make(map[string]struct{})
			}
			byService[t.ServiceID][t.ID] = struct{}{}
		}
		if t.NodeID != "" {
			if byNode[t.NodeID] == nil {
				byNode[t.NodeID] = make(map[string]struct{})
			}
			byNode[t.NodeID][t.ID] = struct{}{}
		}
	}
	c.mu.Lock()
	c.tasks = m
	c.tasksByService = byService
	c.tasksByNode = byNode
	c.mu.Unlock()
}
```

**Step 5: Rewrite ListTasksByService and ListTasksByNode to use indexes**

```go
func (c *Cache) ListTasksByService(serviceID string) []swarm.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := c.tasksByService[serviceID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

func (c *Cache) ListTasksByNode(nodeID string) []swarm.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := c.tasksByNode[nodeID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out
}
```

**Step 6: Run tests and benchmarks**

Run: `go test ./internal/cache/ -v`
Expected: All existing tests pass (TestCache_ListTasksByService, TestCache_ListTasksByNode, etc.)

Run: `go test -bench=BenchmarkListTasksBy -benchmem ./internal/cache/`
Expected: Major improvement at size=1000 (from ~50µs linear scan to ~O(matching tasks))

**Step 7: Commit**

```
feat: add secondary indexes for tasks by service and node
```

---

### Task 2: Incremental stack maintenance

Replace `rebuildStacks()` with incremental add/remove. Each Set/Delete for services, configs, secrets, networks, volumes updates only the affected stack entry.

**Files:**
- Modify: `internal/cache/cache.go` (replace rebuildStacks with addToStack/removeFromStack, update all Set*/Delete*/Replace* methods that currently call rebuildStacks)
- Test: `internal/cache/cache_test.go` (existing TestCache_SetService_DerivedStack covers correctness)
- Bench: `internal/cache/cache_bench_test.go` (existing benchmarks measure improvement)

**Step 1: Add incremental stack helpers**

Replace `rebuildStacks()` with these helpers (called with lock held):

```go
const stackLabel = "com.docker.stack.namespace"

func (c *Cache) addToStack(resource, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		s = Stack{Name: ns}
	}
	switch resource {
	case "service":
		s.Services = appendUnique(s.Services, id)
	case "config":
		s.Configs = appendUnique(s.Configs, id)
	case "secret":
		s.Secrets = appendUnique(s.Secrets, id)
	case "network":
		s.Networks = appendUnique(s.Networks, id)
	case "volume":
		s.Volumes = appendUnique(s.Volumes, id)
	}
	c.stacks[ns] = s
}

func (c *Cache) removeFromStack(resource, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		return
	}
	switch resource {
	case "service":
		s.Services = removeStr(s.Services, id)
	case "config":
		s.Configs = removeStr(s.Configs, id)
	case "secret":
		s.Secrets = removeStr(s.Secrets, id)
	case "network":
		s.Networks = removeStr(s.Networks, id)
	case "volume":
		s.Volumes = removeStr(s.Volumes, id)
	}
	if len(s.Services)+len(s.Configs)+len(s.Secrets)+len(s.Networks)+len(s.Volumes) == 0 {
		delete(c.stacks, ns)
	} else {
		c.stacks[ns] = s
	}
}

func appendUnique(sl []string, v string) []string {
	for _, s := range sl {
		if s == v {
			return sl
		}
	}
	return append(sl, v)
}

func removeStr(sl []string, v string) []string {
	for i, s := range sl {
		if s == v {
			return append(sl[:i], sl[i+1:]...)
		}
	}
	return sl
}
```

**Step 2: Update Set methods to use incremental helpers**

For SetService (and similarly SetConfig, SetSecret, SetNetwork, SetVolume):

```go
func (c *Cache) SetService(s swarm.Service) {
	c.mu.Lock()
	if old, ok := c.services[s.ID]; ok {
		c.removeFromStack("service", old.ID, old.Spec.Labels)
	}
	c.services[s.ID] = s
	c.addToStack("service", s.ID, s.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "service", Action: "update", ID: s.ID, Resource: s})
}
```

Apply the same pattern for:
- `SetConfig` — labels at `cfg.Spec.Labels`
- `SetSecret` — labels at `s.Spec.Labels`
- `SetNetwork` — labels at `n.Labels`
- `SetVolume` — labels at `v.Labels`

**Step 3: Update Delete methods**

For DeleteService (and similarly DeleteConfig, DeleteSecret, DeleteNetwork, DeleteVolume):

```go
func (c *Cache) DeleteService(id string) {
	c.mu.Lock()
	if old, ok := c.services[id]; ok {
		c.removeFromStack("service", id, old.Spec.Labels)
	}
	delete(c.services, id)
	c.mu.Unlock()
	c.notify(Event{Type: "service", Action: "remove", ID: id})
}
```

**Step 4: Update Replace methods to use full rebuild**

The Replace* methods do a full swap, so they still need a full rebuild. Keep `rebuildStacks()` as a private method but only called from Replace* methods:

```go
func (c *Cache) ReplaceServices(services []swarm.Service) {
	m := make(map[string]swarm.Service, len(services))
	for _, s := range services {
		m[s.ID] = s
	}
	c.mu.Lock()
	c.services = m
	c.rebuildStacks()
	c.mu.Unlock()
}
```

Keep the existing `rebuildStacks()` method unchanged — it's still used by all Replace* methods. The performance win comes from Set/Delete being O(1) instead of O(all resources).

**Step 5: Run tests and benchmarks**

Run: `go test ./internal/cache/ -v`
Expected: All tests pass, especially TestCache_SetService_DerivedStack

Run: `go test -bench=BenchmarkSetService -benchmem ./internal/cache/`
Expected: SetService drops from ~400µs to near SetNode levels (~100-200ns) at size=1000

**Step 6: Commit**

```
perf: incremental stack maintenance on Set/Delete operations
```

---

### Task 3: Switch to goccy/go-json

Drop-in replacement for `encoding/json`. Only two production files use it.

**Files:**
- Modify: `go.mod` (add dependency)
- Modify: `internal/api/handlers.go` (swap import)
- Modify: `internal/api/sse.go` (swap import)
- Bench: `internal/api/handlers_bench_test.go` (existing benchmarks measure improvement)

**Step 1: Add dependency**

Run: `go get github.com/goccy/go-json`

**Step 2: Swap import in handlers.go**

Change `"encoding/json"` to `json "github.com/goccy/go-json"` in `internal/api/handlers.go`.

**Step 3: Swap import in sse.go**

Change `"encoding/json"` to `json "github.com/goccy/go-json"` in `internal/api/sse.go`.

**Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: All tests pass (the test file still uses `encoding/json` for decoding — that's fine, we only swap the encode side in production code)

**Step 5: Run benchmarks**

Run: `go test -bench=BenchmarkHandleList -benchmem ./internal/api/`
Expected: ~2-3x improvement on list endpoints

**Step 6: Commit**

```
perf: switch to goccy/go-json for faster serialization
```

---

### Task 4: Run full benchmark suite and verify

**Step 1: Run all tests**

Run: `go test ./...`
Expected: All pass

**Step 2: Run full benchmarks**

Run: `go test -bench=. -benchmem -count=1 ./internal/cache/ ./internal/api/`
Expected: Significant improvements across the board, especially SetService, ListTasksByService/ByNode, and HandleList* endpoints.

**Step 3: Commit benchmark results as a comment in the design doc or PR description**
