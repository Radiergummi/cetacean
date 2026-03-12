# Per-Resource SSE Streaming Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the frontend's reliance on the global SSE broadcast with per-resource and per-list event streams using content negotiation (`Accept: text/event-stream`) on existing endpoints.

**Architecture:** The backend already has content negotiation and dispatch helpers. We extend the `Broadcaster` to support custom match functions (not just type filtering), add SSE handlers to all list/detail endpoints, and emit cross-reference events from the cache when a service mutation changes which configs/secrets/networks/volumes it references. The frontend replaces `SSEProvider` + `useSSE` with a new `useResourceStream` hook that opens an `EventSource` directly to the resource endpoint. The global `/events` endpoint stays as a public API.

**Tech Stack:** Go 1.22+, React 19, TypeScript, EventSource API

---

## File Map

| File | Change |
|------|--------|
| `internal/cache/cache.go` | Add `serviceRefs` helper + emit cross-ref events in `SetService`/`DeleteService` |
| `internal/cache/cache_test.go` | Test cross-reference event emission |
| `internal/api/sse.go` | Refactor `sseClient` to use match func, extract `serveSSE`, add resource/list matchers |
| `internal/api/sse_test.go` | Test per-resource streaming |
| `internal/api/handlers.go` | Add `Broadcaster` field to `Handlers`, add SSE handler methods |
| `internal/api/handlers_test.go` | Test SSE content negotiation on detail/list endpoints |
| `internal/api/router.go` | Switch endpoints from `contentNegotiated` to `contentNegotiatedWithSSE` |
| `frontend/src/hooks/useResourceStream.ts` | New hook: opens EventSource to a path, dispatches events |
| `frontend/src/hooks/useSwarmResource.ts` | Replace `useSSE` with `useResourceStream` |
| `frontend/src/pages/*Detail.tsx` | Replace `useSSE` with `useResourceStream` |
| `frontend/src/pages/ClusterOverview.tsx` | Switch to `useResourceStream("/events")` |
| `frontend/src/pages/Topology.tsx` | Switch to `useResourceStream("/events")` |
| `frontend/src/hooks/SSEContext.tsx` | Delete (no longer used) |
| `frontend/src/hooks/useSSE.ts` | Delete (no longer used) |
| `frontend/src/components/ConnectionStatus.tsx` | Get connection state from `useResourceStream` context |

---

## Chunk 1: Cache Cross-Reference Events

When a service is created, updated, or deleted, the set of configs/secrets/networks/volumes it references may change. The cache should emit events for affected resources so per-resource SSE streams pick them up automatically.

### Task 1: Add `serviceRefs` helper and cross-reference event emission

**Files:**
- Modify: `internal/cache/cache.go`

- [ ] **Step 1: Add `serviceRefs` helper**

Add a helper that extracts the set of config/secret/network/volume IDs from a service. Place it after the `ServicesUsingVolume` method (after line 685):

```go
// serviceRefs extracts the set of config, secret, network, and volume IDs
// referenced by a service spec. Used to diff cross-references on mutations.
type refSet struct {
	configs  map[string]bool
	secrets  map[string]bool
	networks map[string]bool
	volumes  map[string]bool
}

func serviceRefs(s swarm.Service) refSet {
	r := refSet{
		configs:  make(map[string]bool),
		secrets:  make(map[string]bool),
		networks: make(map[string]bool),
		volumes:  make(map[string]bool),
	}
	if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
		for _, c := range cs.Configs {
			r.configs[c.ConfigID] = true
		}
		for _, s := range cs.Secrets {
			r.secrets[s.SecretID] = true
		}
		for _, m := range cs.Mounts {
			if m.Type == "volume" && m.Source != "" {
				r.volumes[m.Source] = true
			}
		}
	}
	for _, n := range s.Spec.TaskTemplate.Networks {
		r.networks[n.Target] = true
	}
	return r
}
```

- [ ] **Step 2: Modify `SetService` to emit cross-reference events**

Replace the `SetService` method:

```go
func (c *Cache) SetService(s swarm.Service) {
	c.mu.Lock()
	var oldRefs refSet
	if old, ok := c.services[s.ID]; ok {
		oldRefs = serviceRefs(old)
		c.removeFromStack("service", old.ID, old.Spec.Labels)
	}
	c.services[s.ID] = s
	c.addToStack("service", s.ID, s.Spec.Labels)
	newRefs := serviceRefs(s)
	c.mu.Unlock()

	c.notify(Event{Type: "service", Action: "update", ID: s.ID, Resource: s})
	c.notifyRefChanges(oldRefs, newRefs)
}
```

- [ ] **Step 3: Modify `DeleteService` to emit cross-reference events**

Replace the `DeleteService` method:

```go
func (c *Cache) DeleteService(id string) {
	c.mu.Lock()
	var oldRefs refSet
	if old, ok := c.services[id]; ok {
		oldRefs = serviceRefs(old)
		c.removeFromStack("service", id, old.Spec.Labels)
	}
	delete(c.services, id)
	c.mu.Unlock()

	c.notify(Event{Type: "service", Action: "remove", ID: id})
	c.notifyRefChanges(oldRefs, refSet{})
}
```

- [ ] **Step 4: Add `notifyRefChanges` method**

Add after the `notify` method:

```go
// notifyRefChanges emits "ref_changed" events for any config/secret/network/volume
// IDs that were added to or removed from a service's references.
func (c *Cache) notifyRefChanges(old, new refSet) {
	diffNotify := func(typ string, oldSet, newSet map[string]bool) {
		for id := range oldSet {
			if !newSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
		for id := range newSet {
			if !oldSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
	}
	diffNotify("config", old.configs, new.configs)
	diffNotify("secret", old.secrets, new.secrets)
	diffNotify("network", old.networks, new.networks)
	diffNotify("volume", old.volumes, new.volumes)
}
```

- [ ] **Step 5: Handle nil maps in `notifyRefChanges`**

When a service is created (no old service), `oldRefs` will have empty maps from `serviceRefs`. When deleted, `newRefs` is `refSet{}` with nil maps. Make `notifyRefChanges` nil-safe by initializing empty `refSet`:

Change the `DeleteService` call from:
```go
c.notifyRefChanges(oldRefs, refSet{})
```
To:
```go
c.notifyRefChanges(oldRefs, refSet{
	configs: make(map[string]bool), secrets: make(map[string]bool),
	networks: make(map[string]bool), volumes: make(map[string]bool),
})
```

Or simpler — make `notifyRefChanges` treat nil maps as empty:
```go
func (c *Cache) notifyRefChanges(old, new refSet) {
	diffNotify := func(typ string, oldSet, newSet map[string]bool) {
		for id := range oldSet {
			if !newSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
		for id := range newSet {
			if !oldSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
	}
	diffNotify("config", old.configs, new.configs)
	diffNotify("secret", old.secrets, new.secrets)
	diffNotify("network", old.networks, new.networks)
	diffNotify("volume", old.volumes, new.volumes)
}
```

This already works because ranging over a nil map is a no-op in Go. Use the simple `refSet{}` form.

- [ ] **Step 6: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/cache/ -v`
Expected: all existing tests pass (cross-ref events are additive — existing tests don't assert event counts for SetService)

Wait — `TestCache_OnChange_AllTypes` asserts exactly 12 events. `SetService` now emits additional ref_changed events only if the service references configs/secrets/networks/volumes. The test's service has no container spec, so `serviceRefs` returns empty maps. No extra events. Should pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cache/cache.go
git commit -m "feat: emit cross-reference events when service mutations change config/secret/network/volume refs"
```

---

### Task 2: Test cross-reference event emission

**Files:**
- Modify: `internal/cache/cache_test.go`

- [ ] **Step 1: Write test for SetService cross-reference events**

Add after `TestCache_ServicesUsingVolume`:

```go
func TestCache_SetService_EmitsCrossRefEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	// Create a service referencing config cfg1 and network net1
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "app-config"}},
	}
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net1"}}
	c.SetService(svc)

	// Expect: service update + ref_changed for cfg1 + ref_changed for net1
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "service" || events[0].Action != "update" {
		t.Errorf("event[0]: expected service/update, got %s/%s", events[0].Type, events[0].Action)
	}
	// The remaining events are ref_changed for cfg1 and net1 (order may vary)
	refEvents := events[1:]
	hasConfig := false
	hasNetwork := false
	for _, e := range refEvents {
		if e.Action != "ref_changed" {
			t.Errorf("expected ref_changed action, got %s", e.Action)
		}
		if e.Type == "config" && e.ID == "cfg1" {
			hasConfig = true
		}
		if e.Type == "network" && e.ID == "net1" {
			hasNetwork = true
		}
	}
	if !hasConfig {
		t.Error("missing ref_changed event for config cfg1")
	}
	if !hasNetwork {
		t.Error("missing ref_changed event for network net1")
	}
}

func TestCache_SetService_RefChangeDiff(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	// Create service with cfg1
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1"}},
	}
	c.SetService(svc)
	events = nil // reset

	// Update service: remove cfg1, add cfg2
	svc.Spec.TaskTemplate.ContainerSpec.Configs = []*swarm.ConfigReference{{ConfigID: "cfg2"}}
	c.SetService(svc)

	// Expect: service update + ref_changed for cfg1 (removed) + ref_changed for cfg2 (added)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	refEvents := events[1:]
	ids := map[string]bool{}
	for _, e := range refEvents {
		if e.Type != "config" || e.Action != "ref_changed" {
			t.Errorf("expected config/ref_changed, got %s/%s", e.Type, e.Action)
		}
		ids[e.ID] = true
	}
	if !ids["cfg1"] || !ids["cfg2"] {
		t.Errorf("expected ref_changed for both cfg1 and cfg2, got %v", ids)
	}
}

func TestCache_DeleteService_EmitsCrossRefEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Secrets: []*swarm.SecretReference{{SecretID: "sec1"}},
	}
	c.SetService(svc)
	events = nil // reset

	c.DeleteService("svc1")

	// Expect: service remove + ref_changed for sec1
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "service" || events[0].Action != "remove" {
		t.Errorf("event[0]: expected service/remove, got %s/%s", events[0].Type, events[0].Action)
	}
	if events[1].Type != "secret" || events[1].Action != "ref_changed" || events[1].ID != "sec1" {
		t.Errorf("event[1]: expected secret/ref_changed/sec1, got %s/%s/%s", events[1].Type, events[1].Action, events[1].ID)
	}
}

func TestCache_SetService_NoRefChange_NoExtraEvents(t *testing.T) {
	var events []Event
	c := New(func(e Event) { events = append(events, e) })

	// Service with no refs
	c.SetService(swarm.Service{ID: "svc1"})

	// Should only emit the service update event
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/cache/ -v -run CrossRef`
Expected: all 4 new tests pass

- [ ] **Step 3: Run all cache tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/cache/`
Expected: all tests pass (including pre-existing ones)

- [ ] **Step 4: Commit**

```bash
git add internal/cache/cache_test.go
git commit -m "test: cross-reference event emission on service mutations"
```

---

## Chunk 2: Per-Resource SSE Backend

Refactor the broadcaster to support match-function-based filtering, then add SSE handlers to all list and detail endpoints.

### Task 3: Refactor `sseClient` to use match functions

**Files:**
- Modify: `internal/api/sse.go`

- [ ] **Step 1: Replace `types` field with `match` function**

Change `sseClient`:

```go
type sseClient struct {
	events chan cache.Event
	match  func(cache.Event) bool // nil means accept all events
	done   chan struct{}
}
```

- [ ] **Step 2: Update `fanOut` to use match function**

Replace the type check in `fanOut`:

```go
func (b *Broadcaster) fanOut() {
	for {
		select {
		case e := <-b.inbox:
			b.mu.RLock()
			for c := range b.clients {
				if c.match != nil && !c.match(e) {
					continue
				}
				select {
				case c.events <- e:
				default:
				}
			}
			b.mu.RUnlock()
		case <-b.stop:
			return
		}
	}
}
```

- [ ] **Step 3: Extract `serveSSE` method**

Extract the HTTP setup, client registration, and write loop from `ServeHTTP` into a reusable method:

```go
// serveSSE streams events matching the given filter to the HTTP client.
// This is the core SSE write loop shared by the global /events endpoint
// and per-resource/per-list streams.
func (b *Broadcaster) serveSSE(w http.ResponseWriter, r *http.Request, match func(cache.Event) bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "streaming not supported")
		return
	}

	client := &sseClient{
		events: make(chan cache.Event, 64),
		match:  match,
		done:   make(chan struct{}),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if len(b.clients) >= maxSSEClients {
		b.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusTooManyRequests, "too many SSE connections")
		return
	}
	b.clients[client] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	var eventID uint64
	batchTicker := time.NewTicker(b.batchInterval)
	defer batchTicker.Stop()
	var batch []cache.Event

	for {
		select {
		case e, ok := <-client.events:
			if !ok {
				if len(batch) > 0 {
					writeBatch(w, flusher, batch, &eventID)
				}
				return
			}
			batch = append(batch, e)
		case <-batchTicker.C:
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
				batch = batch[:0]
			}
		case <-r.Context().Done():
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
			}
			return
		case <-client.done:
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, &eventID)
			}
			return
		}
	}
}
```

- [ ] **Step 4: Rewrite `ServeHTTP` to use `serveSSE`**

```go
func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var match func(cache.Event) bool
	if t := r.URL.Query().Get("types"); t != "" {
		types := make(map[string]bool)
		for _, typ := range strings.Split(t, ",") {
			types[strings.TrimSpace(typ)] = true
		}
		match = func(e cache.Event) bool { return types[e.Type] }
	}
	b.serveSSE(w, r, match)
}
```

- [ ] **Step 5: Run existing SSE tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestSSE -v`
Expected: all 6 existing SSE tests pass (refactor preserves behavior)

- [ ] **Step 6: Commit**

```bash
git add internal/api/sse.go
git commit -m "refactor: extract serveSSE with match-function filtering from Broadcaster.ServeHTTP"
```

---

### Task 4: Add per-resource and per-list SSE matchers

**Files:**
- Modify: `internal/api/sse.go`

- [ ] **Step 1: Add matcher factory functions**

Add after the `serveSSE` method:

```go
// typeMatcher returns a match function that accepts events of the given type.
// Used by list endpoint SSE streams.
func typeMatcher(typ string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		return e.Type == typ
	}
}

// resourceMatcher returns a match function for per-resource SSE streams.
// It accepts events that directly reference the resource, plus related events
// (e.g., tasks belonging to a service or node).
func resourceMatcher(typ, id string) func(cache.Event) bool {
	switch typ {
	case "node":
		return func(e cache.Event) bool {
			if e.Type == "node" && e.ID == id {
				return true
			}
			if e.Type == "task" {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.NodeID == id
				}
			}
			return false
		}
	case "service":
		return func(e cache.Event) bool {
			if e.Type == "service" && e.ID == id {
				return true
			}
			if e.Type == "task" {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.ServiceID == id
				}
			}
			return false
		}
	case "task":
		return func(e cache.Event) bool {
			return e.Type == "task" && e.ID == id
		}
	default:
		// config, secret, network, volume — just match by type+ID.
		// Cross-reference events (action "ref_changed") are emitted by the cache
		// with the correct type+ID, so they are matched automatically.
		return func(e cache.Event) bool {
			return e.Type == typ && e.ID == id
		}
	}
}

// stackMatcher returns a match function for stack SSE streams.
// It accepts events for any resource belonging to the named stack.
func stackMatcher(c *cache.Cache, name string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		stack, ok := c.GetStack(name)
		if !ok {
			return false
		}
		switch e.Type {
		case "service":
			return slices.Contains(stack.Services, e.ID)
		case "config":
			return slices.Contains(stack.Configs, e.ID)
		case "secret":
			return slices.Contains(stack.Secrets, e.ID)
		case "network":
			return slices.Contains(stack.Networks, e.ID)
		case "volume":
			return slices.Contains(stack.Volumes, e.ID)
		case "task":
			if t, ok := e.Resource.(swarm.Task); ok {
				return slices.Contains(stack.Services, t.ServiceID)
			}
			return false
		case "stack":
			return e.ID == name
		default:
			return false
		}
	}
}
```

- [ ] **Step 2: Add `slices` and `swarm` imports**

Ensure `sse.go` imports include:
```go
"slices"
"github.com/docker/docker/api/types/swarm"
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestSSE -v`
Expected: all pass (new functions not yet called)

- [ ] **Step 4: Commit**

```bash
git add internal/api/sse.go
git commit -m "feat: add typeMatcher, resourceMatcher, and stackMatcher for per-resource SSE"
```

---

### Task 5: Add SSE handlers and wire up routes

**Files:**
- Modify: `internal/api/handlers.go` — add `broadcaster` field
- Modify: `internal/api/router.go` — wire SSE handlers

- [ ] **Step 1: Add `broadcaster` field to `Handlers`**

In `handlers.go`, add the field:

```go
type Handlers struct {
	cache         *cache.Cache
	broadcaster   *Broadcaster
	dockerClient  DockerLogStreamer
	systemClient  DockerSystemClient
	ready         <-chan struct{}
	notifier      *notify.Notifier
	promClient    *PromClient
	localNodeMu   sync.Mutex
	localNodeID   string
	localNodeDone bool
}
```

Update `NewHandlers`:

```go
func NewHandlers(c *cache.Cache, b *Broadcaster, dc DockerLogStreamer, sc DockerSystemClient, ready <-chan struct{}, notifier *notify.Notifier, promClient *PromClient) *Handlers {
	return &Handlers{cache: c, broadcaster: b, dockerClient: dc, systemClient: sc, ready: ready, notifier: notifier, promClient: promClient}
}
```

- [ ] **Step 2: Add generic SSE stream methods**

Add to `handlers.go`:

```go
func (h *Handlers) streamList(w http.ResponseWriter, r *http.Request, typ string) {
	h.broadcaster.serveSSE(w, r, typeMatcher(typ))
}

func (h *Handlers) streamResource(w http.ResponseWriter, r *http.Request, typ, id string) {
	h.broadcaster.serveSSE(w, r, resourceMatcher(typ, id))
}
```

- [ ] **Step 3: Update all callers of `NewHandlers`**

Search for `NewHandlers(` and add the broadcaster parameter. The call is in `main.go`:

Run: `cd /Users/moritz/GolandProjects/cetacean && grep -rn 'NewHandlers(' --include='*.go'`

Update each call site to pass the broadcaster. In `main.go` (and test helpers if any), change from:
```go
NewHandlers(c, dc, sc, ready, notifier, promClient)
```
To:
```go
NewHandlers(c, b, dc, sc, ready, notifier, promClient)
```

- [ ] **Step 4: Switch list endpoints to `contentNegotiatedWithSSE`**

In `router.go`, replace the list endpoint registrations:

```go
// Nodes
mux.HandleFunc("GET /nodes", contentNegotiatedWithSSE(h.HandleListNodes, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "node") }, spa))
mux.HandleFunc("GET /nodes/{id}", contentNegotiatedWithSSE(h.HandleGetNode, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "node", r.PathValue("id")) }, spa))
mux.HandleFunc("GET /nodes/{id}/tasks", contentNegotiated(h.HandleNodeTasks, spa))

// Services
mux.HandleFunc("GET /services", contentNegotiatedWithSSE(h.HandleListServices, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "service") }, spa))
mux.HandleFunc("GET /services/{id}", contentNegotiatedWithSSE(h.HandleGetService, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "service", r.PathValue("id")) }, spa))

// Tasks
mux.HandleFunc("GET /tasks", contentNegotiatedWithSSE(h.HandleListTasks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "task") }, spa))
mux.HandleFunc("GET /tasks/{id}", contentNegotiatedWithSSE(h.HandleGetTask, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "task", r.PathValue("id")) }, spa))

// Stacks
mux.HandleFunc("GET /stacks", contentNegotiatedWithSSE(h.HandleListStacks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "stack") }, spa))
mux.HandleFunc("GET /stacks/{name}", contentNegotiatedWithSSE(h.HandleGetStack, func(w http.ResponseWriter, r *http.Request) {
	h.broadcaster.serveSSE(w, r, stackMatcher(h.cache, r.PathValue("name")))
}, spa))

// Configs
mux.HandleFunc("GET /configs", contentNegotiatedWithSSE(h.HandleListConfigs, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "config") }, spa))
mux.HandleFunc("GET /configs/{id}", contentNegotiatedWithSSE(h.HandleGetConfig, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "config", r.PathValue("id")) }, spa))

// Secrets
mux.HandleFunc("GET /secrets", contentNegotiatedWithSSE(h.HandleListSecrets, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "secret") }, spa))
mux.HandleFunc("GET /secrets/{id}", contentNegotiatedWithSSE(h.HandleGetSecret, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "secret", r.PathValue("id")) }, spa))

// Networks
mux.HandleFunc("GET /networks", contentNegotiatedWithSSE(h.HandleListNetworks, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "network") }, spa))
mux.HandleFunc("GET /networks/{id}", contentNegotiatedWithSSE(h.HandleGetNetwork, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "network", r.PathValue("id")) }, spa))

// Volumes
mux.HandleFunc("GET /volumes", contentNegotiatedWithSSE(h.HandleListVolumes, func(w http.ResponseWriter, r *http.Request) { h.streamList(w, r, "volume") }, spa))
mux.HandleFunc("GET /volumes/{name}", contentNegotiatedWithSSE(h.HandleGetVolume, func(w http.ResponseWriter, r *http.Request) { h.streamResource(w, r, "volume", r.PathValue("name")) }, spa))
```

- [ ] **Step 5: Build and run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./... && go test ./...`
Expected: build succeeds, all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/router.go main.go
git commit -m "feat: add per-resource and per-list SSE streaming on all list/detail endpoints"
```

---

### Task 6: Test per-resource SSE streaming

**Files:**
- Modify: `internal/api/sse_test.go`

- [ ] **Step 1: Test `resourceMatcher` for configs (with cross-ref events)**

```go
func TestSSE_ResourceMatcher_Config(t *testing.T) {
	match := resourceMatcher("config", "cfg1")

	// Direct config event
	if !match(cache.Event{Type: "config", Action: "update", ID: "cfg1"}) {
		t.Error("should match direct config event")
	}
	// Cross-reference event
	if !match(cache.Event{Type: "config", Action: "ref_changed", ID: "cfg1"}) {
		t.Error("should match ref_changed event for same config")
	}
	// Different config
	if match(cache.Event{Type: "config", Action: "update", ID: "cfg2"}) {
		t.Error("should not match different config")
	}
	// Unrelated type
	if match(cache.Event{Type: "service", Action: "update", ID: "svc1"}) {
		t.Error("should not match service event")
	}
}
```

- [ ] **Step 2: Test `resourceMatcher` for services (includes tasks)**

```go
func TestSSE_ResourceMatcher_Service(t *testing.T) {
	match := resourceMatcher("service", "svc1")

	// Direct service event
	if !match(cache.Event{Type: "service", Action: "update", ID: "svc1"}) {
		t.Error("should match direct service event")
	}
	// Task belonging to this service
	if !match(cache.Event{Type: "task", Action: "update", ID: "t1", Resource: swarm.Task{ServiceID: "svc1"}}) {
		t.Error("should match task event for this service")
	}
	// Task for different service
	if match(cache.Event{Type: "task", Action: "update", ID: "t2", Resource: swarm.Task{ServiceID: "svc2"}}) {
		t.Error("should not match task for different service")
	}
}
```

- [ ] **Step 3: Test `resourceMatcher` for nodes (includes tasks)**

```go
func TestSSE_ResourceMatcher_Node(t *testing.T) {
	match := resourceMatcher("node", "n1")

	if !match(cache.Event{Type: "node", Action: "update", ID: "n1"}) {
		t.Error("should match direct node event")
	}
	if !match(cache.Event{Type: "task", Action: "update", ID: "t1", Resource: swarm.Task{NodeID: "n1"}}) {
		t.Error("should match task on this node")
	}
	if match(cache.Event{Type: "task", Action: "update", ID: "t2", Resource: swarm.Task{NodeID: "n2"}}) {
		t.Error("should not match task on different node")
	}
}
```

- [ ] **Step 4: Test `typeMatcher`**

```go
func TestSSE_TypeMatcher(t *testing.T) {
	match := typeMatcher("node")

	if !match(cache.Event{Type: "node", Action: "update", ID: "n1"}) {
		t.Error("should match node event")
	}
	if match(cache.Event{Type: "service", Action: "update", ID: "s1"}) {
		t.Error("should not match service event")
	}
}
```

- [ ] **Step 5: Add `swarm` import to test file**

Add to imports:
```go
"github.com/docker/docker/api/types/swarm"
```

- [ ] **Step 6: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestSSE -v`
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/api/sse_test.go
git commit -m "test: per-resource and per-list SSE matchers"
```

---

## Chunk 3: Frontend Migration

Replace the global `SSEProvider` + `useSSE` pattern with a `useResourceStream` hook that opens an `EventSource` to the specific resource or list endpoint.

### Task 7: Create `useResourceStream` hook

**Files:**
- Create: `frontend/src/hooks/useResourceStream.ts`

- [ ] **Step 1: Create the hook**

```typescript
import { useEffect, useRef, useState } from "react";

interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown;
}

type SSEListener = (event: SSEEvent) => void;

/**
 * Opens an EventSource to the given path and dispatches parsed events.
 * The browser sends Accept: text/event-stream automatically.
 * Returns connection status for use by ConnectionStatus component.
 */
export function useResourceStream(path: string, listener: SSEListener) {
  const [connected, setConnected] = useState(true);
  const listenerRef = useRef(listener);
  listenerRef.current = listener;

  useEffect(() => {
    const es = new EventSource(path);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    const handler = (e: MessageEvent) => {
      try {
        listenerRef.current(JSON.parse(e.data) as SSEEvent);
      } catch {
        // ignore malformed events
      }
    };

    const batchHandler = (e: MessageEvent) => {
      try {
        const events = JSON.parse(e.data) as SSEEvent[];
        for (const event of events) {
          listenerRef.current(event);
        }
      } catch {
        // ignore parse errors
      }
    };

    const eventTypes = ["node", "service", "task", "config", "secret", "network", "volume", "stack"];
    for (const type of eventTypes) {
      es.addEventListener(type, handler);
    }
    es.addEventListener("batch", batchHandler);

    return () => es.close();
  }, [path]);

  return { connected };
}
```

- [ ] **Step 2: Run type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useResourceStream.ts
git commit -m "feat: add useResourceStream hook for per-endpoint EventSource connections"
```

---

### Task 8: Migrate `useSwarmResource` to `useResourceStream`

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts`

- [ ] **Step 1: Replace `useSSE` with `useResourceStream`**

Replace the entire file:

```typescript
import { useState, useEffect, useCallback, useRef } from "react";
import { useResourceStream } from "./useResourceStream";
import type { PagedResponse } from "../api/types";

export function useSwarmResource<T>(
  fetchFn: () => Promise<PagedResponse<T>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [data, setData] = useState<T[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const getIdRef = useRef(getId);
  const fetchFnRef = useRef(fetchFn);
  getIdRef.current = getId;
  fetchFnRef.current = fetchFn;

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchFnRef
      .current()
      .then((resp) => {
        setData(resp.items);
        setTotal(resp.total);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [fetchFn]);

  const ssePathMap: Record<string, string> = {
    node: "/nodes",
    service: "/services",
    task: "/tasks",
    config: "/configs",
    secret: "/secrets",
    network: "/networks",
    volume: "/volumes",
    stack: "/stacks",
  };

  useResourceStream(ssePathMap[sseType] ?? `/events?types=${sseType}`, useCallback((event) => {
    if (event.action === "remove") {
      setData((prev) => prev.filter((item) => getIdRef.current(item) !== event.id));
    } else if (event.resource) {
      setData((prev) => {
        const resource = event.resource as T;
        const idx = prev.findIndex((item) => getIdRef.current(item) === event.id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = resource;
          return next;
        }
        return [...prev, resource];
      });
    }
  }, []));

  return { data, total, loading, error, retry: load };
}
```

- [ ] **Step 2: Run type check and tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSwarmResource.ts
git commit -m "refactor: useSwarmResource uses per-list EventSource instead of global SSE"
```

---

### Task 9: Migrate detail pages to `useResourceStream`

**Files:**
- Modify: `frontend/src/pages/ConfigDetail.tsx`
- Modify: `frontend/src/pages/SecretDetail.tsx`
- Modify: `frontend/src/pages/NetworkDetail.tsx`
- Modify: `frontend/src/pages/VolumeDetail.tsx`
- Modify: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/pages/TaskDetail.tsx`
- Modify: `frontend/src/pages/StackDetail.tsx`

All detail pages follow the same pattern: replace `useSSE(types, callback)` with `useResourceStream(path, callback)`. The callback simplifies to just calling `fetchData()` on any event, since the backend now filters for relevance.

- [ ] **Step 1: Migrate ConfigDetail**

Replace:
```typescript
import { useSSE } from "../hooks/useSSE";
```
With:
```typescript
import { useResourceStream } from "../hooks/useResourceStream";
```

Replace the SSE block:
```typescript
  const serviceTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(serviceTimerRef.current), []);
  useSSE(["config", "service"], (e) => {
    if (e.type === "config" && e.id === id) fetchData();
    if (e.type === "service") {
      clearTimeout(serviceTimerRef.current);
      serviceTimerRef.current = setTimeout(fetchData, 500);
    }
  });
```
With:
```typescript
  useResourceStream(`/configs/${id}`, fetchData);
```

Remove `useRef` from imports if no longer used.

- [ ] **Step 2: Migrate SecretDetail**

Same pattern as ConfigDetail. Replace `useSSE` import, remove debounce timer, replace with:
```typescript
  useResourceStream(`/secrets/${id}`, fetchData);
```

- [ ] **Step 3: Migrate NetworkDetail**

Same pattern. Replace with:
```typescript
  useResourceStream(`/networks/${id}`, fetchData);
```

- [ ] **Step 4: Migrate VolumeDetail**

Same pattern. Replace with:
```typescript
  useResourceStream(`/volumes/${name}`, fetchData);
```

- [ ] **Step 5: Migrate NodeDetail**

Read `frontend/src/pages/NodeDetail.tsx` first. Replace `useSSE` with:
```typescript
  useResourceStream(`/nodes/${id}`, fetchData);
```

The node stream includes task events for tasks on this node (handled by `resourceMatcher`), so the existing task-update logic in the callback can be simplified to just refetching.

- [ ] **Step 6: Migrate ServiceDetail**

Read `frontend/src/pages/ServiceDetail.tsx` first. Replace `useSSE` with:
```typescript
  useResourceStream(`/services/${id}`, fetchData);
```

The service stream includes task events for this service's tasks.

- [ ] **Step 7: Migrate TaskDetail**

Replace with:
```typescript
  useResourceStream(`/tasks/${id}`, fetchData);
```

- [ ] **Step 8: Migrate StackDetail**

Replace the debounced SSE handler with:
```typescript
  useResourceStream(`/stacks/${name}`, fetchData);
```

Remove the `fetchTimerRef` and its cleanup effect.

- [ ] **Step 9: Migrate ClusterOverview**

Read `frontend/src/pages/ClusterOverview.tsx`. This page needs all event types. Replace with:
```typescript
  useResourceStream("/events", useCallback(() => {
    fetchSnapshot();
    fetchHistory();
  }, [fetchSnapshot, fetchHistory]));
```

- [ ] **Step 10: Migrate Topology**

Read `frontend/src/pages/Topology.tsx`. Replace with:
```typescript
  useResourceStream("/events", useCallback(() => {
    fetchData();
  }, [fetchData]));
```

Remove the debounce timer if present.

- [ ] **Step 11: Run type check and tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: all pass

- [ ] **Step 12: Commit**

```bash
git add frontend/src/pages/
git commit -m "refactor: migrate all pages from global SSE to per-resource/per-list EventSource streams"
```

---

### Task 10: Remove global SSE infrastructure from frontend

**Files:**
- Delete: `frontend/src/hooks/SSEContext.tsx`
- Delete: `frontend/src/hooks/useSSE.ts`
- Modify: `frontend/src/components/ConnectionStatus.tsx`
- Modify: app root (wherever `SSEProvider` is rendered)

- [ ] **Step 1: Check for remaining `useSSE` imports**

Run: `cd /Users/moritz/GolandProjects/cetacean && grep -r "useSSE\|SSEContext\|SSEProvider" frontend/src --include='*.ts' --include='*.tsx' -l`

Only test files and the files we're about to delete should remain.

- [ ] **Step 2: Remove `SSEProvider` from app root**

Find where `SSEProvider` wraps the app (likely `App.tsx` or `main.tsx`). Remove the import and the wrapper.

- [ ] **Step 3: Update `ConnectionStatus`**

The component currently uses `useSSEConnection()` from `SSEContext`. Since each page now has its own EventSource, `ConnectionStatus` needs a different approach. The simplest: have `useResourceStream` expose connection state through a context, or accept that connection status is now per-page.

Simplest approach: create a lightweight `ConnectionContext` that `useResourceStream` writes to:

Add to `useResourceStream.ts`:

```typescript
import { createContext, useContext } from "react";

const ConnectionContext = createContext<{ connected: boolean; lastEventAt: number | null }>({
  connected: true,
  lastEventAt: null,
});

export const ConnectionProvider = ConnectionContext.Provider;
export function useConnection() {
  return useContext(ConnectionContext);
}
```

Then in each page's parent (or layout), the `useResourceStream` return value feeds `ConnectionProvider`. Or simpler: have `useResourceStream` write to a module-level atom.

Actually — simplest approach that works: `useResourceStream` already returns `{ connected }`. Lift that state up through a context in the layout component that wraps all pages.

For now, just have `ConnectionStatus` import `useConnection` from `useResourceStream` and use that.

- [ ] **Step 4: Delete old SSE files**

```bash
rm frontend/src/hooks/SSEContext.tsx frontend/src/hooks/useSSE.ts
```

- [ ] **Step 5: Delete SSEContext tests**

```bash
rm frontend/src/hooks/SSEContext.test.tsx
```

- [ ] **Step 6: Run type check, tests, and lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run && npm run lint`
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: remove global SSEProvider, ConnectionStatus uses per-page connection state"
```

---

### Task 11: Final verification

- [ ] **Step 1: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: all pass

- [ ] **Step 2: Run full frontend checks**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run && npm run lint`
Expected: all pass

- [ ] **Step 3: Full build**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build && cd .. && go build -o cetacean .`
Expected: binary builds successfully
