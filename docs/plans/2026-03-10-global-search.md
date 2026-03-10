# Global Search Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global search feature that searches across all 8 resource types from a single Cmd+K command palette, with a full-page fallback at `/search?q=...`.

**Architecture:** New `GET /api/search?q=` backend endpoint searches all cached resources (names, images, labels) and returns grouped results capped at 3 per type. Frontend adds a command palette overlay triggered from the nav bar, plus a dedicated search page for full results.

**Tech Stack:** Go (backend handler), React 19 + TypeScript (frontend components), Tailwind CSS v4

---

## File Structure

### Backend (Go)
| File | Action | Responsibility |
|------|--------|---------------|
| `internal/api/handlers.go` | Modify | Add `HandleSearch` handler |
| `internal/api/handlers_test.go` | Modify | Add tests for `HandleSearch` |
| `internal/api/router.go` | Modify | Register `GET /api/search` route |

### Frontend (TypeScript/React)
| File | Action | Responsibility |
|------|--------|---------------|
| `frontend/src/api/types.ts` | Modify | Add `SearchResult`, `SearchResponse`, `ResourceType` types |
| `frontend/src/api/client.ts` | Modify | Add `api.search(q)` method |
| `frontend/src/components/SearchPalette.tsx` | Create | Command palette overlay with input, grouped results, keyboard nav |
| `frontend/src/components/GlobalSearch.tsx` | Create | Nav bar trigger button + Cmd+K listener + palette state |
| `frontend/src/pages/SearchPage.tsx` | Create | Full-page search results at `/search?q=...` |
| `frontend/src/App.tsx` | Modify | Add `GlobalSearch` to nav, add `/search` route |

---

## Chunk 1: Backend

### Task 1: Write `HandleSearch` handler test

**Files:**
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write the test**

Add to `handlers_test.go`. Test searches across multiple resource types and verifies the grouped response shape. Populate cache with a service named "nginx-web" (image "nginx:1.25"), a config named "nginx.conf", a network named "nginx-net" (overlay driver), and a node named "worker-1". Search for "nginx" — expect 3 matches (service, config, network), not the node.

```go
func TestHandleSearch(t *testing.T) {
	c := cache.New(nil)

	// Add a service with "nginx" in name and image
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "nginx-web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "nginx:1.25-alpine",
				},
			},
		},
	})

	// Add a config with "nginx" in name
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "nginx.conf"},
		},
	})

	// Add a network with "nginx" in name
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "nginx-net",
		Driver: "overlay",
	})

	// Add a node that does NOT match "nginx"
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=nginx", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp struct {
		Query   string                              `json:"query"`
		Results map[string][]struct{ Name string }  `json:"results"`
		Counts  map[string]int                      `json:"counts"`
		Total   int                                 `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("total=%d, want 3", resp.Total)
	}
	if len(resp.Results["services"]) != 1 {
		t.Errorf("services=%d, want 1", len(resp.Results["services"]))
	}
	if len(resp.Results["configs"]) != 1 {
		t.Errorf("configs=%d, want 1", len(resp.Results["configs"]))
	}
	if len(resp.Results["networks"]) != 1 {
		t.Errorf("networks=%d, want 1", len(resp.Results["networks"]))
	}
	if _, ok := resp.Results["nodes"]; ok {
		t.Error("nodes should not appear in results")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHandleSearch -v`
Expected: FAIL — `HandleSearch` does not exist yet.

### Task 2: Write `HandleSearch` test for label matching

**Files:**
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write the test**

Add a test that verifies label key and value matching. Create a volume with name "data-vol" and label `team=nginx-platform`. Search for "nginx" — should match the volume via its label value.

```go
func TestHandleSearch_MatchesLabels(t *testing.T) {
	c := cache.New(nil)

	c.SetVolume(volume.Volume{
		Name:   "data-vol",
		Driver: "local",
		Labels: map[string]string{"team": "nginx-platform"},
	})

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=nginx", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	var resp struct {
		Results map[string][]struct{ Name string } `json:"results"`
		Total   int                                `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Total != 1 {
		t.Errorf("total=%d, want 1", resp.Total)
	}
	if len(resp.Results["volumes"]) != 1 {
		t.Errorf("volumes=%d, want 1", len(resp.Results["volumes"]))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHandleSearch -v`
Expected: FAIL — still no `HandleSearch`.

### Task 3: Write `HandleSearch` test for edge cases

**Files:**
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write tests for empty query and cap at 3**

```go
func TestHandleSearch_EmptyQuery(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleSearch_CapsAtThreePerType(t *testing.T) {
	c := cache.New(nil)
	for i := 0; i < 5; i++ {
		c.SetService(swarm.Service{
			ID:   fmt.Sprintf("svc%d", i),
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: fmt.Sprintf("web-%d", i)},
			},
		})
	}

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=web", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	var resp struct {
		Results map[string][]struct{ Name string } `json:"results"`
		Counts  map[string]int                     `json:"counts"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Results["services"]) != 3 {
		t.Errorf("results services=%d, want 3 (capped)", len(resp.Results["services"]))
	}
	if resp.Counts["services"] != 5 {
		t.Errorf("count services=%d, want 5 (uncapped)", resp.Counts["services"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestHandleSearch -v`

### Task 4: Implement `HandleSearch`

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add the handler**

Add to `handlers.go` after the existing list handlers. The handler:

1. Reads `q` from query params, returns 400 if empty.
2. Reads optional `limit` param (default 3, 0 means unlimited — for full page).
3. Lowercases the query once.
4. For each resource type, iterates the cache list, matches against name + image + labels.
5. Builds a `searchResult` struct (`ID`, `Name`, `Detail`) for each match.
6. Caps results at `limit` per type (but always counts all matches).
7. Returns JSON with `query`, `results` (map), `counts` (map), `total`.

```go
type searchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

func (h *Handlers) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: q")
		return
	}

	limit := 3
	if v := r.URL.Query().Get("limit"); v == "0" {
		limit = 0 // unlimited
	}

	query := strings.ToLower(q)
	match := func(fields ...string) bool {
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), query) {
				return true
			}
		}
		return false
	}
	matchLabels := func(labels map[string]string) bool {
		for k, v := range labels {
			if strings.Contains(strings.ToLower(k), query) || strings.Contains(strings.ToLower(v), query) {
				return true
			}
		}
		return false
	}

	type group struct {
		key     string
		results []searchResult
		count   int
	}

	// Define search groups in display order
	groups := []group{
		{key: "services"}, {key: "stacks"}, {key: "nodes"}, {key: "tasks"},
		{key: "configs"}, {key: "secrets"}, {key: "networks"}, {key: "volumes"},
	}

	// Services
	for _, s := range h.cache.ListServices() {
		image := ""
		if s.Spec.TaskTemplate.ContainerSpec != nil {
			image = s.Spec.TaskTemplate.ContainerSpec.Image
		}
		if match(s.Spec.Name, image) || matchLabels(s.Spec.Labels) {
			groups[0].count++
			if limit == 0 || len(groups[0].results) < limit {
				groups[0].results = append(groups[0].results, searchResult{
					ID: s.ID, Name: s.Spec.Name, Detail: strings.Split(image, "@")[0],
				})
			}
		}
	}

	// Stacks
	for _, s := range h.cache.ListStacks() {
		if match(s.Name) {
			groups[1].count++
			if limit == 0 || len(groups[1].results) < limit {
				groups[1].results = append(groups[1].results, searchResult{
					ID: s.Name, Name: s.Name, Detail: fmt.Sprintf("%d services", len(s.Services)),
				})
			}
		}
	}

	// Nodes
	for _, n := range h.cache.ListNodes() {
		if match(n.Description.Hostname, n.Status.Addr) || matchLabels(n.Spec.Labels) {
			groups[2].count++
			if limit == 0 || len(groups[2].results) < limit {
				groups[2].results = append(groups[2].results, searchResult{
					ID: n.ID, Name: n.Description.Hostname,
					Detail: string(n.Spec.Role) + ", " + string(n.Status.State),
				})
			}
		}
	}

	// Tasks — match by service name (cross-ref) and image
	svcNames := make(map[string]string)
	for _, s := range h.cache.ListServices() {
		svcNames[s.ID] = s.Spec.Name
	}
	for _, t := range h.cache.ListTasks() {
		image := ""
		if t.Spec.ContainerSpec != nil {
			image = t.Spec.ContainerSpec.Image
		}
		svcName := svcNames[t.ServiceID]
		if match(svcName, image) || matchLabels(t.Spec.ContainerSpec.Labels()) {
			// Note: ContainerSpec.Labels() may not exist — check nil first.
			// Actually labels on tasks are in Spec.ContainerSpec.Labels which is a map.
			// Need to guard nil ContainerSpec.
		}
	}
	// Simplified: skip task label matching for nil-safety, match on svcName + image
	groups[3] = group{key: "tasks"}
	for _, t := range h.cache.ListTasks() {
		image := ""
		var labels map[string]string
		if t.Spec.ContainerSpec != nil {
			image = t.Spec.ContainerSpec.Image
			labels = t.Spec.ContainerSpec.Labels
		}
		svcName := svcNames[t.ServiceID]
		if match(svcName, image) || matchLabels(labels) {
			groups[3].count++
			if limit == 0 || len(groups[3].results) < limit {
				groups[3].results = append(groups[3].results, searchResult{
					ID: t.ID, Name: svcName + "." + fmt.Sprint(t.Slot),
					Detail: string(t.Status.State),
				})
			}
		}
	}

	// Configs
	for _, c := range h.cache.ListConfigs() {
		if match(c.Spec.Name) || matchLabels(c.Spec.Labels) {
			groups[4].count++
			if limit == 0 || len(groups[4].results) < limit {
				groups[4].results = append(groups[4].results, searchResult{
					ID: c.ID, Name: c.Spec.Name,
					Detail: c.CreatedAt.Format("2006-01-02"),
				})
			}
		}
	}

	// Secrets
	for _, s := range h.cache.ListSecrets() {
		s.Spec.Data = nil // never expose
		if match(s.Spec.Name) || matchLabels(s.Spec.Labels) {
			groups[5].count++
			if limit == 0 || len(groups[5].results) < limit {
				groups[5].results = append(groups[5].results, searchResult{
					ID: s.ID, Name: s.Spec.Name,
					Detail: s.CreatedAt.Format("2006-01-02"),
				})
			}
		}
	}

	// Networks
	for _, n := range h.cache.ListNetworks() {
		if match(n.Name) || matchLabels(n.Labels) {
			groups[6].count++
			if limit == 0 || len(groups[6].results) < limit {
				groups[6].results = append(groups[6].results, searchResult{
					ID: n.ID, Name: n.Name, Detail: n.Driver,
				})
			}
		}
	}

	// Volumes
	for _, v := range h.cache.ListVolumes() {
		if match(v.Name) || matchLabels(v.Labels) {
			groups[7].count++
			if limit == 0 || len(groups[7].results) < limit {
				groups[7].results = append(groups[7].results, searchResult{
					ID: v.Name, Name: v.Name, Detail: v.Driver,
				})
			}
		}
	}

	// Build response
	results := make(map[string][]searchResult)
	counts := make(map[string]int)
	total := 0
	for _, g := range groups {
		counts[g.key] = g.count
		total += g.count
		if len(g.results) > 0 {
			results[g.key] = g.results
		}
	}

	writeJSON(w, map[string]any{
		"query":   q,
		"results": results,
		"counts":  counts,
		"total":   total,
	})
}
```

Note: The above is the reference implementation. The implementer should clean up the duplicate task iteration (the first attempt with the comment can be removed — only the second version is needed). Also guard against nil `ContainerSpec` on tasks.

- [ ] **Step 2: Register the route**

Add to `internal/api/router.go` alongside the other GET routes:

```go
mux.HandleFunc("GET /api/search", h.HandleSearch)
```

Place it before the SPA fallback and after the other API routes.

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/api/ -run TestHandleSearch -v`
Expected: All 4 tests PASS.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat: add global search API endpoint (GET /api/search)"
```

---

## Chunk 2: Frontend Types + API Client

### Task 5: Add search types and API method

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types to `types.ts`**

Append to the end of `frontend/src/api/types.ts`:

```typescript
// Global search
export type SearchResourceType =
  | "services" | "stacks" | "nodes" | "tasks"
  | "configs" | "secrets" | "networks" | "volumes";

export interface SearchResult {
  id: string;
  name: string;
  detail: string;
}

export interface SearchResponse {
  query: string;
  results: Partial<Record<SearchResourceType, SearchResult[]>>;
  counts: Record<SearchResourceType, number>;
  total: number;
}
```

- [ ] **Step 2: Add API method to `client.ts`**

Add to the `api` object in `frontend/src/api/client.ts`:

```typescript
search: (q: string, limit?: number) =>
  fetchJSON<SearchResponse>(
    `/api/search?q=${encodeURIComponent(q)}${limit !== undefined ? `&limit=${limit}` : ""}`
  ),
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add search types and API client method"
```

---

## Chunk 3: SearchPalette Component

### Task 6: Create `SearchPalette` component

**Files:**
- Create: `frontend/src/components/SearchPalette.tsx`

- [ ] **Step 1: Write the component**

The palette is a modal overlay with:
- Backdrop (click to close)
- Centered dialog with search input + results area + footer
- Debounced fetch (300ms) to `/api/search?q=...`
- Results grouped by type with uppercase section headers
- Keyboard navigation: ArrowUp/Down moves highlight, Enter navigates, Escape closes
- Each result row shows name + detail, clicking navigates to detail page

```typescript
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Search } from "lucide-react";
import { api } from "../api/client";
import type { SearchResourceType, SearchResponse, SearchResult } from "../api/types";

const TYPE_ORDER: SearchResourceType[] = [
  "services", "stacks", "nodes", "tasks",
  "configs", "secrets", "networks", "volumes",
];

const TYPE_LABELS: Record<SearchResourceType, string> = {
  services: "Services", stacks: "Stacks", nodes: "Nodes", tasks: "Tasks",
  configs: "Configs", secrets: "Secrets", networks: "Networks", volumes: "Volumes",
};

function resourcePath(type: SearchResourceType, id: string): string {
  return `/${type === "tasks" ? "tasks" : type}/${id}`;
}

interface FlatItem { type: SearchResourceType; result: SearchResult }

export default function SearchPalette({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [data, setData] = useState<SearchResponse | null>(null);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  // Debounced fetch
  useEffect(() => {
    if (!query.trim()) { setData(null); return; }
    const id = setTimeout(() => {
      api.search(query).then(setData).catch(() => setData(null));
    }, 200);
    return () => clearTimeout(id);
  }, [query]);

  // Reset active index when results change
  useEffect(() => { setActive(0); }, [data]);

  // Flatten results for keyboard nav
  const flat: FlatItem[] = [];
  if (data) {
    for (const type of TYPE_ORDER) {
      const items = data.results[type];
      if (items) for (const result of items) flat.push({ type, result });
    }
  }

  const go = useCallback((item: FlatItem) => {
    navigate(resourcePath(item.type, item.result.id));
    onClose();
  }, [navigate, onClose]);

  const onKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setActive(i => Math.min(i + 1, flat.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setActive(i => Math.max(i - 1, 0)); }
    else if (e.key === "Enter" && flat[active]) { go(flat[active]); }
    else if (e.key === "Escape") { onClose(); }
  }, [flat, active, go, onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
         onClick={onClose}>
      <div className="fixed inset-0 bg-background/60 backdrop-blur-sm" />
      <div className="relative w-full max-w-lg rounded-xl border bg-popover shadow-2xl"
           onClick={e => e.stopPropagation()} onKeyDown={onKeyDown}>
        {/* Input */}
        <div className="flex items-center gap-2 border-b px-4 py-3">
          <Search className="h-4 w-4 text-muted-foreground shrink-0" />
          <input
            ref={inputRef}
            autoFocus
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search all resources..."
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <kbd className="hidden sm:inline text-[10px] text-muted-foreground border rounded px-1.5 py-0.5">Esc</kbd>
        </div>

        {/* Results */}
        {flat.length > 0 && (
          <div className="max-h-72 overflow-y-auto p-1.5">
            {TYPE_ORDER.map(type => {
              const items = data?.results[type];
              if (!items?.length) return null;
              return (
                <div key={type}>
                  <div className="px-2.5 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {TYPE_LABELS[type]}
                  </div>
                  {items.map((r, i) => {
                    const idx = flat.findIndex(f => f.type === type && f.result.id === r.id);
                    return (
                      <button
                        key={r.id}
                        className={`flex w-full items-center gap-3 rounded-md px-2.5 py-1.5 text-sm text-left transition-colors ${
                          idx === active ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                        }`}
                        onClick={() => go({ type, result: r })}
                        onMouseEnter={() => setActive(idx)}
                      >
                        <span className="font-medium truncate">{r.name}</span>
                        {r.detail && (
                          <span className="ml-auto text-xs text-muted-foreground truncate">{r.detail}</span>
                        )}
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>
        )}

        {/* No results */}
        {query.trim() && data && flat.length === 0 && (
          <div className="px-4 py-8 text-center text-sm text-muted-foreground">
            No results for &ldquo;{query}&rdquo;
          </div>
        )}

        {/* Footer */}
        {data && data.total > 0 && (
          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
            <span>{data.total} result{data.total !== 1 ? "s" : ""}</span>
            <button
              className="text-primary hover:underline"
              onClick={() => { navigate(`/search?q=${encodeURIComponent(query)}`); onClose(); }}
            >
              View all results &rarr;
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/SearchPalette.tsx
git commit -m "feat: add SearchPalette command palette component"
```

---

### Task 7: Create `GlobalSearch` nav bar trigger

**Files:**
- Create: `frontend/src/components/GlobalSearch.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Write the trigger component**

```typescript
import { useCallback, useEffect, useState } from "react";
import { Search } from "lucide-react";
import SearchPalette from "./SearchPalette";

export default function GlobalSearch() {
  const [open, setOpen] = useState(false);

  const onKeyDown = useCallback((e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "k") {
      e.preventDefault();
      setOpen(o => !o);
    }
  }, []);

  useEffect(() => {
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onKeyDown]);

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="flex items-center gap-1.5 rounded-md border bg-muted/50 px-2.5 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
      >
        <Search className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">Search...</span>
        <kbd className="hidden sm:inline text-[10px] border rounded px-1 py-0.5 ml-1">⌘K</kbd>
      </button>
      {open && <SearchPalette onClose={() => setOpen(false)} />}
    </>
  );
}
```

- [ ] **Step 2: Add to nav bar in `App.tsx`**

In `App.tsx`, import `GlobalSearch` and place it in the nav bar right side, before `ThemeToggle`:

```tsx
import GlobalSearch from "./components/GlobalSearch";
```

In the `Layout` component, inside the `flex items-center gap-1` div (around line 42), add `<GlobalSearch />` before the hidden md nav links div:

```tsx
<div className="flex items-center gap-1">
    <GlobalSearch />
    <div className="hidden md:flex items-center gap-1">
        <NavLinks/>
    </div>
    <ThemeToggle/>
    {/* hamburger button... */}
</div>
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/GlobalSearch.tsx frontend/src/App.tsx
git commit -m "feat: add GlobalSearch trigger to nav bar with Cmd+K shortcut"
```

---

## Chunk 4: Full Search Page + Route

### Task 8: Create `SearchPage`

**Files:**
- Create: `frontend/src/pages/SearchPage.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Write the page component**

Full-page view at `/search?q=...`. Fetches with `limit=0` for all results. Reuses the grouped-by-type layout. Search input at top for refinement.

```typescript
import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../api/client";
import type { SearchResourceType, SearchResponse } from "../api/types";
import PageHeader from "../components/PageHeader";
import SearchInput from "../components/SearchInput";
import EmptyState from "../components/EmptyState";
import { SkeletonTable } from "../components/LoadingSkeleton";

const TYPE_ORDER: SearchResourceType[] = [
  "services", "stacks", "nodes", "tasks",
  "configs", "secrets", "networks", "volumes",
];

const TYPE_LABELS: Record<SearchResourceType, string> = {
  services: "Services", stacks: "Stacks", nodes: "Nodes", tasks: "Tasks",
  configs: "Configs", secrets: "Secrets", networks: "Networks", volumes: "Volumes",
};

function resourcePath(type: SearchResourceType, id: string): string {
  return `/${type}/${id}`;
}

export default function SearchPage() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const [input, setInput] = useState(q);
  const [data, setData] = useState<SearchResponse | null>(null);
  const [loading, setLoading] = useState(false);

  // Sync input when URL changes (e.g., back/forward)
  useEffect(() => { setInput(params.get("q") ?? ""); }, [params]);

  // Debounced URL update
  useEffect(() => {
    const id = setTimeout(() => {
      setParams(input ? { q: input } : {}, { replace: true });
    }, 300);
    return () => clearTimeout(id);
  }, [input, setParams]);

  // Fetch when q changes
  const fetchResults = useCallback(() => {
    if (!q) { setData(null); return; }
    setLoading(true);
    api.search(q, 0).then(setData).catch(() => setData(null)).finally(() => setLoading(false));
  }, [q]);

  useEffect(() => { fetchResults(); }, [fetchResults]);

  return (
    <div>
      <PageHeader title="Search" />
      <div className="mb-6">
        <SearchInput value={input} onChange={setInput} placeholder="Search all resources..." />
      </div>

      {loading && <SkeletonTable columns={2} />}

      {!loading && q && data && data.total === 0 && (
        <EmptyState message={`No results for "${q}"`} />
      )}

      {!loading && data && data.total > 0 && (
        <div className="space-y-6">
          {TYPE_ORDER.map(type => {
            const items = data.results[type];
            if (!items?.length) return null;
            return (
              <div key={type}>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                  {TYPE_LABELS[type]} ({data.counts[type]})
                </h3>
                <div className="rounded-lg border divide-y">
                  {items.map(r => (
                    <Link
                      key={r.id}
                      to={resourcePath(type, r.id)}
                      className="flex items-center justify-between px-4 py-2.5 text-sm hover:bg-muted transition-colors"
                    >
                      <span className="font-medium">{r.name}</span>
                      {r.detail && <span className="text-xs text-muted-foreground">{r.detail}</span>}
                    </Link>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Add route in `App.tsx`**

Import `SearchPage` and add the route:

```tsx
import SearchPage from "./pages/SearchPage";
```

Add inside `<Routes>`:

```tsx
<Route path="/search" element={<SearchPage />} />
```

- [ ] **Step 3: Type-check and test**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`
Expected: No type errors, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/SearchPage.tsx frontend/src/App.tsx
git commit -m "feat: add full search results page at /search"
```

### Task 9: Final verification

- [ ] **Step 1: Build the full project**

```bash
cd frontend && npm run build && cd ..
go build -o cetacean .
```

Expected: Clean build.

- [ ] **Step 2: Run all tests**

```bash
go test ./...
cd frontend && npx vitest run
```

Expected: All pass.

- [ ] **Step 3: Commit design doc**

```bash
git add docs/plans/2026-03-10-global-search-design.md docs/plans/2026-03-10-global-search.md .gitignore
git commit -m "docs: add global search design and implementation plan"
```
