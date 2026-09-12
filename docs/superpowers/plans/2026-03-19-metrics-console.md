# Metrics Query Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:
> executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/metrics` query console page with PromQL input, chart visualization, result table, and autocompletion —
while collapsing the existing `/-/metrics/query` and `/-/metrics/query_range` proxy endpoints into `/metrics`.

**Architecture:** The backend Prometheus proxy is refactored from `/-/metrics/*` to `/metrics` with content
negotiation (HTML → SPA console, JSON → proxied query, SSE → streaming). A shared `proxyTo` helper is extracted
to avoid duplicating proxy logic across handlers. Two new meta endpoints (`/-/metrics/labels`,
`/-/metrics/labels/{name}`) serve label metadata for autocompletion. The frontend adds a new page with a query
textarea, reuses the existing `TimeSeriesChart` for visualization, and adds a simple dropdown autocomplete
component. Phase 1 builds the console without completion; Phase 2 adds metric name + function completion;
Phase 3 adds label completion.

**Tech Stack:** Go stdlib `net/http`, React 19, TypeScript, Chart.js (via existing `TimeSeriesChart`), Tailwind CSS v4

---

## Routing Design

The new `/metrics` endpoint collapses multiple current endpoints:

| Current                                                       | New                                             | Content Type                                 |
|---------------------------------------------------------------|-------------------------------------------------|----------------------------------------------|
| `/-/metrics/query?query=...`                                  | `/metrics?query=...`                            | `application/json` → proxied instant query   |
| `/-/metrics/query_range?query=...&start=...&end=...&step=...` | `/metrics?query=...&start=...&end=...&step=...` | `application/json` → proxied range query     |
| `/-/metrics/query_range?query=...&step=...&range=...`         | `/metrics?query=...&step=...&range=...`         | `text/event-stream` → SSE streaming          |
| (new)                                                         | `/metrics`                                      | `text/html` → SPA console page               |
| (new)                                                         | `/-/metrics/labels?match[]=...`                 | `application/json` → proxied label names     |
| (new)                                                         | `/-/metrics/labels/{name}`                      | `application/json` → proxied label values    |
| `/-/metrics/status`                                           | `/-/metrics/status`                             | unchanged (meta endpoint, stays under `/-/`) |

**Backward compatibility:** The old `/-/metrics/query` and `/-/metrics/query_range` paths will remain as-is during Phase
1. After the frontend is migrated to `/metrics`, the old paths can be removed in a follow-up. This avoids a flag-day
migration.

**Distinguishing instant vs. range queries at `/metrics`:** The proxy handler inspects query params: if `start` and
`end` are present, it proxies to Prometheus `/api/v1/query_range`; otherwise, it proxies to `/api/v1/query`. This
matches how Prometheus' own API works and is unambiguous since instant queries never have `start`/`end`.

**Router type constraint:** `NewRouter` currently receives `promProxy http.Handler`, which is either a
`*PrometheusProxy` or a `PrometheusNotConfiguredHandler`. The new `HandleMetrics` method lives on
`*PrometheusProxy`, so `NewRouter` needs a new `*PrometheusProxy` parameter (which may be nil when Prometheus is
unconfigured). `HandleMetrics` checks for nil receiver and returns 503. The existing `promProxy http.Handler`
parameter stays during the transition for `ServeHTTP` compatibility, then gets removed in Task 10.

## File Structure

### Backend — New/Modified

| File                              | Action    | Responsibility                                                                                                                                                                                                        |
|-----------------------------------|-----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `internal/api/prometheus.go`      | Modify    | Extract `proxyTo` helper; add `HandleMetrics` (instant/range routing); add `HandleMetricsLabels` and `HandleMetricsLabelValues`; handle nil receiver for unconfigured Prometheus                                       |
| `internal/api/router.go`          | Modify    | Add `*PrometheusProxy` parameter; register `GET /metrics` with `contentNegotiatedWithSSE`; register `GET /-/metrics/labels` and `GET /-/metrics/labels/{name}` as meta endpoints. Keep old `/-/metrics/` routes temporarily |
| `internal/api/metricsstream.go`   | No change | Already works with any path — reads `query`, `step`, `range` from URL params                                                                                                                                         |
| `internal/api/prometheus_test.go` | Modify    | Add tests for new paths, instant/range routing, nil proxy, and label endpoints                                                                                                                                        |

### Frontend — New

| File                                                    | Action           | Responsibility                                                                                        |
|---------------------------------------------------------|------------------|-------------------------------------------------------------------------------------------------------|
| `frontend/src/pages/MetricsConsole.tsx`                 | Create           | Page component: query input, range picker, chart, result table. URL state: `?q=`, `?range=`, `?step=` |
| `frontend/src/components/metrics/QueryInput.tsx`        | Create           | Textarea with run button, Cmd+Enter keybinding, optional autocomplete dropdown                        |
| `frontend/src/components/metrics/QueryResultTable.tsx`  | Create           | Tabular display of instant query results (metric{labels} → value)                                     |
| `frontend/src/components/metrics/useQueryCompletion.ts` | Create (Phase 2) | Hook that fetches metric names + provides filtered suggestions                                        |

### Frontend — Modified

| File                              | Action | Change                                                                                                                                                          |
|-----------------------------------|--------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `frontend/src/App.tsx`            | Modify | Add lazy route for `/metrics`                                                                                                                                   |
| `frontend/src/api/client.ts`      | Modify | Add `metricsLabels()`, `metricsLabelValues()` methods (calling `/-/metrics/labels`); add `/metrics` variants of existing query methods; keep old methods temporarily |
| `frontend/src/api/client.test.ts` | Modify | Add tests for new API methods                                                                                                                                   |
| `frontend/vite.config.ts`         | Modify | Add `metrics` to proxy regex                                                                                                                                    |

---

## Phase 1: Query Console (no completion)

### Task 1: Backend — Extract `proxyTo` helper and add `/metrics` handler

**Files:**

- Modify: `internal/api/prometheus.go`
- Modify: `internal/api/router.go`
- Modify: `main.go` (pass `*PrometheusProxy` to `NewRouter`)
- Test: `internal/api/prometheus_test.go`

The proxy currently has inline proxy logic in `ServeHTTP`. We extract a `proxyTo` helper first, then build
`HandleMetrics` on top of it. This avoids writing code in Task 1 that Task 6 immediately refactors.

- [ ] **Step 1: Extract `proxyTo` helper and refactor `ServeHTTP` to use it**

In `internal/api/prometheus.go`, add the shared helper:

```go
// proxyTo sends a proxied request to the given Prometheus API path with the given params.
// Does not read r.URL.Path — the caller determines the target path.
func (p *PrometheusProxy) proxyTo(w http.ResponseWriter, r *http.Request, promPath string, params url.Values) {
	targetURL := p.baseURL + promPath
	if encoded := params.Encode(); encoded != "" {
		targetURL += "?" + encoded
	}
	outReq, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		slog.Error("failed to create prometheus request", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to create prometheus request")
		return
	}
	resp, err := p.client.Do(outReq)
	if err != nil {
		slog.Error("prometheus unreachable", "url", p.baseURL, "error", err)
		writeProblem(w, r, http.StatusBadGateway, "prometheus unreachable")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, maxPrometheusResponseBytes)); err != nil {
		slog.Warn("prometheus proxy copy error", "error", err)
	}
}
```

Then refactor `ServeHTTP` to use it:

```go
func (p *PrometheusProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/-/metrics")
	if !allowedPrometheusPaths[path] {
		writeProblem(w, r, http.StatusForbidden, "forbidden prometheus endpoint")
		return
	}
	allowed := url.Values{}
	for _, key := range []string{"query", "time", "timeout", "start", "end", "step"} {
		if v := r.URL.Query().Get(key); v != "" {
			allowed.Set(key, v)
		}
	}
	p.proxyTo(w, r, "/api/v1"+path, allowed)
}
```

- [ ] **Step 2: Run existing tests to verify refactor is correct**

Run: `go test ./internal/api/ -run TestPrometheus -v`
Expected: All existing proxy tests still pass

- [ ] **Step 3: Write failing tests for the new `HandleMetrics` handler**

Add to `internal/api/prometheus_test.go`:

```go
func TestMetricsProxyHandler_InstantQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("expected /api/v1/query, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("expected query=up, got %s", r.URL.Query().Get("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)
	req := httptest.NewRequest("GET", "/metrics?query=up", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_RangeQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("expected /api/v1/query_range, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)
	req := httptest.NewRequest("GET", "/metrics?query=up&start=100&end=200&step=15", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_MissingQuery(t *testing.T) {
	proxy := NewPrometheusProxy("http://localhost:1")
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestMetricsProxyHandler_NilProxy(t *testing.T) {
	var proxy *PrometheusProxy
	req := httptest.NewRequest("GET", "/metrics?query=up", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetrics(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestMetricsProxyHandler -v`
Expected: FAIL — `HandleMetrics` method does not exist

- [ ] **Step 5: Implement `HandleMetrics` on `*PrometheusProxy`**

```go
// HandleMetrics serves /metrics as a unified Prometheus proxy.
// If start+end params are present, proxies to /api/v1/query_range.
// Otherwise, proxies to /api/v1/query.
// Returns 503 if the receiver is nil (Prometheus not configured).
func (p *PrometheusProxy) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	q := r.URL.Query()
	query := q.Get("query")
	if query == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing required parameter: query")
		return
	}

	promPath := "/api/v1/query"
	if q.Get("start") != "" && q.Get("end") != "" {
		promPath = "/api/v1/query_range"
	}

	allowed := url.Values{}
	for _, key := range []string{"query", "time", "timeout", "start", "end", "step"} {
		if v := q.Get(key); v != "" {
			allowed.Set(key, v)
		}
	}

	p.proxyTo(w, r, promPath, allowed)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestMetricsProxyHandler -v`
Expected: PASS

- [ ] **Step 7: Update `NewRouter` to accept `*PrometheusProxy` and register `/metrics`**

Change `NewRouter` signature to add a `metricsProxy *PrometheusProxy` parameter (may be nil). The existing
`promProxy http.Handler` stays for now. Register the new route:

```go
// Metrics console (content-negotiated: HTML=SPA, JSON=proxy, SSE=stream)
mux.HandleFunc("GET /metrics", contentNegotiatedWithSSE(
	metricsProxy.HandleMetrics,
	h.HandleMetricsStream,
	spa,
))
```

Update `main.go` to pass the `*PrometheusProxy` (or nil) to `NewRouter`.

- [ ] **Step 8: Run full test suite**

Run: `go test ./internal/api/ -v`
Expected: All pass

- [ ] **Step 9: Commit**

```
feat(api): add /metrics as content-negotiated Prometheus proxy

Extracts proxyTo helper from ServeHTTP to share proxy logic. Adds
HandleMetrics which routes instant vs range queries by presence of
start+end params. Returns 503 when Prometheus is not configured.
Old /-/metrics/ routes remain for backward compatibility.
```

---

### Task 2: Frontend — API client and route setup

**Files:**

- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/api/client.test.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/vite.config.ts`

- [ ] **Step 1: Add new API methods to `client.ts`**

```typescript
queryMetrics: (query: string, time?: string) => {
  const params = new URLSearchParams({ query });
  if (time) params.set("time", time);
  return fetchJSON<PrometheusResponse>(`/metrics?${params}`);
},
queryMetricsRange: (query: string, start: string, end: string, step: string) => {
  const params = new URLSearchParams({ query, start, end, step });
  return fetchJSON<PrometheusResponse>(`/metrics?${params}`);
},
queryMetricsStreamURL: (query: string, step: number, range: number): string => {
  const params = new URLSearchParams({
    query,
    step: String(step),
    range: String(range),
  });
  return `/metrics?${params}`;
},
```

- [ ] **Step 2: Add tests for new API methods**

In `client.test.ts`, add tests that verify the URL construction produces `/metrics?query=...` paths.

- [ ] **Step 3: Add `metrics` to the Vite proxy regex**

In `vite.config.ts`, add `metrics` to the proxy pattern:

```typescript
"^/(nodes|services|tasks|configs|secrets|networks|volumes|stacks|search|events|topology|cluster|swarm|plugins|disk-usage|history|notifications|prometheus|metrics|api|-|debug)": {
```

- [ ] **Step 4: Add the lazy route in `App.tsx`**

```typescript
const MetricsConsole = lazy(() => import("./pages/MetricsConsole"));

// In Routes, before the topology route:
<Route path="/metrics" element={<MetricsConsole />} />
```

- [ ] **Step 5: Add "Metrics" to the nav links array in `App.tsx`**

```typescript
{ to: "/metrics", label: "Metrics", keys: ["g", "m"] },
```

Add the hotkey registration:

```typescript
"g m": useCallback(() => navigate("/metrics"), [navigate]),
```

- [ ] **Step 6: Run `npx tsc -b --noEmit` to verify types**

Expected: No errors

- [ ] **Step 7: Commit**

```
feat(frontend): add /metrics API client methods and route

Adds queryMetrics, queryMetricsRange, queryMetricsStreamURL to the API
client pointing at the new /metrics endpoint. Registers the lazy-loaded
MetricsConsole page and nav link.
```

---

### Task 3: Frontend — Query result table component

**Files:**

- Create: `frontend/src/components/metrics/QueryResultTable.tsx`
- Test: `frontend/src/components/metrics/QueryResultTable.test.tsx`

- [ ] **Step 1: Write a test for the result table**

```typescript
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import QueryResultTable from "./QueryResultTable";

describe("QueryResultTable", () => {
  it("renders metric name, labels, and value", () => {
    const data = {
      resultType: "vector" as const,
      result: [
        {
          metric: { __name__: "up", instance: "localhost:9090", job: "prometheus" },
          value: [1710000000, "1"] as [number, string],
        },
      ],
    };
    render(<QueryResultTable data={data} />);
    expect(screen.getByText("up")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("renders empty state when no results", () => {
    const data = { resultType: "vector" as const, result: [] };
    render(<QueryResultTable data={data} />);
    expect(screen.getByText(/no results/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/components/metrics/QueryResultTable.test.tsx`
Expected: FAIL — module not found

- [ ] **Step 3: Implement `QueryResultTable`**

The component renders a table with columns: Metric, Labels, Value, Timestamp. For vector results, each row is one
series. For matrix results, show the latest value per series. Labels are rendered as `key="value"` badges, excluding
`__name__`.

```typescript
interface VectorResult {
  metric: Record<string, string>;
  value: [number, string];
}

interface MatrixResult {
  metric: Record<string, string>;
  values: [number, string][];
}

interface Props {
  data: {
    resultType: "vector" | "matrix" | "scalar" | "string";
    result: VectorResult[] | MatrixResult[];
  };
}
```

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Commit**

```
feat(frontend): add QueryResultTable component for metrics console
```

---

### Task 4: Frontend — Query input component

**Files:**

- Create: `frontend/src/components/metrics/QueryInput.tsx`

- [ ] **Step 1: Implement `QueryInput`**

A textarea (auto-resizes 1-5 rows) with a Run button. Props:

```typescript
interface Props {
  value: string;
  onChange: (value: string) => void;
  onRun: () => void;
  loading: boolean;
}
```

Key behaviors:

- Cmd+Enter (or Ctrl+Enter) triggers `onRun`
- Monospace font, placeholder "Enter a PromQL expression..."
- Run button shows spinner when `loading`
- The textarea should use `font-mono` and a comfortable size

- [ ] **Step 2: Commit**

```
feat(frontend): add QueryInput component for metrics console
```

---

### Task 5: Frontend — MetricsConsole page

**Files:**

- Create: `frontend/src/pages/MetricsConsole.tsx`

- [ ] **Step 1: Wire up state and URL params**

Set up the component with URL-persisted state:

```typescript
const [searchParams, setSearchParams] = useSearchParams();
const [input, setInput] = useState(searchParams.get("q") ?? "");
const range = searchParams.get("range") ?? "1h";
const [result, setResult] = useState<PrometheusData | null>(null);
const [loading, setLoading] = useState(false);
const [error, setError] = useState<string | null>(null);
const monitoring = useMonitoringStatus();
```

Render the basic page skeleton: `PageHeader` with title "Query Console", `MonitoringStatus` banner when unconfigured.

- [ ] **Step 2: Add query execution**

Implement the `runQuery` callback:

1. Update URL params: `setSearchParams({ q: input, range })`
2. Set loading state
3. Call `api.queryMetrics(input)` for the instant result
4. Store result or error in state
5. Clear loading state

Wire `QueryInput`'s `onRun` to `runQuery`. Auto-run on mount if `?q=` is present.

- [ ] **Step 3: Add range controls and chart**

Add a `SegmentedControl` with presets (1h, 6h, 24h, 7d). Below it, render `TimeSeriesChart` when a query has been
executed. Pass `query`, `range`, `unit: "auto"`. Wrap in `ErrorBoundary`.

- [ ] **Step 4: Add result table**

Render `QueryResultTable` below the chart with the instant query result.

- [ ] **Step 5: Verify the page renders and executes queries**

Run: `cd frontend && npm run dev`
Navigate to `/metrics`, enter `up`, press Run. Verify chart and table render.

- [ ] **Step 6: Run type check and tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass

- [ ] **Step 7: Commit**

```
feat(frontend): add MetricsConsole page with query input, chart, and result table
```

---

## Phase 2: Metric Name + Function Autocompletion

### Task 6: Backend — Add label metadata endpoints

**Files:**

- Modify: `internal/api/prometheus.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/prometheus_test.go`

- [ ] **Step 1: Write failing tests for label proxying**

```go
func TestMetricsProxyHandler_Labels(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/labels" {
			t.Errorf("expected /api/v1/labels, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":["__name__","instance","job"]}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)
	req := httptest.NewRequest("GET", "/-/metrics/labels", nil)
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_LabelValues(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/label/job/values" {
			t.Errorf("expected /api/v1/label/job/values, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":["prometheus","node-exporter"]}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)
	req := httptest.NewRequest("GET", "/-/metrics/labels/job", nil)
	req.SetPathValue("name", "job")
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabelValues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestMetricsProxyHandler_LabelValues_NilProxy(t *testing.T) {
	var proxy *PrometheusProxy
	req := httptest.NewRequest("GET", "/-/metrics/labels/job", nil)
	req.SetPathValue("name", "job")
	w := httptest.NewRecorder()
	proxy.HandleMetricsLabelValues(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

- [ ] **Step 3: Implement `HandleMetricsLabels` and `HandleMetricsLabelValues`**

```go
// HandleMetricsLabels proxies to /api/v1/labels with optional match[] param.
func (p *PrometheusProxy) HandleMetricsLabels(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}
	allowed := url.Values{}
	for _, m := range r.URL.Query()["match[]"] {
		allowed.Add("match[]", m)
	}
	for _, key := range []string{"start", "end"} {
		if v := r.URL.Query().Get(key); v != "" {
			allowed.Set(key, v)
		}
	}
	p.proxyTo(w, r, "/api/v1/labels", allowed)
}

// HandleMetricsLabelValues proxies to /api/v1/label/{name}/values.
func (p *PrometheusProxy) HandleMetricsLabelValues(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing label name")
		return
	}
	allowed := url.Values{}
	for _, m := range r.URL.Query()["match[]"] {
		allowed.Add("match[]", m)
	}
	p.proxyTo(w, r, "/api/v1/label/"+url.PathEscape(name)+"/values", allowed)
}
```

- [ ] **Step 4: Register routes in router**

```go
mux.HandleFunc("GET /-/metrics/labels", metricsProxy.HandleMetricsLabels)
mux.HandleFunc("GET /-/metrics/labels/{name}", metricsProxy.HandleMetricsLabelValues)
```

Note: these are meta endpoints under `/-/`, so no content negotiation — JSON only, like `/-/metrics/status`.
They inherit the auth exemption for `/-/*` routes, which is intentional: label metadata is not sensitive.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/ -v`
Expected: All pass

- [ ] **Step 6: Commit**

```
feat(api): add /-/metrics/labels and /-/metrics/labels/{name} proxy endpoints

Proxies to Prometheus /api/v1/labels and /api/v1/label/{name}/values
for PromQL autocompletion metadata. Supports match[] filter params.
```

---

### Task 7: Frontend — Metric name + function completion

**Files:**

- Create: `frontend/src/components/metrics/useQueryCompletion.ts`
- Modify: `frontend/src/components/metrics/QueryInput.tsx`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add API client methods**

```typescript
metricsLabels: (match?: string) => {
  const params = new URLSearchParams();
  if (match) params.set("match[]", match);
  return fetchJSON<{ data: string[] }>(`/-/metrics/labels?${params}`).then((r) => r.data);
},
metricsLabelValues: (name: string) =>
  fetchJSON<{ data: string[] }>(`/-/metrics/labels/${encodeURIComponent(name)}`).then(
    (r) => r.data,
  ),
```

- [ ] **Step 2: Implement `useQueryCompletion` hook**

```typescript
interface Suggestion {
  label: string;
  type: "metric" | "function" | "label" | "value";
  detail?: string;
}

function useQueryCompletion(enabled: boolean): {
  suggestions: Suggestion[];
  loading: boolean;
  complete: (query: string, cursorPosition: number) => void;
  clear: () => void;
}
```

On first call to `complete()`:

1. Fetch metric names via `api.metricsLabelValues("__name__")` (cached — only fetched once, stored in a ref)
2. Combine with a static list of PromQL functions (hardcoded in the hook, ~60 entries)
3. Filter by the current token at cursor position (simple: split on non-alphanumeric-underscore, take the last token)
4. Return top 20 filtered results

The heuristic for determining what to suggest:

- If cursor is inside `{}` → label suggestions (Phase 3)
- Otherwise → metric names + functions, filtered by current prefix

- [ ] **Step 3: Add dropdown UI to `QueryInput`**

Add a `<ul>` dropdown below the textarea that:

- Shows when `suggestions.length > 0`
- Supports arrow key navigation + Enter to select + Escape to dismiss
- Positioned below the textarea (not at cursor — simpler, and the input is only 1-5 lines)
- Each item shows the name and a type badge ("metric" / "fn")
- Selecting a suggestion replaces the current token in the textarea

- [ ] **Step 4: Verify completion works**

Run dev server, navigate to `/metrics`, type `container_` — should see dropdown with matching metric names.

- [ ] **Step 5: Run type check and tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 6: Commit**

```
feat(frontend): add PromQL autocompletion with metric names and functions
```

---

## Phase 3: Label Name + Value Completion

### Task 8: Frontend — Label completion in `{}`

**Files:**

- Modify: `frontend/src/components/metrics/useQueryCompletion.ts`

- [ ] **Step 1: Extend the cursor context heuristic**

Add a `getCursorContext` function that determines what type of completion is needed.
The approach: scan backward from cursor to find the nearest unmatched `{`. If found,
scan forward from `{` to cursor to determine if we're in a label name or value position.

```typescript
type CursorContext =
  | { type: "metric" }
  | { type: "label"; metricName: string }
  | { type: "value"; metricName: string; labelName: string };

function getCursorContext(query: string, cursor: number): CursorContext {
  // Step 1: Find the nearest unmatched { before cursor
  let braceDepth = 0;
  let bracePos = -1;

  for (let i = cursor - 1; i >= 0; i--) {
    if (query[i] === "}") braceDepth++;
    if (query[i] === "{") {
      if (braceDepth === 0) {
        bracePos = i;
        break;
      }
      braceDepth--;
    }
  }

  if (bracePos === -1) return { type: "metric" };

  // Step 2: Extract metric name before {
  const beforeBrace = query.slice(0, bracePos);
  const metricMatch = beforeBrace.match(/([a-zA-Z_:][a-zA-Z0-9_:]*)$/);
  const metricName = metricMatch?.[1] ?? "";

  // Step 3: Scan the content between { and cursor to find context
  const insideBraces = query.slice(bracePos + 1, cursor);

  // After =" means we're typing a label value
  const valueMatch = insideBraces.match(/(\w+)\s*=~?\s*"[^"]*$/);
  if (valueMatch) {
    return { type: "value", metricName, labelName: valueMatch[1] };
  }

  // Otherwise we're typing a label name
  return { type: "label", metricName };
}
```

- [ ] **Step 2: Fetch label names on demand**

When context is `{ type: "label" }`, call `api.metricsLabels(metricName)` to get label names for that metric. Cache
per metric name.

- [ ] **Step 3: Fetch label values on demand**

When context is `{ type: "value", labelName }`, call `api.metricsLabelValues(labelName)`. Cache per label name.

- [ ] **Step 4: Verify label completion works**

Type `container_cpu_usage_seconds_total{` → should show label names.
Type `container_cpu_usage_seconds_total{job="` → should show label values for `job`.

- [ ] **Step 5: Run type check and tests**

- [ ] **Step 6: Commit**

```
feat(frontend): add label name and value autocompletion inside PromQL braces
```

---

## Phase 4: Migration and Cleanup

### Task 9: Migrate existing frontend to `/metrics` endpoints

**Files:**

- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/api/client.test.ts`
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx` (SSE URL construction)

- [ ] **Step 1: Update `metricsQuery`, `metricsQueryRange`, `metricsStreamURL` to use `/metrics`**

Change the paths from `/-/metrics/query` and `/-/metrics/query_range` to `/metrics`. Keep the old method names as
aliases if needed, or rename.

- [ ] **Step 2: Update tests**

- [ ] **Step 3: Verify all existing metrics panels still work**

Navigate to cluster overview, service detail, node detail — verify charts load and stream correctly.

- [ ] **Step 4: Commit**

```
refactor(frontend): migrate all metrics API calls from /-/metrics to /metrics
```

---

### Task 10: Remove old `/-/metrics/query` and `/-/metrics/query_range` routes

**Files:**

- Modify: `internal/api/router.go`
- Modify: `internal/api/prometheus.go` (remove old `ServeHTTP` if no longer used, remove `allowedPrometheusPaths`)
- Modify: `main.go` (check `serveDualListeners` — the Tsnet meta listener has its own route registration that may
  reference `/-/metrics/` routes; keep those for the meta listener or update them)
- Modify: `api/openapi.yaml`
- Modify: `docs/api.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Remove old route registrations from router**

Remove:

```go
mux.HandleFunc("GET /-/metrics/query_range", ...)
mux.Handle("GET /-/metrics/", promProxy)
```

Remove the old `promProxy http.Handler` parameter from `NewRouter` — `metricsProxy *PrometheusProxy` now
covers all use cases. Keep `/-/metrics/status` — that stays as a meta endpoint.

- [ ] **Step 2: Update OpenAPI spec**

Remove the `/-/metrics/{path}` and `/-/metrics/query_range` entries.
Add/update the `/metrics` entry with all three content types (JSON, SSE, HTML).
Add entries for `/-/metrics/labels` and `/-/metrics/labels/{name}`.

- [ ] **Step 3: Update `docs/api.md` and `CLAUDE.md`**

Update the architecture section, route table, and endpoint documentation to reflect the new `/metrics` endpoint.

- [ ] **Step 4: Run full test suite**

Run: `make check`
Expected: All pass

- [ ] **Step 5: Commit**

```
refactor(api): remove deprecated /-/metrics/query and /-/metrics/query_range routes

All Prometheus queries now go through /metrics with content negotiation.
/-/metrics/status, /-/metrics/labels, and /-/metrics/labels/{name}
remain as meta endpoints.
```

---

## Notes

**What is intentionally excluded:**

- Query history sidebar/dropdown (URL history via browser back/forward is sufficient)
- Multiple query panels (one query at a time — use MetricsPanel for dashboards)
- PromQL syntax-aware parsing (heuristic cursor context is good enough)
- Autocomplete for aggregation operators/keywords (functions cover the main value)
- `/-/metrics` for application metrics (future work, out of scope for this plan)
