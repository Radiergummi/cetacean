# Monitoring Onboarding Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the generic "set env var" Prometheus banner with auto-detection of monitoring sources and targeted setup guidance, plus split the compose files so monitoring is independently deployable.

**Architecture:** New `GET /api/metrics/status` endpoint probes Prometheus for `up{}` targets by job name, compares against known node count. Frontend replaces `usePrometheusConfigured` with `useMonitoringStatus` and replaces `PrometheusBanner` with `MonitoringStatus` that shows contextual guidance per detection state. Compose files split into cetacean-only and monitoring overlay.

**Tech Stack:** Go stdlib + existing PromClient, React hooks, Tailwind CSS

---

## Chunk 1: Backend — Metrics Status Endpoint

### Task 1: Add `HandleMonitoringStatus` handler with tests

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleMonitoringStatus_NoPrometheus(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		PrometheusConfigured bool `json:"prometheusConfigured"`
		PrometheusReachable  bool `json:"prometheusReachable"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=false")
	}
	if resp.PrometheusReachable {
		t.Error("expected prometheusReachable=false")
	}
}

func TestHandleMonitoringStatus_WithPrometheus(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var result string
		switch {
		case strings.Contains(query, `job="node-exporter"`):
			result = `[{"metric":{"instance":"10.0.0.1:9100"},"value":[1,"1"]},{"metric":{"instance":"10.0.0.2:9100"},"value":[1,"1"]}]`
		case strings.Contains(query, `job="cadvisor"`):
			result = `[{"metric":{"instance":"10.0.0.1:8080"},"value":[1,"1"]},{"metric":{"instance":"10.0.0.2:8080"},"value":[1,"1"]}]`
		default:
			result = `[]`
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":%s}}`, result)
	}))
	defer prom.Close()

	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	c.SetNode(swarm.Node{ID: "n2", Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	h := NewHandlers(c, nil, nil, closedReady(), nil, NewPromClient(prom.URL))

	req := httptest.NewRequest("GET", "/api/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		PrometheusConfigured bool `json:"prometheusConfigured"`
		PrometheusReachable  bool `json:"prometheusReachable"`
		NodeExporter         *struct {
			Targets int `json:"targets"`
			Nodes   int `json:"nodes"`
		} `json:"nodeExporter"`
		Cadvisor *struct {
			Targets int `json:"targets"`
			Nodes   int `json:"nodes"`
		} `json:"cadvisor"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if !resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=true")
	}
	if !resp.PrometheusReachable {
		t.Error("expected prometheusReachable=true")
	}
	if resp.NodeExporter == nil || resp.NodeExporter.Targets != 2 {
		t.Errorf("expected 2 node-exporter targets, got %+v", resp.NodeExporter)
	}
	if resp.Cadvisor == nil || resp.Cadvisor.Targets != 2 {
		t.Errorf("expected 2 cadvisor targets, got %+v", resp.Cadvisor)
	}
	if resp.NodeExporter.Nodes != 2 {
		t.Errorf("expected nodes=2, got %d", resp.NodeExporter.Nodes)
	}
}

func TestHandleMonitoringStatus_PrometheusUnreachable(t *testing.T) {
	// Point to a closed server so the connection fails
	h := NewHandlers(cache.New(nil), nil, nil, closedReady(), nil, NewPromClient("http://127.0.0.1:1"))

	req := httptest.NewRequest("GET", "/api/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		PrometheusConfigured bool `json:"prometheusConfigured"`
		PrometheusReachable  bool `json:"prometheusReachable"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=true")
	}
	if resp.PrometheusReachable {
		t.Error("expected prometheusReachable=false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleMonitoringStatus -v`
Expected: FAIL — `HandleMonitoringStatus` not defined

- [ ] **Step 3: Write the handler**

Add to `internal/api/handlers.go`, after `HandleClusterMetrics`:

```go
type MonitoringStatus struct {
	PrometheusConfigured bool           `json:"prometheusConfigured"`
	PrometheusReachable  bool           `json:"prometheusReachable"`
	NodeExporter         *TargetStatus  `json:"nodeExporter"`
	Cadvisor             *TargetStatus  `json:"cadvisor"`
}

type TargetStatus struct {
	Targets int `json:"targets"`
	Nodes   int `json:"nodes"`
}

func (h *Handlers) HandleMonitoringStatus(w http.ResponseWriter, r *http.Request) {
	status := MonitoringStatus{
		PrometheusConfigured: h.promClient != nil,
	}

	if h.promClient == nil {
		writeJSON(w, status)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	nodeCount := len(h.cache.AllNodes())

	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	// Probe node-exporter
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx, `up{job="node-exporter"}`)
		if err != nil {
			return
		}
		mu.Lock()
		status.PrometheusReachable = true
		status.NodeExporter = &TargetStatus{Targets: len(results), Nodes: nodeCount}
		mu.Unlock()
	}()

	// Probe cAdvisor
	go func() {
		defer wg.Done()
		results, err := h.promClient.InstantQuery(ctx, `up{job="cadvisor"}`)
		if err != nil {
			return
		}
		mu.Lock()
		status.PrometheusReachable = true
		status.Cadvisor = &TargetStatus{Targets: len(results), Nodes: nodeCount}
		mu.Unlock()
	}()

	wg.Wait()

	// If neither query set reachable (both failed), do a simple connectivity check
	if !status.PrometheusReachable {
		results, err := h.promClient.InstantQuery(ctx, `vector(1)`)
		if err == nil && len(results) > 0 {
			status.PrometheusReachable = true
		}
	}

	writeJSON(w, status)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestHandleMonitoringStatus -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Register the route**

In `internal/api/router.go`, add after line 76 (`mux.Handle("GET /api/metrics/", promProxy)`):

```go
mux.HandleFunc("GET /api/metrics/status", h.HandleMonitoringStatus)
```

**Important:** This must be registered BEFORE the `/api/metrics/` catch-all so it takes precedence.

- [ ] **Step 6: Run full backend tests**

Run: `go test ./internal/api/ -v`
Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go internal/api/router.go
git commit -m "feat: add GET /api/metrics/status endpoint for monitoring source detection"
```

---

### Task 2: Split compose files

**Files:**
- Modify: `docker-compose.yml`
- Create: `docker-compose.monitoring.yml`

- [ ] **Step 1: Create `docker-compose.monitoring.yml`**

```yaml
version: "3.8"

services:
  prometheus:
    image: prom/prometheus:v3.3.0
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus_data:/prometheus
    networks:
      - monitoring
    deploy:
      placement:
        constraints:
          - node.role == manager
      resources:
        limits:
          cpus: "1.0"
          memory: 2G
        reservations:
          cpus: "0.25"
          memory: 512M

  cadvisor:
    image: gcr.io/cadvisor/cadvisor:v0.51.0
    command:
      - '--store_container_labels=true'
      - '--docker_only=true'
    volumes:
      - /:/rootfs:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /run/docker/containerd/containerd.sock:/run/containerd/containerd.sock:ro
      - /sys:/sys:ro
      - /var/lib/docker/:/var/lib/docker:ro
    networks:
      - monitoring
    deploy:
      mode: global
      resources:
        limits:
          cpus: "0.5"
          memory: 256M
        reservations:
          cpus: "0.1"
          memory: 64M

  node-exporter:
    image: prom/node-exporter:v1.9.0
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/rootfs:ro
    command:
      - '--path.procfs=/host/proc'
      - '--path.sysfs=/host/sys'
      - '--path.rootfs=/rootfs'
      - '--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)'
    networks:
      - monitoring
    deploy:
      mode: global
      resources:
        limits:
          cpus: "0.5"
          memory: 128M
        reservations:
          cpus: "0.1"
          memory: 32M

networks:
  monitoring:
    driver: overlay

volumes:
  prometheus_data:
```

- [ ] **Step 2: Trim `docker-compose.yml` to Cetacean-only**

Replace `docker-compose.yml` with:

```yaml
version: "3.8"

services:
  cetacean:
    image: cetacean:latest
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      # Uncomment and set to enable metrics (requires monitoring stack):
      # CETACEAN_PROMETHEUS_URL: http://prometheus:9090
      - CETACEAN_PROMETHEUS_URL=${CETACEAN_PROMETHEUS_URL:-}
    networks:
      - monitoring
    deploy:
      placement:
        constraints:
          - node.role == manager
      resources:
        limits:
          cpus: "2.0"
          memory: 1G
        reservations:
          cpus: "0.5"
          memory: 256M

networks:
  monitoring:
    external: true
    name: cetacean-monitoring_monitoring
```

- [ ] **Step 3: Verify compose files parse**

Run: `docker compose -f docker-compose.yml config > /dev/null && docker compose -f docker-compose.monitoring.yml config > /dev/null && echo OK`
Expected: OK (no parse errors)

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml docker-compose.monitoring.yml
git commit -m "refactor: split monitoring stack into separate compose overlay"
```

---

## Chunk 2: Frontend — Monitoring Status Hook and Banner

### Task 3: Add `MonitoringStatus` type and API method

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add type to `frontend/src/api/types.ts`**

Add at end of file:

```typescript
export interface TargetStatus {
  targets: number;
  nodes: number;
}

export interface MonitoringStatus {
  prometheusConfigured: boolean;
  prometheusReachable: boolean;
  nodeExporter: TargetStatus | null;
  cadvisor: TargetStatus | null;
}
```

- [ ] **Step 2: Add API method to `frontend/src/api/client.ts`**

Add import of `MonitoringStatus` to the import block at top of file, alongside other type imports.

Add to the `api` object (after `clusterMetrics`):

```typescript
monitoringStatus: () => fetchJSON<MonitoringStatus>("/metrics/status"),
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add monitoringStatus API method and types"
```

---

### Task 4: Create `useMonitoringStatus` hook

**Files:**
- Create: `frontend/src/hooks/useMonitoringStatus.ts`

- [ ] **Step 1: Create the hook**

```typescript
import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";

let cached: MonitoringStatus | null = null;
let inflight: Promise<void> | null = null;

export function _resetMonitoringStatusCache() {
  cached = null;
  inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cached);

  useEffect(() => {
    if (cached != null) return;
    if (!inflight) {
      inflight = api
        .monitoringStatus()
        .then((s) => {
          cached = s;
        })
        .catch(() => {
          cached = {
            prometheusConfigured: false,
            prometheusReachable: false,
            nodeExporter: null,
            cadvisor: null,
          };
        });
    }
    inflight.then(() => setStatus(cached));
  }, []);

  return status;
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/hooks/useMonitoringStatus.ts
git commit -m "feat: add useMonitoringStatus hook"
```

---

### Task 5: Create `MonitoringStatus` banner component

**Files:**
- Create: `frontend/src/components/metrics/MonitoringStatus.tsx`

- [ ] **Step 1: Create the component**

This component handles 4 states: nothing configured, unreachable, partial sources, fully healthy.

```tsx
import { useState } from "react";
import { AlertTriangle, BarChart3, CheckCircle2, X } from "lucide-react";
import type { MonitoringStatus as Status } from "@/api/types";

const DISMISS_KEY = "cetacean:dismiss-monitoring-banner";

interface Props {
  status: Status;
  /** Limit hints to a specific source (e.g. only show cAdvisor hint on ServiceDetail) */
  source?: "nodeExporter" | "cadvisor";
}

export default function MonitoringStatus({ status, source }: Props) {
  const [dismissed, setDismissed] = useState(
    () => localStorage.getItem(DISMISS_KEY) === "true",
  );

  // Fully healthy — nothing to show
  if (status.prometheusConfigured && status.prometheusReachable) {
    const neOk = !status.nodeExporter || status.nodeExporter.targets >= status.nodeExporter.nodes;
    const caOk = !status.cadvisor || status.cadvisor.targets >= status.cadvisor.nodes;
    if (neOk && caOk) return null;
  }

  // State A: Nothing configured (dismissible)
  if (!status.prometheusConfigured) {
    if (dismissed) return null;
    return (
      <Banner
        icon={<BarChart3 className="size-5 text-blue-400 shrink-0" />}
        border="border-blue-500/30"
        bg="bg-blue-500/10"
        textColor="text-blue-200"
        onDismiss={() => {
          localStorage.setItem(DISMISS_KEY, "true");
          setDismissed(true);
        }}
      >
        <p className="text-sm">
          <strong>Monitoring not configured.</strong> Deploy the monitoring stack
          to enable CPU, memory, and disk metrics across your cluster.
        </p>
        <pre className="mt-2 text-xs bg-blue-500/10 rounded px-2 py-1 overflow-x-auto">
          docker stack deploy -c docker-compose.monitoring.yml cetacean-monitoring
        </pre>
        <p className="mt-1 text-xs text-blue-300/70">
          Then set{" "}
          <code className="rounded bg-blue-500/20 px-1 py-0.5 font-mono">
            CETACEAN_PROMETHEUS_URL
          </code>{" "}
          and restart Cetacean.
        </p>
      </Banner>
    );
  }

  // State B: Prometheus unreachable (not dismissible)
  if (!status.prometheusReachable) {
    return (
      <Banner
        icon={<AlertTriangle className="size-5 text-amber-400 shrink-0" />}
        border="border-amber-500/30"
        bg="bg-amber-500/10"
        textColor="text-amber-200"
      >
        <p className="text-sm">
          <strong>Cannot reach Prometheus</strong> — metrics unavailable. Check
          that the Prometheus service is running and reachable from Cetacean.
        </p>
      </Banner>
    );
  }

  // State C: Partial sources
  const hints: string[] = [];

  if (source !== "cadvisor" && status.nodeExporter) {
    const { targets, nodes } = status.nodeExporter;
    if (targets === 0) {
      hints.push("node-exporter not detected — node metrics (CPU, memory, disk) unavailable.");
    } else if (targets < nodes) {
      hints.push(`node-exporter reporting on ${targets} of ${nodes} nodes.`);
    }
  }

  if (source !== "nodeExporter" && status.cadvisor) {
    const { targets, nodes } = status.cadvisor;
    if (targets === 0) {
      hints.push("cAdvisor not detected — container metrics (service CPU/memory) unavailable.");
    } else if (targets < nodes) {
      hints.push(`cAdvisor reporting on ${targets} of ${nodes} nodes.`);
    }
  }

  if (hints.length === 0) return null;

  return (
    <Banner
      icon={<BarChart3 className="size-5 text-blue-400 shrink-0" />}
      border="border-blue-500/30"
      bg="bg-blue-500/10"
      textColor="text-blue-200"
      onDismiss={() => {
        localStorage.setItem(DISMISS_KEY, "true");
        setDismissed(true);
      }}
    >
      <p className="text-sm">
        <strong>Monitoring partially configured</strong>
      </p>
      <ul className="mt-1 text-sm list-disc list-inside space-y-0.5">
        {hints.map((h) => (
          <li key={h}>{h}</li>
        ))}
      </ul>
    </Banner>
  );
}

function Banner({
  icon,
  border,
  bg,
  textColor,
  onDismiss,
  children,
}: {
  icon: React.ReactNode;
  border: string;
  bg: string;
  textColor: string;
  onDismiss?: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className={`flex items-start gap-3 rounded-lg border ${border} ${bg} px-4 py-3 mb-4`}>
      {icon}
      <div className={`flex-1 ${textColor}`}>{children}</div>
      {onDismiss && (
        <button
          onClick={onDismiss}
          className="text-current opacity-40 hover:opacity-70 transition-opacity shrink-0"
          aria-label="Dismiss"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/metrics/MonitoringStatus.tsx
git commit -m "feat: add MonitoringStatus banner component with 4 detection states"
```

---

### Task 6: Wire up new components and remove old ones

**Files:**
- Modify: `frontend/src/components/metrics/index.ts`
- Modify: `frontend/src/pages/ClusterOverview.tsx`
- Modify: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/pages/NodeList.tsx`
- Modify: `frontend/src/pages/NodeList.test.tsx`
- Modify: `frontend/src/pages/ClusterOverview.test.tsx`
- Modify: `frontend/src/components/metrics/CapacitySection.tsx`
- Delete: `frontend/src/components/metrics/PrometheusBanner.tsx`
- Delete: `frontend/src/hooks/usePrometheusConfigured.ts`

- [ ] **Step 1: Update `frontend/src/components/metrics/index.ts`**

Replace the `PrometheusBanner` export with `MonitoringStatus`:

```typescript
export { default as MonitoringStatus } from "./MonitoringStatus";
```

Remove the line:
```typescript
export { default as PrometheusBanner } from "./PrometheusBanner";
```

- [ ] **Step 2: Update `ClusterOverview.tsx`**

Replace import:
```typescript
// Old:
import { PrometheusBanner, CapacitySection } from "../components/metrics";
// New:
import { MonitoringStatus, CapacitySection } from "../components/metrics";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
```

Inside the component, add the hook call near the top (after existing hooks):
```typescript
const monitoring = useMonitoringStatus();
```

Replace the `<PrometheusBanner configured={snapshot.prometheusConfigured} />` line with:
```tsx
{monitoring && <MonitoringStatus status={monitoring} />}
```

- [ ] **Step 3: Update `CapacitySection.tsx`**

Change `snapshot.prometheusConfigured` checks to accept monitoring status. The simplest approach: `CapacitySection` already receives `snapshot` which has `prometheusConfigured`. No change needed here — it already works correctly. The banner is what guides the user; CapacitySection falls back to reservations when Prometheus has no data.

(No changes needed — skip this step.)

- [ ] **Step 4: Update `NodeDetail.tsx`**

Replace import:
```typescript
// Old:
import { usePrometheusConfigured } from "../hooks/usePrometheusConfigured";
// New:
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
```

Replace hook call:
```typescript
// Old:
const hasPrometheus = usePrometheusConfigured();
// New:
const monitoring = useMonitoringStatus();
const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
```

After the `<PageHeader>` block (or wherever appropriate), add the contextual banner for node-exporter:
```tsx
{monitoring && !hasPrometheus && <MonitoringStatus status={monitoring} source="nodeExporter" />}
```

Import `MonitoringStatus`:
```typescript
import { MonitoringStatus as MonitoringBanner } from "../components/metrics";
```

(Use alias to avoid name collision if needed, or just import directly.)

- [ ] **Step 5: Update `ServiceDetail.tsx`**

Same pattern as NodeDetail:

Replace import:
```typescript
// Old:
import {usePrometheusConfigured} from "../hooks/usePrometheusConfigured";
// New:
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
```

Replace hook call:
```typescript
// Old:
const hasPrometheus = usePrometheusConfigured();
// New:
const monitoring = useMonitoringStatus();
const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
```

- [ ] **Step 6: Update `NodeList.tsx`**

Replace import and hook call with same pattern as above:
```typescript
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
// ...
const monitoring = useMonitoringStatus();
const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
```

Check how `hasPrometheus` is used in NodeList and make sure the replacement works. (It's used to conditionally show gauge columns.)

- [ ] **Step 7: Update `NodeList.test.tsx`**

Replace:
```typescript
import { _resetPrometheusCache } from "../hooks/usePrometheusConfigured";
```
With:
```typescript
import { _resetMonitoringStatusCache } from "../hooks/useMonitoringStatus";
```

Replace all calls to `_resetPrometheusCache()` with `_resetMonitoringStatusCache()`.

- [ ] **Step 8: Update `ClusterOverview.test.tsx`**

Replace the mock:
```typescript
// Old:
PrometheusBanner: () => null,
// New:
MonitoringStatus: () => null,
```

- [ ] **Step 9: Update `frontend/src/hooks/useNodeMetrics.ts`**

This file also imports `usePrometheusConfigured`. Replace:
```typescript
import { usePrometheusConfigured } from "./usePrometheusConfigured";
```
With:
```typescript
import { useMonitoringStatus } from "./useMonitoringStatus";
```

And replace the hook call:
```typescript
// Old:
const hasPrometheus = usePrometheusConfigured();
// New:
const monitoring = useMonitoringStatus();
const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
```

- [ ] **Step 10: Delete old files**

```bash
rm frontend/src/components/metrics/PrometheusBanner.tsx
rm frontend/src/hooks/usePrometheusConfigured.ts
```

- [ ] **Step 11: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 12: Run frontend tests**

Run: `cd frontend && npx vitest run`
Expected: all tests pass

- [ ] **Step 13: Run frontend lint**

Run: `cd frontend && npm run lint`
Expected: no errors

- [ ] **Step 14: Commit**

```bash
git add -A
git commit -m "feat: replace PrometheusBanner with MonitoringStatus auto-detection banner"
```

---

## Chunk 3: Cleanup and Documentation

### Task 7: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add monitoring status endpoint to CLAUDE.md**

In the Backend section under `api/handlers.go`, add a note about the new endpoint:
- `HandleMonitoringStatus` — `GET /api/metrics/status` probes Prometheus for node-exporter and cAdvisor targets, returns detection status. Response cached by frontend at module level.

In the Environment variables table, add a note that `CETACEAN_PROMETHEUS_URL` enables the full monitoring stack (Prometheus + cAdvisor + node-exporter), and that `docker-compose.monitoring.yml` provides the monitoring stack.

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document monitoring status endpoint and compose split"
```

### Task 8: Final verification

- [ ] **Step 1: Run full backend tests**

Run: `go test ./...`
Expected: all tests pass

- [ ] **Step 2: Run full frontend checks**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run && npm run lint`
Expected: all pass

- [ ] **Step 3: Verify compose files**

Run: `docker compose -f docker-compose.yml config > /dev/null && docker compose -f docker-compose.monitoring.yml config > /dev/null`
Expected: both parse without errors
