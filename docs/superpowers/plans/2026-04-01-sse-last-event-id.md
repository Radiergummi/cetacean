# SSE Last-Event-ID Replay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable SSE clients to resume from where they left off on reconnect using the standard `Last-Event-ID` header, replaying missed events from the history ring buffer.

**Architecture:** Add a `HistoryID` field to `cache.Event`, populated in `notify()` from the history's monotonic counter. The SSE broadcaster uses this as the wire `id:` instead of a per-connection counter. On reconnect, `ServeSSE` reads `Last-Event-ID`, queries the history ring for missed entries, and replays them before entering the live event loop. Only type-level streams (list pages) get precise replay; detail/stack streams fall back to a `sync` event.

**Tech Stack:** Go (stdlib + existing cache/SSE packages), TypeScript/React (minimal change)

**Spec:** `docs/superpowers/specs/2026-04-01-sse-last-event-id-design.md`

---

### Task 1: Add `HistoryID` field to `cache.Event`

**Files:**
- Modify: `internal/cache/cache.go:29-35` (Event struct)
- Modify: `internal/cache/cache.go:131-151` (notify method)

- [ ] **Step 1: Add `HistoryID` to the `Event` struct**

In `internal/cache/cache.go`, add the field:

```go
type Event struct {
	Type       EventType `json:"type"`
	Action     string    `json:"action"`
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Resource   any       `json:"resource,omitempty"`
	HistoryID  uint64    `json:"-"`
}
```

`json:"-"` because this field is internal plumbing — the SSE wire format uses `ToSSEEvent` which doesn't include it.

- [ ] **Step 2: Populate `HistoryID` in `notify()`**

In `internal/cache/cache.go`, update the `notify` method to capture the history ID after `Append`, and set it for sync events too:

```go
func (c *Cache) notify(e Event) {
	if e.Name == "" {
		e.Name = ExtractName(e)
	}

	if e.Type != EventSync {
		c.history.Append(HistoryEntry{
			Type:       e.Type,
			Action:     e.Action,
			ResourceID: e.ID,
			Name:       e.Name,
		})
		e.HistoryID = c.history.Count()
		metrics.RecordCacheMutation(string(e.Type), e.Action)
	} else {
		e.HistoryID = c.history.Count()
	}
	if c.onChange != nil {
		c.onChange(e)
	}
}
```

- [ ] **Step 3: Verify existing tests still pass**

Run: `go test ./internal/cache/ -v -count=1`
Expected: All existing tests PASS (this change is additive — no behavior change).

- [ ] **Step 4: Commit**

```bash
git add internal/cache/cache.go
git commit -m "feat(cache): add HistoryID field to Event for SSE replay"
```

---

### Task 2: Add `History.Since()` and `History.Count()`

**Files:**
- Modify: `internal/cache/history.go` (add methods)
- Modify: `internal/cache/history_test.go` (add tests)

- [ ] **Step 1: Write failing tests for `Count()`**

In `internal/cache/history_test.go`, add:

```go
func TestHistory_Count_Empty(t *testing.T) {
	h := NewHistory(10)
	if c := h.Count(); c != 0 {
		t.Fatalf("expected 0, got %d", c)
	}
}

func TestHistory_Count_AfterAppends(t *testing.T) {
	h := NewHistory(10)
	for range 5 {
		h.Append(HistoryEntry{Type: "service", Action: "update"})
	}
	if c := h.Count(); c != 5 {
		t.Fatalf("expected 5, got %d", c)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run TestHistory_Count -v`
Expected: FAIL — `h.Count undefined`

- [ ] **Step 3: Implement `Count()`**

In `internal/cache/history.go`, add after the `Append` method:

```go
// Count returns the ID of the most recently appended entry, or 0 if empty.
func (h *History) Count() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run TestHistory_Count -v`
Expected: PASS

- [ ] **Step 5: Write failing tests for `Since()`**

In `internal/cache/history_test.go`, add:

```go
func TestHistory_Since_Basic(t *testing.T) {
	h := NewHistory(10)
	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Action: "update", Name: names[i]})
	}

	entries, ok := h.Since(2)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after ID 2, got %d", len(entries))
	}
	// Chronological order (oldest first)
	if entries[0].Name != "c" || entries[1].Name != "d" || entries[2].Name != "e" {
		t.Errorf("unexpected order: %v", entries)
	}
}

func TestHistory_Since_CaughtUp(t *testing.T) {
	h := NewHistory(10)
	for range 3 {
		h.Append(HistoryEntry{Type: "service"})
	}

	entries, ok := h.Since(3)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries when caught up, got %d", len(entries))
	}
}

func TestHistory_Since_Overwritten(t *testing.T) {
	h := NewHistory(3) // ring size 3
	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	// ID 1 and 2 have been overwritten (ring holds IDs 3, 4, 5)
	_, ok := h.Since(1)
	if ok {
		t.Fatal("expected ok=false for overwritten ID")
	}
}

func TestHistory_Since_FutureID(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service"})

	_, ok := h.Since(999)
	if ok {
		t.Fatal("expected ok=false for future ID")
	}
}

func TestHistory_Since_Zero(t *testing.T) {
	h := NewHistory(10)
	for i := range 3 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}

	entries, ok := h.Since(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after ID 0, got %d", len(entries))
	}
	if entries[0].Name != "a" {
		t.Errorf("expected oldest first, got %q", entries[0].Name)
	}
}

func TestHistory_Since_WrappedRing(t *testing.T) {
	h := NewHistory(3)
	for i := range 5 {
		h.Append(HistoryEntry{Type: "service", Name: names[i]})
	}
	// Ring holds IDs 3 ("c"), 4 ("d"), 5 ("e")

	entries, ok := h.Since(3)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after ID 3, got %d", len(entries))
	}
	if entries[0].Name != "d" || entries[1].Name != "e" {
		t.Errorf("unexpected entries: %v", entries)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run TestHistory_Since -v`
Expected: FAIL — `h.Since undefined`

- [ ] **Step 7: Implement `Since()`**

In `internal/cache/history.go`, add after `Count()`:

```go
// Since returns all entries with ID > afterID in chronological order.
// Returns ok=false if afterID has been overwritten or is a future ID,
// meaning the caller cannot trust the result is complete.
func (h *History) Since(afterID uint64) ([]HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Future ID or empty history
	if afterID > h.count {
		return nil, false
	}

	// Caught up — no new entries
	if afterID == h.count {
		return nil, true
	}

	// Determine the oldest ID still in the ring
	var oldestID uint64
	if h.full {
		oldestID = h.count - uint64(h.size) + 1
	} else {
		oldestID = 1
	}

	// afterID has been overwritten
	if afterID > 0 && afterID < oldestID {
		return nil, false
	}

	// Collect entries with ID > afterID in chronological order.
	// Walk the ring from oldest to newest.
	total := h.size
	if !h.full {
		total = h.cursor
	}

	var result []HistoryEntry
	for i := total - 1; i >= 0; i-- {
		idx := h.cursor - 1 - i
		if idx < 0 {
			idx += h.size
		}

		e := h.entries[idx]
		if e.ID > afterID {
			result = append(result, e)
		}
	}

	return result, true
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run TestHistory_Since -v`
Expected: All PASS

- [ ] **Step 9: Run all cache tests**

Run: `go test ./internal/cache/ -v -count=1`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/cache/history.go internal/cache/history_test.go
git commit -m "feat(cache): add History.Since() and Count() for SSE replay"
```

---

### Task 3: Switch SSE wire `id:` to use `HistoryID`

**Files:**
- Modify: `internal/api/sse/broadcaster.go:369-383` (WriteBatch)
- Modify: `internal/api/sse/broadcaster_test.go` (update assertions)

- [ ] **Step 1: Write a test for history-based SSE IDs**

In `internal/api/sse/broadcaster_test.go`, add:

```go
func TestSSE_WriteBatch_UsesHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "service", Action: "update", ID: "s1", HistoryID: 42},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 42\n") {
		t.Errorf("expected id: 42, got %q", output)
	}
}

func TestSSE_WriteBatch_BatchUsesMaxHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "service", Action: "update", ID: "s1", HistoryID: 10},
		{Type: "service", Action: "update", ID: "s2", HistoryID: 12},
		{Type: "node", Action: "update", ID: "n1", HistoryID: 11},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 12\n") {
		t.Errorf("expected id: 12 (max), got %q", output)
	}
}

func TestSSE_WriteBatch_SyncUsesHistoryID(t *testing.T) {
	var buf bytes.Buffer
	f := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	events := []cache.Event{
		{Type: "sync", Action: "full_sync", HistoryID: 500},
	}
	WriteBatch(&buf, f, events)

	output := buf.String()
	if !strings.Contains(output, "id: 500\n") {
		t.Errorf("expected id: 500, got %q", output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/sse/ -run TestSSE_WriteBatch -v`
Expected: FAIL — WriteBatch signature has changed (or old tests use the `eventID *uint64` param)

- [ ] **Step 3: Update `WriteBatch` to use `HistoryID`**

In `internal/api/sse/broadcaster.go`, replace the `WriteBatch` function:

```go
func WriteBatch(w io.Writer, flusher http.Flusher, events []cache.Event) {
	var maxID uint64
	for _, e := range events {
		if e.HistoryID > maxID {
			maxID = e.HistoryID
		}
	}

	if len(events) == 1 {
		data, _ := json.Marshal(ToSSEEvent(events[0]))
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", maxID, events[0].Type, data)
	} else {
		enriched := make([]Event, len(events))
		for i, e := range events {
			enriched[i] = ToSSEEvent(e)
		}
		data, _ := json.Marshal(enriched)
		fmt.Fprintf(w, "id: %d\nevent: batch\ndata: %s\n\n", maxID, data)
	}
	flusher.Flush()
}
```

- [ ] **Step 4: Update `ServeSSE` to remove the per-connection `eventID` counter**

In `internal/api/sse/broadcaster.go`, in the `ServeSSE` method:
- Remove `var eventID uint64` (line 170)
- Update all `WriteBatch` calls to remove the `&eventID` argument:
  - Line 182: `WriteBatch(w, flusher, batch)`
  - Line 189: `WriteBatch(w, flusher, batch)`
  - Line 198: `WriteBatch(w, flusher, batch)`
  - Line 203: `WriteBatch(w, flusher, batch)`

- [ ] **Step 5: Update existing tests that use the old `WriteBatch` signature**

Search for all `WriteBatch(` calls in test files and remove the `&eventID` argument. The old pattern:
```go
var eventID uint64
WriteBatch(w, f, events, &eventID)
```
becomes:
```go
WriteBatch(w, f, events)
```

Also update any assertions that check for `id: 1` (old per-connection counter) to check for `id: 0` or the appropriate `HistoryID` value from the test events.

- [ ] **Step 6: Run all SSE tests**

Run: `go test ./internal/api/sse/ -v -count=1`
Expected: All PASS

- [ ] **Step 7: Run full test suite to catch any other callers**

Run: `go test ./... 2>&1 | head -50`
Expected: All PASS (no other packages call `WriteBatch` directly)

- [ ] **Step 8: Commit**

```bash
git add internal/api/sse/broadcaster.go internal/api/sse/broadcaster_test.go
git commit -m "feat(sse): use history ID as SSE event ID instead of per-connection counter"
```

---

### Task 4: Add replay support to `ServeSSE`

**Files:**
- Modify: `internal/api/sse/broadcaster.go` (ServeSSE, new ReplaySource interface)
- Modify: `internal/api/sse/broadcaster_test.go` (replay tests)

- [ ] **Step 1: Define the `ReplaySource` interface**

The broadcaster needs to query the history ring but shouldn't import the cache package's concrete type. Add an interface at the top of `internal/api/sse/broadcaster.go`:

```go
// ReplaySource provides access to the history ring buffer for Last-Event-ID replay.
type ReplaySource interface {
	Since(afterID uint64) ([]cache.HistoryEntry, bool)
	Count() uint64
}
```

Add a `replay` field to `Broadcaster`:

```go
type Broadcaster struct {
	mu                sync.RWMutex
	clients           map[*sseClient]struct{}
	closed            bool
	inbox             chan cache.Event
	stop              chan struct{}
	batchInterval     time.Duration
	keepaliveInterval time.Duration
	writeError        ErrorWriter
	replay            ReplaySource
}
```

Update `NewBroadcaster` to accept it:

```go
func NewBroadcaster(batchInterval time.Duration, writeError ErrorWriter, replay ReplaySource) *Broadcaster {
```

Store `replay` in the struct. `replay` may be nil (tests that don't need replay).

- [ ] **Step 2: Write failing tests for replay**

In `internal/api/sse/broadcaster_test.go`, add a mock replay source and tests:

```go
type mockReplaySource struct {
	entries []cache.HistoryEntry
	count   uint64
}

func (m *mockReplaySource) Since(afterID uint64) ([]cache.HistoryEntry, bool) {
	if afterID > m.count {
		return nil, false
	}
	var result []cache.HistoryEntry
	for _, e := range m.entries {
		if e.ID > afterID {
			result = append(result, e)
		}
	}
	return result, true
}

func (m *mockReplaySource) Count() uint64 { return m.count }

func TestSSE_ReplayOnReconnect(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 5, Type: "service", Action: "update", ResourceID: "s1", Name: "web"},
			{ID: 6, Type: "node", Action: "update", ResourceID: "n1", Name: "node-1"},
			{ID: 7, Type: "service", Action: "update", ResourceID: "s2", Name: "api"},
		},
		count: 7,
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	go b.fanOut()
	defer b.Close()

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Last-Event-ID", "4")

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, TypeMatcher("service"), "service")
		close(done)
	}()
	waitForClients(t, b, 1)
	b.Close()
	<-done

	body := w.bodyString()
	// Should contain replayed service events (IDs 5 and 7) but not node event (ID 6)
	if !strings.Contains(body, `"action":"update"`) {
		t.Errorf("expected replayed events, got %q", body)
	}
	if strings.Contains(body, `"node-1"`) {
		t.Errorf("should not contain node events on a service type stream")
	}
}

func TestSSE_ReplayTooOld_SendsSync(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{},
		count:   100,
	}
	// Since(1) returns ok=false because entries are empty but count is 100
	replay.entries = nil
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	go b.fanOut()
	defer b.Close()

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Last-Event-ID", "1")

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, TypeMatcher("service"), "service")
		close(done)
	}()
	waitForClients(t, b, 1)
	b.Close()
	<-done

	body := w.bodyString()
	if !strings.Contains(body, "event: sync") {
		t.Errorf("expected sync event for too-old ID, got %q", body)
	}
}

func TestSSE_ReplayIneligible_SendsSync(t *testing.T) {
	replay := &mockReplaySource{
		entries: []cache.HistoryEntry{
			{ID: 5, Type: "service", Action: "update", ResourceID: "s1"},
		},
		count: 5,
	}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	go b.fanOut()
	defer b.Close()

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/services/s1", nil)
	req.Header.Set("Last-Event-ID", "3")

	done := make(chan struct{})
	go func() {
		// Empty replayType = ineligible for replay
		b.ServeSSE(w, req, ResourceMatcher("service", "s1"), "")
		close(done)
	}()
	waitForClients(t, b, 1)
	b.Close()
	<-done

	body := w.bodyString()
	if !strings.Contains(body, "event: sync") {
		t.Errorf("expected sync fallback for ineligible stream, got %q", body)
	}
}

func TestSSE_NoLastEventID_NoReplay(t *testing.T) {
	replay := &mockReplaySource{count: 10}
	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, replay)
	go b.fanOut()
	defer b.Close()

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/services", nil)
	// No Last-Event-ID header

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, TypeMatcher("service"), "service")
		close(done)
	}()
	waitForClients(t, b, 1)
	b.Close()
	<-done

	body := w.bodyString()
	// Should contain only keepalive or nothing, no replay or sync
	if strings.Contains(body, "event: sync") {
		t.Errorf("should not send sync without Last-Event-ID")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/api/sse/ -run "TestSSE_Replay|TestSSE_NoLastEventID" -v`
Expected: FAIL — `ServeSSE` signature doesn't match (missing `replayType` param)

- [ ] **Step 4: Implement replay in `ServeSSE`**

Update the `ServeSSE` signature to accept `replayType`:

```go
func (b *Broadcaster) ServeSSE(
	w http.ResponseWriter,
	r *http.Request,
	match func(cache.Event) bool,
	replayType cache.EventType,
) {
```

After registering the client and setting headers (after `flusher.Flush()`, before the defer), add the replay logic:

```go
	flusher.Flush()

	// Replay missed events on reconnect. skipBelow tracks the highest
	// replayed history ID so the main loop can skip duplicates.
	var skipBelow uint64
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" && b.replay != nil {
		if id, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			skipBelow = b.replayEvents(w, flusher, id, replayType)
		}
	}

	defer func() {
```

Add the `replayEvents` method:

```go
// replayEvents replays missed events from history. Returns the highest
// replayed history ID (0 if nothing was replayed). The caller uses this
// to skip duplicate live events in the main loop.
func (b *Broadcaster) replayEvents(
	w io.Writer,
	flusher http.Flusher,
	afterID uint64,
	replayType cache.EventType,
) uint64 {
	// Ineligible streams (detail/stack) always get sync
	if replayType == "" {
		b.writeSyncEvent(w, flusher)
		return b.replay.Count()
	}

	entries, ok := b.replay.Since(afterID)
	if !ok {
		b.writeSyncEvent(w, flusher)
		return b.replay.Count()
	}

	if len(entries) == 0 {
		return afterID
	}

	// Filter entries by replay type and convert to cache.Event
	var replay []cache.Event
	for _, e := range entries {
		if e.Type != replayType {
			continue
		}
		replay = append(replay, cache.Event{
			Type:      e.Type,
			Action:    e.Action,
			ID:        e.ResourceID,
			Name:      e.Name,
			HistoryID: e.ID,
		})
	}

	maxID := entries[len(entries)-1].ID
	if len(replay) > 0 {
		WriteBatch(w, flusher, replay)
	}

	return maxID
}

func (b *Broadcaster) writeSyncEvent(w io.Writer, flusher http.Flusher) {
	syncEvent := cache.Event{
		Type:      cache.EventSync,
		Action:    "full_sync",
		HistoryID: b.replay.Count(),
	}
	WriteBatch(w, flusher, []cache.Event{syncEvent})
}
```

Also add deduplication in the main event loop. In the `case e, ok := <-client.events:` branch, skip events that were already replayed:

```go
		case e, ok := <-client.events:
			if !ok {
				if len(batch) > 0 {
					WriteBatch(w, flusher, batch)
				}
				return
			}
			if skipBelow > 0 && e.HistoryID > 0 && e.HistoryID <= skipBelow {
				continue
			}
			skipBelow = 0 // Stop checking after first non-duplicate
			batch = append(batch, e)
```

Add `"strconv"` to the import block.

- [ ] **Step 5: Update `ServeHTTP` to pass empty `replayType`**

In `broadcaster.go`, the `ServeHTTP` method (the legacy `/events` endpoint) calls `b.ServeSSE`. Update it:

```go
func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var match func(cache.Event) bool
	if t := r.URL.Query().Get("types"); t != "" {
		types := make(map[string]bool)
		for typ := range strings.SplitSeq(t, ",") {
			types[strings.TrimSpace(typ)] = true
		}
		match = func(e cache.Event) bool { return types[string(e.Type)] }
	}
	b.ServeSSE(w, r, match, "")
}
```

- [ ] **Step 6: Update all existing tests that call `NewBroadcaster` or `ServeSSE`**

All existing `NewBroadcaster(interval, errorWriter)` calls gain a `nil` third argument:
```go
NewBroadcaster(0, noopErrorWriter, nil)
```

All existing `b.ServeSSE(w, req, match)` calls gain an empty `replayType`:
```go
b.ServeSSE(w, req, match, "")
```

This applies to files:
- `internal/api/sse/broadcaster_test.go`
- `internal/api/sse/broadcaster_bench_test.go`
- `internal/api/integration_test.go`
- `internal/api/openapi_test.go`
- `internal/api/middleware_test.go`

- [ ] **Step 7: Run all SSE tests**

Run: `go test ./internal/api/sse/ -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/api/sse/broadcaster.go internal/api/sse/broadcaster_test.go internal/api/sse/broadcaster_bench_test.go
git commit -m "feat(sse): implement Last-Event-ID replay with history-based event IDs"
```

---

### Task 5: Wire replay into call sites

**Files:**
- Modify: `main.go:157` (pass history to broadcaster)
- Modify: `internal/api/health_handlers.go:38-48` (streamList, streamResource)
- Modify: `internal/api/router.go:95,300-301` (stack SSE, legacy events endpoint)
- Modify: `internal/api/integration_test.go` (update NewBroadcaster call)
- Modify: `internal/api/openapi_test.go` (update NewBroadcaster call)
- Modify: `internal/api/middleware_test.go` (update NewBroadcaster call)

- [ ] **Step 1: Update `main.go` to pass history to broadcaster**

The broadcaster is created before the cache, so reorder: create the cache first, then pass `stateCache.History()` to the broadcaster.

```go
// State cache — broadcasts changes via SSE
stateCache := cache.New(nil)

// SSE broadcaster
broadcaster := sse.NewBroadcaster(cfg.SSEBatchInterval, api.WriteErrorCode, stateCache.History())
defer broadcaster.Close()

// Wire up the onChange callback now that both exist
stateCache.SetOnChange(func(e cache.Event) {
	broadcaster.Broadcast(e)
})
```

This requires adding a `SetOnChange` method to `Cache`. In `internal/cache/cache.go`:

```go
func (c *Cache) SetOnChange(fn OnChangeFunc) {
	c.onChange = fn
}
```

And update `New` to accept `nil` onChange (it already works — `notify` checks `c.onChange != nil`).

- [ ] **Step 2: Update `streamList` to pass `replayType`**

In `internal/api/health_handlers.go`:

```go
func (h *Handlers) streamList(w http.ResponseWriter, r *http.Request, typ cache.EventType) {
	typMatch := sse.TypeMatcher(typ)
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, typMatch), typ)
}
```

- [ ] **Step 3: Update `streamResource` to pass empty `replayType`**

In `internal/api/health_handlers.go`:

```go
func (h *Handlers) streamResource(
	w http.ResponseWriter, r *http.Request, typ cache.EventType, id string,
) {
	resMatch := sse.ResourceMatcher(typ, id)
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, resMatch), "")
}
```

- [ ] **Step 4: Update stack and legacy SSE call sites in `router.go`**

In `internal/api/router.go`, update the stack SSE handler (around line 301):
```go
h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, stackMatch), "")
```

And the legacy events endpoint (around line 95):
```go
b.ServeSSE(w, r, h.aclMatchWrap(r, nil), "")
```

Wait — this is `b.ServeSSE(w, r, ...)` directly on the broadcaster, not via handlers. Check if this is `ServeHTTP` or a direct call. If it's `ServeHTTP`, it's already handled in Task 4 Step 5. If it's a direct `ServeSSE` call, add the empty `replayType`.

- [ ] **Step 5: Update test files that create broadcasters**

In `internal/api/integration_test.go`, `internal/api/openapi_test.go`, `internal/api/middleware_test.go`: update `sse.NewBroadcaster(...)` calls to include `nil` as the third argument.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add main.go internal/cache/cache.go internal/api/health_handlers.go internal/api/router.go internal/api/integration_test.go internal/api/openapi_test.go internal/api/middleware_test.go
git commit -m "feat: wire SSE Last-Event-ID replay into all SSE endpoints"
```

---

### Task 6: Frontend — handle replay events without resource payload

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts:96` (fallback for missing resource)

- [ ] **Step 1: Update the SSE event handler**

In `frontend/src/hooks/useSwarmResource.ts`, the current logic at line 96 is:

```typescript
} else if (event.resource) {
```

Events without a resource payload (replayed events) are silently ignored. Add a fallback that triggers a refetch:

```typescript
      } else if (event.resource) {
        const resource = event.resource as T;
        const index = previous.findIndex((item) => getIdRef.current(item) === event.id);

        if (index >= 0) {
          const next = [...previous];
          next[index] = resource;
          setData(next);
        } else {
          setData([...previous, resource]);
          setSSEOffset((offset) => offset + 1);
        }
      } else if (event.action !== "remove") {
        // Replayed event without resource payload — refetch to pick up changes
        loadRef.current();
      }
```

- [ ] **Step 2: Verify the frontend builds**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No type errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSwarmResource.ts
git commit -m "feat(frontend): handle SSE replay events without resource payload"
```

---

### Task 7: Integration test — end-to-end replay

**Files:**
- Modify: `internal/api/sse/broadcaster_test.go` (add integration-style test)

- [ ] **Step 1: Write an end-to-end replay test**

This test verifies the full flow: events are broadcast, a client connects with `Last-Event-ID`, and receives replayed events followed by live events.

In `internal/api/sse/broadcaster_test.go`:

```go
func TestSSE_EndToEnd_ReplayThenLive(t *testing.T) {
	h := cache.NewHistory(100)
	// Simulate 5 past events
	for i := range 5 {
		h.Append(cache.HistoryEntry{
			Type:       "service",
			Action:     "update",
			ResourceID: fmt.Sprintf("s%d", i),
			Name:       fmt.Sprintf("svc-%d", i),
		})
	}

	b := NewBroadcaster(10*time.Millisecond, noopErrorWriter, h)
	go b.fanOut()
	defer b.Close()

	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Last-Event-ID", "3") // Should replay IDs 4, 5

	done := make(chan struct{})
	go func() {
		b.ServeSSE(w, req, TypeMatcher("service"), "service")
		close(done)
	}()
	waitForClients(t, b, 1)

	// Now broadcast a live event
	b.Broadcast(cache.Event{
		Type: "service", Action: "create", ID: "s5", Name: "svc-5", HistoryID: 6,
	})

	waitForBody(t, w, "svc-5")
	b.Close()
	<-done

	body := w.bodyString()
	// Should contain replayed events (svc-3, svc-4) and live event (svc-5)
	if !strings.Contains(body, "svc-3") || !strings.Contains(body, "svc-4") {
		t.Errorf("missing replayed events in %q", body)
	}
	if !strings.Contains(body, "svc-5") {
		t.Errorf("missing live event in %q", body)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/api/sse/ -run TestSSE_EndToEnd -v`
Expected: PASS

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

- [ ] **Step 4: Run frontend lint and type check**

Run: `cd frontend && npm run lint && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/api/sse/broadcaster_test.go
git commit -m "test(sse): add end-to-end Last-Event-ID replay test"
```
