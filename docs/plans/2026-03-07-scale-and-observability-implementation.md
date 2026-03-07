# Scale & Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add server-side pagination/sorting, SSE event batching, an in-memory event journal with history API, and network/placement topology visualization.

**Architecture:** Three independent phases. Phase 1 adds pagination helpers to the Go cache and handlers, wraps responses in `{items, total}` envelopes, and updates the frontend to consume paginated data with virtual scrolling. Phase 2 adds a ring buffer event journal to the cache with a REST API and activity feed UI. Phase 3 adds topology graph endpoints and a D3/React Force Graph visualization page.

**Tech Stack:** Go (stdlib HTTP, sync), React 19, TypeScript, TanStack Virtual, D3 force layout (or @xyflow/react)

---

## Phase 1: Scale

### Task 1: Generic Pagination & Sorting Helpers (Backend)

**Files:**
- Create: `internal/api/pagination.go`
- Create: `internal/api/pagination_test.go`

**Step 1: Write the failing tests**

```go
// internal/api/pagination_test.go
package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := &http.Request{URL: &url.URL{}}
	p := parsePagination(r)
	if p.Limit != 50 || p.Offset != 0 {
		t.Errorf("got limit=%d offset=%d, want 50/0", p.Limit, p.Offset)
	}
}

func TestParsePagination_Custom(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "limit=10&offset=20"}}
	p := parsePagination(r)
	if p.Limit != 10 || p.Offset != 20 {
		t.Errorf("got limit=%d offset=%d, want 10/20", p.Limit, p.Offset)
	}
}

func TestParsePagination_MaxLimit(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "limit=9999"}}
	p := parsePagination(r)
	if p.Limit != 200 {
		t.Errorf("got limit=%d, want 200", p.Limit)
	}
}

func TestApplyPagination(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	result := applyPagination(items, PageParams{Limit: 3, Offset: 2})
	if len(result.Items) != 3 || result.Total != 10 {
		t.Errorf("got %d items total=%d, want 3/10", len(result.Items), result.Total)
	}
	if result.Items[0] != 3 {
		t.Errorf("got first=%d, want 3", result.Items[0])
	}
}

func TestApplyPagination_BeyondEnd(t *testing.T) {
	items := []int{1, 2, 3}
	result := applyPagination(items, PageParams{Limit: 50, Offset: 10})
	if len(result.Items) != 0 || result.Total != 3 {
		t.Errorf("got %d items total=%d, want 0/3", len(result.Items), result.Total)
	}
}

func TestSortItems(t *testing.T) {
	type item struct{ Name string }
	items := []item{{Name: "charlie"}, {Name: "alpha"}, {Name: "bravo"}}
	accessors := map[string]func(item) string{
		"name": func(i item) string { return i.Name },
	}
	sorted := sortItems(items, "name", "asc", accessors)
	if sorted[0].Name != "alpha" || sorted[2].Name != "charlie" {
		t.Errorf("unexpected order: %v", sorted)
	}
}

func TestSortItems_Desc(t *testing.T) {
	type item struct{ Name string }
	items := []item{{Name: "alpha"}, {Name: "charlie"}, {Name: "bravo"}}
	accessors := map[string]func(item) string{
		"name": func(i item) string { return i.Name },
	}
	sorted := sortItems(items, "name", "desc", accessors)
	if sorted[0].Name != "charlie" || sorted[2].Name != "alpha" {
		t.Errorf("unexpected order: %v", sorted)
	}
}

func TestSortItems_InvalidKey(t *testing.T) {
	type item struct{ Name string }
	items := []item{{Name: "b"}, {Name: "a"}}
	accessors := map[string]func(item) string{
		"name": func(i item) string { return i.Name },
	}
	sorted := sortItems(items, "invalid", "asc", accessors)
	// Should return unsorted (original order)
	if sorted[0].Name != "b" {
		t.Errorf("invalid key should not sort, got %v", sorted)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestParsePagination -v && go test ./internal/api/ -run TestApplyPagination -v && go test ./internal/api/ -run TestSortItems -v`
Expected: FAIL — functions not defined

**Step 3: Write minimal implementation**

```go
// internal/api/pagination.go
package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type PageParams struct {
	Limit  int
	Offset int
	Sort   string
	Dir    string
}

type PagedResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func parsePagination(r *http.Request) PageParams {
	q := r.URL.Query()
	p := PageParams{
		Limit:  defaultLimit,
		Sort:   q.Get("sort"),
		Dir:    q.Get("dir"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Limit = n
		}
	}
	if p.Limit > maxLimit {
		p.Limit = maxLimit
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.Offset = n
		}
	}
	if p.Dir == "" {
		p.Dir = "asc"
	}
	return p
}

func applyPagination[T any](items []T, p PageParams) PagedResponse[T] {
	total := len(items)
	if p.Offset >= total {
		return PagedResponse[T]{Items: []T{}, Total: total}
	}
	end := p.Offset + p.Limit
	if end > total {
		end = total
	}
	return PagedResponse[T]{Items: items[p.Offset:end], Total: total}
}

func sortItems[T any](items []T, key, dir string, accessors map[string]func(T) string) []T {
	accessor, ok := accessors[strings.ToLower(key)]
	if !ok {
		return items
	}
	sorted := make([]T, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := accessor(sorted[i]), accessor(sorted[j])
		if dir == "desc" {
			a, b = b, a
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	return sorted
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run "TestParsePagination|TestApplyPagination|TestSortItems" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/pagination.go internal/api/pagination_test.go
git commit -m "feat: add generic pagination and sorting helpers"
```

---

### Task 2: Update List Handlers to Use Pagination (Backend)

**Files:**
- Modify: `internal/api/handlers.go:62-66` (HandleListNodes and all other list handlers)
- Modify: `internal/api/handlers_test.go` (add pagination tests)

**Step 1: Write failing tests for paginated list responses**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleListServices_Paginated(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "alpha"}}})
	c.SetService(swarm.Service{ID: "s2", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "bravo"}}})
	c.SetService(swarm.Service{ID: "s3", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "charlie"}}})

	h := NewHandlers(c, nil)
	req := httptest.NewRequest("GET", "/api/services?limit=2&offset=0&sort=name&dir=asc", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp struct {
		Items []swarm.Service `json:"items"`
		Total int             `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total != 3 {
		t.Errorf("total=%d, want 3", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Errorf("items=%d, want 2", len(resp.Items))
	}
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Spec.Name)
	}
}

func TestHandleListNodes_Paginated(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "node-a"}})
	c.SetNode(swarm.Node{ID: "n2", Description: swarm.NodeDescription{Hostname: "node-b"}})

	h := NewHandlers(c, nil)
	req := httptest.NewRequest("GET", "/api/nodes?limit=1&sort=hostname&dir=asc", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp struct {
		Items []swarm.Node `json:"items"`
		Total int          `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total != 2 || len(resp.Items) != 1 {
		t.Errorf("total=%d items=%d, want 2/1", resp.Total, len(resp.Items))
	}
	if resp.Items[0].Description.Hostname != "node-a" {
		t.Errorf("first=%s, want node-a", resp.Items[0].Description.Hostname)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandleList.*Paginated" -v`
Expected: FAIL — response is bare array, not `{items, total}`

**Step 3: Update all list handlers**

Replace each list handler in `internal/api/handlers.go`. Pattern for each:

```go
func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	nodes = searchFilter(nodes, r.URL.Query().Get("search"), func(n swarm.Node) string { return n.Description.Hostname })
	p := parsePagination(r)
	nodes = sortItems(nodes, p.Sort, p.Dir, map[string]func(swarm.Node) string{
		"hostname": func(n swarm.Node) string { return n.Description.Hostname },
		"role":     func(n swarm.Node) string { return string(n.Spec.Role) },
		"status":   func(n swarm.Node) string { return string(n.Status.State) },
	})
	writeJSON(w, applyPagination(nodes, p))
}
```

Apply same pattern to: `HandleListServices` (sort by name, image, mode), `HandleListTasks` (sort by service, state, node), `HandleListStacks` (sort by name), `HandleListConfigs` (sort by name), `HandleListSecrets` (sort by name), `HandleListNetworks` (sort by name, driver), `HandleListVolumes` (sort by name, driver).

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS (existing tests will need updating — they now receive `{items: [...], total: N}` instead of bare arrays)

**Step 5: Update existing handler tests**

Any existing tests that decode bare arrays from list endpoints need to decode `PagedResponse` instead. Check all tests calling list handlers and update the decode target.

**Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: paginated response envelope for all list endpoints"
```

---

### Task 3: SSE Event Batching (Backend)

**Files:**
- Modify: `internal/api/sse.go`
- Modify: `internal/api/sse_test.go`

**Step 1: Write failing test for batched events**

Add to `internal/api/sse_test.go`:

```go
func TestBroadcaster_BatchesEvents(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	// Send multiple events rapidly
	for i := 0; i < 5; i++ {
		b.Broadcast(cache.Event{Type: "task", Action: "update", ID: fmt.Sprintf("t%d", i)})
	}

	// Verify events arrive (batching is transparent to the channel consumer)
	// The batching is only in the SSE write path, not in the channel
}
```

**Step 2: Implement batching in SSE write loop**

In the `ServeHTTP` method of the broadcaster, accumulate events for up to 100ms before flushing:

```go
// In the SSE client serve loop, replace the single-event write with:
batchTicker := time.NewTicker(100 * time.Millisecond)
defer batchTicker.Stop()
var batch []cache.Event

for {
	select {
	case e, ok := <-client.events:
		if !ok {
			if len(batch) > 0 {
				writeBatch(w, flusher, batch, eventID)
			}
			return
		}
		batch = append(batch, e)
	case <-batchTicker.C:
		if len(batch) > 0 {
			writeBatch(w, flusher, batch, eventID)
			batch = batch[:0]
		}
	case <-keepalive.C:
		// flush any pending batch first
		if len(batch) > 0 {
			writeBatch(w, flusher, batch, eventID)
			batch = batch[:0]
		}
		fmt.Fprint(w, ": keepalive\n\n")
		flusher.Flush()
	case <-r.Context().Done():
		return
	}
}
```

The batch format uses `event: batch`:
```go
func writeBatch(w io.Writer, flusher http.Flusher, events []cache.Event, eventID *uint64) {
	if len(events) == 1 {
		// Single event — write as before for backward compatibility
		data, _ := json.Marshal(events[0])
		*eventID++
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", *eventID, events[0].Type, data)
	} else {
		data, _ := json.Marshal(events)
		*eventID++
		fmt.Fprintf(w, "id: %d\nevent: batch\ndata: %s\n\n", *eventID, data)
	}
	flusher.Flush()
}
```

**Step 3: Run all SSE tests**

Run: `go test ./internal/api/ -run TestBroadcast -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/sse.go internal/api/sse_test.go
git commit -m "feat: SSE event batching with 100ms window"
```

---

### Task 4: Frontend — Update API Client for Paginated Responses

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

**Step 1: Add PagedResponse type**

In `frontend/src/api/types.ts`, add:

```typescript
export interface PagedResponse<T> {
  items: T[];
  total: number;
}
```

**Step 2: Update API client functions**

In `frontend/src/api/client.ts`, update list endpoints to accept pagination params and return `PagedResponse<T>`:

```typescript
interface ListParams {
  limit?: number;
  offset?: number;
  sort?: string;
  dir?: "asc" | "desc";
  search?: string;
}

function buildListURL(path: string, params?: ListParams): string {
  const qs = new URLSearchParams();
  if (params?.limit) qs.set("limit", String(params.limit));
  if (params?.offset) qs.set("offset", String(params.offset));
  if (params?.sort) qs.set("sort", params.sort);
  if (params?.dir) qs.set("dir", params.dir);
  if (params?.search) qs.set("search", params.search);
  const query = qs.toString();
  return `${path}${query ? `?${query}` : ""}`;
}

export const api = {
  nodes: (params?: ListParams) => fetchJSON<PagedResponse<Node>>(buildListURL("/nodes", params)),
  services: (params?: ListParams) => fetchJSON<PagedResponse<Service>>(buildListURL("/services", params)),
  tasks: (params?: ListParams) => fetchJSON<PagedResponse<Task>>(buildListURL("/tasks", params)),
  stacks: (params?: ListParams) => fetchJSON<PagedResponse<Stack>>(buildListURL("/stacks", params)),
  configs: (params?: ListParams) => fetchJSON<PagedResponse<Config>>(buildListURL("/configs", params)),
  secrets: (params?: ListParams) => fetchJSON<PagedResponse<Secret>>(buildListURL("/secrets", params)),
  networks: (params?: ListParams) => fetchJSON<PagedResponse<Network>>(buildListURL("/networks", params)),
  volumes: (params?: ListParams) => fetchJSON<PagedResponse<Volume>>(buildListURL("/volumes", params)),
  // ... keep all other endpoints unchanged
};
```

**Step 3: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: paginated API client with ListParams support"
```

---

### Task 5: Frontend — Update useSwarmResource for Paginated Data

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts`

**Step 1: Update hook to handle `PagedResponse`**

The hook now receives `{items, total}` from the API instead of bare arrays. It also needs to handle `event: batch` SSE events.

```typescript
import { useState, useEffect, useCallback, useRef } from "react";
import { useSSE } from "./useSSE";
import type { PagedResponse } from "@/api/types";

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

  useEffect(load, []);

  useSSE(
    [sseType],
    useCallback((event) => {
      if (event.action === "remove") {
        setData((prev) => prev.filter((item) => getIdRef.current(item) !== event.id));
        setTotal((prev) => prev - 1);
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
        // If it was a new item (not update), increment total
        setTotal((prev) => {
          // We can't reliably distinguish create vs update from SSE alone,
          // so leave total as-is — it's a hint, not authoritative
          return prev;
        });
      }
    }, []),
  );

  return { data, total, loading, error, retry: load };
}
```

**Step 2: Update SSEContext to handle batch events**

In `frontend/src/hooks/SSEContext.tsx`, add a handler for `event: batch`:

```typescript
// In the EventSource setup, add:
es.addEventListener("batch", (e) => {
  const events = JSON.parse(e.data);
  for (const event of events) {
    // Dispatch each event in the batch to listeners
    notifyListeners(event);
  }
});
```

**Step 3: Update all list page components**

Each list page that calls `useSwarmResource` now passes a function returning `PagedResponse<T>` instead of `T[]`. Since the API client already returns `PagedResponse`, this should work if the pages use `api.services()` etc. But pages that do client-side search need to move search to the API call:

```typescript
// Before:
const { data: services } = useSwarmResource(api.services, "service", (s) => s.ID);
const filtered = useMemo(() => services.filter(...), [services, search]);

// After:
const { data: services, total } = useSwarmResource(
  () => api.services({ search, sort: sortKey, dir: sortDir }),
  "service",
  (s) => s.ID,
);
// No more client-side filtering/sorting — server handles it
```

This means `useSwarmResource`'s `fetchFn` will change on every search/sort change, triggering a refetch. Update the hook to watch for fetchFn changes:

```typescript
// In useSwarmResource, replace the static useEffect with one that re-fetches:
const fetchFnRef = useRef(fetchFn);
useEffect(() => {
  fetchFnRef.current = fetchFn;
  load();
}, [fetchFn]);
```

But be careful with reference stability — the caller should memoize the fetchFn or the hook should accept params directly. **Decision:** Pass params as a dependency array value, not a new function reference each render. Revisit this during implementation — the exact API depends on how pages pass search/sort state.

**Step 4: Run frontend lint and dev server**

Run: `cd frontend && npm run lint && npm run dev`
Expected: No lint errors. Dev server loads pages correctly.

**Step 5: Commit**

```bash
git add frontend/src/hooks/useSwarmResource.ts frontend/src/hooks/SSEContext.tsx frontend/src/pages/*.tsx
git commit -m "feat: frontend pagination support with server-side search/sort"
```

---

### Task 6: Frontend — Virtual Scrolling for List Pages

**Files:**
- Modify: `frontend/package.json` (add @tanstack/react-virtual)
- Modify: `frontend/src/components/DataTable.tsx` (add virtualization)
- Modify: List pages as needed

**Step 1: Install TanStack Virtual**

Run: `cd frontend && npm install @tanstack/react-virtual`

**Step 2: Update DataTable with virtual scrolling**

```typescript
import { useVirtualizer } from "@tanstack/react-virtual";

// In DataTable, wrap the tbody in a virtualizer:
// - Measure the scroll container
// - Only render visible rows + overscan
// - Use estimated row height (e.g., 48px)
```

The key change: the `<tbody>` renders only visible rows using `virtualizer.getVirtualItems()`. The scroll container needs a fixed height (e.g., `calc(100vh - navbar - header)`).

**Step 3: Verify list pages work with virtualization**

Run: `cd frontend && npm run dev`
Navigate to each list page, verify scrolling works smoothly.

**Step 4: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/components/DataTable.tsx
git commit -m "feat: virtual scrolling in DataTable with TanStack Virtual"
```

---

## Phase 2: Event Journal

### Task 7: Ring Buffer Implementation (Backend)

**Files:**
- Create: `internal/cache/history.go`
- Create: `internal/cache/history_test.go`

**Step 1: Write failing tests**

```go
// internal/cache/history_test.go
package cache

import (
	"testing"
	"time"
)

func TestHistory_Append(t *testing.T) {
	h := NewHistory(5)
	h.Append(HistoryEntry{Type: "service", Action: "update", ResourceID: "s1", Name: "nginx"})

	entries := h.List(HistoryQuery{Limit: 10})
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Name != "nginx" {
		t.Errorf("name=%s, want nginx", entries[0].Name)
	}
	if entries[0].ID == 0 {
		t.Error("ID should be auto-assigned")
	}
}

func TestHistory_RingOverflow(t *testing.T) {
	h := NewHistory(3)
	h.Append(HistoryEntry{ResourceID: "1"})
	h.Append(HistoryEntry{ResourceID: "2"})
	h.Append(HistoryEntry{ResourceID: "3"})
	h.Append(HistoryEntry{ResourceID: "4"}) // overwrites "1"

	entries := h.List(HistoryQuery{Limit: 10})
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Most recent first
	if entries[0].ResourceID != "4" {
		t.Errorf("first=%s, want 4", entries[0].ResourceID)
	}
	if entries[2].ResourceID != "2" {
		t.Errorf("last=%s, want 2", entries[2].ResourceID)
	}
}

func TestHistory_FilterByType(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service", ResourceID: "s1"})
	h.Append(HistoryEntry{Type: "node", ResourceID: "n1"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "s2"})

	entries := h.List(HistoryQuery{Type: "service", Limit: 10})
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestHistory_FilterByResourceID(t *testing.T) {
	h := NewHistory(10)
	h.Append(HistoryEntry{Type: "service", ResourceID: "s1"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "s2"})
	h.Append(HistoryEntry{Type: "service", ResourceID: "s1"})

	entries := h.List(HistoryQuery{ResourceID: "s1", Limit: 10})
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestHistory_Limit(t *testing.T) {
	h := NewHistory(100)
	for i := 0; i < 50; i++ {
		h.Append(HistoryEntry{ResourceID: "x"})
	}
	entries := h.List(HistoryQuery{Limit: 5})
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run TestHistory -v`
Expected: FAIL — types not defined

**Step 3: Implement ring buffer**

```go
// internal/cache/history.go
package cache

import (
	"sync"
	"time"
)

type HistoryEntry struct {
	ID         uint64    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary,omitempty"`
}

type HistoryQuery struct {
	Type       string
	ResourceID string
	Limit      int
}

type History struct {
	mu      sync.RWMutex
	entries []HistoryEntry
	size    int
	cursor  int // next write position
	count   int // total entries written (for ID generation)
	full    bool
}

func NewHistory(size int) *History {
	return &History{
		entries: make([]HistoryEntry, size),
		size:    size,
	}
}

func (h *History) Append(e HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	e.ID = uint64(h.count)
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	h.entries[h.cursor] = e
	h.cursor = (h.cursor + 1) % h.size
	if h.count >= h.size {
		h.full = true
	}
}

func (h *History) List(q HistoryQuery) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	var result []HistoryEntry
	n := h.size
	if !h.full {
		n = h.cursor
	}

	// Iterate from newest to oldest
	for i := 0; i < n && len(result) < limit; i++ {
		idx := (h.cursor - 1 - i + h.size) % h.size
		e := h.entries[idx]
		if q.Type != "" && e.Type != q.Type {
			continue
		}
		if q.ResourceID != "" && e.ResourceID != q.ResourceID {
			continue
		}
		result = append(result, e)
	}
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run TestHistory -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cache/history.go internal/cache/history_test.go
git commit -m "feat: ring buffer event journal for state change history"
```

---

### Task 8: Wire History to Cache + Add API Endpoint

**Files:**
- Modify: `internal/cache/cache.go` (add History field)
- Modify: `internal/api/handlers.go` (add HandleHistory)
- Modify: `internal/api/router.go` (add route)
- Add tests in: `internal/api/handlers_test.go`

**Step 1: Add History to Cache**

In `internal/cache/cache.go`, add a `History` field to `Cache` and populate it in `notify()`:

```go
type Cache struct {
	// ... existing fields ...
	history *History
}

func New(onChange OnChangeFunc) *Cache {
	return &Cache{
		// ... existing init ...
		history: NewHistory(10000),
	}
}

func (c *Cache) notify(e Event) {
	// Record to history
	entry := HistoryEntry{
		Type:       e.Type,
		Action:     e.Action,
		ResourceID: e.ID,
	}
	// Extract name from resource if available
	entry.Name = extractName(e)
	c.history.Append(entry)

	if c.onChange != nil {
		c.onChange(e)
	}
}

func (c *Cache) History() *History { return c.history }
```

`extractName` is a helper that type-switches on `e.Resource` to extract a human-readable name (e.g., `swarm.Service` → `s.Spec.Name`).

**Step 2: Add history handler and route**

```go
// In handlers.go:
func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	entries := h.cache.History().List(cache.HistoryQuery{
		Type:       q.Get("type"),
		ResourceID: q.Get("resourceId"),
		Limit:      limit,
	})
	writeJSON(w, entries)
}
```

```go
// In router.go, add:
mux.HandleFunc("GET /api/history", h.HandleHistory)
```

**Step 3: Write tests**

```go
func TestHandleHistory(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	c.SetService(swarm.Service{ID: "s2", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "redis"}}})

	h := NewHandlers(c, nil)
	req := httptest.NewRequest("GET", "/api/history?type=service", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var entries []cache.HistoryEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cache/cache.go internal/api/handlers.go internal/api/router.go internal/api/handlers_test.go
git commit -m "feat: history API endpoint wired to cache event journal"
```

---

### Task 9: Frontend — Activity Feed Component

**Files:**
- Modify: `frontend/src/api/client.ts` (add history endpoint)
- Create: `frontend/src/components/ActivityFeed.tsx`
- Modify: `frontend/src/pages/ClusterOverview.tsx` (add activity feed)
- Modify: Detail pages (add history tab/section)

**Step 1: Add history to API client**

```typescript
// In client.ts, add to api object:
history: (params?: { type?: string; resourceId?: string; limit?: number }) => {
  const qs = new URLSearchParams();
  if (params?.type) qs.set("type", params.type);
  if (params?.resourceId) qs.set("resourceId", params.resourceId);
  if (params?.limit) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return fetchJSON<HistoryEntry[]>(`/history${query ? `?${query}` : ""}`);
},
```

Add `HistoryEntry` type in `types.ts`:

```typescript
export interface HistoryEntry {
  id: number;
  timestamp: string;
  type: string;
  action: string;
  resourceId: string;
  name: string;
  summary?: string;
}
```

**Step 2: Create ActivityFeed component**

```typescript
// frontend/src/components/ActivityFeed.tsx
// A vertical timeline of HistoryEntry items
// Each entry shows: icon (by action), name, type badge, relative timestamp
// Props: entries: HistoryEntry[], loading: boolean
```

**Step 3: Add to ClusterOverview**

Fetch recent history on mount, display below stat cards. Subscribe to SSE to auto-refresh when new events arrive.

**Step 4: Add to detail pages**

ServiceDetail, NodeDetail, StackDetail get a "Recent Activity" section that fetches history filtered by `resourceId`.

**Step 5: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/api/types.ts frontend/src/components/ActivityFeed.tsx frontend/src/pages/*.tsx
git commit -m "feat: activity feed component with history API integration"
```

---

## Phase 3: Topology

### Task 10: Topology API Endpoints (Backend)

**Files:**
- Create: `internal/api/topology.go`
- Create: `internal/api/topology_test.go`
- Modify: `internal/api/router.go`

**Step 1: Define types and write failing tests**

```go
// internal/api/topology_test.go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func TestHandleNetworkTopology(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "web_default", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	h := NewHandlers(c, nil)
	req := httptest.NewRequest("GET", "/api/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp NetworkTopology
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Errorf("got %d edges, want 1 (nginx<->api via net1)", len(resp.Edges))
	}
	if len(resp.Networks) != 1 {
		t.Errorf("got %d networks, want 1", len(resp.Networks))
	}
}

func TestHandlePlacementTopology(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}, Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}, Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", NodeID: "n1", Slot: 1, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	h := NewHandlers(c, nil)
	req := httptest.NewRequest("GET", "/api/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	var resp PlacementTopology
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Nodes) != 1 {
		t.Errorf("got %d nodes, want 1", len(resp.Nodes))
	}
	if len(resp.Nodes[0].Tasks) != 1 {
		t.Errorf("got %d tasks, want 1", len(resp.Nodes[0].Tasks))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandle.*Topology -v`
Expected: FAIL

**Step 3: Implement topology handlers**

```go
// internal/api/topology.go
package api

import "net/http"

type NetworkTopology struct {
	Nodes    []TopoServiceNode `json:"nodes"`
	Edges    []TopoEdge        `json:"edges"`
	Networks []TopoNetwork     `json:"networks"`
}

type TopoServiceNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Stack    string `json:"stack,omitempty"`
	Replicas int    `json:"replicas"`
}

type TopoEdge struct {
	Source   string   `json:"source"`
	Target  string   `json:"target"`
	Networks []string `json:"networks"`
}

type TopoNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
}

type PlacementTopology struct {
	Nodes []TopoClusterNode `json:"nodes"`
}

type TopoClusterNode struct {
	ID       string         `json:"id"`
	Hostname string         `json:"hostname"`
	Role     string         `json:"role"`
	State    string         `json:"state"`
	Tasks    []TopoTask     `json:"tasks"`
}

type TopoTask struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	ServiceName string `json:"serviceName"`
	State       string `json:"state"`
	Slot        int    `json:"slot"`
}

func (h *Handlers) HandleNetworkTopology(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	networks := h.cache.ListNetworks()

	// Build network lookup
	netMap := make(map[string]string) // id -> name
	var topoNets []TopoNetwork
	for _, n := range networks {
		if n.Driver == "overlay" {
			netMap[n.ID] = n.Name
			topoNets = append(topoNets, TopoNetwork{ID: n.ID, Name: n.Name, Driver: n.Driver})
		}
	}

	// Build service nodes and network membership
	var nodes []TopoServiceNode
	svcNetworks := make(map[string]map[string]bool) // serviceID -> set of networkIDs
	for _, svc := range services {
		replicas := 0
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			replicas = int(*svc.Spec.Mode.Replicated.Replicas)
		}
		stack := svc.Spec.Labels["com.docker.stack.namespace"]
		nodes = append(nodes, TopoServiceNode{
			ID: svc.ID, Name: svc.Spec.Name, Stack: stack, Replicas: replicas,
		})
		nets := make(map[string]bool)
		for _, vip := range svc.Endpoint.VirtualIPs {
			if _, ok := netMap[vip.NetworkID]; ok {
				nets[vip.NetworkID] = true
			}
		}
		svcNetworks[svc.ID] = nets
	}

	// Build edges: services sharing overlay networks
	edgeMap := make(map[string]*TopoEdge) // "svcA:svcB" -> edge
	for i, s1 := range services {
		for j := i + 1; j < len(services); j++ {
			s2 := services[j]
			var shared []string
			for netID := range svcNetworks[s1.ID] {
				if svcNetworks[s2.ID][netID] {
					shared = append(shared, netID)
				}
			}
			if len(shared) > 0 {
				edgeMap[s1.ID+":"+s2.ID] = &TopoEdge{
					Source: s1.ID, Target: s2.ID, Networks: shared,
				}
			}
		}
	}
	var edges []TopoEdge
	for _, e := range edgeMap {
		edges = append(edges, *e)
	}

	writeJSON(w, NetworkTopology{Nodes: nodes, Edges: edges, Networks: topoNets})
}

func (h *Handlers) HandlePlacementTopology(w http.ResponseWriter, r *http.Request) {
	nodes := h.cache.ListNodes()
	services := h.cache.ListServices()

	// Service name lookup
	svcNames := make(map[string]string)
	for _, svc := range services {
		svcNames[svc.ID] = svc.Spec.Name
	}

	var result []TopoClusterNode
	for _, node := range nodes {
		tasks := h.cache.ListTasksByNode(node.ID)
		var topoTasks []TopoTask
		for _, t := range tasks {
			topoTasks = append(topoTasks, TopoTask{
				ID:          t.ID,
				ServiceID:   t.ServiceID,
				ServiceName: svcNames[t.ServiceID],
				State:       string(t.Status.State),
				Slot:        t.Slot,
			})
		}
		result = append(result, TopoClusterNode{
			ID:       node.ID,
			Hostname: node.Description.Hostname,
			Role:     string(node.Spec.Role),
			State:    string(node.Status.State),
			Tasks:    topoTasks,
		})
	}

	writeJSON(w, PlacementTopology{Nodes: result})
}
```

```go
// In router.go, add:
mux.HandleFunc("GET /api/topology/networks", h.HandleNetworkTopology)
mux.HandleFunc("GET /api/topology/placement", h.HandlePlacementTopology)
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -run TestHandle.*Topology -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/topology.go internal/api/topology_test.go internal/api/router.go
git commit -m "feat: network and placement topology API endpoints"
```

---

### Task 11: Frontend — Topology Page with Network Graph

**Files:**
- Modify: `frontend/package.json` (add d3-force or @xyflow/react)
- Create: `frontend/src/pages/Topology.tsx`
- Modify: `frontend/src/api/client.ts` (add topology endpoints)
- Modify: `frontend/src/api/types.ts` (add topology types)
- Modify: `frontend/src/App.tsx` (add route and nav link)

**Step 1: Add topology types and API client**

```typescript
// types.ts additions:
export interface NetworkTopology {
  nodes: TopoServiceNode[];
  edges: TopoEdge[];
  networks: TopoNetwork[];
}
export interface TopoServiceNode {
  id: string;
  name: string;
  stack?: string;
  replicas: number;
}
export interface TopoEdge {
  source: string;
  target: string;
  networks: string[];
}
export interface TopoNetwork {
  id: string;
  name: string;
  driver: string;
}
export interface PlacementTopology {
  nodes: TopoClusterNode[];
}
export interface TopoClusterNode {
  id: string;
  hostname: string;
  role: string;
  state: string;
  tasks: TopoTask[];
}
export interface TopoTask {
  id: string;
  serviceId: string;
  serviceName: string;
  state: string;
  slot: number;
}
```

```typescript
// client.ts additions:
topologyNetworks: () => fetchJSON<NetworkTopology>("/topology/networks"),
topologyPlacement: () => fetchJSON<PlacementTopology>("/topology/placement"),
```

**Step 2: Install graph library**

Run: `cd frontend && npm install d3-force @types/d3-force`

Or if choosing React Flow: `cd frontend && npm install @xyflow/react`

**Decision point:** Evaluate both during implementation. D3 force gives more control, React Flow gives more built-in interaction. Start with D3 force — it's lighter and more flexible.

**Step 3: Create Topology page**

```typescript
// frontend/src/pages/Topology.tsx
// - Tab toggle: "Network" | "Placement"
// - Network tab: SVG with D3 force simulation
//   - Circles for services, sized by replica count
//   - Lines for shared networks
//   - Color by stack
//   - Tooltip on hover showing service name, stack, replicas
//   - Click navigates to /services/:id
// - Placement tab: SVG with node rectangles containing task circles
//   - Rectangles for cluster nodes
//   - Small circles for tasks, colored by service
//   - Legend showing service-to-color mapping
```

**Step 4: Add route and nav link**

In `App.tsx`:
- Add `<Route path="/topology" element={<Topology />} />`
- Add "Topology" to NavLinks

**Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/pages/Topology.tsx frontend/src/api/client.ts frontend/src/api/types.ts frontend/src/App.tsx
git commit -m "feat: topology page with network and placement graph views"
```

---

### Task 12: Frontend — Polish Topology Interactions

**Files:**
- Modify: `frontend/src/pages/Topology.tsx`

**Step 1: Add interactivity**

- Zoom/pan via D3 zoom behavior
- Drag nodes in force layout
- Click service node → navigate to `/services/:id`
- Click cluster node → navigate to `/nodes/:id`
- Hover shows tooltip with details
- Legend for stack colors (network view) and service colors (placement view)
- SSE subscription to re-render on topology changes (new service, task moved)

**Step 2: Add loading and empty states**

- Skeleton placeholder while fetching topology data
- Empty state if no overlay networks or no nodes

**Step 3: Test in dev server**

Run: `cd frontend && npm run dev`
Navigate to `/topology`, verify both views render and interactions work.

**Step 4: Commit**

```bash
git add frontend/src/pages/Topology.tsx
git commit -m "feat: topology graph interactivity — zoom, drag, tooltips, navigation"
```

---

## Integration Checklist

After all tasks are complete:

1. Run full backend test suite: `go test ./...`
2. Run frontend lint: `cd frontend && npm run lint`
3. Run frontend format check: `cd frontend && npm run fmt:check`
4. Build frontend: `cd frontend && npm run build`
5. Build Go binary: `go build -o cetacean .`
6. Smoke test: Run locally, verify all pages load, pagination works, topology renders
7. Run `make check` for full CI-equivalent validation
