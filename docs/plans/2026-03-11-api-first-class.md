# First-Class API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Transform Cetacean's internal API into a first-class, externally consumable API with content negotiation, JSON-LD responses, RFC 9457 errors, and OpenAPI documentation.

**Architecture:** Content negotiation on shared URLs (no `/api/v1/` prefix). Accept header determines response format: JSON, HTML (SPA), or SSE. File extension suffixes (`.json`, `.html`) override Accept. Versioning via vendor media type (`application/vnd.cetacean.v1+json`). Meta endpoints under `/-/`.

**Tech Stack:** Go stdlib `net/http`, JSON-LD context document, RFC 9457 problem details, RFC 8288 Link headers, hand-written OpenAPI spec (YAML), Vite proxy config update.

**Design doc:** `docs/plans/2026-03-11-api-first-class-design.md`

---

### Task 1: Content Negotiation Middleware

Add middleware that resolves the effective content type from extension suffix or Accept header, and makes it available to handlers via context.

**Files:**
- Create: `internal/api/negotiate.go`
- Create: `internal/api/negotiate_test.go`

**Step 1: Write the failing tests**

```go
// negotiate_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNegotiateExtensionJSON(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
		// Extension should be stripped from path
		if r.URL.Path != "/services" {
			t.Errorf("path=%q, want /services", r.URL.Path)
		}
	}))
	req := httptest.NewRequest("GET", "/services.json", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateExtensionHTML(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services.html", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateAcceptJSON(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Accept", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateAcceptVendorVersioned(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Accept", "application/vnd.cetacean.v1+json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateAcceptHTML(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateAcceptSSE(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeSSE {
			t.Errorf("got %v, want SSE", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateDefaultJSON(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default)", ct)
		}
	}))
	// No Accept header, no extension → default to JSON
	req := httptest.NewRequest("GET", "/services", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateWildcardDefaultJSON(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (wildcard default)", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Accept", "*/*")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestNegotiateExtensionOverridesAccept(t *testing.T) {
	handler := negotiate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (extension overrides Accept)", ct)
		}
	}))
	req := httptest.NewRequest("GET", "/services.json", nil)
	req.Header.Set("Accept", "text/html")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestNegotiate -v`
Expected: compilation errors (types and functions don't exist yet)

**Step 3: Implement negotiate.go**

```go
// negotiate.go
package api

import (
	"context"
	"net/http"
	"strings"
)

type ContentType int

const (
	ContentTypeJSON ContentType = iota
	ContentTypeHTML
	ContentTypeSSE
)

type contentTypeKey struct{}

func ContentTypeFromContext(ctx context.Context) ContentType {
	if ct, ok := ctx.Value(contentTypeKey{}).(ContentType); ok {
		return ct
	}
	return ContentTypeJSON
}

func negotiate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct, stripped := resolveContentType(r)
		if stripped != "" {
			r = r.Clone(r.Context())
			r.URL.Path = stripped
			r.URL.RawPath = ""
		}
		ctx := context.WithValue(r.Context(), contentTypeKey{}, ct)
		w.Header().Set("Vary", "Accept")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func resolveContentType(r *http.Request) (ContentType, string) {
	// 1. Extension suffix (highest priority)
	path := r.URL.Path
	if strings.HasSuffix(path, ".json") {
		return ContentTypeJSON, strings.TrimSuffix(path, ".json")
	}
	if strings.HasSuffix(path, ".html") {
		return ContentTypeHTML, strings.TrimSuffix(path, ".html")
	}

	// 2. Accept header
	accept := r.Header.Get("Accept")
	if accept == "" || accept == "*/*" {
		return ContentTypeJSON, ""
	}

	ct := parseAccept(accept)
	return ct, ""
}

func parseAccept(accept string) ContentType {
	// Parse Accept header with quality values, return highest priority match.
	// Supported types: application/json, application/vnd.cetacean.v1+json,
	//                  text/html, text/event-stream
	type entry struct {
		ct ContentType
		q  float64
	}

	var best entry
	best.q = -1

	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		mediaType := part
		q := 1.0

		if idx := strings.Index(part, ";"); idx != -1 {
			mediaType = strings.TrimSpace(part[:idx])
			params := part[idx+1:]
			for _, param := range strings.Split(params, ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					if v, err := strconv.ParseFloat(param[2:], 64); err == nil {
						q = v
					}
				}
			}
		}

		var ct ContentType
		var match bool
		switch {
		case mediaType == "application/json" || strings.HasPrefix(mediaType, "application/vnd.cetacean."):
			ct, match = ContentTypeJSON, true
		case mediaType == "text/html" || mediaType == "application/xhtml+xml":
			ct, match = ContentTypeHTML, true
		case mediaType == "text/event-stream":
			ct, match = ContentTypeSSE, true
		case mediaType == "*/*":
			ct, match = ContentTypeJSON, true
			q -= 0.001 // slightly lower priority than explicit match
		}

		if match && q > best.q {
			best = entry{ct: ct, q: q}
		}
	}

	if best.q < 0 {
		return ContentTypeJSON // fallback
	}
	return best.ct
}
```

Note: add `"strconv"` to the imports.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestNegotiate -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/api/negotiate.go internal/api/negotiate_test.go
git commit -m "feat(api): add content negotiation middleware"
```

---

### Task 2: RFC 9457 Problem Details Error Responses

Replace `writeError` with RFC 9457-compliant problem details.

**Files:**
- Create: `internal/api/problem.go`
- Create: `internal/api/problem_test.go`
- Modify: `internal/api/handlers.go` (replace `writeError` calls)

**Step 1: Write the failing tests**

```go
// problem_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes/abc", nil)
	// inject a request ID via context
	ctx := context.WithValue(r.Context(), reqIDKey, "test-req-id")
	r = r.WithContext(ctx)

	writeProblem(w, r, http.StatusNotFound, "node abc not found")

	if w.Code != 404 {
		t.Errorf("status=%d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type=%q, want application/problem+json", ct)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Type != "about:blank" {
		t.Errorf("type=%q, want about:blank", p.Type)
	}
	if p.Status != 404 {
		t.Errorf("status=%d, want 404", p.Status)
	}
	if p.Detail != "node abc not found" {
		t.Errorf("detail=%q", p.Detail)
	}
	if p.Instance != "/nodes/abc" {
		t.Errorf("instance=%q, want /nodes/abc", p.Instance)
	}
	if p.RequestID != "test-req-id" {
		t.Errorf("requestId=%q, want test-req-id", p.RequestID)
	}
}

func TestWriteProblemTyped(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services", nil)

	writeProblemTyped(w, r, ProblemDetail{
		Type:   "urn:cetacean:error:filter-invalid",
		Title:  "Invalid Filter Expression",
		Status: http.StatusBadRequest,
		Detail: "unexpected token at position 12",
	})

	if w.Code != 400 {
		t.Errorf("status=%d, want 400", w.Code)
	}
	var p ProblemDetail
	json.NewDecoder(w.Body).Decode(&p)
	if p.Type != "urn:cetacean:error:filter-invalid" {
		t.Errorf("type=%q", p.Type)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestWriteProblem -v`
Expected: compilation errors

**Step 3: Implement problem.go**

```go
// problem.go
package api

import (
	"net/http"

	"github.com/goccy/go-json"
)

// ProblemDetail represents an RFC 9457 Problem Details object.
type ProblemDetail struct {
	Context   string `json:"@context,omitempty"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := ProblemDetail{
		Context:   "/api/context.jsonld",
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: RequestIDFrom(r.Context()),
	}
	writeProblemTyped(w, r, p)
}

func writeProblemTyped(w http.ResponseWriter, r *http.Request, p ProblemDetail) {
	if p.Context == "" {
		p.Context = "/api/context.jsonld"
	}
	if p.Instance == "" {
		p.Instance = r.URL.Path
	}
	if p.RequestID == "" {
		p.RequestID = RequestIDFrom(r.Context())
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestWriteProblem -v`
Expected: all PASS

**Step 5: Replace writeError with writeProblem across handlers.go**

Search for all `writeError(w,` calls and replace with `writeProblem(w, r,`. The signature change adds the `*http.Request` parameter so that `instance` and `requestId` can be populated.

Specific replacements:
- `writeError(w, http.StatusNotFound, "...")` → `writeProblem(w, r, http.StatusNotFound, "...")`
- `writeError(w, http.StatusBadRequest, "...")` → `writeProblem(w, r, http.StatusBadRequest, "...")`
- `writeError(w, http.StatusServiceUnavailable, "...")` → `writeProblem(w, r, http.StatusServiceUnavailable, "...")`
- `writeError(w, http.StatusInternalServerError, "...")` → `writeProblem(w, r, http.StatusInternalServerError, "...")`

For the filter expression error in `exprFilter`, use `writeProblemTyped` with `urn:cetacean:error:filter-invalid`.

For SSE/log connection limit errors (currently 503), change to 429 with `Retry-After` header:
```go
w.Header().Set("Retry-After", "5")
writeProblem(w, r, http.StatusTooManyRequests, "too many concurrent log streams")
```

Delete the old `writeError` function.

**Step 6: Update exprFilter to pass *http.Request**

The `exprFilter` function currently takes `w http.ResponseWriter` to write errors. Update signature to also accept `r *http.Request`:
```go
func exprFilter[T any](items []T, expr string, env func(T) map[string]any, w http.ResponseWriter, r *http.Request) ([]T, bool)
```

Update all call sites.

**Step 7: Run all tests**

Run: `go test ./internal/api/ -v`
Expected: existing tests fail on response shape — update them to expect `application/problem+json` content-type and new JSON shape for error cases.

**Step 8: Fix failing tests**

Update any test that checks error response bodies to expect `ProblemDetail` shape instead of `{"error": ..., "status": ...}`.

**Step 9: Commit**

```bash
git add internal/api/problem.go internal/api/problem_test.go internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat(api): RFC 9457 problem details error responses"
```

---

### Task 3: JSON-LD Response Wrappers

Add JSON-LD `@context`, `@id`, `@type` to all responses. Normalize detail endpoints to consistent wrapper shape.

**Files:**
- Create: `internal/api/jsonld.go`
- Create: `internal/api/jsonld_test.go`
- Modify: `internal/api/pagination.go` (add `@context`, `@type` to PagedResponse)
- Modify: `internal/api/handlers.go` (normalize detail responses)

**Step 1: Write the failing tests**

```go
// jsonld_test.go
package api

import (
	"encoding/json"
	"testing"
)

func TestDetailResponse(t *testing.T) {
	resp := NewDetailResponse("/nodes/abc", "Node", map[string]any{
		"ID": "abc",
	}, nil)

	b, _ := json.Marshal(resp)
	var m map[string]any
	json.Unmarshal(b, &m)

	if m["@context"] != "/api/context.jsonld" {
		t.Errorf("@context=%v", m["@context"])
	}
	if m["@id"] != "/nodes/abc" {
		t.Errorf("@id=%v", m["@id"])
	}
	if m["@type"] != "Node" {
		t.Errorf("@type=%v", m["@type"])
	}
}

func TestCollectionResponse(t *testing.T) {
	items := []map[string]any{
		{"@id": "/services/a", "@type": "Service", "ID": "a"},
	}
	resp := NewCollectionResponse(items, 10, 50, 0)

	b, _ := json.Marshal(resp)
	var m map[string]any
	json.Unmarshal(b, &m)

	if m["@context"] != "/api/context.jsonld" {
		t.Errorf("@context=%v", m["@context"])
	}
	if m["@type"] != "Collection" {
		t.Errorf("@type=%v", m["@type"])
	}
	if m["total"].(float64) != 10 {
		t.Errorf("total=%v", m["total"])
	}
	if m["limit"].(float64) != 50 {
		t.Errorf("limit=%v", m["limit"])
	}
	if m["offset"].(float64) != 0 {
		t.Errorf("offset=%v", m["offset"])
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestDetailResponse|TestCollectionResponse" -v`
Expected: compilation errors

**Step 3: Implement jsonld.go**

```go
// jsonld.go
package api

const jsonLDContext = "/api/context.jsonld"

// DetailResponse wraps a single resource with JSON-LD metadata.
type DetailResponse struct {
	Context  string       `json:"@context"`
	ID       string       `json:"@id"`
	Type     string       `json:"@type"`
	Resource any          `json:"-"` // embedded dynamically
	Extra    map[string]any `json:"-"`
}

// For clean JSON serialization, implement MarshalJSON that merges the fields.
// The resource key (e.g. "node", "service") and cross-refs are in Extra.

func NewDetailResponse(id, typ string, resource any, extra map[string]any) map[string]any {
	m := map[string]any{
		"@context": jsonLDContext,
		"@id":      id,
		"@type":    typ,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// CollectionResponse is the JSON-LD wrapper for list endpoints.
type CollectionResponse[T any] struct {
	Context string `json:"@context"`
	Type    string `json:"@type"`
	Items   []T    `json:"items"`
	Total   int    `json:"total"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func NewCollectionResponse[T any](items []T, total, limit, offset int) CollectionResponse[T] {
	if items == nil {
		items = []T{}
	}
	return CollectionResponse[T]{
		Context: jsonLDContext,
		Type:    "Collection",
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run "TestDetailResponse|TestCollectionResponse" -v`
Expected: all PASS

**Step 5: Update applyPagination to return CollectionResponse**

Modify `internal/api/pagination.go`:
- `applyPagination` returns `CollectionResponse[T]` instead of `PagedResponse[T]`
- It takes `PageParams` so it can echo `limit` and `offset`
- Delete `PagedResponse` struct (replaced by `CollectionResponse`)

**Step 6: Normalize detail handlers**

In `handlers.go`, update all detail handlers to use `NewDetailResponse`:

- `HandleNodeDetail`: currently returns bare node → wrap in `NewDetailResponse("/nodes/"+id, "Node", map[string]any{"node": node, "services": refs})`
- `HandleServiceDetail`: currently returns bare service → same pattern
- `HandleTaskDetail`: currently returns bare enriched task → same pattern with `"service"` and `"node"` cross-refs
- `HandleConfigDetail`: already returns `{config, services}` → add `@context`, `@id`, `@type`
- `HandleSecretDetail`: same as config
- `HandleNetworkDetail`: same
- `HandleVolumeDetail`: same
- `HandleStackDetail`: already returns a struct — add JSON-LD fields

For cross-references, convert `ServiceRef` to include `@id`:
```go
type ServiceRef struct {
	AtID string `json:"@id"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

This change is in `internal/cache/cache.go`. The `@id` value is `/services/<ID>`. Update `ServicesUsingConfig`, `ServicesUsingSecret`, `ServicesUsingNetwork`, `ServicesUsingVolume` to populate `AtID`.

**Step 7: Add @id and @type to list items**

For list endpoints, each item in the `items` array needs `@id` and `@type`. Since Docker API types don't have these fields, wrap items before serialization. Create a helper:

```go
func withJSONLD(id, typ string, resource any) map[string]any {
	// Serialize resource to map, then add @id and @type
}
```

Or use a wrapper struct with embedded resource + JSON-LD fields. The cleanest approach: add `@id` and `@type` at the serialization boundary in each list handler, since each resource type has a different ID field (`.ID` vs `.Name`).

**Step 8: Run all tests, fix failures**

Run: `go test ./internal/api/ -v`
Expected: many tests fail on response shape. Update all test assertions for the new JSON-LD wrapped responses.

**Step 9: Commit**

```bash
git add internal/api/jsonld.go internal/api/jsonld_test.go internal/api/pagination.go internal/api/handlers.go internal/cache/cache.go
git commit -m "feat(api): JSON-LD response wrappers with @context, @id, @type"
```

---

### Task 4: RFC 8288 Link Headers for Pagination

Add `Link` header with `rel="next"` and `rel="prev"` to paginated responses.

**Files:**
- Modify: `internal/api/pagination.go`
- Modify: `internal/api/pagination_test.go`

**Step 1: Write the failing tests**

```go
// Add to pagination_test.go
func TestPaginationLinkHeaders(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		total    int
		limit    int
		offset   int
		wantNext string
		wantPrev string
	}{
		{
			name:     "first page with more",
			path:     "/services",
			total:    100,
			limit:    50,
			offset:   0,
			wantNext: `</services?limit=50&offset=50>; rel="next"`,
			wantPrev: "",
		},
		{
			name:     "middle page",
			path:     "/services",
			total:    150,
			limit:    50,
			offset:   50,
			wantNext: `</services?limit=50&offset=100>; rel="next"`,
			wantPrev: `</services?limit=50&offset=0>; rel="prev"`,
		},
		{
			name:     "last page",
			path:     "/services",
			total:    100,
			limit:    50,
			offset:   50,
			wantNext: "",
			wantPrev: `</services?limit=50&offset=0>; rel="prev"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writePaginationLinks(w, tt.path, tt.total, tt.limit, tt.offset)
			link := w.Header().Get("Link")
			if tt.wantNext != "" && !strings.Contains(link, tt.wantNext) {
				t.Errorf("missing next link in %q", link)
			}
			if tt.wantPrev != "" && !strings.Contains(link, tt.wantPrev) {
				t.Errorf("missing prev link in %q", link)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestPaginationLinkHeaders -v`

**Step 3: Implement writePaginationLinks**

```go
// Add to pagination.go
func writePaginationLinks(w http.ResponseWriter, path string, total, limit, offset int) {
	var links []string

	if offset+limit < total {
		links = append(links, fmt.Sprintf("<%s?limit=%d&offset=%d>; rel=\"next\"", path, limit, offset+limit))
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		links = append(links, fmt.Sprintf("<%s?limit=%d&offset=%d>; rel=\"prev\"", path, limit, prev))
	}

	if len(links) > 0 {
		w.Header().Set("Link", strings.Join(links, ", "))
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestPaginationLinkHeaders -v`

**Step 5: Wire into list handlers**

Add `writePaginationLinks(w, r.URL.Path, resp.Total, resp.Limit, resp.Offset)` before `writeJSON` in each list handler.

**Step 6: Run all tests**

Run: `go test ./internal/api/ -v`

**Step 7: Commit**

```bash
git add internal/api/pagination.go internal/api/pagination_test.go internal/api/handlers.go
git commit -m "feat(api): RFC 8288 Link headers for pagination"
```

---

### Task 5: RFC 8631 Self-Discovery Link Headers

Add `Link` headers for API self-discovery to all responses.

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/middleware_test.go`

**Step 1: Write the failing test**

```go
// Add to middleware_test.go
func TestDiscoveryLinkHeaders(t *testing.T) {
	handler := discoveryLinks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/services", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	link := w.Header().Get("Link")
	if !strings.Contains(link, `</api>; rel="service-desc"`) {
		t.Errorf("missing service-desc in Link: %q", link)
	}
	if !strings.Contains(link, `</api/context.jsonld>; rel="describedby"`) {
		t.Errorf("missing describedby in Link: %q", link)
	}
}
```

**Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestDiscoveryLinkHeaders -v`

**Step 3: Implement discoveryLinks middleware**

```go
// Add to middleware.go
func discoveryLinks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Link", `</api>; rel="service-desc"`)
		w.Header().Add("Link", `</api/context.jsonld>; rel="describedby"`)
		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestDiscoveryLinkHeaders -v`

**Step 5: Add to middleware chain in router.go**

Update the middleware chain:
```go
requestID(recovery(securityHeaders(discoveryLinks(requestLogger(mux)))))
```

Only apply to content-negotiated routes (not `/-/` meta endpoints). See Task 7 for router restructuring.

**Step 6: Commit**

```bash
git add internal/api/middleware.go internal/api/middleware_test.go
git commit -m "feat(api): RFC 8631 self-discovery Link headers"
```

---

### Task 6: Caching Headers (ETag + Cache-Control)

Add ETag to JSON responses and Cache-Control to static resources.

**Files:**
- Create: `internal/api/etag.go`
- Create: `internal/api/etag_test.go`
- Modify: `internal/api/handlers.go` (add ETag support to writeJSON)

**Step 1: Write the failing tests**

```go
// etag_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETagGeneration(t *testing.T) {
	body := []byte(`{"items":[],"total":0}`)
	etag := computeETag(body)
	if etag == "" {
		t.Error("empty etag")
	}
	if etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Errorf("etag not quoted: %q", etag)
	}
}

func TestETagConditionalRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONWithETag(w, r, map[string]any{"ok": true})
	})

	// First request — get the ETag
	req1 := httptest.NewRequest("GET", "/services", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}
	if w1.Code != 200 {
		t.Fatalf("status=%d, want 200", w1.Code)
	}

	// Second request with If-None-Match — should get 304
	req2 := httptest.NewRequest("GET", "/services", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != 304 {
		t.Errorf("status=%d, want 304", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Error("304 should have empty body")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestETag -v`

**Step 3: Implement etag.go**

```go
// etag.go
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/goccy/go-json"
)

func computeETag(body []byte) string {
	h := sha256.Sum256(body)
	return `"` + hex.EncodeToString(h[:16]) + `"`
}

func writeJSONWithETag(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to encode response")
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)

	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestETag -v`

**Step 5: Replace writeJSON with writeJSONWithETag in handlers**

Update `writeJSON` calls in handlers that serve cacheable content (list + detail endpoints). Keep the old `writeJSON` for health/ready endpoints where ETag overhead isn't useful.

For static resources (`/api/context.jsonld`, `/api` spec), set `Cache-Control: public, max-age=3600` directly in the handler.

**Step 6: Run all tests**

Run: `go test ./internal/api/ -v`

**Step 7: Commit**

```bash
git add internal/api/etag.go internal/api/etag_test.go internal/api/handlers.go
git commit -m "feat(api): ETag caching with conditional 304 responses"
```

---

### Task 7: Router Restructuring

Restructure the router to use content negotiation. Remove `/api/` prefix from resource routes. Move health/ready to `/-/`. Add `/api` for OpenAPI spec/playground.

**Files:**
- Modify: `internal/api/router.go`
- Create: `internal/api/apidoc.go` (serves OpenAPI spec + playground)
- Create: `internal/api/context.go` (serves JSON-LD context)
- Modify: `internal/api/router.go` (new route structure)

**Step 1: Create the JSON-LD context document**

```go
// context.go
package api

import "net/http"

const jsonLDContextDoc = `{
  "@context": {
    "@vocab": "urn:cetacean:",
    "items": {"@container": "@set"},
    "type": "urn:ietf:rfc:9457#type",
    "title": "urn:ietf:rfc:9457#title",
    "status": "urn:ietf:rfc:9457#status",
    "detail": "urn:ietf:rfc:9457#detail",
    "instance": "urn:ietf:rfc:9457#instance"
  }
}`

func HandleContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/ld+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(jsonLDContextDoc))
}
```

**Step 2: Create the OpenAPI doc handler**

```go
// apidoc.go
package api

import (
	"io/fs"
	"net/http"
)

// HandleAPIDoc serves the OpenAPI spec (JSON) or playground (HTML)
// based on content negotiation.
func HandleAPIDoc(specFS fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			// Serve OpenAPI playground (Scalar/Swagger UI)
			// For now, redirect or serve a minimal HTML page that loads the spec
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(apiPlaygroundHTML))
		default:
			// Serve the raw OpenAPI spec
			data, err := fs.ReadFile(specFS, "openapi.yaml")
			if err != nil {
				writeProblem(w, r, http.StatusInternalServerError, "openapi spec not found")
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write(data)
		}
	}
}

const apiPlaygroundHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Cetacean API</title>
  <meta charset="utf-8"/>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</head>
<body>
  <script id="api-reference" data-url="/api" data-configuration='{"spec": {"url": "/api.json"}}'></script>
</body>
</html>`
```

Note: The playground HTML will need refinement. This is a starting point.

**Step 3: Restructure router.go**

The key change: resource routes lose their `/api/` prefix, content negotiation middleware wraps them, and the SPA handler is invoked when `ContentTypeHTML` is negotiated.

```go
func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler, specFS fs.FS, enablePprof bool) http.Handler {
	mux := http.NewServeMux()

	// --- Meta endpoints (no content negotiation) ---
	mux.HandleFunc("GET /-/health", h.HandleHealth)
	mux.HandleFunc("GET /-/ready", h.HandleReady)
	mux.HandleFunc("GET /-/metrics/status", h.HandleMonitoringStatus)
	mux.Handle("GET /-/metrics/", promProxy)

	// --- API documentation ---
	mux.HandleFunc("GET /api/context.jsonld", HandleContext)
	mux.Handle("GET /api", HandleAPIDoc(specFS))

	// --- Content-negotiated resource routes ---
	// Register all resource routes without /api/ prefix
	mux.HandleFunc("GET /nodes", h.HandleListNodes)
	mux.HandleFunc("GET /nodes/{id}", h.HandleNodeDetail)
	mux.HandleFunc("GET /nodes/{id}/tasks", h.HandleNodeTasks)
	// ... (all other resource routes)
	mux.Handle("GET /events", b)

	// SPA fallback — registered last
	mux.Handle("/", spa)

	// Middleware chain for all routes
	var handler http.Handler = mux
	handler = requestLogger(handler)
	handler = discoveryLinks(handler)  // only for non-meta routes; see below
	handler = negotiate(handler)
	handler = securityHeaders(handler)
	handler = recovery(handler)
	handler = requestID(handler)

	return handler
}
```

**Important routing change:** Each handler now needs to check `ContentTypeFromContext` and either serve JSON or delegate to the SPA. Create a helper:

```go
func (h *Handlers) contentNegotiated(jsonHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			// Return 406 unless the handler supports SSE
			writeProblem(w, r, http.StatusNotAcceptable, "this endpoint does not support text/event-stream")
		default:
			jsonHandler(w, r)
		}
	}
}
```

Then in the router:
```go
mux.HandleFunc("GET /nodes", h.contentNegotiated(h.HandleListNodes, spa))
mux.HandleFunc("GET /events", h.contentNegotiated(nil, spa)) // SSE-only, handled specially
```

For endpoints that support SSE (events, logs), use a variant that allows SSE:
```go
func (h *Handlers) contentNegotiatedWithSSE(jsonHandler, sseHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := ContentTypeFromContext(r.Context())
		switch ct {
		case ContentTypeHTML:
			spa.ServeHTTP(w, r)
		case ContentTypeSSE:
			sseHandler(w, r)
		default:
			jsonHandler(w, r)
		}
	}
}
```

**Step 4: Update middleware to skip discovery headers on /-/ routes**

In `discoveryLinks`, check the path prefix:
```go
if strings.HasPrefix(r.URL.Path, "/-/") {
	next.ServeHTTP(w, r)
	return
}
```

**Step 5: Run all tests, fix path references**

Tests currently use `/api/...` paths. Update all `httptest.NewRequest` paths to remove the `/api/` prefix for resource routes, and use `/-/` for meta endpoints.

Run: `go test ./internal/api/ -v`

**Step 6: Commit**

```bash
git add internal/api/router.go internal/api/apidoc.go internal/api/context.go internal/api/handlers.go
git commit -m "feat(api): restructure router with content negotiation"
```

---

### Task 8: SSE JSON-LD Payloads and 429 Status

Update SSE event payloads to include `@id` and `@type`. Change connection limit responses from 503 to 429.

**Files:**
- Modify: `internal/api/sse.go`
- Modify: `internal/api/sse_test.go`
- Modify: `internal/api/handlers.go` (log streaming connection limit)

**Step 1: Write the failing tests**

```go
// Add to sse_test.go
func TestSSEEventPayloadHasJSONLD(t *testing.T) {
	// Create a broadcaster, connect a client, send an event, verify @id and @type in data
}

func TestSSEConnectionLimit429(t *testing.T) {
	// Fill broadcaster to maxSSEClients, verify next connection gets 429 + Retry-After
}
```

**Step 2: Update SSE event serialization**

In `sse.go`, modify `writeBatch` (or the event serialization) to add `@id` and `@type` to each event's data payload. The cache `Event` type already has `Type` and `ResourceID` — use these to construct the `@id` path.

Create a helper to map resource type to URL path:
```go
func resourcePath(resourceType, resourceID string) string {
	switch resourceType {
	case "node": return "/nodes/" + resourceID
	case "service": return "/services/" + resourceID
	case "task": return "/tasks/" + resourceID
	case "config": return "/configs/" + resourceID
	case "secret": return "/secrets/" + resourceID
	case "network": return "/networks/" + resourceID
	case "volume": return "/volumes/" + resourceID
	case "stack": return "/stacks/" + resourceID
	default: return ""
	}
}
```

**Step 3: Change connection limit from 503 to 429**

In `sse.go` `ServeHTTP`:
```go
w.Header().Set("Retry-After", "5")
writeProblem(w, r, http.StatusTooManyRequests, "too many SSE connections")
```

In `handlers.go` `serveLogsSSE`:
```go
w.Header().Set("Retry-After", "5")
writeProblem(w, r, http.StatusTooManyRequests, "too many concurrent log streams")
```

Note: `Broadcaster.ServeHTTP` currently uses `http.Error()` directly. It needs access to `*http.Request` for `writeProblem` — update the handler signature or use the request from `ServeHTTP` params.

**Step 4: Run all tests**

Run: `go test ./internal/api/ -v`

**Step 5: Commit**

```bash
git add internal/api/sse.go internal/api/sse_test.go internal/api/handlers.go
git commit -m "feat(api): JSON-LD SSE payloads and 429 for connection limits"
```

---

### Task 9: Frontend Migration

Update the frontend to work with the new API structure.

**Files:**
- Modify: `frontend/src/api/client.ts` (remove `/api` base, add Accept headers)
- Modify: `frontend/vite.config.ts` (update proxy config)
- Modify: `frontend/src/hooks/SSEContext.tsx` (update EventSource URL)

**Step 1: Update client.ts**

```typescript
// Remove BASE prefix — paths are now root-relative
const BASE = "";

// Headers already set Accept: application/json — this is correct and now essential
const headers = { Accept: "application/json" };
```

Update all endpoint paths:
- `/api/nodes` → `/nodes`
- `/api/services` → `/services`
- etc.

Update meta endpoint paths:
- `/api/health` → `/-/health`
- `/api/ready` → `/-/ready`
- `/api/metrics/status` → `/-/metrics/status`
- `/api/metrics/query` → `/-/metrics/query`
- `/api/metrics/query_range` → `/-/metrics/query_range`

Update SSE URLs:
- `/api/events` → `/events`
- Service/task log stream URLs similarly

**Step 2: Update SSEContext.tsx**

Change EventSource URL from `/api/events` to `/events`. The EventSource API doesn't set Accept headers by default — verify that the negotiate middleware handles EventSource connections correctly (they send `Accept: text/event-stream`).

**Step 3: Update response type interfaces**

In `frontend/src/api/types.ts`, add JSON-LD fields to response types:

```typescript
interface CollectionResponse<T> {
  "@context": string;
  "@type": "Collection";
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

interface DetailResponse {
  "@context": string;
  "@id": string;
  "@type": string;
}
```

Update existing types to extend these or adapt the field access patterns.

**Step 4: Update Vite proxy config**

```typescript
// vite.config.ts
server: {
  proxy: {
    // Proxy all requests to backend except Vite's own assets
    "^/(nodes|services|tasks|configs|secrets|networks|volumes|stacks|search|events|topology|cluster|swarm|plugins|disk-usage|history|notifications|api|-/)": {
      target: "http://localhost:9000",
    },
  },
},
```

Alternatively, use a simpler approach: proxy everything and let Vite's own middleware handle its assets first. Check Vite docs for the cleanest pattern.

**Step 5: Update error handling in frontend**

Error responses are now RFC 9457. Update `fetchJSON` to parse `ProblemDetail`:

```typescript
if (!res.ok) {
  const problem = await res.json().catch(() => null);
  throw new Error(problem?.detail || `${res.status} ${res.statusText}`);
}
```

**Step 6: Run frontend type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

**Step 7: Build and verify**

Run: `cd frontend && npm run build`

**Step 8: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/api/types.ts frontend/src/hooks/SSEContext.tsx frontend/vite.config.ts
git commit -m "feat(frontend): migrate to content-negotiated API"
```

---

### Task 10: OpenAPI Spec

Write the hand-written OpenAPI spec covering all endpoints.

**Files:**
- Create: `api/openapi.yaml`

**Step 1: Write the OpenAPI spec**

Create `api/openapi.yaml` with:
- `openapi: "3.1.0"`
- `info` with title, version, description
- `servers` section (relative URL `/`)
- `paths` for every endpoint
- `components/schemas` for all response types (CollectionResponse, DetailResponse, ProblemDetail, each resource type)
- `components/headers` for Link, ETag, Vary, Retry-After
- Tag groups: Nodes, Services, Tasks, Configs, Secrets, Networks, Volumes, Stacks, Search, Topology, Cluster, Monitoring, Events

This is a large file. Write it methodically, one resource group at a time. Use `$ref` for shared schemas.

**Step 2: Embed in binary**

In `main.go`, add:
```go
//go:embed api/openapi.yaml
var openapiSpec embed.FS
```

Pass to `NewRouter` so it can serve the spec.

**Step 3: Verify it parses**

Install `swagger-cli` or use `go-openapi` to validate:
```bash
npx @redocly/cli lint api/openapi.yaml
```

**Step 4: Commit**

```bash
git add api/openapi.yaml main.go
git commit -m "feat(api): hand-written OpenAPI 3.1 spec"
```

---

### Task 11: OpenAPI Validation Tests

Write tests that validate handler responses against the OpenAPI spec.

**Files:**
- Create: `internal/api/openapi_test.go`

**Step 1: Write validation test**

Use `getkin/kin-openapi` to load the spec and validate responses:

```go
// openapi_test.go
package api

import (
	"testing"
	// use kin-openapi for schema validation
)

func TestResponsesMatchOpenAPISpec(t *testing.T) {
	// Load api/openapi.yaml
	// For each endpoint in the spec:
	//   1. Create a handler with mock cache data
	//   2. Make a request
	//   3. Validate response status, content-type, and body against spec schema
}
```

Cover at minimum:
- One list endpoint (e.g., `/nodes`)
- One detail endpoint (e.g., `/nodes/{id}`)
- One error case (404)
- One paginated response with Link headers
- The health endpoint

**Step 2: Add kin-openapi dependency**

Run: `go get github.com/getkin/kin-openapi`

**Step 3: Run tests**

Run: `go test ./internal/api/ -run TestResponsesMatchOpenAPISpec -v`

**Step 4: Commit**

```bash
git add internal/api/openapi_test.go go.mod go.sum
git commit -m "test(api): OpenAPI spec validation tests"
```

---

### Task 12: Markdown API Documentation

Write the human-readable API reference.

**Files:**
- Create: `docs/api.md`

**Step 1: Write docs/api.md**

Structure:
1. **Overview** — what the API is, read-only, content negotiation
2. **Authentication** — none, reverse proxy guidance
3. **Content Negotiation** — Accept header, extensions, versioning
4. **Common Parameters** — pagination (`limit`, `offset`), sorting (`sort`, `dir`), search (`search`), filtering (`filter`)
5. **Response Format** — JSON-LD fields, collection shape, detail shape
6. **Errors** — RFC 9457, problem types, examples
7. **Real-Time Events (SSE)** — `/events`, type filtering, reconnect
8. **Caching** — ETag, If-None-Match, Cache-Control
9. **Endpoint Reference** — grouped by resource type, with curl examples
10. **Rate Limits** — connection limits for SSE (256) and log streams (128)

**Step 2: Commit**

```bash
git add docs/api.md
git commit -m "docs: hand-written API reference"
```

---

### Task 13: Integration Smoke Test

End-to-end test that builds the binary and verifies content negotiation works.

**Files:**
- Modify: `internal/api/handlers_test.go` (add integration-style tests)

**Step 1: Write integration tests**

```go
func TestContentNegotiationIntegration(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-1"}})
	h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
	b := NewBroadcaster(100 * time.Millisecond)
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>SPA</html>"))
	})
	router := NewRouter(h, b, http.NotFoundHandler(), spa, specFS, false)

	// JSON request
	req := httptest.NewRequest("GET", "/nodes", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected JSON content type")
	}

	// HTML request → SPA
	req2 := httptest.NewRequest("GET", "/nodes", nil)
	req2.Header.Set("Accept", "text/html")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), "SPA") {
		t.Error("expected SPA response for HTML accept")
	}

	// Extension override
	req3 := httptest.NewRequest("GET", "/nodes.json", nil)
	req3.Header.Set("Accept", "text/html")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Header().Get("Content-Type") != "application/json" {
		t.Error("extension should override Accept header")
	}

	// 406 for SSE on non-SSE endpoint
	req4 := httptest.NewRequest("GET", "/nodes", nil)
	req4.Header.Set("Accept", "text/event-stream")
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)
	if w4.Code != 406 {
		t.Errorf("expected 406, got %d", w4.Code)
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/api/ -run TestContentNegotiationIntegration -v`

**Step 3: Run full test suite**

Run: `make check`

**Step 4: Commit**

```bash
git add internal/api/handlers_test.go
git commit -m "test(api): content negotiation integration tests"
```

---

### Task 14: Final Cleanup

Remove dead code, update CLAUDE.md, verify everything builds.

**Files:**
- Modify: `CLAUDE.md` (update API docs section, endpoint paths)
- Remove: any dead code (old `writeError`, old `/api/` path references)

**Step 1: Search for remaining /api/ references**

```bash
grep -r '"/api/' internal/ frontend/src/ --include="*.go" --include="*.ts" --include="*.tsx"
```

Fix any stragglers (test files, comments, etc.).

**Step 2: Update CLAUDE.md**

Update the Architecture section to reflect:
- Content negotiation model
- New URL structure (no `/api/` prefix for resources, `/-/` for meta)
- RFC 9457 errors
- JSON-LD responses
- OpenAPI spec location

**Step 3: Full build and test**

```bash
cd frontend && npm install && npm run build && cd ..
make check
go build -o cetacean .
```

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: final cleanup for first-class API"
```
