# Self-Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose Prometheus metrics about Cetacean's own operation at `GET /-/metrics`.

**Architecture:** New `internal/metrics` package owns a custom Prometheus registry and defines all metric descriptors. Subsystems call `metrics.RecordX()` functions; they never import Prometheus directly. HTTP instrumentation via middleware wrapping the mux.

**Tech Stack:** `github.com/prometheus/client_golang/prometheus`, `promhttp`

---

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/metrics/metrics.go` | Registry, all metric descriptors, `RecordX` functions, HTTP handler |
| Create | `internal/metrics/metrics_test.go` | Unit tests for all `RecordX` functions |
| Modify | `go.mod` | Add `prometheus/client_golang` dependency |
| Modify | `internal/api/router.go` | Register `GET /-/metrics` endpoint, add metrics middleware |
| Modify | `internal/api/middleware.go` | HTTP metrics middleware using `statusWriter` |
| Modify | `internal/api/sse/broadcaster.go` | SSE connection/event/drop metrics |
| Modify | `internal/cache/cache.go` | Cache mutation metrics |
| Modify | `internal/docker/watcher.go` | Sync duration metrics |
| Modify | `internal/api/prometheus/proxy.go` | Upstream request metrics |
| Modify | `internal/recommendations/engine.go` | Checker duration and result count metrics |
| Modify | `main.go` | Wire metrics registry into router |

---

### Task 1: Create the metrics package with HTTP metrics

**Files:**
- Create: `internal/metrics/metrics.go`
- Create: `internal/metrics/metrics_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Add prometheus dependency**

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

- [ ] **Step 2: Write failing test for HTTP metrics**

```go
// internal/metrics/metrics_test.go
package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/golang"
)

func gatherMetric(t *testing.T, name string) *io_prometheus_client.MetricFamily {
	t.Helper()
	families, err := Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func TestRecordHTTPRequest(t *testing.T) {
	RecordHTTPRequest("GET /nodes", "GET", 200, 0.005, 0, 1024)

	fam := gatherMetric(t, "cetacean_http_requests_total")
	if fam == nil {
		t.Fatal("metric cetacean_http_requests_total not found")
	}
	if len(fam.GetMetric()) == 0 {
		t.Fatal("expected at least one metric sample")
	}

	m := fam.GetMetric()[0]
	if m.GetCounter().GetValue() != 1 {
		t.Errorf("expected count 1, got %f", m.GetCounter().GetValue())
	}
}
```

Run: `go test ./internal/metrics/ -run TestRecordHTTPRequest -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Write minimal metrics package**

```go
// internal/metrics/metrics.go
package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the custom Prometheus registry for Cetacean metrics.
var Registry = prometheus.NewRegistry()

var (
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cetacean_http_requests_total",
		Help: "Total HTTP requests by method, handler, and status code.",
	}, []string{"method", "handler", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cetacean_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "handler"})

	httpRequestSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cetacean_http_request_size_bytes",
		Help:    "HTTP request body size in bytes.",
		Buckets: prometheus.ExponentialBuckets(64, 4, 8),
	}, []string{"method", "handler"})

	httpResponseSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cetacean_http_response_size_bytes",
		Help:    "HTTP response body size in bytes.",
		Buckets: prometheus.ExponentialBuckets(64, 4, 8),
	}, []string{"method", "handler"})
)

func init() {
	Registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		httpRequestSize,
		httpResponseSize,
	)
}

// RecordHTTPRequest records metrics for a completed HTTP request.
func RecordHTTPRequest(handler, method string, status int, durationSeconds float64, requestBytes, responseBytes int64) {
	statusStr := strconv.Itoa(status)
	httpRequestsTotal.WithLabelValues(method, handler, statusStr).Inc()
	httpRequestDuration.WithLabelValues(method, handler).Observe(durationSeconds)
	httpRequestSize.WithLabelValues(method, handler).Observe(float64(requestBytes))
	httpResponseSize.WithLabelValues(method, handler).Observe(float64(responseBytes))
}

// Handler returns an http.Handler that serves the metrics endpoint.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/metrics/ -run TestRecordHTTPRequest -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(metrics): add metrics package with HTTP request metrics
```

---

### Task 2: Add SSE, cache, proxy, and recommendation metrics

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/metrics/metrics_test.go`

- [ ] **Step 1: Write failing tests**

```go
// append to internal/metrics/metrics_test.go

func TestRecordSSEConnect(t *testing.T) {
	RecordSSEConnect()
	fam := gatherMetric(t, "cetacean_sse_connections_active")
	if fam == nil {
		t.Fatal("metric not found")
	}
	if fam.GetMetric()[0].GetGauge().GetValue() != 1 {
		t.Errorf("expected 1, got %f", fam.GetMetric()[0].GetGauge().GetValue())
	}

	RecordSSEDisconnect()
	fam = gatherMetric(t, "cetacean_sse_connections_active")
	if fam.GetMetric()[0].GetGauge().GetValue() != 0 {
		t.Errorf("expected 0 after disconnect, got %f", fam.GetMetric()[0].GetGauge().GetValue())
	}
}

func TestRecordSSEBroadcast(t *testing.T) {
	RecordSSEBroadcast()
	fam := gatherMetric(t, "cetacean_sse_events_broadcast_total")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestRecordSSEDrop(t *testing.T) {
	RecordSSEDrop()
	fam := gatherMetric(t, "cetacean_sse_events_dropped_total")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestRecordCacheMutation(t *testing.T) {
	RecordCacheMutation("service", "update")
	fam := gatherMetric(t, "cetacean_cache_mutations_total")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestSetCacheResources(t *testing.T) {
	SetCacheResources("nodes", 5)
	fam := gatherMetric(t, "cetacean_cache_resources")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestObserveSyncDuration(t *testing.T) {
	ObserveSyncDuration(1.5)
	fam := gatherMetric(t, "cetacean_cache_sync_duration_seconds")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestRecordPrometheusRequest(t *testing.T) {
	RecordPrometheusRequest(200, 0.1)
	fam := gatherMetric(t, "cetacean_prometheus_requests_total")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestObserveRecommendationCheck(t *testing.T) {
	ObserveRecommendationCheck("config", 0.05)
	fam := gatherMetric(t, "cetacean_recommendations_check_duration_seconds")
	if fam == nil {
		t.Fatal("metric not found")
	}
}

func TestSetRecommendationCounts(t *testing.T) {
	SetRecommendationCounts(3, 2, 1)
	fam := gatherMetric(t, "cetacean_recommendations_total")
	if fam == nil {
		t.Fatal("metric not found")
	}
	if len(fam.GetMetric()) != 3 {
		t.Errorf("expected 3 severity labels, got %d", len(fam.GetMetric()))
	}
}
```

Run: `go test ./internal/metrics/ -v`
Expected: FAIL — undefined functions

- [ ] **Step 2: Implement all remaining metric descriptors and record functions**

Append to `internal/metrics/metrics.go`:

```go
var (
	// SSE
	sseConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cetacean_sse_connections_active",
		Help: "Number of active SSE client connections.",
	})
	sseBroadcastTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cetacean_sse_events_broadcast_total",
		Help: "Total SSE events broadcast to clients.",
	})
	sseDroppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cetacean_sse_events_dropped_total",
		Help: "Total SSE events dropped due to full buffers.",
	})

	// Cache
	cacheResources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cetacean_cache_resources",
		Help: "Number of cached resources by type.",
	}, []string{"type"})
	cacheSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cetacean_cache_sync_duration_seconds",
		Help:    "Duration of full cache syncs from Docker.",
		Buckets: prometheus.DefBuckets,
	})
	cacheMutations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cetacean_cache_mutations_total",
		Help: "Total cache mutations by resource type and action.",
	}, []string{"type", "action"})

	// Prometheus proxy
	prometheusRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cetacean_prometheus_requests_total",
		Help: "Total proxied Prometheus requests by upstream status.",
	}, []string{"status"})
	prometheusRequestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cetacean_prometheus_request_duration_seconds",
		Help:    "Duration of proxied Prometheus requests.",
		Buckets: prometheus.DefBuckets,
	})

	// Recommendations
	recommendationsCheckDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cetacean_recommendations_check_duration_seconds",
		Help:    "Duration of recommendation checker runs.",
		Buckets: prometheus.DefBuckets,
	}, []string{"checker"})
	recommendationsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cetacean_recommendations_total",
		Help: "Current recommendation count by severity.",
	}, []string{"severity"})
)

func init() {
	Registry.MustRegister(
		sseConnectionsActive,
		sseBroadcastTotal,
		sseDroppedTotal,
		cacheResources,
		cacheSyncDuration,
		cacheMutations,
		prometheusRequestsTotal,
		prometheusRequestDuration,
		recommendationsCheckDuration,
		recommendationsTotal,
	)
}

// SSE

func RecordSSEConnect()    { sseConnectionsActive.Inc() }
func RecordSSEDisconnect() { sseConnectionsActive.Dec() }
func RecordSSEBroadcast()  { sseBroadcastTotal.Inc() }
func RecordSSEDrop()       { sseDroppedTotal.Inc() }

// Cache

func SetCacheResources(resourceType string, count int) {
	cacheResources.WithLabelValues(resourceType).Set(float64(count))
}

func ObserveSyncDuration(seconds float64) {
	cacheSyncDuration.Observe(seconds)
}

func RecordCacheMutation(resourceType, action string) {
	cacheMutations.WithLabelValues(resourceType, action).Inc()
}

// Prometheus proxy

func RecordPrometheusRequest(status int, durationSeconds float64) {
	prometheusRequestsTotal.WithLabelValues(strconv.Itoa(status)).Inc()
	prometheusRequestDuration.Observe(durationSeconds)
}

// Recommendations

func ObserveRecommendationCheck(checker string, durationSeconds float64) {
	recommendationsCheckDuration.WithLabelValues(checker).Observe(durationSeconds)
}

func SetRecommendationCounts(critical, warning, info int) {
	recommendationsTotal.WithLabelValues("critical").Set(float64(critical))
	recommendationsTotal.WithLabelValues("warning").Set(float64(warning))
	recommendationsTotal.WithLabelValues("info").Set(float64(info))
}
```

Merge both `init()` blocks into one.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/metrics/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat(metrics): add SSE, cache, proxy, and recommendation metric descriptors
```

---

### Task 3: Wire HTTP metrics middleware and endpoint

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/router.go`
- Modify: `main.go`

- [ ] **Step 1: Add metrics middleware to middleware.go**

Add after the existing `requestLogger` middleware:

```go
// instrumentMetrics records Prometheus metrics for each request.
func instrumentMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		duration := time.Since(start).Seconds()

		handler := r.Pattern
		if handler == "" {
			handler = "unknown"
		}

		metrics.RecordHTTPRequest(
			handler,
			r.Method,
			sw.status,
			duration,
			r.ContentLength,
			int64(sw.written),
		)
	})
}
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Register endpoint and middleware in router.go**

In `NewRouter`, add the `/-/metrics` endpoint near the other `/-/` routes:

```go
mux.Handle("GET /-/metrics", metrics.Handler())
```

Add `instrumentMetrics` to the middleware chain, just before `requestLogger`:

```go
handler = requestLogger(handler)
handler = instrumentMetrics(handler)  // ← add here
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 3: Run the full test suite**

Run: `go test ./internal/api/ -count=1 -timeout=120s`
Expected: PASS

- [ ] **Step 4: Manual smoke test** (optional if running locally)

Run: `curl -s http://localhost:9000/-/metrics | head -20`
Expected: Prometheus exposition format with `cetacean_http_*` metrics

- [ ] **Step 5: Commit**

```
feat(metrics): wire HTTP metrics middleware and /-/metrics endpoint
```

---

### Task 4: Instrument SSE broadcaster

**Files:**
- Modify: `internal/api/sse/broadcaster.go`

- [ ] **Step 1: Add metrics calls to broadcaster**

In `Broadcast()`, after the successful channel send (line ~60):
```go
case b.inbox <- e:
	metrics.RecordSSEBroadcast()
```

In the `default` (drop) case (line ~62):
```go
default:
	metrics.RecordSSEDrop()
	slog.Warn(...)
```

In `ServeHTTP` (or wherever clients connect), after adding to `b.clients`:
```go
b.clients[client] = struct{}{}
metrics.RecordSSEConnect()
```

In the deferred cleanup where client is removed:
```go
delete(b.clients, client)
metrics.RecordSSEDisconnect()
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Run tests**

Run: `go test ./internal/api/sse/ -count=1 -v`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat(metrics): instrument SSE broadcaster with connection and event metrics
```

---

### Task 5: Instrument cache mutations

**Files:**
- Modify: `internal/cache/cache.go`

- [ ] **Step 1: Add metrics calls to notify()**

In the `notify` method, record each mutation:

```go
func (c *Cache) notify(e Event) {
	if e.Type != EventSync {
		metrics.RecordCacheMutation(string(e.Type), e.Action)
		c.history.Append(...)
	}
	...
}
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Add resource count updates to ReplaceAll()**

At the end of `ReplaceAll`, after the lock is released and before notify:

```go
metrics.SetCacheResources("nodes", len(nodes))
metrics.SetCacheResources("services", len(services))
metrics.SetCacheResources("tasks", len(tasks))
metrics.SetCacheResources("configs", len(configs))
metrics.SetCacheResources("secrets", len(secrets))
metrics.SetCacheResources("networks", len(networks))
metrics.SetCacheResources("volumes", len(volumes))
```

Use the local slices (already converted to maps under lock), or read from `c.Snapshot()` counts.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cache/ -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat(metrics): instrument cache mutations and resource counts
```

---

### Task 6: Instrument sync duration

**Files:**
- Modify: `internal/docker/watcher.go`

- [ ] **Step 1: Time fullSync and record**

In the `fullSync` method, wrap the sync with timing:

```go
func (w *Watcher) fullSync(ctx context.Context) error {
	start := time.Now()
	slog.Info("starting full sync")

	data, err := w.client.FullSync(ctx)
	if err != nil {
		slog.Error("full sync failed", "error", err)
		return err
	}

	w.store.ReplaceAll(data)
	metrics.ObserveSyncDuration(time.Since(start).Seconds())
	...
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Run tests**

Run: `go test ./internal/docker/ -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat(metrics): instrument Docker full sync duration
```

---

### Task 7: Instrument Prometheus proxy

**Files:**
- Modify: `internal/api/prometheus/proxy.go`

- [ ] **Step 1: Time upstream requests in proxyTo**

At the start of `proxyTo`, before `p.client.Do(outReq)`:

```go
proxyStart := time.Now()
resp, err := p.client.Do(outReq)
proxyDuration := time.Since(proxyStart).Seconds()
if err != nil {
	metrics.RecordPrometheusRequest(0, proxyDuration)
	...
	return
}
defer resp.Body.Close()
metrics.RecordPrometheusRequest(resp.StatusCode, proxyDuration)
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Run tests**

Run: `go test ./internal/api/prometheus/ -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat(metrics): instrument Prometheus proxy upstream requests
```

---

### Task 8: Instrument recommendation engine

**Files:**
- Modify: `internal/recommendations/engine.go`

- [ ] **Step 1: Time checker runs and report result counts**

In the `tick` method, time each checker goroutine:

```go
go func(idx int) {
	tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	recs := e.checkers[idx].checker.Check(tickCtx)
	metrics.ObserveRecommendationCheck(
		e.checkers[idx].checker.Name(),
		time.Since(start).Seconds(),
	)
	ch <- result{idx, recs}
}(i)
```

After merging results and storing them (after the `e.mu.Unlock()`), update the severity counts:

```go
var critical, warning, info int
for _, r := range merged {
	switch r.Severity {
	case SeverityCritical:
		critical++
	case SeverityWarning:
		warning++
	case SeverityInfo:
		info++
	}
}
metrics.SetRecommendationCounts(critical, warning, info)
```

Add import: `"github.com/radiergummi/cetacean/internal/metrics"`

- [ ] **Step 2: Run tests**

Run: `go test ./internal/recommendations/ -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```
feat(metrics): instrument recommendation engine checker duration and counts
```

---

### Task 9: Final integration test and compose config

**Files:**
- Modify: compose config (user's deployment repo)

- [ ] **Step 1: Run full check suite**

Run: `make check`
Expected: All lint, format, and tests pass

- [ ] **Step 2: Smoke test the endpoint**

Run locally and verify:
```bash
curl -s http://localhost:9000/-/metrics | grep cetacean_
```

Expected: Lines for all 14 metric names defined in the spec.

- [ ] **Step 3: Update compose labels**

In the cetacean service deploy labels, add:
```yaml
prometheus.endpoint: /-/metrics
```

- [ ] **Step 4: Final commit**

```
feat(metrics): add self-metrics endpoint with HTTP, SSE, cache, proxy, and recommendation instrumentation
```

Or squash into a single release commit if preferred.
