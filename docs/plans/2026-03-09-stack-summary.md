# Stack Summary Endpoint & StackList Redesign

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `GET /api/stacks/summary` endpoint that returns per-stack health, resource limits, and live resource usage (from Prometheus), then redesign the StackList frontend page to show cluster health at a glance.

**Architecture:** New `StackSummary` struct aggregated from cached stacks/services/tasks. A small Prometheus query helper issues two instant queries (memory + CPU usage grouped by stack namespace), joins results. Frontend replaces table/card view with health-oriented cards showing task states, resource bars, and deploy status.

**Tech Stack:** Go (stdlib net/http, existing cache), Prometheus HTTP API, React + TypeScript + Tailwind CSS

---

### Task 1: Add `StackSummary` type and `ListStackSummaries` to cache

**Files:**
- Modify: `internal/cache/cache.go` (add `StackSummary` type near `Stack`/`StackDetail`)
- Modify: `internal/cache/cache.go` (add `ListStackSummaries` method)
- Test: `internal/cache/cache_test.go`

**Step 1: Write the failing test**

Add to `internal/cache/cache_test.go`:

```go
func TestCache_ListStackSummaries(t *testing.T) {
	c := New(nil)

	// Service with 2 replicas, memory limit 512MB, CPU limit 0.5 cores
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
				Resources: &swarm.ResourceRequirements{
					Limits: &swarm.Limit{
						MemoryBytes: 512 * 1024 * 1024,
						NanoCPUs:    500_000_000,
					},
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(2)},
			},
		},
	})

	// Service mid-update
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_api",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "api:latest"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(1)},
			},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
	})

	// Tasks: 2 running for svc1, 1 failed for svc2
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})

	// Config and network in same stack
	c.SetConfig(swarm.Config{ID: "cfg1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{
		Name: "myapp_config", Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
	}}})
	c.SetNetwork(network.Summary{ID: "net1", Name: "myapp_default", Labels: map[string]string{"com.docker.stack.namespace": "myapp"}})

	summaries := c.ListStackSummaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 stack summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.Name != "myapp" {
		t.Errorf("name=%q, want myapp", s.Name)
	}
	if s.ServiceCount != 2 {
		t.Errorf("serviceCount=%d, want 2", s.ServiceCount)
	}
	if s.DesiredTasks != 3 {
		t.Errorf("desiredTasks=%d, want 3", s.DesiredTasks)
	}
	if s.TasksByState["running"] != 2 {
		t.Errorf("running=%d, want 2", s.TasksByState["running"])
	}
	if s.TasksByState["failed"] != 1 {
		t.Errorf("failed=%d, want 1", s.TasksByState["failed"])
	}
	if s.UpdatingServices != 1 {
		t.Errorf("updatingServices=%d, want 1", s.UpdatingServices)
	}
	if s.MemoryLimitBytes != 2*512*1024*1024 {
		t.Errorf("memoryLimitBytes=%d, want %d", s.MemoryLimitBytes, 2*512*1024*1024)
	}
	if s.CPULimitCores != 1.0 {
		t.Errorf("cpuLimitCores=%f, want 1.0", s.CPULimitCores)
	}
	if s.ConfigCount != 1 {
		t.Errorf("configCount=%d, want 1", s.ConfigCount)
	}
	if s.NetworkCount != 1 {
		t.Errorf("networkCount=%d, want 1", s.NetworkCount)
	}
}

func uint64Ptr(v uint64) *uint64 { return &v }
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -run TestCache_ListStackSummaries -v`
Expected: FAIL — `ListStackSummaries` and `StackSummary` don't exist yet.

**Step 3: Write minimal implementation**

Add to `internal/cache/cache.go` near the `StackDetail` type:

```go
type StackSummary struct {
	Name             string         `json:"name"`
	ServiceCount     int            `json:"serviceCount"`
	ConfigCount      int            `json:"configCount"`
	SecretCount      int            `json:"secretCount"`
	NetworkCount     int            `json:"networkCount"`
	VolumeCount      int            `json:"volumeCount"`
	DesiredTasks     int            `json:"desiredTasks"`
	TasksByState     map[string]int `json:"tasksByState"`
	UpdatingServices int            `json:"updatingServices"`
	MemoryLimitBytes int64          `json:"memoryLimitBytes"`
	CPULimitCores    float64        `json:"cpuLimitCores"`
	MemoryUsageBytes int64          `json:"memoryUsageBytes"`
	CPUUsagePercent  float64        `json:"cpuUsagePercent"`
}
```

Add `ListStackSummaries` method:

```go
func (c *Cache) ListStackSummaries() []StackSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]StackSummary, 0, len(c.stacks))
	for _, stack := range c.stacks {
		s := StackSummary{
			Name:         stack.Name,
			ServiceCount: len(stack.Services),
			ConfigCount:  len(stack.Configs),
			SecretCount:  len(stack.Secrets),
			NetworkCount: len(stack.Networks),
			VolumeCount:  len(stack.Volumes),
			TasksByState: make(map[string]int),
		}

		for _, svcID := range stack.Services {
			svc, ok := c.services[svcID]
			if !ok {
				continue
			}

			// Desired replicas
			if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				s.DesiredTasks += int(*svc.Spec.Mode.Replicated.Replicas)
			} else if svc.Spec.Mode.Global != nil {
				s.DesiredTasks += len(c.nodes)
			}

			// Update status
			if svc.UpdateStatus != nil && svc.UpdateStatus.State == swarm.UpdateStateUpdating {
				s.UpdatingServices++
			}

			// Resource limits (multiplied by replica count)
			replicas := 1
			if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				replicas = int(*svc.Spec.Mode.Replicated.Replicas)
			} else if svc.Spec.Mode.Global != nil {
				replicas = len(c.nodes)
			}
			if res := svc.Spec.TaskTemplate.Resources; res != nil && res.Limits != nil {
				s.MemoryLimitBytes += int64(replicas) * res.Limits.MemoryBytes
				s.CPULimitCores += float64(replicas) * float64(res.Limits.NanoCPUs) / 1e9
			}

			// Task states
			for taskID := range c.tasksByService[svcID] {
				if t, ok := c.tasks[taskID]; ok {
					s.TasksByState[string(t.Status.State)]++
				}
			}
		}

		out = append(out, s)
	}
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/ -run TestCache_ListStackSummaries -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "feat: add StackSummary type and ListStackSummaries to cache"
```

---

### Task 2: Add Prometheus query helper

**Files:**
- Create: `internal/api/promquery.go`
- Test: `internal/api/promquery_test.go`

**Step 1: Write the failing test**

Create `internal/api/promquery_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromClient_InstantQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"container_label_com_docker_stack_namespace": "myapp"}, "value": [1234567890, "1073741824"]},
					{"metric": {"container_label_com_docker_stack_namespace": "monitoring"}, "value": [1234567890, "536870912"]}
				]
			}
		}`))
	}))
	defer prom.Close()

	pc := NewPromClient(prom.URL)
	results, err := pc.InstantQuery(context.Background(), `sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes)`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Labels["container_label_com_docker_stack_namespace"] != "myapp" {
		t.Errorf("unexpected label: %v", results[0].Labels)
	}
	if results[0].Value != 1073741824 {
		t.Errorf("unexpected value: %f", results[0].Value)
	}
}

func TestPromClient_InstantQuery_Unreachable(t *testing.T) {
	pc := NewPromClient("http://127.0.0.1:1")
	_, err := pc.InstantQuery(context.Background(), "up")
	if err == nil {
		t.Fatal("expected error for unreachable prometheus")
	}
}

func TestPromClient_InstantQuery_ErrorResponse(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "error", "errorType": "bad_data", "error": "invalid query"}`))
	}))
	defer prom.Close()

	pc := NewPromClient(prom.URL)
	_, err := pc.InstantQuery(context.Background(), "bad{")
	if err == nil {
		t.Fatal("expected error for error response")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestPromClient -v`
Expected: FAIL — `PromClient`, `NewPromClient`, `InstantQuery` don't exist.

**Step 3: Write minimal implementation**

Create `internal/api/promquery.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

type PromClient struct {
	baseURL string
	client  *http.Client
}

func NewPromClient(baseURL string) *PromClient {
	return &PromClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type PromResult struct {
	Labels map[string]string
	Value  float64
}

func (pc *PromClient) InstantQuery(ctx context.Context, query string) ([]PromResult, error) {
	u := pc.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus query failed: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Status    string `json:"status"`
		Error     string `json:"error"`
		ErrorType string `json:"errorType"`
		Data      struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("prometheus response parse error: %w", err)
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s: %s", body.ErrorType, body.Error)
	}

	results := make([]PromResult, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		var valStr string
		if err := json.Unmarshal(r.Value[1], &valStr); err != nil {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		results = append(results, PromResult{
			Labels: r.Metric,
			Value:  val,
		})
	}
	return results, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestPromClient -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/api/promquery.go internal/api/promquery_test.go
git commit -m "feat: add Prometheus instant query client"
```

---

### Task 3: Add `HandleStackSummary` handler

**Files:**
- Modify: `internal/api/handlers.go` (add `HandleStackSummary`, add `promClient` field to `Handlers`)
- Modify: `internal/api/router.go` (register route)
- Modify: `main.go` (pass `PromClient` to `NewHandlers`)
- Test: `internal/api/handlers_test.go`

**Step 1: Write the failing test**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleStackSummary(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(2)},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	// Fake Prometheus that returns memory usage
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, "memory") {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"104857600"]}]}}`))
		} else {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"45.2"]}]}}`))
		}
	}))
	defer prom.Close()

	h := NewHandlers(c, nil, closedReady(), nil, NewPromClient(prom.URL))
	req := httptest.NewRequest("GET", "/api/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []cache.StackSummary
	json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Name != "myapp" {
		t.Errorf("name=%q, want myapp", summaries[0].Name)
	}
	if summaries[0].TasksByState["running"] != 2 {
		t.Errorf("running=%d, want 2", summaries[0].TasksByState["running"])
	}
	if summaries[0].MemoryUsageBytes != 104857600 {
		t.Errorf("memoryUsageBytes=%d, want 104857600", summaries[0].MemoryUsageBytes)
	}
}

func TestHandleStackSummary_PrometheusDown(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(1)},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	// Unreachable Prometheus
	h := NewHandlers(c, nil, closedReady(), nil, NewPromClient("http://127.0.0.1:1"))
	req := httptest.NewRequest("GET", "/api/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	// Should still return 200 with zero usage values
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []cache.StackSummary
	json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].MemoryUsageBytes != 0 {
		t.Errorf("expected 0 memory usage when prometheus is down, got %d", summaries[0].MemoryUsageBytes)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHandleStackSummary -v`
Expected: FAIL — `NewHandlers` signature doesn't accept `PromClient`, `HandleStackSummary` doesn't exist.

**Step 3: Write minimal implementation**

Modify `internal/api/handlers.go`:

Add `promClient` field to `Handlers`:
```go
type Handlers struct {
	cache        *cache.Cache
	dockerClient DockerLogStreamer
	ready        <-chan struct{}
	notifier     *notify.Notifier
	promClient   *PromClient
}
```

Update `NewHandlers`:
```go
func NewHandlers(c *cache.Cache, dc DockerLogStreamer, ready <-chan struct{}, notifier *notify.Notifier, promClient *PromClient) *Handlers {
	return &Handlers{cache: c, dockerClient: dc, ready: ready, notifier: notifier, promClient: promClient}
}
```

Add the handler:
```go
const stackNamespaceLabel = "container_label_com_docker_stack_namespace"

func (h *Handlers) HandleStackSummary(w http.ResponseWriter, r *http.Request) {
	summaries := h.cache.ListStackSummaries()

	if h.promClient != nil && len(summaries) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		memByStack := h.queryStackMetric(ctx,
			`sum by (`+stackNamespaceLabel+`)(container_memory_usage_bytes)`)
		cpuByStack := h.queryStackMetric(ctx,
			`sum by (`+stackNamespaceLabel+`)(rate(container_cpu_usage_seconds_total[5m])) * 100`)

		for i := range summaries {
			summaries[i].MemoryUsageBytes = int64(memByStack[summaries[i].Name])
			summaries[i].CPUUsagePercent = cpuByStack[summaries[i].Name]
		}
	}

	if summaries == nil {
		summaries = []cache.StackSummary{}
	}
	writeJSON(w, summaries)
}

func (h *Handlers) queryStackMetric(ctx context.Context, query string) map[string]float64 {
	results, err := h.promClient.InstantQuery(ctx, query)
	if err != nil {
		slog.Warn("prometheus stack metric query failed", "error", err)
		return nil
	}
	out := make(map[string]float64, len(results))
	for _, r := range results {
		if name := r.Labels[stackNamespaceLabel]; name != "" {
			out[name] = r.Value
		}
	}
	return out
}
```

Add `"log/slog"` to the imports of `handlers.go` if not already present.

Update `main.go` line ~99:
```go
handlers := api.NewHandlers(stateCache, dockerClient, watcher.Ready(), notifier, api.NewPromClient(cfg.PrometheusURL))
```

Add route in `internal/api/router.go` in the Stacks section:
```go
mux.HandleFunc("GET /api/stacks/summary", h.HandleStackSummary)
```

Update ALL existing `NewHandlers` calls in test files to pass `nil` as the last argument (the `promClient`). Search for `NewHandlers(` in test files and append `, nil` to each call.

**Step 4: Run all tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS (all tests including new ones)

**Step 5: Commit**

```
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go main.go
git commit -m "feat: add GET /api/stacks/summary endpoint with Prometheus metrics"
```

---

### Task 4: Add `StackSummary` type and `stacksSummary` API call to frontend

**Files:**
- Modify: `frontend/src/api/types.ts` (add `StackSummary` type)
- Modify: `frontend/src/api/client.ts` (add `stacksSummary` call)

**Step 1: Add the TypeScript type**

Add to `frontend/src/api/types.ts`:

```typescript
export interface StackSummary {
  name: string;
  serviceCount: number;
  configCount: number;
  secretCount: number;
  networkCount: number;
  volumeCount: number;
  desiredTasks: number;
  tasksByState: Record<string, number>;
  updatingServices: number;
  memoryLimitBytes: number;
  cpuLimitCores: number;
  memoryUsageBytes: number;
  cpuUsagePercent: number;
}
```

**Step 2: Add the API call**

Add to `frontend/src/api/client.ts` in the `api` object:

```typescript
stacksSummary: () => fetchJSON<StackSummary[]>("/stacks/summary"),
```

Add `StackSummary` to the import from `./types`.

**Step 3: Verify frontend builds**

Run: `cd frontend && npm run build`
Expected: Build succeeds.

**Step 4: Commit**

```
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add StackSummary type and API call to frontend"
```

---

### Task 5: Redesign StackList page

**Files:**
- Modify: `frontend/src/pages/StackList.tsx` (full rewrite)
- Modify: `frontend/src/pages/StackList.test.tsx` (update tests for new API)

**Step 1: Rewrite `StackList.tsx`**

Replace the entire file. The new page:
- Fetches from `api.stacksSummary()` instead of `api.stacks()`
- Renders a responsive card grid (no table view)
- Each card shows: stack name, health dot (green/yellow/red), task bar, resource usage bars, update badge, resource counts
- Search still works via client-side filtering (the summary endpoint returns all stacks)
- Subscribes to SSE for live updates (refetch on stack/service/task events)

Key visual elements per card:
- **Health dot**: green if `running >= desiredTasks`, yellow if `tasksByState` has pending/starting/preparing, red if any failed
- **Task bar**: horizontal stacked bar proportional to task counts by state
- **Memory bar**: `memoryUsageBytes / memoryLimitBytes` as a progress bar with percentage. Omitted if `memoryLimitBytes == 0`
- **CPU bar**: `cpuUsagePercent / (cpuLimitCores * 100)` as a progress bar. Omitted if `cpuLimitCores == 0`
- **Update badge**: small "Updating N" pill shown if `updatingServices > 0`

Implementation note: This page no longer uses `useSwarmResource` (which expects `PagedResponse<T>`). Instead, use a direct `useEffect` + `useState` pattern with SSE subscription for refetch, similar to `ClusterOverview.tsx`.

```tsx
import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { StackSummary } from "../api/types";
import { useSSE } from "../hooks/useSSE";
import { useSearchParam } from "../hooks/useSearchParam";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { formatBytes } from "../lib/formatBytes";

export default function StackList() {
  const [search, setSearch] = useSearchParam("q");
  const [summaries, setSummaries] = useState<StackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const load = useCallback(() => {
    api
      .stacksSummary()
      .then((data) => {
        setSummaries(data);
        setError(null);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  useSSE(
    ["stack", "service", "task"],
    useCallback(() => { load(); }, [load]),
  );

  const filtered = search
    ? summaries.filter((s) =>
        s.name.toLowerCase().includes(search.toLowerCase()),
      )
    : summaries;

  if (loading)
    return (
      <div>
        <PageHeader title="Stacks" />
        <SkeletonTable columns={6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={load} />;

  return (
    <div>
      <PageHeader title="Stacks" />
      <div className="mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search stacks..." />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((stack) => (
            <StackCard key={stack.name} stack={stack} />
          ))}
        </div>
      )}
    </div>
  );
}

function stackHealth(s: StackSummary): "healthy" | "warning" | "critical" {
  if ((s.tasksByState["failed"] ?? 0) > 0) return "critical";
  const running = s.tasksByState["running"] ?? 0;
  if (running < s.desiredTasks) return "warning";
  return "healthy";
}

const HEALTH_COLORS = {
  healthy: "bg-green-500",
  warning: "bg-yellow-500",
  critical: "bg-red-500",
} as const;

const HEALTH_BORDER = {
  healthy: "",
  warning: "",
  critical: "border-red-200 dark:border-red-900 bg-red-50/50 dark:bg-red-950/20",
} as const;

function StackCard({ stack }: { stack: StackSummary }) {
  const health = stackHealth(stack);
  const running = stack.tasksByState["running"] ?? 0;
  const failed = stack.tasksByState["failed"] ?? 0;
  const other = Object.entries(stack.tasksByState)
    .filter(([k]) => k !== "running" && k !== "failed")
    .reduce((sum, [, v]) => sum + v, 0);
  const totalTasks = running + failed + other;

  return (
    <Link
      to={`/stacks/${stack.name}`}
      className={`block rounded-lg border p-4 hover:border-foreground/20 hover:shadow-sm transition-all ${HEALTH_BORDER[health]}`}
    >
      {/* Header */}
      <div className="flex items-center gap-2 mb-3">
        <span className={`shrink-0 w-2.5 h-2.5 rounded-full ${HEALTH_COLORS[health]}`} />
        <span className="font-medium truncate">{stack.name}</span>
        {stack.updatingServices > 0 && (
          <span className="ml-auto text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
            Updating {stack.updatingServices}
          </span>
        )}
      </div>

      {/* Task bar */}
      <div className="mb-3">
        <div className="flex justify-between text-xs text-muted-foreground mb-1">
          <span>Tasks</span>
          <span className="tabular-nums">{running}/{stack.desiredTasks}</span>
        </div>
        {totalTasks > 0 ? (
          <div className="h-2 rounded-full bg-muted overflow-hidden flex">
            {running > 0 && (
              <div
                className="bg-green-500 transition-all"
                style={{ width: `${(running / totalTasks) * 100}%` }}
              />
            )}
            {other > 0 && (
              <div
                className="bg-yellow-500 transition-all"
                style={{ width: `${(other / totalTasks) * 100}%` }}
              />
            )}
            {failed > 0 && (
              <div
                className="bg-red-500 transition-all"
                style={{ width: `${(failed / totalTasks) * 100}%` }}
              />
            )}
          </div>
        ) : (
          <div className="h-2 rounded-full bg-muted" />
        )}
      </div>

      {/* Resource bars */}
      {stack.memoryLimitBytes > 0 && (
        <ResourceBar
          label="Memory"
          used={stack.memoryUsageBytes}
          limit={stack.memoryLimitBytes}
          format={formatBytes}
        />
      )}
      {stack.cpuLimitCores > 0 && (
        <ResourceBar
          label="CPU"
          used={stack.cpuUsagePercent}
          limit={stack.cpuLimitCores * 100}
          format={(v) => `${v.toFixed(0)}%`}
        />
      )}

      {/* Resource counts footer */}
      <div className="flex gap-3 mt-3 pt-3 border-t text-[10px] text-muted-foreground">
        <span>{stack.serviceCount} svc</span>
        {stack.configCount > 0 && <span>{stack.configCount} cfg</span>}
        {stack.secretCount > 0 && <span>{stack.secretCount} sec</span>}
        {stack.networkCount > 0 && <span>{stack.networkCount} net</span>}
        {stack.volumeCount > 0 && <span>{stack.volumeCount} vol</span>}
      </div>
    </Link>
  );
}

function ResourceBar({
  label,
  used,
  limit,
  format,
}: {
  label: string;
  used: number;
  limit: number;
  format: (v: number) => string;
}) {
  const pct = limit > 0 ? Math.min((used / limit) * 100, 100) : 0;
  const color = pct > 90 ? "bg-red-500" : pct > 70 ? "bg-yellow-500" : "bg-blue-500";

  return (
    <div className="mb-2">
      <div className="flex justify-between text-xs text-muted-foreground mb-0.5">
        <span>{label}</span>
        <span className="tabular-nums">{format(used)} / {format(limit)}</span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div className={`h-full rounded-full ${color} transition-all`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}
```

**Step 2: Update tests**

Rewrite `frontend/src/pages/StackList.test.tsx` to mock `api.stacksSummary` instead of `api.stacks`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import StackList from "./StackList";
import type { StackSummary } from "../api/types";

class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;
  constructor(_url: string) {
    MockEventSource.instance = this;
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }
  close() {
    this.closed = true;
  }
}

vi.mock("../api/client", () => ({
  api: {
    stacksSummary: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockSummary = vi.mocked(api.stacksSummary);

const fakeSummary = (name: string, overrides?: Partial<StackSummary>): StackSummary => ({
  name,
  serviceCount: 2,
  configCount: 1,
  secretCount: 0,
  networkCount: 1,
  volumeCount: 0,
  desiredTasks: 3,
  tasksByState: { running: 3 },
  updatingServices: 0,
  memoryLimitBytes: 1024 * 1024 * 1024,
  memoryUsageBytes: 512 * 1024 * 1024,
  cpuLimitCores: 2,
  cpuUsagePercent: 45,
  ...overrides,
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockSummary.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <SSEProvider>{children}</SSEProvider>
    </MemoryRouter>
  );
}

describe("StackList", () => {
  it("renders stack summaries", async () => {
    mockSummary.mockResolvedValue([fakeSummary("monitoring"), fakeSummary("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("filters by search", async () => {
    mockSummary.mockResolvedValue([fakeSummary("monitoring"), fakeSummary("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search stacks..."), {
      target: { value: "app" },
    });

    await waitFor(() => {
      expect(screen.queryByText("monitoring")).not.toBeInTheDocument();
    });
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockSummary.mockResolvedValue([]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No stacks found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockSummary.mockRejectedValue(new Error("Failed"));
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Failed")).toBeInTheDocument();
    });
  });

  it("shows update badge when services are updating", async () => {
    mockSummary.mockResolvedValue([fakeSummary("myapp", { updatingServices: 2 })]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Updating 2")).toBeInTheDocument();
    });
  });
});
```

**Step 3: Verify frontend builds and tests pass**

Run: `cd frontend && npm run build && npm test`
Expected: Build succeeds, all tests pass.

**Step 4: Commit**

```
git add frontend/src/pages/StackList.tsx frontend/src/pages/StackList.test.tsx
git commit -m "feat: redesign StackList with health cards, resource bars, and deploy status"
```

---

### Task 6: Verify end-to-end

**Step 1: Run all backend tests**

Run: `go test ./...`
Expected: All pass.

**Step 2: Run all frontend tests**

Run: `cd frontend && npm test`
Expected: All pass.

**Step 3: Run linting**

Run: `make check`
Expected: Clean.

**Step 4: Final commit (if any fixes needed)**

Fix any lint/format issues, commit.

Plan complete and saved to `docs/plans/2026-03-09-stack-summary.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open a new session with executing-plans, batch execution with checkpoints

Which approach?