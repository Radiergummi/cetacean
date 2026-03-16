# SSE Metrics Streaming Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace polling-based metrics refresh with SSE streaming — the server periodically queries Prometheus and pushes new data points to connected clients.

**Architecture:** New `HandleMetricsStream` method on `Handlers` uses `PromClient` to run periodic Prometheus queries and stream results as SSE events. Frontend `TimeSeriesChart` opens an `EventSource` after initial fetch and appends incoming data points. Content negotiation on `/-/metrics/query_range` routes JSON to the existing proxy, SSE to the new handler.

**Tech Stack:** Go stdlib (`net/http`, `sync/atomic`), existing `PromClient`, React `EventSource` API, Chart.js

**Spec:** `docs/superpowers/specs/2026-03-16-sse-metrics-streaming-design.md`

---

## File Structure

### Backend (create)
- `internal/api/metricsstream.go` — `HandleMetricsStream` handler, connection counter, SSE write helpers
- `internal/api/metricsstream_test.go` — tests for the streaming handler

### Backend (modify)
- `internal/api/promquery.go` — add `RangeQueryRaw` and `InstantQueryRaw` methods
- `internal/api/promquery_test.go` — tests for new methods
- `internal/api/router.go` — extract `/-/metrics/query_range` route with `contentNegotiatedWithSSE`

### Frontend (modify)
- `frontend/src/api/client.ts` — add `metricsStreamURL` helper
- `frontend/src/components/metrics/TimeSeriesChart.tsx` — add SSE subscription after initial fetch
- `frontend/src/components/metrics/MetricsPanel.tsx` — remove auto-refresh toggle

---

## Chunk 1: Backend — PromClient raw query methods

### Task 1: Add `RangeQueryRaw` to PromClient

**Files:**
- Modify: `internal/api/promquery.go`
- Modify: `internal/api/promquery_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/api/promquery_test.go`, add a test for `RangeQueryRaw`. Use `httptest.NewServer` to mock Prometheus returning a `query_range` response.

```go
func TestRangeQueryRaw(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"up"},"values":[[1710000000,"1"],[1710000015,"1"]]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("unexpected query: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("start") == "" || r.URL.Query().Get("end") == "" || r.URL.Query().Get("step") == "" {
			t.Error("missing start/end/step params")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	pc := NewPromClient(srv.URL)
	raw, err := pc.RangeQueryRaw(context.Background(), "up", "1710000000", "1710000015", "15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != body {
		t.Errorf("unexpected body: %s", string(raw))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestRangeQueryRaw -v`
Expected: FAIL — `RangeQueryRaw` not defined.

- [ ] **Step 3: Write implementation**

In `internal/api/promquery.go`, add after the existing `InstantQuery` method:

```go
// RangeQueryRaw executes a Prometheus range query and returns the raw JSON response bytes.
func (pc *PromClient) RangeQueryRaw(ctx context.Context, query, start, end, step string) ([]byte, error) {
	u := pc.baseURL + "/api/v1/query_range?query=" + url.QueryEscape(query) +
		"&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end) +
		"&step=" + url.QueryEscape(step)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
```

Add `"io"` to imports if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestRangeQueryRaw -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/promquery.go internal/api/promquery_test.go
git commit -m "feat: add RangeQueryRaw to PromClient"
```

### Task 2: Add `InstantQueryRaw` to PromClient

**Files:**
- Modify: `internal/api/promquery.go`
- Modify: `internal/api/promquery_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestInstantQueryRaw(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up"},"value":[1710000030,"1"]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	pc := NewPromClient(srv.URL)
	raw, err := pc.InstantQueryRaw(context.Background(), "up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != body {
		t.Errorf("unexpected body: %s", string(raw))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestInstantQueryRaw -v`
Expected: FAIL — `InstantQueryRaw` not defined.

- [ ] **Step 3: Write implementation**

```go
// InstantQueryRaw executes a Prometheus instant query and returns the raw JSON response bytes.
func (pc *PromClient) InstantQueryRaw(ctx context.Context, query string) ([]byte, error) {
	u := pc.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestInstantQueryRaw -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/promquery.go internal/api/promquery_test.go
git commit -m "feat: add InstantQueryRaw to PromClient"
```

### Task 3: Test PromClient raw methods handle errors

**Files:**
- Modify: `internal/api/promquery_test.go`

- [ ] **Step 1: Write tests for error cases**

```go
func TestRangeQueryRaw_PrometheusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"invalid query"}`))
	}))
	defer srv.Close()

	pc := NewPromClient(srv.URL)
	_, err := pc.RangeQueryRaw(context.Background(), "bad{", "0", "1", "1")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestInstantQueryRaw_ConnectionRefused(t *testing.T) {
	pc := NewPromClient("http://127.0.0.1:1") // nothing listening
	_, err := pc.InstantQueryRaw(context.Background(), "up")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/api/ -run "TestRangeQueryRaw_Prometheus|TestInstantQueryRaw_Connection" -v`
Expected: PASS (both error paths return non-nil error)

- [ ] **Step 3: Commit**

```bash
git add internal/api/promquery_test.go
git commit -m "test: add error case tests for PromClient raw methods"
```

---

## Chunk 2: Backend — Metrics stream handler

### Task 4: Create `HandleMetricsStream` handler skeleton

**Files:**
- Create: `internal/api/metricsstream.go`
- Create: `internal/api/metricsstream_test.go`

- [ ] **Step 1: Write the failing test**

Test that the handler validates params, returns 400 for missing query, returns 503 when PromClient is nil.

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsStream_MissingQuery(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/-/metrics/query_range", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMetricsStream_NoPromClient(t *testing.T) {
	h := &Handlers{} // promClient is nil
	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run "TestMetricsStream_Missing|TestMetricsStream_NoProm" -v`
Expected: FAIL — `HandleMetricsStream` not defined.

- [ ] **Step 3: Write handler skeleton**

Create `internal/api/metricsstream.go`:

```go
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
)

const maxMetricsStreamClients = 64

var metricsStreamCount atomic.Int32

// HandleMetricsStream serves a Server-Sent Events stream of Prometheus query results.
// The handler periodically executes the given PromQL query and pushes results as SSE events.
func (h *Handlers) HandleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if h.promClient == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing required parameter: query")
		return
	}

	step := 15
	if s := r.URL.Query().Get("step"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 5 || v > 300 {
			writeProblem(w, r, http.StatusBadRequest, "step must be between 5 and 300 seconds")
			return
		}
		step = v
	}

	rangeSec := 3600
	if rs := r.URL.Query().Get("range"); rs != "" {
		v, err := strconv.Atoi(rs)
		if err == nil && v > 0 {
			rangeSec = v
		}
	}

	if int(metricsStreamCount.Load()) >= maxMetricsStreamClients {
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusTooManyRequests, "too many metrics stream connections")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "streaming not supported")
		return
	}

	metricsStreamCount.Add(1)
	defer metricsStreamCount.Add(-1)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	// Streaming loop will be implemented in the next task.
	_ = step
	_ = rangeSec
	_ = query
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run "TestMetricsStream_Missing|TestMetricsStream_NoProm" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/metricsstream.go internal/api/metricsstream_test.go
git commit -m "feat: add HandleMetricsStream skeleton with param validation"
```

### Task 5: Test connection limit

**Files:**
- Modify: `internal/api/metricsstream_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestMetricsStream_ConnectionLimit(t *testing.T) {
	metricsStreamCount.Store(int32(maxMetricsStreamClients))
	defer metricsStreamCount.Store(0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}
	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up", nil)
	w := httptest.NewRecorder()
	h.HandleMetricsStream(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "5" {
		t.Error("expected Retry-After: 5 header")
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestMetricsStream_ConnectionLimit -v`
Expected: PASS (already implemented in skeleton)

- [ ] **Step 3: Commit**

```bash
git add internal/api/metricsstream_test.go
git commit -m "test: add connection limit test for metrics stream"
```

### Task 6: Implement the streaming loop

**Files:**
- Modify: `internal/api/metricsstream.go`

- [ ] **Step 1: Write a test for the streaming loop**

Test that the handler sends an `initial` event, then `point` events on each tick. Use a mock Prometheus server and a short step interval.

```go
func TestMetricsStream_StreamsEvents(t *testing.T) {
	testTickerInterval = 10 * time.Millisecond
	defer func() { testTickerInterval = 0 }()

	queryCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query_range" {
			w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		} else {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: initial") {
		t.Error("expected initial event in body")
	}
	if !strings.Contains(body, "event: point") {
		t.Error("expected at least one point event in body")
	}
	if queryCount.Load() < 2 {
		t.Errorf("expected at least 2 Prometheus calls (range + instant), got %d", queryCount.Load())
	}
}
```

Add `"context"`, `"strings"`, `"time"`, `"sync/atomic"` to imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestMetricsStream_StreamsEvents -v`
Expected: FAIL — no `event: initial` in response body.

- [ ] **Step 3: Implement the streaming loop**

Replace the placeholder at the end of `HandleMetricsStream` with the streaming loop. The tick query runs in a goroutine so the `inflight` guard actually prevents query pile-up when Prometheus is slow. A `results` channel funnels query results back to the SSE write loop (single writer to `ResponseWriter`).

The ticker interval is controlled via `tickerInterval` (a package-level variable, defaulting to the step duration) so tests can override it to avoid slow test runs.

```go
	ctx := r.Context()

	// Send initial range query result.
	now := strconv.FormatInt(time.Now().Unix(), 10)
	start := strconv.FormatInt(time.Now().Unix()-int64(rangeSec), 10)
	stepStr := strconv.Itoa(step)
	initial, err := h.promClient.RangeQueryRaw(ctx, query, start, now, stepStr)
	if err != nil {
		fmt.Fprintf(w, "event: query_error\ndata: %s\n\n", marshalErrorEvent(err))
		flusher.Flush()
	} else {
		writeSSEEvent(w, flusher, "initial", initial)
	}

	interval := time.Duration(step) * time.Second
	if testTickerInterval > 0 {
		interval = testTickerInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	type queryResult struct {
		data []byte
		err  error
	}
	results := make(chan queryResult, 1)
	var inflight atomic.Bool

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if inflight.Load() {
				continue // skip if previous query still running
			}
			inflight.Store(true)
			go func() {
				raw, err := h.promClient.InstantQueryRaw(ctx, query)
				inflight.Store(false)
				select {
				case results <- queryResult{raw, err}:
				case <-ctx.Done():
				}
			}()
		case res := <-results:
			if ctx.Err() != nil {
				return
			}
			if res.err != nil {
				fmt.Fprintf(w, "event: query_error\ndata: %s\n\n", marshalErrorEvent(res.err))
				flusher.Flush()
			} else {
				writeSSEEvent(w, flusher, "point", res.data)
			}
			keepalive.Reset(15 * time.Second)
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
```

Add these helper functions and the test hook to the same file:

```go
// testTickerInterval overrides the tick interval in tests. Zero means use the step value.
var testTickerInterval time.Duration

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}

func marshalErrorEvent(err error) string {
	msg := err.Error()
	b, _ := json.Marshal(map[string]string{"error": msg, "errorType": "server_error"})
	return string(b)
}
```

Add `"encoding/json"`, `"time"`, `"sync/atomic"` to imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestMetricsStream_StreamsEvents -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/metricsstream.go internal/api/metricsstream_test.go
git commit -m "feat: implement metrics stream SSE loop with initial + point events"
```

### Task 7: Test error event on Prometheus failure

**Files:**
- Modify: `internal/api/metricsstream_test.go`

- [ ] **Step 1: Write the test**

```go
func TestMetricsStream_ErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=up&step=15", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "event: query_error") {
		t.Error("expected query_error event when Prometheus returns 500")
	}
	if !strings.Contains(body, "server_error") {
		t.Error("expected errorType in error event data")
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestMetricsStream_ErrorEvent -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/metricsstream_test.go
git commit -m "test: verify error event on Prometheus failure during streaming"
```

---

## Chunk 3: Backend — Router wiring

### Task 8: Wire `HandleMetricsStream` into the router

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Extract `/-/metrics/query_range` as a separate route**

In `internal/api/router.go`, find the line:
```go
mux.Handle("GET /-/metrics/", promProxy)
```

Add a new route **before** it for the specific `query_range` path:

```go
mux.HandleFunc("GET /-/metrics/query_range", contentNegotiatedWithSSE(
	promProxy.ServeHTTP,
	h.HandleMetricsStream,
	spa,
))
mux.Handle("GET /-/metrics/", promProxy)
```

- [ ] **Step 2: Run all tests to verify nothing is broken**

Run: `go test ./internal/api/ -v`
Expected: All tests pass. The catch-all `/-/metrics/` route continues to handle `/query` and other paths.

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: wire HandleMetricsStream via content negotiation on query_range"
```

---

## Chunk 4: Frontend — SSE subscription in TimeSeriesChart

### Task 9: Add `metricsStreamURL` helper to API client

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add the helper function**

Add to the `api` object in `frontend/src/api/client.ts`:

```typescript
metricsStreamURL: (query: string, step: number, range: number): string => {
  const params = new URLSearchParams({ query, step: String(step), range: String(range) });
  return `/-/metrics/query_range?${params}`;
},
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add metricsStreamURL helper to API client"
```

### Task 10: Add SSE subscription to TimeSeriesChart

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add SSE connection logic after initial fetch**

In `TimeSeriesChart.tsx`, add a new `useEffect` after the existing `fetchData` effect (after line 258). This effect opens an `EventSource` when the chart has data and is on a live range (no custom from/to):

```typescript
// SSE streaming for live ranges
useEffect(() => {
  if (!fetchedData || from != null || to != null) return;

  const rangeSec = RANGE_SECONDS[range] || 3600;
  const step = Math.max(Math.floor(rangeSec / 300), 15);
  const url = api.metricsStreamURL(query, step, rangeSec);
  const es = new EventSource(url);

  es.addEventListener("initial", (e: MessageEvent) => {
    try {
      const resp = JSON.parse(e.data) as PrometheusResponse;
      if (!resp.data?.result?.length) return;
      const result = resp.data.result;
      const timestamps = result[0].values!.map((v) => Number(v[0]));
      const labels = timestamps.map((ts) => new Date(ts * 1000).toLocaleTimeString());
      const series = result.map((s, i) => ({
        label: seriesLabel(s.metric, result.length === 1 ? title : undefined),
        color: colorOverride ?? getChartColor(i),
        data: s.values!.map((v) => Number(v[1])),
      }));
      setFetchedData({ labels, timestamps, series });
      onSeriesInfo?.(series.map((s) => ({ label: s.label, color: s.color })));
      setState("data");
    } catch { /* ignore parse errors */ }
  });

  es.addEventListener("point", (e: MessageEvent) => {
    try {
      const resp = JSON.parse(e.data) as PrometheusResponse;
      if (!resp.data?.result?.length) return;
      setFetchedData((prev) => {
        if (!prev) return prev;
        const ts = Number(resp.data.result[0].value![0]);
        const timeLabel = new Date(ts * 1000).toLocaleTimeString();
        const newTimestamps = [...prev.timestamps.slice(1), ts];
        const newLabels = [...prev.labels.slice(1), timeLabel];
        const newSeries = prev.series.map((s) => {
          const match = resp.data.result.find(
            (r) => seriesLabel(r.metric) === s.label
          );
          const val = match ? Number(match.value![1]) : 0;
          return { ...s, data: [...s.data.slice(1), val] };
        });
        return { labels: newLabels, timestamps: newTimestamps, series: newSeries };
      });
    } catch { /* ignore parse errors */ }
  });

  es.addEventListener("query_error", (e: MessageEvent) => {
    // Application-level Prometheus error — logged but stream continues
    console.warn("[metrics stream] Prometheus error:", e.data);
  });

  es.onerror = () => {
    // Connection-level error — close and fall back to polling
    es.close();
  };

  // Close connection when tab is hidden to save resources
  const visHandler = () => {
    if (document.visibilityState === "hidden") {
      es.close();
    }
  };
  document.addEventListener("visibilitychange", visHandler);

  return () => {
    es.close();
    document.removeEventListener("visibilitychange", visHandler);
  };
}, [fetchedData ? query : null, range, from, to]);
```

Import `api` is already imported. Add `PrometheusResponse` to the types import if not already there:

```typescript
import type { PrometheusResponse } from "../../api/types";
```

The dependency `fetchedData ? query : null` ensures the effect only runs once `fetchedData` is set (after initial fetch). When `query` or `range` changes, `fetchData` runs first (setting `fetchedData` to new data), then this effect reconnects. When the tab is hidden, the EventSource is closed; when visible again, the effect doesn't auto-rerun, but the next step handles that.

- [ ] **Step 2: Add visibility-based refetch**

Add another `useEffect` that refetches data (and triggers SSE reconnect) when the tab becomes visible after being hidden:

```typescript
// Refetch + reconnect SSE when tab becomes visible after being hidden
useEffect(() => {
  const handler = () => {
    if (document.visibilityState === "visible") {
      fetchData();
    }
  };
  document.addEventListener("visibilitychange", handler);
  return () => document.removeEventListener("visibilitychange", handler);
}, [fetchData]);
```

This naturally restarts the SSE connection (via the dependency chain: `fetchData` → `setFetchedData` → SSE effect reruns).

- [ ] **Step 3: Run type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: add SSE subscription for real-time metrics streaming"
```

### Task 11: Remove auto-refresh from MetricsPanel

**Files:**
- Modify: `frontend/src/components/metrics/MetricsPanel.tsx`

- [ ] **Step 1: Remove auto-refresh state and controls**

In `MetricsPanel.tsx`:

1. Remove the `autoRefresh` state and its effect:
   ```typescript
   // DELETE these lines:
   const [autoRefresh, setAutoRefresh] = useState(false);
   // ...
   useEffect(() => {
     if (!autoRefresh) return;
     const interval = setInterval(() => setRefreshKey((k) => k + 1), 30000);
     return () => clearInterval(interval);
   }, [autoRefresh]);
   ```

2. Remove the auto-refresh toggle button from the controls JSX:
   ```typescript
   // DELETE this IconButton:
   <IconButton
     onClick={() => setAutoRefresh((v) => !v)}
     title={autoRefresh ? "Pause auto-refresh" : "Auto-refresh (30s)"}
     icon={autoRefresh ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
     active={autoRefresh}
   />
   ```

3. Remove unused imports: `Play`, `Square` from lucide-react.

- [ ] **Step 2: Run type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/metrics/MetricsPanel.tsx
git commit -m "feat: remove auto-refresh toggle, replaced by SSE streaming"
```

---

## Chunk 5: Integration test and cleanup

### Task 12: End-to-end integration test

**Files:**
- Modify: `internal/api/metricsstream_test.go`

- [ ] **Step 1: Write integration test with full lifecycle**

Test that connects, receives initial event, receives a point event after one tick, then disconnects cleanly.

```go
func TestMetricsStream_FullLifecycle(t *testing.T) {
	testTickerInterval = 20 * time.Millisecond
	defer func() { testTickerInterval = 0 }()

	queryCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		ts := time.Now().Unix()
		if r.URL.Path == "/api/v1/query_range" {
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"cpu"},"values":[[%d,"42"]]}]}}`, ts)
		} else {
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"cpu"},"value":[%d,"43"]}]}}`, ts)
		}
	}))
	defer srv.Close()

	h := &Handlers{promClient: NewPromClient(srv.URL)}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/-/metrics/query_range?query=cpu&step=15&range=300", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleMetricsStream(w, req)

	body := w.Body.String()

	// Should have initial event with metric data
	if !strings.Contains(body, "event: initial") {
		t.Error("missing initial event")
	}
	if !strings.Contains(body, `"cpu"`) {
		t.Error("initial event should contain metric name")
	}

	// Should have at least one point event from the ticker
	if !strings.Contains(body, "event: point") {
		t.Error("missing point event")
	}

	// Verify range + at least one instant query
	if queryCount.Load() < 2 {
		t.Errorf("expected at least 2 Prometheus calls, got %d", queryCount.Load())
	}

	// Verify connection counter was decremented
	if metricsStreamCount.Load() != 0 {
		t.Error("connection counter should be 0 after handler returns")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/api/ -run TestMetricsStream_FullLifecycle -v`
Expected: PASS

- [ ] **Step 3: Run all backend tests**

Run: `go test ./...`
Expected: All pass.

- [ ] **Step 4: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add internal/api/metricsstream_test.go
git commit -m "test: add full lifecycle integration test for metrics streaming"
```

### Task 13: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update architecture docs**

Add to the `api/` section in CLAUDE.md under the existing bullet points:

```
- **`api/metricsstream.go`** — SSE streaming handler for Prometheus queries. Registered at `GET /-/metrics/query_range` via content negotiation (JSON → proxy, SSE → stream handler). Periodically runs instant queries and pushes `point` events; sends full `initial` event on connect. 64-connection limit, 15s keepalive, skips ticks if previous query is in-flight.
```

Add to the `promquery.go` description:

```
Also provides `RangeQueryRaw` and `InstantQueryRaw` for raw JSON byte responses (used by metrics stream handler).
```

Update the frontend `hooks/useResourceStream.ts` or `TimeSeriesChart` description to mention SSE metrics streaming:

```
SSE streaming: live range charts (1h/6h/24h/7d) open an `EventSource` to `/-/metrics/query_range` after the initial JSON fetch, receiving `initial` (full range) and `point` (single value) events for real-time updates. Custom time ranges use JSON-only.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document SSE metrics streaming in CLAUDE.md"
```
