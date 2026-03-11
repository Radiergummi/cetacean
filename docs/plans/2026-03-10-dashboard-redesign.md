# Dashboard Redesign Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the counts-heavy ClusterOverview with a health-first dashboard: health cards → capacity bars → activity feed.

**Architecture:** Extend `ClusterSnapshot` with convergence/reservation fields computed during the existing `Snapshot()` iteration. Add a new `/api/cluster/metrics` endpoint for Prometheus utilization data. Rewrite `ClusterOverview.tsx` with layout B (health row + two-column capacity/activity).

**Tech Stack:** Go (backend), React 19 + TypeScript + Tailwind CSS v4 (frontend)

**Spec:** `docs/plans/2026-03-10-dashboard-redesign-design.md`

---

## File Structure

**Backend (modify):**
- `internal/cache/cache.go:53-64` — extend `ClusterSnapshot` struct with new fields
- `internal/cache/cache.go:674-709` — extend `Snapshot()` to compute new fields
- `internal/cache/cache_test.go` — add test for new snapshot fields
- `internal/api/handlers.go` — add `HandleClusterMetrics` handler
- `internal/api/handlers_test.go` — add test for new handler
- `internal/api/router.go:19` — register new route

**Frontend (modify):**
- `frontend/src/api/client.ts:56-67` — extend `ClusterSnapshot` interface, add `clusterMetrics` method
- `frontend/src/pages/ClusterOverview.tsx` — full rewrite with new layout

**Frontend (create):**
- `frontend/src/components/PrometheusBanner.tsx` — dismissible setup banner
- `frontend/src/components/CapacitySection.tsx` — utilization or reservation bars

---

## Task 1: Extend ClusterSnapshot with convergence and reservation fields

**Files:**
- Modify: `internal/cache/cache.go:53-64` (struct) and `internal/cache/cache.go:674-709` (Snapshot method)
- Test: `internal/cache/cache_test.go`

- [ ] **Step 1: Write failing test for new snapshot fields**

Add to `internal/cache/cache_test.go`:

```go
func TestSnapshot_ConvergenceAndReservations(t *testing.T) {
	c := New(nil)

	// Node with drain availability
	c.SetNode(swarm.Node{
		ID:     "n1",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:   swarm.NodeSpec{Availability: swarm.NodeAvailabilityDrain},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 8_589_934_592},
		},
	})
	c.SetNode(swarm.Node{
		ID:     "n2",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec:   swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 8_589_934_592},
		},
	})

	// Converged service: 2 desired, 2 running, with reservations
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(2))}},
			TaskTemplate: swarm.TaskSpec{
				Resources: &swarm.ResourceRequirements{
					Reservations: &swarm.Resources{
						NanoCPUs:    500_000_000,
						MemoryBytes: 256 * 1024 * 1024,
					},
				},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	// Degraded service: 3 desired, 1 running
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "api"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(3))}},
		},
	})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t4", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})

	snap := c.Snapshot()

	if snap.ServicesConverged != 1 {
		t.Errorf("ServicesConverged=%d, want 1", snap.ServicesConverged)
	}
	if snap.ServicesDegraded != 1 {
		t.Errorf("ServicesDegraded=%d, want 1", snap.ServicesDegraded)
	}
	if snap.NodesDraining != 1 {
		t.Errorf("NodesDraining=%d, want 1", snap.NodesDraining)
	}
	// Reservations: svc1 has 2 replicas * 500m CPU = 1 core, 2 * 256MB = 512MB
	if snap.ReservedCPU != 1_000_000_000 {
		t.Errorf("ReservedCPU=%d, want 1000000000", snap.ReservedCPU)
	}
	if snap.ReservedMemory != 512*1024*1024 {
		t.Errorf("ReservedMemory=%d, want %d", snap.ReservedMemory, 512*1024*1024)
	}
}
```

Note: `ptr` helper may already exist; if not, add `func ptr[T any](v T) *T { return &v }` at test file level.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/cache/ -run TestSnapshot_ConvergenceAndReservations -v`
Expected: FAIL — `ClusterSnapshot` has no `ServicesConverged` field.

- [ ] **Step 3: Extend ClusterSnapshot struct**

In `internal/cache/cache.go`, add fields to the `ClusterSnapshot` struct (after line 62):

```go
type ClusterSnapshot struct {
	NodeCount         int            `json:"nodeCount"`
	ServiceCount      int            `json:"serviceCount"`
	TaskCount         int            `json:"taskCount"`
	StackCount        int            `json:"stackCount"`
	TasksByState      map[string]int `json:"tasksByState"`
	NodesReady        int            `json:"nodesReady"`
	NodesDown         int            `json:"nodesDown"`
	NodesDraining     int            `json:"nodesDraining"`
	TotalCPU          int            `json:"totalCPU"`
	TotalMemory       int64          `json:"totalMemory"`
	ServicesConverged int            `json:"servicesConverged"`
	ServicesDegraded  int            `json:"servicesDegraded"`
	ReservedCPU       int64          `json:"reservedCPU"`
	ReservedMemory    int64          `json:"reservedMemory"`
	LastSync          time.Time      `json:"lastSync"`
}
```

- [ ] **Step 4: Extend Snapshot() method**

In `internal/cache/cache.go`, modify the `Snapshot()` method. After the existing node loop (line 695), add:

1. In the node loop, add draining count:
```go
if n.Spec.Availability == swarm.NodeAvailabilityDrain {
    nodesDraining++
}
```

2. After the node loop, add service convergence + reservation computation:
```go
// Count running tasks per service
runningByService := make(map[string]int)
for _, t := range c.tasks {
    if t.Status.State == swarm.TaskStateRunning {
        runningByService[t.ServiceID]++
    }
}

var servicesConverged, servicesDegraded int
var reservedCPU, reservedMemory int64
for _, svc := range c.services {
    // Convergence check
    if svc.Spec.Mode.Replicated != nil {
        desired := int(*svc.Spec.Mode.Replicated.Replicas)
        if runningByService[svc.ID] >= desired {
            servicesConverged++
        } else {
            servicesDegraded++
        }
    } else {
        // Global services: count as converged (simplified)
        servicesConverged++
    }

    // Reservations
    if svc.Spec.TaskTemplate.Resources != nil && svc.Spec.TaskTemplate.Resources.Reservations != nil {
        res := svc.Spec.TaskTemplate.Resources.Reservations
        replicas := int64(1)
        if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
            replicas = int64(*svc.Spec.Mode.Replicated.Replicas)
        }
        reservedCPU += res.NanoCPUs * replicas
        reservedMemory += res.MemoryBytes * replicas
    }
}
```

3. Add the new fields to the return struct.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/cache/ -run TestSnapshot -v`
Expected: ALL Snapshot tests PASS (both old and new).

- [ ] **Step 6: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: ALL PASS. The `HandleCluster` test will still pass because it only checks `NodeCount`/`ServiceCount`.

- [ ] **Step 7: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "feat: add convergence and reservation fields to ClusterSnapshot"
```

---

## Task 2: Add `/api/cluster/metrics` endpoint

**Files:**
- Modify: `internal/api/handlers.go` (add handler)
- Modify: `internal/api/router.go:19` (add route)
- Test: `internal/api/handlers_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleClusterMetrics_NoPrometheus(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, closedReady(), nil, nil) // nil promClient

	req := httptest.NewRequest("GET", "/api/cluster/metrics", nil)
	w := httptest.NewRecorder()
	h.HandleClusterMetrics(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleClusterMetrics_WithPrometheus(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var val string
		switch {
		case strings.Contains(query, "cpu_seconds"):
			val = "0.62"
		case strings.Contains(query, "memory_usage"):
			val = "47400000000"
		case strings.Contains(query, "filesystem_size"):
			val = "500000000000"
		case strings.Contains(query, "filesystem_avail"):
			val = "295000000000"
		default:
			val = "0"
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"` + val + `"]}]}}`)
	}))
	defer prom.Close()

	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:     "n1",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 64_000_000_000},
		},
	})
	h := NewHandlers(c, nil, closedReady(), nil, NewPromClient(prom.URL))

	req := httptest.NewRequest("GET", "/api/cluster/metrics", nil)
	w := httptest.NewRecorder()
	h.HandleClusterMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		CPU    struct{ Percent float64 } `json:"cpu"`
		Memory struct{ Percent float64 } `json:"memory"`
		Disk   struct{ Percent float64 } `json:"disk"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.CPU.Percent == 0 {
		t.Error("expected non-zero CPU percent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestHandleClusterMetrics -v`
Expected: FAIL — `HandleClusterMetrics` does not exist.

- [ ] **Step 3: Implement HandleClusterMetrics**

Add to `internal/api/handlers.go`:

```go
type ClusterMetrics struct {
	CPU    ResourceMetric `json:"cpu"`
	Memory ResourceMetric `json:"memory"`
	Disk   ResourceMetric `json:"disk"`
}

type ResourceMetric struct {
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	Percent float64 `json:"percent"`
}

func (h *Handlers) HandleClusterMetrics(w http.ResponseWriter, r *http.Request) {
	if h.promClient == nil {
		writeError(w, http.StatusNotFound, "prometheus not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	snap := h.cache.Snapshot()

	var metrics ClusterMetrics
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(3)

	// CPU utilization
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx,
			`sum(rate(node_cpu_seconds_total{mode!="idle"}[5m])) / sum(rate(node_cpu_seconds_total[5m])) * 100`)
		if err != nil {
			slog.Warn("cluster metrics: CPU query failed", "error", err)
			return
		}
		if len(results) > 0 {
			mu.Lock()
			metrics.CPU = ResourceMetric{
				Used:    float64(snap.TotalCPU) * results[0].Value / 100,
				Total:   float64(snap.TotalCPU),
				Percent: results[0].Value,
			}
			mu.Unlock()
		}
	}()

	// Memory utilization
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx,
			`sum(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes)`)
		if err != nil {
			slog.Warn("cluster metrics: memory query failed", "error", err)
			return
		}
		if len(results) > 0 {
			mu.Lock()
			total := float64(snap.TotalMemory)
			used := results[0].Value
			pct := 0.0
			if total > 0 {
				pct = used / total * 100
			}
			metrics.Memory = ResourceMetric{Used: used, Total: total, Percent: pct}
			mu.Unlock()
		}
	}()

	// Disk utilization
	go func() {
		defer wg.Done()
		type pair struct{ total, avail float64 }
		var p pair
		var pmu sync.Mutex
		var dwg sync.WaitGroup
		dwg.Add(2)
		go func() {
			defer dwg.Done()
			r, err := h.promClient.InstantQuery(ctx, `sum(node_filesystem_size_bytes{mountpoint="/"})`)
			if err == nil && len(r) > 0 {
				pmu.Lock()
				p.total = r[0].Value
				pmu.Unlock()
			}
		}()
		go func() {
			defer dwg.Done()
			r, err := h.promClient.InstantQuery(ctx, `sum(node_filesystem_avail_bytes{mountpoint="/"})`)
			if err == nil && len(r) > 0 {
				pmu.Lock()
				p.avail = r[0].Value
				pmu.Unlock()
			}
		}()
		dwg.Wait()

		if p.total > 0 {
			used := p.total - p.avail
			mu.Lock()
			metrics.Disk = ResourceMetric{
				Used:    used,
				Total:   p.total,
				Percent: used / p.total * 100,
			}
			mu.Unlock()
		}
	}()

	wg.Wait()
	writeJSON(w, metrics)
}
```

- [ ] **Step 4: Register the route**

In `internal/api/router.go`, after line 19 (`HandleReady`), add:

```go
mux.HandleFunc("GET /api/cluster/metrics", h.HandleClusterMetrics)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestHandleClusterMetrics -v`
Expected: PASS

- [ ] **Step 6: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat: add /api/cluster/metrics endpoint for Prometheus utilization"
```

---

## Task 3: Update frontend API client and types

**Files:**
- Modify: `frontend/src/api/client.ts:56-67` (extend ClusterSnapshot, add clusterMetrics)

- [ ] **Step 1: Extend ClusterSnapshot interface**

In `frontend/src/api/client.ts`, update the `ClusterSnapshot` interface:

```typescript
export interface ClusterSnapshot {
  nodeCount: number;
  serviceCount: number;
  taskCount: number;
  stackCount: number;
  tasksByState: Record<string, number>;
  nodesReady: number;
  nodesDown: number;
  nodesDraining: number;
  totalCPU: number;
  totalMemory: number;
  servicesConverged: number;
  servicesDegraded: number;
  reservedCPU: number;
  reservedMemory: number;
  prometheusConfigured: boolean;
}

export interface ClusterMetrics {
  cpu: { used: number; total: number; percent: number };
  memory: { used: number; total: number; percent: number };
  disk: { used: number; total: number; percent: number };
}
```

- [ ] **Step 2: Add clusterMetrics API method**

In the `api` object, add:

```typescript
clusterMetrics: () => fetchJSON<ClusterMetrics>("/cluster/metrics"),
```

- [ ] **Step 3: Type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: extend ClusterSnapshot types and add clusterMetrics API method"
```

---

## Task 4: Create PrometheusBanner component

**Files:**
- Create: `frontend/src/components/PrometheusBanner.tsx`

- [ ] **Step 1: Create the component**

```tsx
import { useState } from "react";
import { X, BarChart3 } from "lucide-react";

const DISMISS_KEY = "cetacean:dismiss-prom-banner";

export default function PrometheusBanner({ configured }: { configured: boolean }) {
  const [dismissed, setDismissed] = useState(
    () => localStorage.getItem(DISMISS_KEY) === "true",
  );

  if (configured || dismissed) return null;

  return (
    <div className="flex items-center gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 px-4 py-3 mb-4">
      <BarChart3 className="size-5 text-blue-400 shrink-0" />
      <p className="text-sm text-blue-200 flex-1">
        Set <code className="rounded bg-blue-500/20 px-1.5 py-0.5 text-xs font-mono">CETACEAN_PROMETHEUS_URL</code> to
        enable CPU, memory, and disk utilization metrics.
      </p>
      <button
        onClick={() => {
          localStorage.setItem(DISMISS_KEY, "true");
          setDismissed(true);
        }}
        className="text-blue-400/60 hover:text-blue-300 transition-colors"
        aria-label="Dismiss"
      >
        <X className="size-4" />
      </button>
    </div>
  );
}
```

- [ ] **Step 2: Type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/PrometheusBanner.tsx
git commit -m "feat: add dismissible Prometheus setup banner component"
```

---

## Task 5: Create CapacitySection component

**Files:**
- Create: `frontend/src/components/CapacitySection.tsx`

- [ ] **Step 1: Create the component**

```tsx
import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ClusterSnapshot, type ClusterMetrics } from "../api/client";

function barColor(percent: number, isReservation: boolean): string {
  const high = isReservation ? 95 : 90;
  const mid = isReservation ? 80 : 70;
  if (percent >= high) return "bg-red-500";
  if (percent >= mid) return "bg-amber-500";
  return "bg-blue-500";
}

function formatBytes(bytes: number): string {
  if (bytes >= 1e12) return (bytes / 1e12).toFixed(1) + " TB";
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + " GB";
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + " MB";
  return (bytes / 1e3).toFixed(0) + " KB";
}

function Bar({
  label,
  percent,
  detail,
  isReservation,
  onClick,
}: {
  label: string;
  percent: number;
  detail: string;
  isReservation: boolean;
  onClick?: () => void;
}) {
  const clamped = Math.min(100, Math.max(0, percent));
  return (
    <div
      className={`rounded-lg border bg-card p-4 ${onClick ? "cursor-pointer hover:border-foreground/20 transition-colors" : ""}`}
      onClick={onClick}
    >
      <div className="flex justify-between text-xs text-muted-foreground mb-2">
        <span className="font-medium">{label}{isReservation ? " (reserved)" : ""}</span>
        <span className="tabular-nums">{clamped.toFixed(0)}%</span>
      </div>
      <div className="h-2 rounded-full bg-muted overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${barColor(clamped, isReservation)}`}
          style={{ width: `${clamped}%` }}
        />
      </div>
      <div className="text-xs text-muted-foreground mt-1.5">{detail}</div>
    </div>
  );
}

export default function CapacitySection({ snapshot }: { snapshot: ClusterSnapshot }) {
  const navigate = useNavigate();
  const [metrics, setMetrics] = useState<ClusterMetrics | null>(null);
  const goToNodes = useCallback(() => navigate("/nodes"), [navigate]);

  useEffect(() => {
    if (!snapshot.prometheusConfigured) return;
    let cancelled = false;
    const load = () => {
      api.clusterMetrics().then((m) => { if (!cancelled) setMetrics(m); }).catch(() => {});
    };
    load();
    const interval = setInterval(load, 30_000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [snapshot.prometheusConfigured]);

  if (snapshot.prometheusConfigured && metrics) {
    return (
      <div className="space-y-3">
        <Bar label="CPU" percent={metrics.cpu.percent}
          detail={`${metrics.cpu.used.toFixed(1)} / ${metrics.cpu.total.toFixed(0)} cores`}
          isReservation={false} onClick={goToNodes} />
        <Bar label="Memory" percent={metrics.memory.percent}
          detail={`${formatBytes(metrics.memory.used)} / ${formatBytes(metrics.memory.total)}`}
          isReservation={false} onClick={goToNodes} />
        <Bar label="Disk" percent={metrics.disk.percent}
          detail={`${formatBytes(metrics.disk.used)} / ${formatBytes(metrics.disk.total)}`}
          isReservation={false} onClick={goToNodes} />
      </div>
    );
  }

  // Fallback: reservation bars from Docker API
  const cpuReservedCores = snapshot.reservedCPU / 1e9;
  const cpuPct = snapshot.totalCPU > 0 ? (cpuReservedCores / snapshot.totalCPU) * 100 : 0;
  const memPct = snapshot.totalMemory > 0 ? (snapshot.reservedMemory / snapshot.totalMemory) * 100 : 0;

  return (
    <div className="space-y-3">
      <Bar label="CPU" percent={cpuPct}
        detail={`${cpuReservedCores.toFixed(1)} / ${snapshot.totalCPU} cores reserved`}
        isReservation={true} onClick={goToNodes} />
      <Bar label="Memory" percent={memPct}
        detail={`${formatBytes(snapshot.reservedMemory)} / ${formatBytes(snapshot.totalMemory)} reserved`}
        isReservation={true} onClick={goToNodes} />
    </div>
  );
}
```

- [ ] **Step 2: Type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CapacitySection.tsx
git commit -m "feat: add CapacitySection with Prometheus utilization and Docker reservation fallback"
```

---

## Task 6: Rewrite ClusterOverview page

**Files:**
- Modify: `frontend/src/pages/ClusterOverview.tsx`

- [ ] **Step 1: Rewrite the page**

Replace the entire contents of `ClusterOverview.tsx`:

```tsx
import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { TrendingUp, TrendingDown } from "lucide-react";
import { api, type ClusterSnapshot } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useSSE } from "../hooks/useSSE";
import PageHeader from "../components/PageHeader";
import ActivityFeed from "../components/ActivityFeed";
import PrometheusBanner from "../components/PrometheusBanner";
import CapacitySection from "../components/CapacitySection";

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null);
  const prevRef = useRef<ClusterSnapshot | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);

  const fetchSnapshot = useCallback(() => {
    api.cluster().then((s) => {
      setSnapshot((prev) => {
        if (prev) prevRef.current = prev;
        return s;
      });
    });
  }, []);

  useEffect(() => {
    fetchSnapshot();
    api
      .history({ limit: 25 })
      .then(setHistory)
      .catch(() => {})
      .finally(() => setHistoryLoading(false));
  }, [fetchSnapshot]);

  useSSE(
    ["node", "service", "task", "stack"],
    useCallback(() => {
      fetchSnapshot();
    }, [fetchSnapshot]),
  );

  if (!snapshot) {
    return (
      <div>
        <PageHeader title="Cluster Overview" />
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border bg-card p-6">
              <div className="h-4 w-20 bg-muted rounded mb-2" />
              <div className="h-8 w-12 bg-muted rounded" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const prev = prevRef.current;
  const tasksFailed = snapshot.tasksByState?.["failed"] || 0;
  const prevFailed = prev?.tasksByState?.["failed"] || 0;
  const tasksRunning = snapshot.tasksByState?.["running"] || 0;

  // Count distinct services with failed tasks (for the "across N services" label)
  // Not available from snapshot — we approximate: if failed > 0, show the count.

  return (
    <div>
      <PageHeader title="Cluster Overview" />

      <PrometheusBanner configured={snapshot.prometheusConfigured} />

      {/* Health Row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <HealthCard
          label="Nodes"
          primary={`${snapshot.nodesReady}/${snapshot.nodeCount} ready`}
          secondary={
            snapshot.nodesDown > 0
              ? `${snapshot.nodesDown} down`
              : snapshot.nodesDraining > 0
                ? `${snapshot.nodesDraining} draining`
                : "all ready"
          }
          status={
            snapshot.nodesDown > 0
              ? "red"
              : snapshot.nodesDraining > 0
                ? "amber"
                : "green"
          }
          to="/nodes"
        />
        <HealthCard
          label="Services"
          primary={`${snapshot.servicesConverged}/${snapshot.serviceCount} converged`}
          secondary={
            snapshot.servicesDegraded > 0
              ? `${snapshot.servicesDegraded} degraded`
              : "all healthy"
          }
          status={snapshot.servicesDegraded > 0 ? "amber" : "green"}
          to="/services"
        />
        <HealthCard
          label="Failed Tasks"
          primary={String(tasksFailed)}
          secondary={tasksFailed > 0 ? "needs attention" : "none"}
          status={tasksFailed > 0 ? "red" : "neutral"}
          delta={prev ? tasksFailed - prevFailed : undefined}
          to="/tasks"
        />
        <HealthCard
          label="Tasks"
          primary={`${tasksRunning} running`}
          secondary={`${snapshot.taskCount} total · ${snapshot.stackCount} stacks`}
          status="neutral"
          to="/tasks"
        />
      </div>

      {/* Two-column: Capacity + Activity */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div>
          <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Capacity
          </h2>
          <CapacitySection snapshot={snapshot} />
        </div>
        <div>
          <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            Recent Activity
          </h2>
          <div className="max-h-80 overflow-y-auto rounded-lg border bg-card p-4">
            <ActivityFeed entries={history} loading={historyLoading} />
          </div>
        </div>
      </div>
    </div>
  );
}

function HealthCard({
  label,
  primary,
  secondary,
  status,
  delta,
  to,
}: {
  label: string;
  primary: string;
  secondary: string;
  status: "green" | "amber" | "red" | "neutral";
  delta?: number;
  to: string;
}) {
  const navigate = useNavigate();

  const borderColor = {
    green: "border-green-500/30",
    amber: "border-amber-500/30",
    red: "border-red-500/30",
    neutral: "",
  }[status];

  const bgTint = {
    green: "bg-green-500/5",
    amber: "bg-amber-500/5",
    red: "bg-red-500/5",
    neutral: "bg-card",
  }[status];

  const primaryColor = {
    green: "text-green-500",
    amber: "text-amber-500",
    red: "text-red-500",
    neutral: "",
  }[status];

  return (
    <div
      className={`rounded-lg border p-5 cursor-pointer hover:border-foreground/20 hover:shadow-sm transition-all ${borderColor} ${bgTint}`}
      onClick={() => navigate(to)}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); navigate(to); } }}
      role="link"
      tabIndex={0}
    >
      <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1.5">
        {label}
      </div>
      <div className="flex items-center gap-2">
        <span className={`text-2xl font-semibold tabular-nums ${primaryColor}`}>
          {primary}
        </span>
        {delta != null && delta !== 0 && (
          <span
            className={`flex items-center gap-0.5 text-xs font-medium ${
              delta > 0 ? "text-red-500" : "text-green-500"
            }`}
          >
            {delta > 0 ? <TrendingUp className="size-3" /> : <TrendingDown className="size-3" />}
            {delta > 0 ? `+${delta}` : delta}
          </span>
        )}
      </div>
      <div className="text-xs text-muted-foreground mt-1">{secondary}</div>
    </div>
  );
}
```

- [ ] **Step 2: Type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: PASS

- [ ] **Step 3: Run frontend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: ALL PASS

- [ ] **Step 4: Run full Go test suite**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ClusterOverview.tsx
git commit -m "feat: rewrite ClusterOverview with health-first dashboard layout"
```
