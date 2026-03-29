# Recommendation Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unified recommendation engine with four checker domains (sizing, config hygiene, operational, cluster), a `/recommendations` page, and a dashboard summary card — replacing the standalone `internal/sizing` package.

**Architecture:** New `internal/recommendations` package with a `Checker` interface and `Engine` orchestrator. Each checker declares its own interval. The engine runs a 60s tick loop, executing checkers whose interval has elapsed. Types migrate from `internal/sizing`. Frontend gets a shared `useRecommendations` hook with module-level caching, a new page, and a dashboard summary card.

**Tech Stack:** Go (backend engine + checkers), React/TypeScript (frontend page + dashboard card), Prometheus PromQL (sizing + operational queries)

---

### Task 1: Create `internal/recommendations` Package — Types

**Files:**
- Create: `internal/recommendations/types.go`
- Create: `internal/recommendations/types_test.go`

Migrate and extend types from `internal/sizing/sizing.go`.

- [ ] **Step 1: Create types.go**

```go
package recommendations

import "time"

type Category string

const (
	// Sizing
	CategoryOverProvisioned  Category = "over-provisioned"
	CategoryApproachingLimit Category = "approaching-limit"
	CategoryAtLimit          Category = "at-limit"
	CategoryNoLimits         Category = "no-limits"
	CategoryNoReservations   Category = "no-reservations"

	// Config hygiene
	CategoryNoHealthcheck   Category = "no-healthcheck"
	CategoryNoRestartPolicy Category = "no-restart-policy"

	// Operational
	CategoryFlakyService    Category = "flaky-service"
	CategoryNodeDiskFull    Category = "node-disk-full"
	CategoryNodeMemPressure Category = "node-memory-pressure"

	// Cluster
	CategorySingleReplica      Category = "single-replica"
	CategoryManagerHasWorkloads Category = "manager-has-workloads"
	CategoryUnevenDistribution  Category = "uneven-distribution"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Scope string

const (
	ScopeService Scope = "service"
	ScopeNode    Scope = "node"
	ScopeCluster Scope = "cluster"
)

type Recommendation struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Scope      Scope    `json:"scope"`
	TargetID   string   `json:"targetId"`
	TargetName string   `json:"targetName"`
	Resource   string   `json:"resource"`
	Message    string   `json:"message"`
	Current    float64  `json:"current"`
	Configured float64  `json:"configured"`
	Suggested  *float64 `json:"suggested,omitempty"`
	FixAction  *string  `json:"fixAction,omitempty"`
}

type Summary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}

// ComputeSummary counts recommendations by severity.
func ComputeSummary(recs []Recommendation) Summary {
	var s Summary
	for _, r := range recs {
		switch r.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityWarning:
			s.Warning++
		case SeverityInfo:
			s.Info++
		}
	}
	return s
}
```

- [ ] **Step 2: Write test for ComputeSummary**

```go
package recommendations

import "testing"

func TestComputeSummary(t *testing.T) {
	recs := []Recommendation{
		{Severity: SeverityCritical},
		{Severity: SeverityCritical},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo},
	}
	s := ComputeSummary(recs)
	if s.Critical != 2 { t.Errorf("critical: got %d, want 2", s.Critical) }
	if s.Warning != 1 { t.Errorf("warning: got %d, want 1", s.Warning) }
	if s.Info != 1 { t.Errorf("info: got %d, want 1", s.Info) }
}

func TestComputeSummary_Empty(t *testing.T) {
	s := ComputeSummary(nil)
	if s.Critical != 0 || s.Warning != 0 || s.Info != 0 {
		t.Errorf("expected all zeros, got %+v", s)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/recommendations/ -v`

- [ ] **Step 4: Commit**

```bash
git add internal/recommendations/
git commit -m "feat(recommendations): add types and category constants"
```

---

### Task 2: Create Checker Interface and Engine

**Files:**
- Create: `internal/recommendations/engine.go`
- Create: `internal/recommendations/engine_test.go`

- [ ] **Step 1: Create engine.go**

```go
package recommendations

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Checker produces recommendations from a specific domain.
type Checker interface {
	Name() string
	Interval() time.Duration
	Check(ctx context.Context) []Recommendation
}

type checkerState struct {
	checker Checker
	last    []Recommendation
	lastRun time.Time
}

// Engine runs all registered checkers and merges results.
type Engine struct {
	checkers []checkerState
	mu       sync.RWMutex
	results  []Recommendation
}

// NewEngine creates an engine with the given checkers.
// Returns nil if no checkers are provided.
func NewEngine(checkers ...Checker) *Engine {
	if len(checkers) == 0 {
		return nil
	}
	states := make([]checkerState, len(checkers))
	for i, c := range checkers {
		states[i] = checkerState{checker: c}
	}
	return &Engine{checkers: states}
}

// Run starts the engine tick loop. Ticks every 60 seconds,
// running only checkers whose interval has elapsed.
func (e *Engine) Run(ctx context.Context) {
	if e == nil {
		return
	}
	// Run all checkers immediately on startup.
	e.tick(ctx, true)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx, false)
		}
	}
}

// Results returns the latest merged recommendations. Nil-safe.
func (e *Engine) Results() []Recommendation {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Recommendation, len(e.results))
	copy(out, e.results)
	return out
}

// Summary returns severity counts. Nil-safe.
func (e *Engine) Summary() Summary {
	return ComputeSummary(e.Results())
}

func (e *Engine) tick(ctx context.Context, force bool) {
	now := time.Now()
	type result struct {
		index int
		recs  []Recommendation
	}

	// Determine which checkers need to run.
	var toRun []int
	for i := range e.checkers {
		cs := &e.checkers[i]
		if force || now.Sub(cs.lastRun) >= cs.checker.Interval() {
			toRun = append(toRun, i)
		}
	}

	if len(toRun) == 0 {
		return
	}

	// Run eligible checkers in parallel.
	ch := make(chan result, len(toRun))
	for _, i := range toRun {
		go func(idx int) {
			tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			recs := e.checkers[idx].checker.Check(tickCtx)
			ch <- result{idx, recs}
		}(i)
	}

	for range toRun {
		r := <-ch
		e.checkers[r.index].last = r.recs
		e.checkers[r.index].lastRun = now
	}

	// Merge all cached results.
	var merged []Recommendation
	for _, cs := range e.checkers {
		merged = append(merged, cs.last...)
	}

	// Sort by severity (critical first).
	sortBySeverity(merged)

	e.mu.Lock()
	e.results = merged
	e.mu.Unlock()

	slog.Debug("recommendations: tick complete", "checkers_run", len(toRun), "total", len(merged))
}

func sortBySeverity(recs []Recommendation) {
	rank := map[Severity]int{SeverityCritical: 0, SeverityWarning: 1, SeverityInfo: 2}
	// Simple insertion sort — recommendation lists are small.
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && rank[recs[j].Severity] < rank[recs[j-1].Severity]; j-- {
			recs[j], recs[j-1] = recs[j-1], recs[j]
		}
	}
}
```

- [ ] **Step 2: Write engine tests**

Test with mock checkers: verify merge, nil-safety, per-checker intervals, severity sorting.

```go
package recommendations

import (
	"context"
	"testing"
	"time"
)

type mockChecker struct {
	name     string
	interval time.Duration
	recs     []Recommendation
}

func (m *mockChecker) Name() string                              { return m.name }
func (m *mockChecker) Interval() time.Duration                   { return m.interval }
func (m *mockChecker) Check(_ context.Context) []Recommendation  { return m.recs }

func TestEngine_NilSafe(t *testing.T) {
	var e *Engine
	if r := e.Results(); r != nil {
		t.Errorf("expected nil, got %v", r)
	}
	s := e.Summary()
	if s.Critical != 0 || s.Warning != 0 || s.Info != 0 {
		t.Errorf("expected zeros, got %+v", s)
	}
}

func TestEngine_MergesCheckers(t *testing.T) {
	e := NewEngine(
		&mockChecker{name: "a", interval: time.Minute, recs: []Recommendation{
			{Category: CategoryNoLimits, Severity: SeverityWarning, TargetName: "svc1"},
		}},
		&mockChecker{name: "b", interval: time.Minute, recs: []Recommendation{
			{Category: CategoryNodeDiskFull, Severity: SeverityCritical, TargetName: "node1"},
		}},
	)
	e.tick(context.Background(), true)
	results := e.Results()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Critical should sort first.
	if results[0].Severity != SeverityCritical {
		t.Errorf("expected critical first, got %s", results[0].Severity)
	}
}

func TestEngine_RespectsInterval(t *testing.T) {
	slow := &mockChecker{name: "slow", interval: 5 * time.Minute, recs: []Recommendation{
		{Severity: SeverityInfo},
	}}
	fast := &mockChecker{name: "fast", interval: time.Minute, recs: []Recommendation{
		{Severity: SeverityWarning},
	}}
	e := NewEngine(slow, fast)

	// Force run — both execute.
	e.tick(context.Background(), true)
	if len(e.Results()) != 2 {
		t.Fatalf("expected 2, got %d", len(e.Results()))
	}

	// Non-forced tick immediately after — neither should run (both just ran).
	fast.recs = nil // If fast runs again, it would return empty.
	e.tick(context.Background(), false)
	// Results should still be 2 (cached from previous run).
	if len(e.Results()) != 2 {
		t.Fatalf("expected 2 (cached), got %d", len(e.Results()))
	}
}

func TestNewEngine_NilForNoCheckers(t *testing.T) {
	e := NewEngine()
	if e != nil {
		t.Error("expected nil engine with no checkers")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/recommendations/ -v`

- [ ] **Step 4: Commit**

```bash
git add internal/recommendations/
git commit -m "feat(recommendations): add checker interface and engine"
```

---

### Task 3: Migrate Sizing Checker

**Files:**
- Create: `internal/recommendations/sizing_checker.go`
- Create: `internal/recommendations/sizing_evaluate.go` (moved from `internal/sizing/evaluate.go`)
- Create: `internal/recommendations/sizing_checker_test.go`
- Create: `internal/recommendations/sizing_evaluate_test.go` (moved from `internal/sizing/evaluate_test.go`)
- Delete: `internal/sizing/` (entire package)

- [ ] **Step 1: Move evaluate.go**

Copy `internal/sizing/evaluate.go` to `internal/recommendations/sizing_evaluate.go`. Change the package declaration to `package recommendations`. Update the import from `"github.com/radiergummi/cetacean/internal/config"` — keep it, the evaluate logic still uses `*config.SizingConfig`.

The `evaluate` function's return type changes: it currently returns `[]Recommendation` using the sizing package's type. After moving, it uses the recommendations package's `Recommendation` type directly. The function needs to populate the new fields:
- `Scope: ScopeService`
- `TargetID` and `TargetName` — the evaluate function doesn't have these. Pass them in via a new parameter or have the checker add them after calling evaluate. **Recommendation:** have evaluate return a simpler internal result, and the checker wraps each with scope/target/fixAction.

Actually, simplest approach: add `serviceName` and `serviceID` parameters to `evaluate` and have it populate all fields directly. But evaluate currently takes `serviceSpec` which already has `name`. Add `id` to `serviceSpec`:

```go
type serviceSpec struct {
	id                string
	name              string
	cpuLimit          int64
	cpuReservation    int64
	memoryLimit       int64
	memoryReservation int64
}
```

Then in evaluate, populate:
- `Scope: ScopeService`
- `TargetID: spec.id`
- `TargetName: spec.name`
- `FixAction`: for at-limit/approaching-limit: `ptr("PATCH /services/{id}/resources")`. For over-provisioned: `ptr("PATCH /services/{id}/resources")`. For no-limits/no-reservations: nil.

- [ ] **Step 2: Move evaluate_test.go**

Copy `internal/sizing/evaluate_test.go` to `internal/recommendations/sizing_evaluate_test.go`. Change package declaration. Update `specWithLimits()` to include `id: "test-id"`. Add assertions for new fields (`Scope`, `TargetID`, `FixAction`) in at least one test.

- [ ] **Step 3: Create sizing_checker.go**

This replaces `internal/sizing/monitor.go`. The `SizingChecker` implements `Checker`:

```go
package recommendations

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

const (
	serviceLabelKey = "container_label_com_docker_swarm_service_name"
	serviceFilter   = `container_label_com_docker_swarm_service_id!=""`

	cpuInstantQuery    = `sum by (` + serviceLabelKey + `)(rate(container_cpu_usage_seconds_total{` + serviceFilter + `}[5m])) * 100`
	memoryInstantQuery = `avg_over_time(sum by (` + serviceLabelKey + `)(container_memory_usage_bytes{` + serviceFilter + `})[1h:])`
)

// QueryFunc executes a Prometheus instant query.
type QueryFunc func(ctx context.Context, query string) ([]prom.Result, error)

// SizingChecker evaluates service resource sizing.
type SizingChecker struct {
	query QueryFunc
	cache *cache.Cache
	cfg   *config.SizingConfig
}

func NewSizingChecker(query QueryFunc, c *cache.Cache, cfg *config.SizingConfig) *SizingChecker {
	return &SizingChecker{query: query, cache: c, cfg: cfg}
}

func (sc *SizingChecker) Name() string             { return "sizing" }
func (sc *SizingChecker) Interval() time.Duration   { return 5 * time.Minute }

func (sc *SizingChecker) Check(ctx context.Context) []Recommendation {
	// Same logic as old monitor.tick — query Prometheus, iterate services, call evaluate.
	// ... (moved from monitor.go tick method)
}
```

The `Check` method contains the query logic from the old `monitor.tick()`: runs 4 parallel Prometheus queries (instant CPU/memory + p95 CPU/memory), iterates `cache.ListServices()`, calls `evaluate()` for each. No tick state, no mutex — the engine handles storage.

Move `extractSpec` from monitor.go, updating it to populate `spec.id` from `svc.ID`.

Move `queryByService` helper and `formatPromDuration` helper.

- [ ] **Step 4: Write sizing_checker_test.go**

Migrate `internal/sizing/monitor_test.go` tests. Update `mockQuery` for the new package. Verify that `Check` returns recommendations with correct `Scope`, `TargetID`, `TargetName`, `FixAction` fields.

- [ ] **Step 5: Delete `internal/sizing/`**

```bash
rm -rf internal/sizing/
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/recommendations/ -v`
Expected: all evaluate + checker tests pass.

Run: `go build ./internal/...`
Expected: `internal/sizing` gone, no lingering imports (will be fixed in Task 5).

- [ ] **Step 7: Commit**

```bash
git add internal/recommendations/ internal/sizing/
git commit -m "feat(recommendations): migrate sizing checker from internal/sizing"
```

---

### Task 4: Config Hygiene Checker

**Files:**
- Create: `internal/recommendations/config_checker.go`
- Create: `internal/recommendations/config_checker_test.go`

- [ ] **Step 1: Write failing tests**

```go
package recommendations

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestConfigChecker_NoHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{},
				// No Healthcheck
			},
		},
	})
	checker := NewConfigChecker(c)
	recs := checker.Check(context.Background())
	found := false
	for _, r := range recs {
		if r.Category == CategoryNoHealthcheck && r.TargetName == "web" {
			found = true
			if r.Scope != ScopeService { t.Errorf("expected service scope") }
			if r.FixAction != nil { t.Errorf("config hints should not be fixable") }
		}
	}
	if !found { t.Error("expected no-healthcheck recommendation") }
}

func TestConfigChecker_NoRestartPolicy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "worker"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "curl", "-f", "http://localhost/"},
					},
				},
				RestartPolicy: &swarm.RestartPolicy{
					Condition: swarm.RestartPolicyConditionNone,
				},
			},
		},
	})
	checker := NewConfigChecker(c)
	recs := checker.Check(context.Background())
	found := false
	for _, r := range recs {
		if r.Category == CategoryNoRestartPolicy { found = true }
	}
	if !found { t.Error("expected no-restart-policy recommendation") }
}

func TestConfigChecker_Healthy(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "healthy"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test: []string{"CMD", "true"},
					},
				},
				RestartPolicy: &swarm.RestartPolicy{
					Condition: swarm.RestartPolicyConditionOnFailure,
				},
			},
		},
	})
	checker := NewConfigChecker(c)
	recs := checker.Check(context.Background())
	if len(recs) != 0 {
		t.Errorf("expected no recommendations for healthy service, got %d", len(recs))
	}
}
```

Note: Check the actual Docker SDK type for `HealthConfig` — it may be `container.HealthConfig` from `github.com/docker/docker/api/types/container`. Read imports used elsewhere in the project's test files.

- [ ] **Step 2: Implement config_checker.go**

```go
package recommendations

import (
	"context"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

type ConfigChecker struct {
	cache *cache.Cache
}

func NewConfigChecker(c *cache.Cache) *ConfigChecker {
	return &ConfigChecker{cache: c}
}

func (cc *ConfigChecker) Name() string           { return "config" }
func (cc *ConfigChecker) Interval() time.Duration { return 60 * time.Second }

func (cc *ConfigChecker) Check(_ context.Context) []Recommendation {
	var recs []Recommendation
	for _, svc := range cc.cache.ListServices() {
		cs := svc.Spec.TaskTemplate.ContainerSpec
		// No health check
		if cs == nil || cs.Healthcheck == nil ||
			(len(cs.Healthcheck.Test) == 1 && cs.Healthcheck.Test[0] == "NONE") {
			recs = append(recs, Recommendation{
				Category:   CategoryNoHealthcheck,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has no health check configured",
			})
		}
		// No restart policy
		rp := svc.Spec.TaskTemplate.RestartPolicy
		if rp == nil || rp.Condition == "none" {
			recs = append(recs, Recommendation{
				Category:   CategoryNoRestartPolicy,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   svc.ID,
				TargetName: svc.Spec.Name,
				Message:    "Service has no restart policy configured",
			})
		}
	}
	return recs
}
```

- [ ] **Step 3: Run tests, commit**

Run: `go test ./internal/recommendations/ -run TestConfigChecker -v`

```bash
git add internal/recommendations/config_checker*
git commit -m "feat(recommendations): add config hygiene checker"
```

---

### Task 5: Operational Checker

**Files:**
- Create: `internal/recommendations/operational_checker.go`
- Create: `internal/recommendations/operational_checker_test.go`

- [ ] **Step 1: Write tests**

Tests for: flaky service (>5 restarts), node disk full (>90%), node memory pressure (>90%), healthy cases returning empty. Use mock `QueryFunc`.

- [ ] **Step 2: Implement operational_checker.go**

```go
type OperationalChecker struct {
	query    QueryFunc
	cache    *cache.Cache
	lookback time.Duration
}

func (oc *OperationalChecker) Name() string           { return "operational" }
func (oc *OperationalChecker) Interval() time.Duration { return 5 * time.Minute }
```

`Check` runs 3 Prometheus queries:
- Flaky services: `sum by (container_label_com_docker_swarm_service_name)(increase(container_last_seen{...}[{lookback}]))` — services with value > 5
- Node disk: `max by (instance)((1 - node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"} / node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"}) * 100)` — instances > 90%
- Node memory: `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100` — instances > 90%

For node checks, map instance labels to node hostnames using the cache (same pattern as `useNodeMetrics` on the frontend — match by hostname or IP).

Note: The exact PromQL for "flaky service" may need adjustment. An alternative: `changes(container_last_seen{...}[{lookback}])` or `count_over_time(...)`. The implementer should verify which metric best captures task restarts in a cAdvisor setup. The test mocks the query results directly, so the PromQL can be refined later.

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/recommendations/operational_checker*
git commit -m "feat(recommendations): add operational checker"
```

---

### Task 6: Cluster Checker

**Files:**
- Create: `internal/recommendations/cluster_checker.go`
- Create: `internal/recommendations/cluster_checker_test.go`

- [ ] **Step 1: Write tests**

Tests for: single-replica service, manager with active availability, uneven distribution (one node 3x tasks of another), healthy cases.

- [ ] **Step 2: Implement cluster_checker.go**

```go
type ClusterChecker struct {
	cache *cache.Cache
}

func (cc *ClusterChecker) Name() string           { return "cluster" }
func (cc *ClusterChecker) Interval() time.Duration { return 60 * time.Second }
```

`Check` does:
- Single replica: iterate services, check `Spec.Mode.Replicated.Replicas == 1`. FixAction: `ptr("PUT /services/{id}/scale")`, Suggested: `ptr(2)`.
- Manager workloads: iterate nodes, check role=manager + availability=active. FixAction: `ptr("PUT /nodes/{id}/availability")`.
- Uneven distribution: count running tasks per node from cache, compute max/min ratio, flag if > 3x.

- [ ] **Step 3: Run tests, commit**

```bash
git add internal/recommendations/cluster_checker*
git commit -m "feat(recommendations): add cluster checker"
```

---

### Task 7: Wire Up Engine and API Endpoint

**Files:**
- Modify: `internal/api/handlers.go` — replace `sizingMonitor *sizing.Monitor` with `engine *recommendations.Engine`, add `HandleRecommendations` handler
- Modify: `internal/api/router.go` — replace `GET /services/sizing` with `GET /recommendations`
- Modify: `main.go` — replace sizing monitor creation with engine creation
- Modify: `internal/config/sizing.go` — keep as-is for v1 (sizing config is reused by the engine; renaming to `[recommendations.sizing]` and adding `CETACEAN_RECOMMENDATIONS_ENABLED`/`INTERVAL` is deferred to avoid a config migration in this task)

- [ ] **Step 1: Update handlers.go**

Replace `sizingMonitor *sizing.Monitor` field with `recEngine *recommendations.Engine`.
Update `NewHandlers` parameter.
Remove `HandleServicesSizing`. Add:

```go
func (h *Handlers) HandleRecommendations(w http.ResponseWriter, r *http.Request) {
	results := h.recEngine.Results()
	summary := recommendations.ComputeSummary(results)
	writeJSONWithETag(w, r, NewDetailResponse("/recommendations", "RecommendationCollection", map[string]any{
		"items":      results,
		"total":      len(results),
		"summary":    summary,
		"computedAt": time.Now(),
	}))
}
```

Note: Uses `DetailResponse` (not `CollectionResponse`) because we need custom fields (`summary`, `computedAt`) that `CollectionResponse` doesn't support. `DetailResponse` has deterministic JSON key ordering via its custom `MarshalJSON` — `@context`, `@id`, `@type` first, then extras sorted alphabetically.

- [ ] **Step 2: Update router.go**

Remove: `mux.HandleFunc("GET /services/sizing", ...)`
Add: `mux.HandleFunc("GET /recommendations", contentNegotiated(h.HandleRecommendations, spa))`

- [ ] **Step 3: Update main.go**

Replace the sizing monitor block (lines 201-213) with engine creation:

```go
// Recommendations engine
var recEngine *recommendations.Engine
sizingCfg, err := config.LoadSizing(fc)
if err != nil {
	slog.Error("failed to load sizing config", "error", err)
	os.Exit(1)
}

var checkers []recommendations.Checker
// Always register cache-only checkers.
checkers = append(checkers,
	recommendations.NewConfigChecker(stateCache),
	recommendations.NewClusterChecker(stateCache),
)
// Register Prometheus-dependent checkers when available.
if promClient != nil {
	checkers = append(checkers,
		recommendations.NewSizingChecker(promClient.InstantQuery, stateCache, sizingCfg),
		recommendations.NewOperationalChecker(promClient.InstantQuery, stateCache, sizingCfg.Lookback),
	)
}
recEngine = recommendations.NewEngine(checkers...)
if recEngine != nil {
	go recEngine.Run(ctx)
	slog.Info("recommendation engine started", "checkers", len(checkers))
}
```

Update `NewHandlers` call — replace `sizingMonitor` with `recEngine`.

Remove import of `"github.com/radiergummi/cetacean/internal/sizing"`, add `"github.com/radiergummi/cetacean/internal/recommendations"`.

- [ ] **Step 4: Fix all test files calling NewHandlers**

Same 11 test files — update the `nil` parameter name/position if the parameter index changed.

- [ ] **Step 5: Build and test**

Run: `go build ./internal/...`
Run: `go test ./internal/...`

- [ ] **Step 6: Commit**

```bash
git add internal/api/ main.go
git commit -m "feat(api): add GET /recommendations endpoint and wire engine"
```

---

### Task 8: Frontend Types and `useRecommendations` Hook

**Files:**
- Modify: `frontend/src/api/types.ts` — replace sizing types with recommendation types
- Modify: `frontend/src/api/client.ts` — replace `serviceSizing` with `recommendations`
- Create: `frontend/src/hooks/useRecommendations.ts`
- Delete: `frontend/src/hooks/useSizingHints.ts`

- [ ] **Step 1: Update types.ts**

Remove `SizingCategory`, `SizingSeverity`, `SizingRecommendation`, `ServiceSizing`.

Add:

```typescript
export type RecommendationCategory =
  | "over-provisioned"
  | "approaching-limit"
  | "at-limit"
  | "no-limits"
  | "no-reservations"
  | "no-healthcheck"
  | "no-restart-policy"
  | "flaky-service"
  | "node-disk-full"
  | "node-memory-pressure"
  | "single-replica"
  | "manager-has-workloads"
  | "uneven-distribution";

export type RecommendationSeverity = "info" | "warning" | "critical";
export type RecommendationScope = "service" | "node" | "cluster";

export interface Recommendation {
  category: RecommendationCategory;
  severity: RecommendationSeverity;
  scope: RecommendationScope;
  targetId: string;
  targetName: string;
  resource: string;
  message: string;
  current: number;
  configured: number;
  suggested?: number;
  fixAction?: string;
}

export interface RecommendationSummary {
  critical: number;
  warning: number;
  info: number;
}

export interface RecommendationsResponse {
  items: Recommendation[];
  total: number;
  summary: RecommendationSummary;
  computedAt: string;
}
```

- [ ] **Step 2: Update client.ts**

Remove `serviceSizing` method.
Add:

```typescript
recommendations: () => fetchJSON<RecommendationsResponse>("/recommendations"),
```

Update imports.

- [ ] **Step 3: Create useRecommendations.ts**

Module-level cache with 60s TTL (same pattern as `useMonitoringStatus`):

```typescript
import { api } from "@/api/client";
import type { Recommendation, RecommendationSummary, RecommendationsResponse } from "@/api/types";
import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";

interface RecommendationsState {
  items: Recommendation[];
  summary: RecommendationSummary;
  total: number;
  hasData: boolean;
}

const emptyState: RecommendationsState = {
  items: [],
  summary: { critical: 0, warning: 0, info: 0 },
  total: 0,
  hasData: false,
};

// Module-level cache shared across all hook consumers.
let cached: RecommendationsState = emptyState;
let cacheTime = 0;
let inflight: Promise<RecommendationsResponse> | null = null;
const cacheTTL = 60_000; // 60 seconds

async function fetchCached(): Promise<RecommendationsState> {
  const now = Date.now();

  if (cached.hasData && now - cacheTime < cacheTTL) {
    return cached;
  }

  if (!inflight) {
    inflight = api.recommendations().finally(() => { inflight = null; });
  }

  const response = await inflight;

  cached = {
    items: response.items ?? [],
    summary: response.summary,
    total: response.total,
    hasData: true,
  };
  cacheTime = Date.now();

  return cached;
}

export function useRecommendations(): RecommendationsState {
  const { pathname } = useLocation();
  const [state, setState] = useState<RecommendationsState>(
    cached.hasData ? cached : emptyState,
  );

  useEffect(() => {
    let cancelled = false;

    fetchCached()
      .then((result) => {
        if (!cancelled) {
          setState(result);
        }
      })
      .catch(() => {
        // Non-critical — fail silently
      });

    return () => {
      cancelled = true;
    };
  }, [pathname]);

  return state;
}
```

- [ ] **Step 4: Delete useSizingHints.ts**

```bash
rm frontend/src/hooks/useSizingHints.ts
```

- [ ] **Step 5: TypeScript check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: errors in SizingBadge, SizingBanner, ServiceList, ServiceDetail — these will be fixed in Tasks 9-10.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/ frontend/src/hooks/
git commit -m "feat(frontend): add recommendation types and useRecommendations hook"
```

---

### Task 9: Rewire Service List and Service Detail

**Files:**
- Modify: `frontend/src/components/SizingBadge.tsx`
- Modify: `frontend/src/components/SizingBanner.tsx`
- Modify: `frontend/src/lib/sizingUtils.ts`
- Modify: `frontend/src/pages/ServiceList.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Update sizingUtils.ts**

Update imports to use `Recommendation` instead of `SizingRecommendation`. The util functions work on the `category` and `severity` fields which haven't changed shape — but the type names are different. Update all type references.

Add new category icons for the new categories:
```typescript
function hintIcon(category: Recommendation["category"]): LucideIcon {
  // existing sizing icons...
  if (category === "no-healthcheck" || category === "no-restart-policy") return TriangleAlert;
  if (category === "flaky-service") return RefreshCw;
  if (category === "node-disk-full" || category === "node-memory-pressure") return AlertTriangle;
  if (category === "single-replica") return Copy;
  if (category === "manager-has-workloads") return Shield;
  if (category === "uneven-distribution") return Scale;
  // ... fallback
}
```

- [ ] **Step 2: Update SizingBadge.tsx**

Change import from `SizingRecommendation` to `Recommendation`. Props: `{ hints: Recommendation[] }`.

- [ ] **Step 3: Update SizingBanner.tsx**

Change import from `SizingRecommendation` to `Recommendation`. Update `buildPatch` to handle new fixAction types:
- If `fixAction` starts with `"PATCH /services/"` → use `api.patchServiceResources`
- If `fixAction` starts with `"PUT /services/"` and contains `/scale` → use `api.put` with `{ replicas: suggested }`
- If `fixAction` starts with `"PUT /nodes/"` and contains `/availability` → use `api.put` with `{ availability: "drain" }`

Rename from SizingBanner to RecommendationBanner (or keep the name — implementer's choice based on how many other import sites change).

- [ ] **Step 4: Update ServiceList.tsx**

Replace:
```typescript
import { useSizingHints } from "../hooks/useSizingHints";
```
With:
```typescript
import { useRecommendations } from "../hooks/useRecommendations";
```

Replace `const sizing = useSizingHints();` with:
```typescript
const { items: recommendations, hasData: hasRecommendations } = useRecommendations();
```

Update the sizing column to filter recommendations:
```typescript
const sizingColumns = hasRecommendations
  ? [{
      header: "Sizing",
      cell: ({ ID }) => {
        const hints = recommendations.filter(
          (r) => r.targetId === ID && sizingCategories.has(r.category),
        );
        return <SizingBadge hints={hints} />;
      },
    }]
  : [];
```

Define `sizingCategories` as a Set of the sizing category values.

- [ ] **Step 5: Update ServiceDetail.tsx**

Replace `useSizingHints` import with `useRecommendations`. Filter recommendations by `targetId === id`:

```typescript
const { items: recommendations } = useRecommendations();
const serviceRecommendations = recommendations.filter((r) => r.targetId === id);
```

Pass `serviceRecommendations` to the banner instead of `sizingHints`.

- [ ] **Step 6: TypeScript check**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 7: Commit**

```bash
git add frontend/src/
git commit -m "feat(frontend): rewire service list and detail to use recommendations"
```

---

### Task 10: Recommendations Page

**Files:**
- Create: `frontend/src/pages/RecommendationsPage.tsx`
- Modify: `frontend/src/App.tsx` — add route and nav link

- [ ] **Step 1: Create RecommendationsPage.tsx**

Page with:
- `useRecommendations()` hook
- PageHeader with title "Recommendations" and severity summary subtitle
- Filter tabs (All / Sizing / Config / Operational / Cluster) — URL-persisted via `useSearchParam("filter")`
- Filtered list rendered as cards, each with: severity dot, category icon, target link, message, "Apply suggested value" button where `fixAction` is present
- Empty state when no recommendations
- Local dismiss on fix (same pattern as banner — track dismissed indices in state)

Follow existing page patterns (see `ServiceList.tsx` for filter/URL state, `SearchPage.tsx` for grouped results).

- [ ] **Step 2: Add route and nav link in App.tsx**

Add to NavLinks array (after topology, before metrics):
```typescript
{ to: "/recommendations", label: "Recommendations", keys: ["g", "r"] },
```

Add route:
```typescript
<Route path="/recommendations" element={<RecommendationsPage />} />
```

- [ ] **Step 3: TypeScript check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/RecommendationsPage.tsx frontend/src/App.tsx
git commit -m "feat(frontend): add /recommendations page"
```

---

### Task 11: Dashboard Summary Card

**Files:**
- Create: `frontend/src/components/RecommendationSummary.tsx`
- Modify: `frontend/src/pages/ClusterOverview.tsx` — add summary card

- [ ] **Step 1: Create RecommendationSummary.tsx**

```typescript
import { useRecommendations } from "@/hooks/useRecommendations";
import { Link } from "react-router-dom";
```

Component:
- Reads `summary` and `total` from `useRecommendations()`
- Returns null when `total === 0` or `!hasData`
- Colored border: red if critical > 0, amber if warning > 0, blue otherwise
- "Recommendations" title, "View all →" link to `/recommendations`
- Severity counts inline, zero counts omitted

- [ ] **Step 2: Add to ClusterOverview.tsx**

Import and render `<RecommendationSummary />` after `<CapacitySection>`.

- [ ] **Step 3: TypeScript check**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/RecommendationSummary.tsx frontend/src/pages/ClusterOverview.tsx
git commit -m "feat(frontend): add recommendation summary card to dashboard"
```

---

### Task 12: Update Documentation and Final Verification

**Files:**
- Modify: `CLAUDE.md` — update architecture section, env vars, endpoint list
- Modify: `docs/configuration.md` — rename sizing section to recommendations
- Modify: `CHANGELOG.md` — add entry

- [ ] **Step 1: Update CLAUDE.md**

- Replace `sizing/` description with `recommendations/` description
- Replace `GET /services/sizing` with `GET /recommendations`
- Update env var table: rename `CETACEAN_SIZING_*` or add `CETACEAN_RECOMMENDATIONS_*` as applicable

- [ ] **Step 2: Update docs/configuration.md**

- Rename "Resource Sizing Hints" section to "Recommendations"
- Add new config keys if any

- [ ] **Step 3: Update CHANGELOG.md**

Add user-facing entry under `[Unreleased]`.

- [ ] **Step 4: Full verification**

Run: `go test ./internal/...`
Run: `cd frontend && npx tsc -b --noEmit && npm run lint`
Run: `cd frontend && npm run build && cd .. && go build -o cetacean .` (if frontend dist exists)

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/configuration.md CHANGELOG.md
git commit -m "docs: update for recommendation feed"
```
