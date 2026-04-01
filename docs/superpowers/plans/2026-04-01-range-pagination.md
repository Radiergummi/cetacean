# Range Request Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add HTTP Range Request pagination with a custom `items` unit as a progressive enhancement over query params, and switch the frontend to infinite scroll using Range headers exclusively.

**Architecture:** Backend `parsePagination` gains Range header parsing with query-param override. A new `writeCollectionResponse` function encapsulates status code selection (200/206/416), `Content-Range`/`Accept-Ranges` headers, and Link header generation. All 8 list handlers switch to the new function. Frontend gets a `fetchRange` helper, `useSwarmResource` becomes page-accumulating with `loadMore`, and `DataTable` gains scroll-triggered infinite loading.

**Tech Stack:** Go stdlib `net/http`, React 19, `@tanstack/react-virtual`, TypeScript, Vitest

---

### Task 1: Range Header Parsing in `parsePagination`

**Files:**
- Modify: `internal/api/pagination.go:13-51`
- Test: `internal/api/pagination_test.go`

- [ ] **Step 1: Write failing tests for Range header parsing**

Add to `internal/api/pagination_test.go`:

```go
func TestParsePagination_RangeHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-24")
	p, err := parsePagination(r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Offset != 0 {
		t.Errorf("expected offset 0, got %d", p.Offset)
	}
	if p.Limit != 25 {
		t.Errorf("expected limit 25, got %d", p.Limit)
	}
	if !p.RangeReq {
		t.Error("expected RangeReq true")
	}
}

func TestParsePagination_RangeHeaderClampsMax(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-999")
	p, err := parsePagination(r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 200 {
		t.Errorf("expected limit clamped to 200, got %d", p.Limit)
	}
	if !p.RangeReq {
		t.Error("expected RangeReq true")
	}
}

func TestParsePagination_QueryParamsOverrideRange(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?limit=10&offset=5", nil)
	r.Header.Set("Range", "items 0-24")
	p, err := parsePagination(r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 10 {
		t.Errorf("expected limit 10, got %d", p.Limit)
	}
	if p.Offset != 5 {
		t.Errorf("expected offset 5, got %d", p.Offset)
	}
	if p.RangeReq {
		t.Error("expected RangeReq false when query params present")
	}
}

func TestParsePagination_NonItemsUnitIgnored(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes=0-100")
	p, err := parsePagination(r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Limit != 50 {
		t.Errorf("expected default limit 50, got %d", p.Limit)
	}
	if p.RangeReq {
		t.Error("expected RangeReq false for non-items unit")
	}
}

func TestParsePagination_MultipartRangeError(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "items 0-9, 50-59")
	_, err := parsePagination(r)

	if err == nil {
		t.Fatal("expected error for multipart range")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestParsePagination_Range -v`
Expected: Compilation error — `parsePagination` returns `PageParams`, not `(PageParams, error)`.

- [ ] **Step 3: Implement Range parsing**

In `internal/api/pagination.go`, update the `PageParams` struct and `parsePagination` function:

```go
type PageParams struct {
	Limit    int
	Offset   int
	Sort     string
	Dir      string
	RangeReq bool
}

// errMultipartRange is returned when a Range header contains multiple ranges.
var errMultipartRange = errors.New("multipart ranges not supported")

func parsePagination(r *http.Request) (PageParams, error) {
	p := PageParams{
		Limit:  50,
		Offset: 0,
		Dir:    "asc",
	}

	p.Sort = r.URL.Query().Get("sort")
	if v := r.URL.Query().Get("dir"); v != "" {
		p.Dir = v
	}
	if p.Dir != "desc" {
		p.Dir = "asc"
	}

	// Query params take priority over Range header.
	hasQueryPagination := r.URL.Query().Has("limit") || r.URL.Query().Has("offset")
	if hasQueryPagination {
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				p.Limit = n
			}
		}
		if p.Limit > 200 {
			p.Limit = 200
		}

		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				p.Offset = n
			}
		}

		return p, nil
	}

	// Try Range header with "items" unit.
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		if start, end, ok := parseItemsRange(rangeHeader); ok {
			p.Offset = start
			p.Limit = end - start + 1
			if p.Limit > 200 {
				p.Limit = 200
			}
			p.RangeReq = true

			return p, nil
		}

		// Check if it was an items range but multipart.
		if strings.HasPrefix(rangeHeader, "items ") && strings.Contains(rangeHeader, ",") {
			return p, errMultipartRange
		}
	}

	return p, nil
}

// parseItemsRange parses "items <start>-<end>" into start and end integers.
// Returns false if the header doesn't match the items unit or is malformed.
func parseItemsRange(header string) (start, end int, ok bool) {
	rest, found := strings.CutPrefix(header, "items ")
	if !found {
		return 0, 0, false
	}

	// Reject multipart ranges.
	if strings.Contains(rest, ",") {
		return 0, 0, false
	}

	startStr, endStr, found := strings.Cut(rest, "-")
	if !found {
		return 0, 0, false
	}

	start, err := strconv.Atoi(strings.TrimSpace(startStr))
	if err != nil || start < 0 {
		return 0, 0, false
	}

	end, err = strconv.Atoi(strings.TrimSpace(endStr))
	if err != nil || end < start {
		return 0, 0, false
	}

	return start, end, true
}
```

Add `"errors"` to the imports.

- [ ] **Step 4: Update all callers of `parsePagination`**

Every list handler calls `p := parsePagination(r)`. Update each to:

```go
p, err := parsePagination(r)
if err != nil {
    writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
    return
}
```

Affected files (the `parsePagination` call site in each):
- `internal/api/node_handlers.go:36`
- `internal/api/service_handlers.go:50`
- `internal/api/task_handlers.go:77`
- `internal/api/config_handlers.go:70`
- `internal/api/secret_handlers.go:75`
- `internal/api/network_handlers.go:76`
- `internal/api/volume_handlers.go:72`
- `internal/api/stack_handlers.go:39`

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestParsePagination -v`
Expected: All `TestParsePagination*` tests pass (both new and existing).

- [ ] **Step 6: Commit**

```bash
git add internal/api/pagination.go internal/api/pagination_test.go internal/api/node_handlers.go internal/api/service_handlers.go internal/api/task_handlers.go internal/api/config_handlers.go internal/api/secret_handlers.go internal/api/network_handlers.go internal/api/volume_handlers.go internal/api/stack_handlers.go
git commit -m "feat(api): add Range header parsing to parsePagination"
```

---

### Task 2: `writeCollectionResponse` Function

**Files:**
- Modify: `internal/api/pagination.go`
- Test: `internal/api/pagination_test.go`

- [ ] **Step 1: Write failing tests for writeCollectionResponse**

Add to `internal/api/pagination_test.go`:

```go
func TestWriteCollectionResponse_RangePartial(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	resp := CollectionResponse[int]{
		Items:  []int{0, 1, 2},
		Total:  100,
		Limit:  3,
		Offset: 0,
	}
	p := PageParams{Limit: 3, Offset: 0, RangeReq: true}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusPartialContent {
		t.Errorf("expected 206, got %d", w.Code)
	}
	cr := w.Header().Get("Content-Range")
	if cr != "items 0-2/100" {
		t.Errorf("expected Content-Range 'items 0-2/100', got %q", cr)
	}
	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges 'items', got %q", ar)
	}
}

func TestWriteCollectionResponse_RangeFullCollection(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	resp := CollectionResponse[int]{
		Items:  []int{0, 1, 2},
		Total:  3,
		Limit:  50,
		Offset: 0,
	}
	p := PageParams{Limit: 50, Offset: 0, RangeReq: true}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for full collection, got %d", w.Code)
	}
	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges 'items', got %q", ar)
	}
}

func TestWriteCollectionResponse_RangeBeyondTotal(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	resp := CollectionResponse[int]{
		Items:  []int{},
		Total:  50,
		Limit:  25,
		Offset: 100,
	}
	p := PageParams{Limit: 25, Offset: 100, RangeReq: true}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("expected 416, got %d", w.Code)
	}
	cr := w.Header().Get("Content-Range")
	if cr != "items */50" {
		t.Errorf("expected Content-Range 'items */50', got %q", cr)
	}
}

func TestWriteCollectionResponse_RangeEmptyCollection(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	resp := CollectionResponse[int]{
		Items:  []int{},
		Total:  0,
		Limit:  50,
		Offset: 0,
	}
	p := PageParams{Limit: 50, Offset: 0, RangeReq: true}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for empty collection, got %d", w.Code)
	}
}

func TestWriteCollectionResponse_QueryParams(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	resp := CollectionResponse[int]{
		Items:  []int{0, 1, 2},
		Total:  100,
		Limit:  3,
		Offset: 0,
	}
	p := PageParams{Limit: 3, Offset: 0, RangeReq: false}

	writeCollectionResponse(w, r, resp, p)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ar := w.Header().Get("Accept-Ranges")
	if ar != "items" {
		t.Errorf("expected Accept-Ranges 'items', got %q", ar)
	}
	link := w.Header().Get("Link")
	if !strings.Contains(link, `rel="next"`) {
		t.Errorf("expected Link next header, got %q", link)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestWriteCollectionResponse -v`
Expected: Compilation error — `writeCollectionResponse` is undefined.

- [ ] **Step 3: Implement writeCollectionResponse**

Add to `internal/api/pagination.go`:

```go
// writeCollectionResponse writes a CollectionResponse with appropriate status
// and headers based on whether the request used Range headers or query params.
func writeCollectionResponse[T any](
	w http.ResponseWriter,
	r *http.Request,
	resp CollectionResponse[T],
	p PageParams,
) {
	w.Header().Set("Accept-Ranges", "items")

	if p.RangeReq {
		// Empty collection: return 200.
		if resp.Total == 0 {
			writeCachedJSON(w, r, resp)
			return
		}

		// Offset beyond total: return 416.
		if p.Offset >= resp.Total && resp.Total > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("items */%d", resp.Total))
			writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, "offset beyond total")
			return
		}

		// Range covers entire collection: return 200.
		end := min(p.Offset+len(resp.Items)-1, resp.Total-1)
		if p.Offset == 0 && end >= resp.Total-1 {
			writeCachedJSON(w, r, resp)
			return
		}

		// Partial content.
		w.Header().Set("Content-Range", fmt.Sprintf("items %d-%d/%d", p.Offset, end, resp.Total))
		writeCachedJSONStatus(w, r, resp, http.StatusPartialContent)
		return
	}

	// Query-param path: Link headers as before.
	writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
	writeCachedJSON(w, r, resp)
}
```

- [ ] **Step 4: Add `writeCachedJSONStatus` helper**

Add to `internal/api/etag.go`:

```go
// writeCachedJSONStatus is like writeCachedJSON but uses the given status code
// instead of 200. Used for 206 Partial Content responses.
func writeCachedJSONStatus(w http.ResponseWriter, r *http.Request, v any, status int) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeErrorCode(w, r, "API009", "failed to serialize response")
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(status)
	w.Write(body)         //nolint:errcheck
	w.Write([]byte{'\n'}) //nolint:errcheck
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestWriteCollectionResponse -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/pagination.go internal/api/pagination_test.go internal/api/etag.go
git commit -m "feat(api): add writeCollectionResponse with Range/206 support"
```

---

### Task 3: Switch List Handlers to `writeCollectionResponse`

**Files:**
- Modify: `internal/api/node_handlers.go`
- Modify: `internal/api/service_handlers.go`
- Modify: `internal/api/task_handlers.go`
- Modify: `internal/api/config_handlers.go`
- Modify: `internal/api/secret_handlers.go`
- Modify: `internal/api/network_handlers.go`
- Modify: `internal/api/volume_handlers.go`
- Modify: `internal/api/stack_handlers.go`

- [ ] **Step 1: Update node handler (standard pattern)**

In `internal/api/node_handlers.go`, replace lines 43-45:

```go
// Before:
resp := applyPagination(r.Context(), nodes, p)
writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
writeCachedJSON(w, r, resp)

// After:
resp := applyPagination(r.Context(), nodes, p)
writeCollectionResponse(w, r, resp, p)
```

The same replacement applies to these handlers that follow the standard pattern (applyPagination → writeCollectionResponse):
- `internal/api/config_handlers.go` (~lines 73-77)
- `internal/api/secret_handlers.go` (~lines 78-82)
- `internal/api/network_handlers.go` (~lines 79-83)
- `internal/api/volume_handlers.go` (~lines 75-79)
- `internal/api/stack_handlers.go` (~lines 42-46)

- [ ] **Step 2: Update service handler (post-pagination transform)**

In `internal/api/service_handlers.go`, the handler transforms `paged.Items` into `ServiceListItem` after pagination, then builds a new `CollectionResponse`. Replace lines 60-75:

```go
// Before:
paged := applyPagination(r.Context(), services, p)
items := make([]ServiceListItem, len(paged.Items))
for i, svc := range paged.Items {
    items[i] = ServiceListItem{
        Service:      svc,
        RunningTasks: h.cache.RunningTaskCount(svc.ID),
    }
}
writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
writeCachedJSON(
    w,
    r,
    NewCollectionResponse(r.Context(), items, paged.Total, paged.Limit, paged.Offset),
)

// After:
paged := applyPagination(r.Context(), services, p)
items := make([]ServiceListItem, len(paged.Items))
for i, svc := range paged.Items {
    items[i] = ServiceListItem{
        Service:      svc,
        RunningTasks: h.cache.RunningTaskCount(svc.ID),
    }
}
writeCollectionResponse(
    w, r,
    NewCollectionResponse(r.Context(), items, paged.Total, paged.Limit, paged.Offset),
    p,
)
```

- [ ] **Step 3: Update task handler (post-pagination transform)**

In `internal/api/task_handlers.go`, replace lines 83-95:

```go
// Before:
paged := applyPagination(r.Context(), tasks, p)
writePaginationLinks(w, r, paged.Total, paged.Limit, paged.Offset)
writeCachedJSON(
    w,
    r,
    NewCollectionResponse(
        r.Context(),
        h.enrichTasks(paged.Items),
        paged.Total,
        paged.Limit,
        paged.Offset,
    ),
)

// After:
paged := applyPagination(r.Context(), tasks, p)
writeCollectionResponse(
    w, r,
    NewCollectionResponse(
        r.Context(),
        h.enrichTasks(paged.Items),
        paged.Total,
        paged.Limit,
        paged.Offset,
    ),
    p,
)
```

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -v`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/node_handlers.go internal/api/service_handlers.go internal/api/task_handlers.go internal/api/config_handlers.go internal/api/secret_handlers.go internal/api/network_handlers.go internal/api/volume_handlers.go internal/api/stack_handlers.go
git commit -m "refactor(api): switch list handlers to writeCollectionResponse"
```

---

### Task 4: Integration Test — Range Request End-to-End

**Files:**
- Test: `internal/api/pagination_test.go`

- [ ] **Step 1: Write integration test using httptest handler**

Add to `internal/api/pagination_test.go`:

```go
func TestRangeRequest_EndToEnd(t *testing.T) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePagination(r)
		if err != nil {
			writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		resp := applyPagination(r.Context(), items, p)
		writeCollectionResponse(w, r, resp, p)
	})

	t.Run("range returns 206 with Content-Range", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 0-9")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusPartialContent {
			t.Errorf("expected 206, got %d", w.Code)
		}
		if cr := w.Header().Get("Content-Range"); cr != "items 0-9/100" {
			t.Errorf("expected Content-Range 'items 0-9/100', got %q", cr)
		}
		if ar := w.Header().Get("Accept-Ranges"); ar != "items" {
			t.Errorf("expected Accept-Ranges 'items', got %q", ar)
		}
	})

	t.Run("query params override range", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items?limit=5&offset=10", nil)
		r.Header.Set("Range", "items 0-9")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if w.Header().Get("Content-Range") != "" {
			t.Error("expected no Content-Range when query params used")
		}
	})

	t.Run("multipart range returns 416", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 0-9, 20-29")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("expected 416, got %d", w.Code)
		}
	})

	t.Run("range beyond total returns 416", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		r.Header.Set("Range", "items 200-209")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Errorf("expected 416, got %d", w.Code)
		}
		if cr := w.Header().Get("Content-Range"); cr != "items */100" {
			t.Errorf("expected Content-Range 'items */100', got %q", cr)
		}
	})

	t.Run("no range and no query params returns 200 with defaults", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/items", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if ar := w.Header().Get("Accept-Ranges"); ar != "items" {
			t.Errorf("expected Accept-Ranges 'items', got %q", ar)
		}
	})
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestRangeRequest_EndToEnd -v`
Expected: All subtests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/api/pagination_test.go
git commit -m "test(api): add Range request end-to-end integration tests"
```

---

### Task 5: Frontend `fetchRange` Helper

**Files:**
- Modify: `frontend/src/api/client.ts:112-127,240-257`
- Test: `frontend/src/api/client.test.ts`

- [ ] **Step 1: Write failing tests for fetchRange and updated list methods**

Add to `frontend/src/api/client.test.ts`:

```typescript
it("sends Range header for nodes list", async () => {
  mockFetch.mockReturnValue(jsonResponse({ items: [{ ID: "n1" }], total: 10, limit: 50, offset: 0 }, 206));
  await api.nodes({ sort: "hostname", dir: "asc" });
  const [url, options] = mockFetch.mock.calls[0];
  expect(url).toBe("/nodes?sort=hostname&dir=asc");
  expect(options.headers.Range).toBe("items 0-49");
  expect(options.headers.Accept).toBe("application/json");
});

it("sends custom Range offset for nodes list", async () => {
  mockFetch.mockReturnValue(jsonResponse({ items: [], total: 10, limit: 50, offset: 50 }, 206));
  await api.nodes({ sort: "hostname", offset: 50 });
  const [url, options] = mockFetch.mock.calls[0];
  expect(url).toBe("/nodes?sort=hostname");
  expect(options.headers.Range).toBe("items 50-99");
});

it("accepts 206 as success", async () => {
  mockFetch.mockReturnValue(jsonResponse({ items: [{ ID: "n1" }], total: 1, limit: 50, offset: 0 }, 206));
  const result = await api.nodes();
  expect(result.items).toHaveLength(1);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/api/client.test.ts`
Expected: Failures — current list methods don't send Range header.

- [ ] **Step 3: Implement fetchRange and update list methods**

In `frontend/src/api/client.ts`:

Replace the `ListParams` interface:

```typescript
export interface ListParams {
  offset?: number;
  sort?: string;
  dir?: "asc" | "desc";
  search?: string;
  filter?: string;
}
```

Replace `buildListURL`:

```typescript
function buildListQueryString(params?: ListParams): string {
  const qs = new URLSearchParams();
  if (params?.sort) qs.set("sort", params.sort);
  if (params?.dir) qs.set("dir", params.dir);
  if (params?.search) qs.set("search", params.search);
  if (params?.filter) qs.set("filter", params.filter);
  const query = qs.toString();
  return query ? `?${query}` : "";
}
```

Add `fetchRange`:

```typescript
const pageSize = 50;

async function fetchRange<T>(
  path: string,
  params?: ListParams,
  signal?: AbortSignal,
): Promise<CollectionResponse<T>> {
  const offset = params?.offset ?? 0;
  const end = offset + pageSize - 1;
  const url = `${path}${buildListQueryString(params)}`;

  const res = await fetch(apiPath(url), {
    headers: {
      Accept: "application/json",
      Range: `items ${offset}-${end}`,
    },
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!res.ok && res.status !== 206) {
    if (res.status === 401 && res.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLogin();
    }

    await throwResponseError(res);
  }

  return res.json();
}
```

Update all list methods to use `fetchRange`:

```typescript
nodes: (params?: ListParams) => fetchRange<Node>("/nodes", params),
services: (params?: ListParams) => fetchRange<ServiceListItem>("/services", params),
tasks: (params?: ListParams) => fetchRange<Task>("/tasks", params),
stacks: (params?: ListParams) => fetchRange<Stack>("/stacks", params),
configs: (params?: ListParams) => fetchRange<Config>("/configs", params),
secrets: (params?: ListParams) => fetchRange<Secret>("/secrets", params),
networks: (params?: ListParams) => fetchRange<Network>("/networks", params),
volumes: (params?: ListParams) => fetchRange<Volume>("/volumes", params),
```

Remove the old `buildListURL` function.

- [ ] **Step 4: Update existing test for nodes fetch**

The existing test `"fetches nodes"` expects headers to be `{ Accept: "application/json" }`. Update it to match the new Range header:

```typescript
it("fetches nodes", async () => {
  mockFetch.mockReturnValue(jsonResponse({ items: [{ ID: "n1" }], total: 1, limit: 50, offset: 0 }));
  const result = await api.nodes();
  expect(result).toEqual({ items: [{ ID: "n1" }], total: 1, limit: 50, offset: 0 });
  expect(mockFetch).toHaveBeenCalledWith(
    "/nodes",
    expect.objectContaining({
      headers: expect.objectContaining({
        Accept: "application/json",
        Range: "items 0-49",
      }),
    }),
  );
});
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/api/client.test.ts`
Expected: All pass.

- [ ] **Step 6: Fix any TypeScript errors across the frontend**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

The `ListParams` interface changed (removed `limit`, added `filter`). Any callers passing `limit` need updating. Check for compilation errors and fix. The main callers are list pages that pass `{ search, sort, dir }` — these should be fine since `limit` was never explicitly passed.

- [ ] **Step 7: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/api/client.ts frontend/src/api/client.test.ts
git commit -m "feat(frontend): add fetchRange helper with Range header pagination"
```

---

### Task 6: `useSwarmResource` — Paginated Accumulation

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts`
- Test: `frontend/src/hooks/useSwarmResource.test.tsx`

- [ ] **Step 1: Write failing tests for infinite scroll behavior**

Add to `frontend/src/hooks/useSwarmResource.test.tsx`:

```typescript
it("exposes loadMore and hasMore for pagination", async () => {
  const page0: Item[] = [
    { ID: "1", Name: "a" },
    { ID: "2", Name: "b" },
  ];
  const page1: Item[] = [{ ID: "3", Name: "c" }];

  const fetchFn = vi
    .fn()
    .mockResolvedValueOnce({ items: page0, total: 3, limit: 2, offset: 0 })
    .mockResolvedValueOnce({ items: page1, total: 3, limit: 2, offset: 2 });

  const { result } = renderHook(
    () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
    { wrapper },
  );

  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data).toEqual(page0);
  expect(result.current.hasMore).toBe(true);

  await act(async () => {
    result.current.loadMore();
  });

  await waitFor(() => expect(result.current.data).toHaveLength(3));
  expect(result.current.data).toEqual([...page0, ...page1]);
  expect(result.current.hasMore).toBe(false);
});

it("resets pages on fetchFn change", async () => {
  const fetchFn1 = vi.fn().mockResolvedValue({
    items: [{ ID: "1", Name: "a" }],
    total: 1,
    limit: 50,
    offset: 0,
  });
  const fetchFn2 = vi.fn().mockResolvedValue({
    items: [{ ID: "2", Name: "b" }],
    total: 1,
    limit: 50,
    offset: 0,
  });

  const { result, rerender } = renderHook(
    ({ fn }) => useSwarmResource(fn, "service", ({ ID }: Item) => ID),
    { wrapper, initialProps: { fn: fetchFn1 } },
  );

  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.data[0].ID).toBe("1");

  rerender({ fn: fetchFn2 });
  await waitFor(() => expect(result.current.data[0].ID).toBe("2"));
});

it("SSE bumps total for unknown items in paginated mode", async () => {
  const fetchFn = vi.fn().mockResolvedValue({
    items: [{ ID: "1", Name: "a" }],
    total: 5,
    limit: 2,
    offset: 0,
  });

  const { result } = renderHook(
    () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
    { wrapper },
  );

  await waitFor(() => expect(result.current.loading).toBe(false));

  // SSE event for an item NOT in loaded pages — should bump total, not append
  act(() =>
    MockEventSource.instance.simulateEvent("service", {
      type: "service",
      action: "update",
      id: "99",
      resource: { ID: "99", Name: "new" },
    }),
  );

  // Total goes up but data length stays same (item not in loaded window)
  expect(result.current.total).toBe(6);
  expect(result.current.data).toHaveLength(1);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/hooks/useSwarmResource.test.tsx`
Expected: Failures — `loadMore`, `hasMore` not returned from hook.

- [ ] **Step 3: Rewrite useSwarmResource with paginated accumulation**

Replace `frontend/src/hooks/useSwarmResource.ts`:

```typescript
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useCallback, useEffect, useRef, useState } from "react";

const pageSize = 50;

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

export function useSwarmResource<T>(
  fetchFn: (offset: number) => Promise<CollectionResponse<T>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [pages, setPages] = useState<Map<number, T[]>>(new Map());
  const [serverTotal, setServerTotal] = useState(0);
  const [sseOffset, setSSEOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const hasLoadedRef = useRef(false);
  const pendingRefetch = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const fetchFnRef = useRef(fetchFn);
  fetchFnRef.current = fetchFn;

  const loadPage = useCallback(
    (pageNumber: number) => {
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      const isFirstPage = pageNumber === 0;
      if (isFirstPage && !hasLoadedRef.current) {
        setLoading(true);
      }
      if (!isFirstPage) {
        setLoadingMore(true);
      }

      setError(null);
      fetchFn(pageNumber * pageSize)
        .then((response) => {
          if (controller.signal.aborted) return;

          if (isFirstPage) {
            setPages(new Map([[0, response.items]]));
            setSSEOffset(0);
          } else {
            setPages((prev) => new Map(prev).set(pageNumber, response.items));
          }

          setServerTotal(response.total);
          hasLoadedRef.current = true;
        })
        .catch((event) => {
          if (!controller.signal.aborted) {
            setError(event);
          }
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            setLoading(false);
            setLoadingMore(false);
          }
        });
    },
    [fetchFn],
  );

  // Load first page on mount and when fetchFn changes (search/sort/filter change).
  useEffect(() => {
    loadPage(0);
    return () => abortRef.current?.abort();
  }, [loadPage]);

  const loadPageRef = useRef(loadPage);
  loadPageRef.current = loadPage;
  const pagesRef = useRef(pages);
  pagesRef.current = pages;

  // Compute flat data from pages.
  const data = Array.from(pages.entries())
    .sort(([a], [b]) => a - b)
    .flatMap(([, items]) => items);

  const total = serverTotal + sseOffset;
  const loadedCount = data.length;
  const hasMore = loadedCount < total;

  const loadMore = useCallback(() => {
    if (loadingMore) return;
    const nextPage = Math.ceil(loadedCount / pageSize);
    loadPageRef.current(nextPage);
  }, [loadedCount, loadingMore]);

  const dataRef = useRef(data);
  dataRef.current = data;

  useResourceStream(
    ssePathMap[sseType] ?? `/events?types=${sseType}`,
    useCallback((event) => {
      if (event.type === "sync") {
        loadPageRef.current(0);
        return;
      }

      const previous = dataRef.current;

      if (event.action === "remove") {
        const existed = previous.some(
          (item) => getIdRef.current(item) === event.id,
        );

        if (existed) {
          // Remove from pages.
          setPages((prev) => {
            const next = new Map<number, T[]>();
            for (const [pageNum, items] of prev) {
              next.set(
                pageNum,
                items.filter((item) => getIdRef.current(item) !== event.id),
              );
            }
            return next;
          });
        }

        setSSEOffset((offset) => offset - 1);
      } else if (event.resource) {
        const resource = event.resource as T;
        const index = previous.findIndex(
          (item) => getIdRef.current(item) === event.id,
        );

        if (index >= 0) {
          // Update in place: find which page it's in.
          setPages((prev) => {
            const next = new Map<number, T[]>();
            for (const [pageNum, items] of prev) {
              const itemIndex = items.findIndex(
                (item) => getIdRef.current(item) === event.id,
              );

              if (itemIndex >= 0) {
                const updated = [...items];
                updated[itemIndex] = resource;
                next.set(pageNum, updated);
              } else {
                next.set(pageNum, items);
              }
            }
            return next;
          });
        } else {
          // Unknown item: just bump total.
          setSSEOffset((offset) => offset + 1);
        }
      } else if (event.action !== "remove") {
        if (!pendingRefetch.current) {
          pendingRefetch.current = true;
          queueMicrotask(() => {
            pendingRefetch.current = false;
            loadPageRef.current(0);
          });
        }
      }
    }, []),
  );

  const retry = useCallback(() => loadPage(0), [loadPage]);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore };
}
```

- [ ] **Step 4: Update the `fetchFn` signature in all list pages**

The hook now expects `fetchFn: (offset: number) => Promise<CollectionResponse<T>>`. Update each list page. For example, `NodeList.tsx`:

```typescript
// Before:
useSwarmResource(
  useCallback(
    () => api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
    [debouncedSearch, sortKey, sortDir],
  ),
  "node",
  ({ ID }: Node) => ID,
);

// After:
useSwarmResource(
  useCallback(
    (offset: number) =>
      api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }),
    [debouncedSearch, sortKey, sortDir],
  ),
  "node",
  ({ ID }: Node) => ID,
);
```

Apply the same pattern to all list pages:
- `frontend/src/pages/NodeList.tsx`
- `frontend/src/pages/ServiceList.tsx`
- `frontend/src/pages/TaskList.tsx`
- `frontend/src/pages/StackList.tsx`
- `frontend/src/pages/ConfigList.tsx`
- `frontend/src/pages/SecretList.tsx`
- `frontend/src/pages/NetworkList.tsx`
- `frontend/src/pages/VolumeList.tsx`

- [ ] **Step 5: Update existing useSwarmResource tests**

The existing tests pass a `fetchFn` that takes no arguments and returns `{ items, total }`. Update them to match the new signature:
- `fetchFn` should accept an `offset: number` parameter (can be ignored in mocks)
- Return value should be `CollectionResponse` shape: `{ items, total, limit: 50, offset: 0 }`

For example:
```typescript
// Before:
const fetchFn = vi.fn().mockResolvedValue({ items, total: 1 });

// After:
const fetchFn = vi.fn().mockResolvedValue({ items, total: 1, limit: 50, offset: 0 });
```

- [ ] **Step 6: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/hooks/useSwarmResource.test.tsx`
Expected: All tests pass.

- [ ] **Step 7: Run TypeScript type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 8: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/hooks/useSwarmResource.ts frontend/src/hooks/useSwarmResource.test.tsx frontend/src/pages/NodeList.tsx frontend/src/pages/ServiceList.tsx frontend/src/pages/TaskList.tsx frontend/src/pages/StackList.tsx frontend/src/pages/ConfigList.tsx frontend/src/pages/SecretList.tsx frontend/src/pages/NetworkList.tsx frontend/src/pages/VolumeList.tsx
git commit -m "feat(frontend): paginated accumulation in useSwarmResource"
```

---

### Task 7: DataTable Infinite Scroll

**Files:**
- Modify: `frontend/src/components/DataTable.tsx`
- Test: `frontend/src/components/DataTable.test.tsx`

- [ ] **Step 1: Write failing tests for infinite scroll props**

Add to `frontend/src/components/DataTable.test.tsx`:

```typescript
it("calls onLoadMore when hasMore is true and data is rendered", async () => {
  const onLoadMore = vi.fn();
  render(
    <DataTable
      columns={columns}
      data={data}
      keyFn={({ id }) => id}
      hasMore
      onLoadMore={onLoadMore}
    />,
  );
  // With only 2 items (well under virtual threshold), the sentinel
  // should be visible and trigger onLoadMore via IntersectionObserver.
  // The test just verifies the props are accepted and the sentinel renders.
  expect(screen.getByTestId("load-more-sentinel")).toBeInTheDocument();
});

it("does not render sentinel when hasMore is false", () => {
  render(
    <DataTable
      columns={columns}
      data={data}
      keyFn={({ id }) => id}
      hasMore={false}
    />,
  );
  expect(screen.queryByTestId("load-more-sentinel")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/components/DataTable.test.tsx`
Expected: Failures — `hasMore` and `onLoadMore` not accepted, no sentinel element.

- [ ] **Step 3: Add infinite scroll to DataTable**

In `frontend/src/components/DataTable.tsx`, update the `Props` interface and component:

```typescript
interface Props<T> {
  columns: Column<T>[];
  data: T[];
  keyFn: (item: T) => string;
  rowClassName?: (item: T) => string;
  onRowClick?: (item: T) => void;
  hasMore?: boolean;
  onLoadMore?: () => void;
}
```

Add the `hasMore` and `onLoadMore` props to the `DataTable` function signature:

```typescript
export default function DataTable<T>({
  columns,
  data,
  keyFn,
  rowClassName,
  onRowClick,
  hasMore,
  onLoadMore,
}: Props<T>) {
```

Also pass them through to `PlainBody` and `VirtualBody` `Props` usage (these sub-components don't need them, only the outer wrapper does).

Add a sentinel row at the bottom of the table, after the body, using `IntersectionObserver`:

```typescript
const sentinelRef = useRef<HTMLTableRowElement>(null);

useEffect(() => {
  if (!hasMore || !onLoadMore || !sentinelRef.current) return;

  const observer = new IntersectionObserver(
    ([entry]) => {
      if (entry.isIntersecting) {
        onLoadMore();
      }
    },
    { root: scrollRef.current, rootMargin: "200px" },
  );

  observer.observe(sentinelRef.current);
  return () => observer.disconnect();
}, [hasMore, onLoadMore]);
```

Add the sentinel `<tr>` after the body `</tbody>` inside the `<table>`, wrapped in a `<tfoot>`:

```tsx
{hasMore && (
  <tfoot>
    <tr
      ref={sentinelRef}
      data-testid="load-more-sentinel"
    >
      <td
        colSpan={columns.length}
        className="p-3 text-center text-sm text-muted-foreground"
      >
        Loading…
      </td>
    </tr>
  </tfoot>
)}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/components/DataTable.test.tsx`
Expected: All pass.

- [ ] **Step 5: Wire up in list pages**

In each list page, pass `hasMore` and `loadMore` to `DataTable`. Example for `NodeList.tsx`:

```tsx
// Before:
<DataTable
  columns={columns}
  data={nodes}
  keyFn={({ ID }) => ID}
  onRowClick={({ ID }) => navigate(`/nodes/${ID}`)}
/>

// After:
<DataTable
  columns={columns}
  data={nodes}
  keyFn={({ ID }) => ID}
  onRowClick={({ ID }) => navigate(`/nodes/${ID}`)}
  hasMore={hasMore}
  onLoadMore={loadMore}
/>
```

This requires destructuring `hasMore` and `loadMore` from `useSwarmResource` in each list page. Apply to all 8 list pages.

- [ ] **Step 6: Run full frontend test suite and type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run && npx tsc -b --noEmit`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/components/DataTable.tsx frontend/src/components/DataTable.test.tsx frontend/src/pages/NodeList.tsx frontend/src/pages/ServiceList.tsx frontend/src/pages/TaskList.tsx frontend/src/pages/StackList.tsx frontend/src/pages/ConfigList.tsx frontend/src/pages/SecretList.tsx frontend/src/pages/NetworkList.tsx frontend/src/pages/VolumeList.tsx
git commit -m "feat(frontend): infinite scroll in DataTable with load-more sentinel"
```

---

### Task 8: Full Stack Verification and Lint

**Files:** None new

- [ ] **Step 1: Run backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: All pass.

- [ ] **Step 2: Run frontend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

- [ ] **Step 3: Run lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: Clean.

- [ ] **Step 4: Run format check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make fmt-check`
Expected: Clean.

- [ ] **Step 5: Build**

Run: `cd /Users/moritz/GolandProjects/cetacean && make build`
Expected: Builds successfully.
