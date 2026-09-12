# Demo Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `cetacean-demo` binary that mocks the Docker Engine API and Prometheus API, serving as a drop-in replacement for a real Docker swarm + Prometheus so the real Cetacean binary can run in demo mode with no infrastructure.

**Architecture:** Two HTTP servers in one binary — a fake Docker Engine API on `:2375` and a fake Prometheus API on `:9090`. The Docker mock serves a hand-crafted "webshop" dataset (3 nodes, 3 stacks, ~12 services, ~30 tasks) with a simulator goroutine that emits realistic events. The Prometheus mock pattern-matches on PromQL queries and returns generated time-series data. Both use `net/http.ServeMux` with the standard library.

**Tech Stack:** Go standard library, Docker Engine API types (`github.com/docker/docker/api/types/*`), `encoding/json`, `math` (for time-series generation)

---

## File Structure

```
cmd/demo-server/
  main.go              — CLI flags, starts both HTTP servers, wires simulator
internal/demo/
  dataset.go           — Builds the full fake swarm state (nodes, services, tasks, etc.)
  docker.go            — Docker Engine API HTTP handlers (list, inspect, events, logs, info, ping)
  prometheus.go        — Prometheus API HTTP handlers (query, query_range, labels)
  simulator.go         — Event generation goroutine (task churn, rolling updates)
  timeseries.go        — Fake metrics data generator (sinusoidal + noise)
```

## Conventions

- The Docker Go SDK sends requests to `/{version}/endpoint` (e.g., `/v1.46/nodes`). The mock server must handle versioned paths. A middleware strips the `/v1.xx/` prefix.
- The SDK uses `WithAPIVersionNegotiation()`, which sends `GET /_ping` on first request. The mock must respond to `/_ping` with `API-Version` and `Docker-Experimental` headers.
- List endpoints (nodes, services, tasks, configs, secrets, networks) return **bare JSON arrays**. Volumes returns `{"Volumes": [...], "Warnings": null}`.
- Inspect endpoints (nodes, services, tasks, configs, secrets) return the object + raw JSON. The SDK calls `*InspectWithRaw`, but the HTTP response is just the JSON object — the SDK extracts raw bytes itself.
- `GET /events` is a **newline-delimited JSON stream** (not SSE). Each line is a `events.Message` JSON object. The connection stays open.
- Service/task logs use Docker's multiplexed stream format (8-byte header per frame: `[stream_type, 0, 0, 0, size_be32]` + payload).

---

### Task 1: Project Skeleton and Main Entry Point

**Files:**
- Create: `cmd/demo-server/main.go`

- [ ] **Step 1: Create the demo server entry point**

```go
// cmd/demo-server/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/radiergummi/cetacean/internal/demo"
)

func main() {
	dockerAddr := flag.String("docker-addr", ":2375", "Docker API listen address")
	promAddr := flag.String("prom-addr", ":9090", "Prometheus API listen address")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dataset := demo.BuildDataset()
	sim := demo.NewSimulator(dataset)

	dockerMux := demo.NewDockerHandler(dataset, sim)
	promMux := demo.NewPrometheusHandler(dataset)

	go sim.Run(ctx)

	errCh := make(chan error, 2)
	go func() {
		slog.Info("docker API listening", "addr", *dockerAddr)
		errCh <- http.ListenAndServe(*dockerAddr, dockerMux)
	}()
	go func() {
		slog.Info("prometheus API listening", "addr", *promAddr)
		errCh <- http.ListenAndServe(*promAddr, promMux)
	}()

	select {
	case err := <-errCh:
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down")
	}
}
```

- [ ] **Step 2: Verify it compiles (with stubs)**

Create the stub files from Task 2-6 first, then run:

```bash
go build ./cmd/demo-server
```

Expected: compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add cmd/demo-server/main.go internal/demo/
git commit -m "feat: add demo server skeleton with main entry point"
```

---

### Task 2: Dataset — Nodes, Networks, Volumes

**Files:**
- Create: `internal/demo/dataset.go`

This task builds the first part of the dataset: nodes, networks, volumes, and the `Dataset` struct that holds everything. Later tasks add services/tasks/configs/secrets.

- [ ] **Step 1: Define the Dataset struct and node/network/volume builders**

```go
// internal/demo/dataset.go
package demo

import (
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// Dataset holds the complete fake swarm state.
type Dataset struct {
	Nodes    []swarm.Node
	Services []swarm.Service
	Tasks    []swarm.Task
	Configs  []swarm.Config
	Secrets  []swarm.Secret
	Networks []network.Summary
	Volumes  []volume.Volume
	Swarm    swarm.Swarm

	// Indexes for fast lookup.
	nodesByID    map[string]*swarm.Node
	servicesByID map[string]*swarm.Service
	tasksByID    map[string]*swarm.Task
	configsByID  map[string]*swarm.Config
	secretsByID  map[string]*swarm.Secret
	networksByID map[string]*network.Summary
	volumesByName map[string]*volume.Volume
}
```

The `BuildDataset()` function constructs:

**3 Nodes:**
- `node-manager-1` — manager, leader, active, 8 CPU / 16 GiB RAM
- `node-worker-1` — worker, active, 4 CPU / 8 GiB RAM
- `node-worker-2` — worker, active, 4 CPU / 8 GiB RAM

**5 Networks:**
- `ingress` — swarm scope, overlay, ingress=true
- `webshop_default` — swarm scope, overlay, stack label
- `monitoring_default` — swarm scope, overlay, stack label
- `infra_default` — swarm scope, overlay, stack label
- `docker_gwbridge` — local scope, bridge

**2 Volumes:**
- `webshop_db-data` — local driver, stack label
- `monitoring_prometheus-data` — local driver, stack label

Use realistic Docker IDs (64-char hex strings truncated to 25 chars like real Docker, e.g., `"nd8a3k5l2m9b7x4q1w6r0j3p5"`). Use realistic timestamps (a few days ago for CreatedAt).

Build all index maps after populating slices.

Build the `swarm.Swarm` object with:
- Realistic swarm ID
- `Spec.Name: "default"`
- `JoinTokens` with placeholder tokens
- Raft config, dispatcher, CA config with defaults
- `TLSInfo` populated

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/demo/
```

Expected: compiles. No tests for data construction — it's declarative.

- [ ] **Step 3: Commit**

```bash
git add internal/demo/dataset.go
git commit -m "feat(demo): add dataset struct with nodes, networks, volumes"
```

---

### Task 3: Dataset — Services, Tasks, Configs, Secrets

**Files:**
- Modify: `internal/demo/dataset.go`

This task adds the three stacks with all their services, tasks, configs, and secrets.

- [ ] **Step 1: Add services to BuildDataset**

**Stack: `webshop`** (6 services):

| Service | Image | Mode | Replicas | Notes |
|---------|-------|------|----------|-------|
| `webshop_frontend` | `nginx:1.27-alpine` | replicated | 3 | Has healthcheck |
| `webshop_api` | `registry.example.com/webshop/api:v2.4.1` | replicated | 3 | Has healthcheck, one task recently failed |
| `webshop_worker` | `registry.example.com/webshop/worker:v2.4.1` | replicated | 2 | |
| `webshop_db` | `postgres:16-alpine` | replicated | 1 | No healthcheck (triggers recommendation), uses secret + config, mounts volume |
| `webshop_cache` | `redis:7-alpine` | replicated | 1 | Has healthcheck |
| `webshop_search` | `elasticsearch:8.17.0` | replicated | 2 | No healthcheck (triggers recommendation), high memory limit |

**Stack: `monitoring`** (3 services):

| Service | Image | Mode | Replicas | Notes |
|---------|-------|------|----------|-------|
| `monitoring_prometheus` | `prom/prometheus:v3.2.1` | replicated | 1 | Mounts volume, uses config |
| `monitoring_grafana` | `grafana/grafana:11.5.2` | replicated | 1 | |
| `monitoring_node-exporter` | `prom/node-exporter:v1.9.0` | global | 1/node (3 tasks) | |

**Stack: `infra`** (2 services):

| Service | Image | Mode | Replicas | Notes |
|---------|-------|------|----------|-------|
| `infra_proxy` | `traefik:v3.3` | replicated | 2 | Uses config, published ports 80/443, recently updated |
| `infra_registry` | `registry:2` | replicated | 1 | Published port 5000 |

Each service needs:
- Unique 25-char hex ID
- `Spec.Name` matching the table
- `Spec.Labels` with `"com.docker.stack.namespace": "<stack>"`  and `"com.docker.stack.image": "<image>"`
- `Spec.TaskTemplate.ContainerSpec.Image` with the full image including a sha256 digest suffix (e.g., `nginx:1.27-alpine@sha256:abc123...`)
- `Spec.Mode` — replicated with count, or global
- `Version.Index` starting at 100+ (incremented)
- `CreatedAt` / `UpdatedAt` timestamps
- `Endpoint.Ports` for services with published ports
- `Spec.TaskTemplate.Resources` with realistic limits/reservations where appropriate
- `Spec.TaskTemplate.ContainerSpec.Healthcheck` where noted
- `Spec.TaskTemplate.ContainerSpec.Configs` / `Spec.TaskTemplate.ContainerSpec.Secrets` references where noted
- `Spec.TaskTemplate.ContainerSpec.Mounts` for volume mounts
- `UpdateStatus` on `infra_proxy` showing a completed rolling update

- [ ] **Step 2: Add tasks for each service**

For each replicated service, create N tasks in `Running` state with:
- Unique 25-char hex ID
- `ServiceID` referencing the service
- `NodeID` distributed across worker nodes (manager gets global tasks only)
- `Slot` number (1-based for replicated)
- `Status.State: swarm.TaskStateRunning`
- `Status.Timestamp` a few hours ago
- `Status.ContainerStatus.ContainerID` — a 64-char hex string
- `DesiredState: swarm.TaskStateRunning`
- `Spec` matching the service's `TaskTemplate`

Special cases:
- `webshop_api`: one additional task in `Failed` state (slot 2, older) — shows restart history. The current slot 2 task is `Running`.
- `monitoring_node-exporter`: one task per node (global mode), NodeID matches each node
- `infra_proxy`: include two old `Shutdown` tasks from the previous update

- [ ] **Step 3: Add configs and secrets**

**Configs:**
- `webshop_db-init` — stack: webshop, data: SQL init script (base64 in real Docker)
- `monitoring_prometheus-config` — stack: monitoring, data: Prometheus YAML config
- `infra_proxy-config` — stack: infra, data: Traefik TOML config

**Secrets:**
- `webshop_db-password` — stack: webshop
- `webshop_api-key` — stack: webshop

Each needs: unique ID, `Spec.Name`, `Spec.Labels` with stack label, `CreatedAt`, `Version`.
Secret `Spec.Data` must be `nil` (matches Cetacean's behavior of clearing secret data).

- [ ] **Step 4: Build index maps**

After all slices are populated, build the `*ByID` / `*ByName` maps for O(1) lookup in the Docker handlers.

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/demo/
```

- [ ] **Step 6: Commit**

```bash
git add internal/demo/dataset.go
git commit -m "feat(demo): add webshop/monitoring/infra stacks with services, tasks, configs, secrets"
```

---

### Task 4: Docker API Handlers — Ping, List, Inspect, Info, Swarm

**Files:**
- Create: `internal/demo/docker.go`
- Test: `internal/demo/docker_test.go`

- [ ] **Step 1: Write tests for the Docker API handler**

```go
// internal/demo/docker_test.go
package demo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/demo"
)

func TestPing(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/_ping", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if v := rec.Header().Get("API-Version"); v == "" {
		t.Fatal("missing API-Version header")
	}
}

func TestListNodes(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/nodes", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var nodes []swarm.Node
	if err := json.NewDecoder(rec.Body).Decode(&nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestListServices(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/services", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var services []swarm.Service
	if err := json.NewDecoder(rec.Body).Decode(&services); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(services) < 10 {
		t.Fatalf("expected at least 10 services, got %d", len(services))
	}
}

func TestListVolumes(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/volumes", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Volumes  []volume.Volume `json:"Volumes"`
		Warnings []string        `json:"Warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(resp.Volumes))
	}
}

func TestInspectNode(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	id := ds.Nodes[0].ID
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/nodes/"+id, nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var node swarm.Node
	if err := json.NewDecoder(rec.Body).Decode(&node); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if node.ID != id {
		t.Fatalf("expected ID %s, got %s", id, node.ID)
	}
}

func TestInspectNotFound(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/nodes/nonexistent", nil))

	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListNetworks(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/networks", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var networks []network.Summary
	if err := json.NewDecoder(rec.Body).Decode(&networks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(networks) < 4 {
		t.Fatalf("expected at least 4 networks, got %d", len(networks))
	}
}

func TestInfo(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/info", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestSwarmInspect(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewDockerHandler(ds, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1.46/swarm", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var sw swarm.Swarm
	if err := json.NewDecoder(rec.Body).Decode(&sw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sw.ID == "" {
		t.Fatal("expected non-empty swarm ID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/demo/ -v -count=1
```

Expected: compilation errors (package doesn't exist yet or functions missing).

- [ ] **Step 3: Implement the Docker handler**

```go
// internal/demo/docker.go
package demo

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
)

const dockerAPIVersion = "1.46"

// NewDockerHandler returns an http.Handler implementing the subset of the
// Docker Engine API that Cetacean uses. The simulator may be nil if event
// streaming is not needed.
func NewDockerHandler(ds *Dataset, sim *Simulator) http.Handler {
	mux := http.NewServeMux()

	// Ping — required for SDK version negotiation.
	ping := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", dockerAPIVersion)
		w.Header().Set("Docker-Experimental", "false")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
	mux.HandleFunc("GET /_ping", ping)
	mux.HandleFunc("HEAD /_ping", ping)

	// List endpoints — return bare JSON arrays (except volumes).
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Nodes)
	})
	mux.HandleFunc("GET /services", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Services)
	})
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Tasks)
	})
	mux.HandleFunc("GET /configs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Configs)
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Secrets)
	})
	mux.HandleFunc("GET /networks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Networks)
	})
	mux.HandleFunc("GET /volumes", func(w http.ResponseWriter, r *http.Request) {
		vols := make([]*volume.Volume, len(ds.Volumes))
		for i := range ds.Volumes {
			vols[i] = &ds.Volumes[i]
		}
		writeJSON(w, volume.ListResponse{Volumes: vols})
	})

	// Inspect endpoints.
	mux.HandleFunc("GET /nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := ds.nodesByID[r.PathValue("id")]; ok {
			writeJSON(w, n)
		} else {
			writeDockerError(w, http.StatusNotFound, "node not found")
		}
	})
	mux.HandleFunc("GET /services/{id}", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := ds.servicesByID[r.PathValue("id")]; ok {
			writeJSON(w, s)
		} else {
			writeDockerError(w, http.StatusNotFound, "service not found")
		}
	})
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if t, ok := ds.tasksByID[r.PathValue("id")]; ok {
			writeJSON(w, t)
		} else {
			writeDockerError(w, http.StatusNotFound, "task not found")
		}
	})
	mux.HandleFunc("GET /configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if c, ok := ds.configsByID[r.PathValue("id")]; ok {
			writeJSON(w, c)
		} else {
			writeDockerError(w, http.StatusNotFound, "config not found")
		}
	})
	mux.HandleFunc("GET /secrets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := ds.secretsByID[r.PathValue("id")]; ok {
			writeJSON(w, s)
		} else {
			writeDockerError(w, http.StatusNotFound, "secret not found")
		}
	})
	mux.HandleFunc("GET /networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if n, ok := ds.networksByID[r.PathValue("id")]; ok {
			writeJSON(w, n)
		} else {
			writeDockerError(w, http.StatusNotFound, "network not found")
		}
	})
	mux.HandleFunc("GET /volumes/{name}", func(w http.ResponseWriter, r *http.Request) {
		if v, ok := ds.volumesByName[r.PathValue("name")]; ok {
			writeJSON(w, v)
		} else {
			writeDockerError(w, http.StatusNotFound, "volume not found")
		}
	})

	// System endpoints.
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.buildInfo())
	})
	mux.HandleFunc("GET /swarm", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.Swarm)
	})
	mux.HandleFunc("GET /swarm/unlockkey", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"UnlockKey": ""})
	})
	mux.HandleFunc("GET /system/df", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ds.buildDiskUsage())
	})

	// Events — newline-delimited JSON stream.
	if sim != nil {
		mux.HandleFunc("GET /events", sim.HandleEvents)
	}

	// Logs — return canned log lines in Docker multiplexed format.
	mux.HandleFunc("GET /services/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		ds.handleLogs(w, r)
	})
	mux.HandleFunc("GET /tasks/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		ds.handleLogs(w, r)
	})

	// Plugins — empty list is fine for demo.
	mux.HandleFunc("GET /plugins", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []types.Plugin{})
	})

	// Version prefix stripping middleware.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /v1.XX/ prefix — the Docker SDK always sends versioned paths.
		path := r.URL.Path
		if len(path) > 4 && path[0] == '/' && path[1] == 'v' {
			if idx := strings.IndexByte(path[2:], '/'); idx > 0 {
				r.URL.Path = path[2+idx:]
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// buildInfo returns a system.Info response with realistic swarm metadata.
func (ds *Dataset) buildInfo() system.Info {
	managers := 0
	for _, n := range ds.Nodes {
		if n.Spec.Role == swarm.NodeRoleManager {
			managers++
		}
	}
	return system.Info{
		ID:             ds.Swarm.ID,
		Name:           ds.Nodes[0].Description.Hostname,
		ServerVersion:  "27.5.1",
		OperatingSystem: "Docker Desktop",
		OSType:         "linux",
		Architecture:   "x86_64",
		NCPU:           8,
		MemTotal:       16 * 1024 * 1024 * 1024,
		Swarm: swarm.Info{
			NodeID: ds.Nodes[0].ID,
			LocalNodeState: swarm.LocalNodeStateActive,
			ControlAvailable: true,
			Nodes:    len(ds.Nodes),
			Managers: managers,
			Cluster: &swarm.ClusterInfo{
				ID: ds.Swarm.ID,
			},
		},
	}
}

// buildDiskUsage returns a synthetic DiskUsage response.
func (ds *Dataset) buildDiskUsage() types.DiskUsage {
	return types.DiskUsage{
		LayersSize: 4 * 1024 * 1024 * 1024, // 4 GiB
	}
}

// handleLogs writes canned log lines in Docker's multiplexed stream format.
func (ds *Dataset) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Docker multiplexed log format: 8-byte header + payload per frame.
	// Header: [stream_type(1), 0, 0, 0, size_big_endian(4)]
	// stream_type: 1=stdout, 2=stderr
	lines := []string{
		"2026-04-01T10:00:00.000000000Z Starting application...",
		"2026-04-01T10:00:01.000000000Z Listening on :8080",
		"2026-04-01T10:00:05.123456789Z GET /health 200 1ms",
		"2026-04-01T10:00:10.234567890Z GET /api/v1/products 200 45ms",
		"2026-04-01T10:00:15.345678901Z POST /api/v1/orders 201 120ms",
		"2026-04-01T10:00:20.456789012Z GET /api/v1/products?category=electronics 200 38ms",
		"2026-04-01T10:00:25.567890123Z GET /health 200 1ms",
	}

	w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
	for _, line := range lines {
		payload := []byte(line + "\n")
		header := [8]byte{1, 0, 0, 0, 0, 0, 0, 0} // stdout
		size := uint32(len(payload))
		header[4] = byte(size >> 24)
		header[5] = byte(size >> 16)
		header[6] = byte(size >> 8)
		header[7] = byte(size)
		w.Write(header[:])
		w.Write(payload)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode failed", "error", err)
	}
}

func writeDockerError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"message": message})
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/demo/ -v -count=1
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/demo/docker.go internal/demo/docker_test.go
git commit -m "feat(demo): implement Docker Engine API handlers with list, inspect, info, logs"
```

---

### Task 5: Event Simulator

**Files:**
- Create: `internal/demo/simulator.go`
- Test: `internal/demo/simulator_test.go`

- [ ] **Step 1: Write test for the simulator**

```go
// internal/demo/simulator_test.go
package demo_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"

	"github.com/radiergummi/cetacean/internal/demo"
)

func TestSimulatorEmitsEvents(t *testing.T) {
	ds := demo.BuildDataset()
	sim := demo.NewSimulator(ds)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go sim.Run(ctx)

	// The simulator should emit at least one event within 3 seconds.
	handler := demo.NewDockerHandler(ds, sim)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1.46/events", nil)
	req = req.WithContext(ctx)

	go handler.ServeHTTP(rec, req)

	// Wait for context to expire, then check we got some events.
	<-ctx.Done()

	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected at least one event, got empty body")
	}

	// Verify first line is valid JSON event.
	lines := splitLines(body)
	if len(lines) == 0 {
		t.Fatal("no event lines")
	}

	var msg events.Message
	if err := json.Unmarshal([]byte(lines[0]), &msg); err != nil {
		t.Fatalf("invalid event JSON: %v", err)
	}
	if msg.Type == "" {
		t.Fatal("event has empty Type")
	}
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
```

(Add `"strings"` to the import.)

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/demo/ -run TestSimulator -v -count=1
```

Expected: fails (NewSimulator doesn't exist).

- [ ] **Step 3: Implement the simulator**

```go
// internal/demo/simulator.go
package demo

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
)

// Simulator generates fake Docker events to make the demo feel alive.
type Simulator struct {
	ds        *Dataset
	mu        sync.RWMutex
	listeners []chan events.Message
}

func NewSimulator(ds *Dataset) *Simulator {
	return &Simulator{ds: ds}
}

// Run generates events until ctx is cancelled. Call from a goroutine.
func (s *Simulator) Run(ctx context.Context) {
	// Emit a task state churn event every 5-15 seconds.
	for {
		delay := 5*time.Second + time.Duration(rand.IntN(10))*time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			s.emitTaskChurn()
		}
	}
}

// emitTaskChurn picks a random running task and emits a container event for it,
// simulating the kind of churn the watcher sees in a real swarm.
func (s *Simulator) emitTaskChurn() {
	ds := s.ds
	if len(ds.Tasks) == 0 {
		return
	}

	task := ds.Tasks[rand.IntN(len(ds.Tasks))]
	svc, ok := ds.servicesByID[task.ServiceID]
	if !ok {
		return
	}

	msg := events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{
			ID: task.Status.ContainerStatus.ContainerID,
			Attributes: map[string]string{
				"com.docker.swarm.task.id":      task.ID,
				"com.docker.swarm.service.name": svc.Spec.Name,
			},
		},
		Time:     time.Now().Unix(),
		TimeNano: time.Now().UnixNano(),
	}

	s.broadcast(msg)
}

func (s *Simulator) broadcast(msg events.Message) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.listeners {
		select {
		case ch <- msg:
		default:
			// Drop if listener is slow.
		}
	}
}

func (s *Simulator) subscribe() chan events.Message {
	ch := make(chan events.Message, 64)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	return ch
}

func (s *Simulator) unsubscribe(ch chan events.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.listeners {
		if l == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

// HandleEvents implements the Docker GET /events streaming endpoint.
// Writes newline-delimited JSON.
func (s *Simulator) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	enc := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if err := enc.Encode(msg); err != nil {
				slog.Debug("event stream write failed", "error", err)
				return
			}
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/demo/ -v -count=1
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/demo/simulator.go internal/demo/simulator_test.go
git commit -m "feat(demo): add event simulator with task churn and streaming endpoint"
```

---

### Task 6: Prometheus Mock

**Files:**
- Create: `internal/demo/prometheus.go`
- Create: `internal/demo/timeseries.go`
- Test: `internal/demo/prometheus_test.go`

- [ ] **Step 1: Write tests for the Prometheus mock**

```go
// internal/demo/prometheus_test.go
package demo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/demo"
)

func TestPromInstantQuery(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewPrometheusHandler(ds)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp promResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("expected status success, got %q", resp.Status)
	}
}

func TestPromRangeQuery(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewPrometheusHandler(ds)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/api/v1/query_range?query=container_cpu_usage_seconds_total&start=1712300000&end=1712400000&step=60",
		nil,
	)
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp promResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("expected status success, got %q", resp.Status)
	}
	if resp.Data.ResultType != "matrix" {
		t.Fatalf("expected matrix, got %q", resp.Data.ResultType)
	}
}

func TestPromLabels(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewPrometheusHandler(ds)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/labels", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one label")
	}
}

func TestPromLabelValues(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewPrometheusHandler(ds)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/label/job/values", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestPromTargets(t *testing.T) {
	ds := demo.BuildDataset()
	handler := demo.NewPrometheusHandler(ds)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/targets", nil))

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// promResponse mirrors the Prometheus API response envelope.
type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string            `json:"resultType"`
		Result     []json.RawMessage `json:"result"`
	} `json:"data"`
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/demo/ -run TestProm -v -count=1
```

Expected: fails (NewPrometheusHandler doesn't exist).

- [ ] **Step 3: Implement time-series generator**

```go
// internal/demo/timeseries.go
package demo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// generateTimeSeries produces a slice of [timestamp, value] pairs.
// baseValue is the center, amplitude controls the sinusoidal swing,
// noiseFactor adds random jitter as a fraction of baseValue.
func generateTimeSeries(
	start, end time.Time,
	step time.Duration,
	baseValue float64,
	amplitude float64,
	noiseFactor float64,
) [][2]any {
	var points [][2]any
	// Use a fixed seed derived from baseValue for reproducible noise.
	r := rand.New(rand.NewPCG(uint64(baseValue*1000), uint64(amplitude*1000)))

	for t := start; !t.After(end); t = t.Add(step) {
		// Sinusoidal daily pattern.
		hours := t.Sub(start).Hours()
		sinValue := amplitude * math.Sin(2*math.Pi*hours/24.0)

		// Random walk noise.
		noise := (r.Float64() - 0.5) * 2 * noiseFactor * baseValue

		value := baseValue + sinValue + noise
		if value < 0 {
			value = 0
		}

		points = append(points, [2]any{
			float64(t.Unix()) + float64(t.Nanosecond())/1e9,
			fmt.Sprintf("%.4f", value),
		})
	}
	return points
}

// generateInstantValue returns a single value with some jitter.
func generateInstantValue(baseValue float64, jitterFraction float64) string {
	jitter := (rand.Float64() - 0.5) * 2 * jitterFraction * baseValue
	return fmt.Sprintf("%.4f", baseValue+jitter)
}
```

- [ ] **Step 4: Implement Prometheus handler**

```go
// internal/demo/prometheus.go
package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// NewPrometheusHandler returns an http.Handler implementing the subset of the
// Prometheus HTTP API that Cetacean queries.
func NewPrometheusHandler(ds *Dataset) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		result := ds.handleInstantQuery(query)
		writePromResponse(w, "vector", result)
	})

	mux.HandleFunc("POST /api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		query := r.FormValue("query")
		result := ds.handleInstantQuery(query)
		writePromResponse(w, "vector", result)
	})

	mux.HandleFunc("GET /api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		stepStr := r.URL.Query().Get("step")

		start := parsePromTime(startStr)
		end := parsePromTime(endStr)
		step := parsePromStep(stepStr)

		result := ds.handleRangeQuery(query, start, end, step)
		writePromResponse(w, "matrix", result)
	})

	mux.HandleFunc("POST /api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		query := r.FormValue("query")
		start := parsePromTime(r.FormValue("start"))
		end := parsePromTime(r.FormValue("end"))
		step := parsePromStep(r.FormValue("step"))

		result := ds.handleRangeQuery(query, start, end, step)
		writePromResponse(w, "matrix", result)
	})

	mux.HandleFunc("GET /api/v1/labels", func(w http.ResponseWriter, r *http.Request) {
		labels := []string{
			"__name__", "container_label_com_docker_stack_namespace",
			"container_label_com_docker_swarm_service_name",
			"container_label_com_docker_swarm_task_id",
			"id", "image", "instance", "job", "name", "node",
		}
		writeJSON(w, map[string]any{"status": "success", "data": labels})
	})

	mux.HandleFunc("GET /api/v1/label/{name}/values", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		values := ds.labelValues(name)
		writeJSON(w, map[string]any{"status": "success", "data": values})
	})

	mux.HandleFunc("GET /api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		targets := ds.buildTargets()
		writeJSON(w, map[string]any{"status": "success", "data": targets})
	})

	return mux
}

// handleInstantQuery pattern-matches on the PromQL query and returns
// appropriate fake results.
func (ds *Dataset) handleInstantQuery(query string) []any {
	now := time.Now()
	ts := float64(now.Unix())

	switch {
	case strings.Contains(query, "up{"):
		// Monitoring status detection — return UP for each node.
		var results []any
		for _, n := range ds.Nodes {
			results = append(results, map[string]any{
				"metric": map[string]string{
					"__name__": "up",
					"instance": n.Description.Hostname + ":9100",
					"job":      extractJob(query),
				},
				"value": [2]any{ts, "1"},
			})
		}
		return results

	case strings.Contains(query, "container_cpu"):
		return ds.instantPerService(ts, 15.0, 0.3)

	case strings.Contains(query, "container_memory"):
		return ds.instantPerService(ts, 256*1024*1024, 0.2)

	case strings.Contains(query, "node_filesystem"):
		var results []any
		for _, n := range ds.Nodes {
			results = append(results, map[string]any{
				"metric": map[string]string{
					"instance": n.Description.Hostname + ":9100",
				},
				"value": [2]any{ts, generateInstantValue(35.0, 0.1)},
			})
		}
		return results

	case strings.Contains(query, "node_memory"):
		var results []any
		for _, n := range ds.Nodes {
			results = append(results, map[string]any{
				"metric": map[string]string{
					"instance": n.Description.Hostname + ":9100",
				},
				"value": [2]any{ts, generateInstantValue(55.0, 0.1)},
			})
		}
		return results

	case strings.Contains(query, "changes(container_last_seen"):
		// Flaky service detection — return 0 for most, >5 for webshop_api.
		var results []any
		for _, svc := range ds.Services {
			val := "0"
			if strings.Contains(svc.Spec.Name, "api") {
				val = "3"
			}
			results = append(results, map[string]any{
				"metric": map[string]string{
					"container_label_com_docker_swarm_service_name": svc.Spec.Name,
				},
				"value": [2]any{ts, val},
			})
		}
		return results

	default:
		// Generic fallback — return one result per service.
		return ds.instantPerService(ts, 50.0, 0.3)
	}
}

func (ds *Dataset) instantPerService(ts float64, base float64, jitter float64) []any {
	var results []any
	for _, svc := range ds.Services {
		stack := svc.Spec.Labels["com.docker.stack.namespace"]
		results = append(results, map[string]any{
			"metric": map[string]string{
				"container_label_com_docker_swarm_service_name": svc.Spec.Name,
				"container_label_com_docker_stack_namespace":    stack,
			},
			"value": [2]any{ts, generateInstantValue(base, jitter)},
		})
	}
	return results
}

// handleRangeQuery returns time-series data for each service.
func (ds *Dataset) handleRangeQuery(query string, start, end time.Time, step time.Duration) []any {
	base := 50.0
	amplitude := 10.0
	noise := 0.1

	switch {
	case strings.Contains(query, "cpu"):
		base, amplitude, noise = 15.0, 8.0, 0.05
	case strings.Contains(query, "memory"):
		base, amplitude, noise = 256*1024*1024, 50*1024*1024, 0.03
	case strings.Contains(query, "filesystem") || strings.Contains(query, "disk"):
		base, amplitude, noise = 35.0, 5.0, 0.02
	case strings.Contains(query, "network") || strings.Contains(query, "transmit") || strings.Contains(query, "receive"):
		base, amplitude, noise = 1024*1024, 512*1024, 0.1
	}

	var results []any
	for i, svc := range ds.Services {
		stack := svc.Spec.Labels["com.docker.stack.namespace"]
		// Offset each service's curve slightly for visual variety.
		svcBase := base * (0.5 + float64(i%5)*0.2)
		points := generateTimeSeries(start, end, step, svcBase, amplitude, noise)

		results = append(results, map[string]any{
			"metric": map[string]string{
				"container_label_com_docker_swarm_service_name": svc.Spec.Name,
				"container_label_com_docker_stack_namespace":    stack,
			},
			"values": points,
		})
	}
	return results
}

func (ds *Dataset) labelValues(name string) []string {
	switch name {
	case "job":
		return []string{"cadvisor", "node-exporter", "prometheus"}
	case "container_label_com_docker_stack_namespace":
		return []string{"webshop", "monitoring", "infra"}
	case "container_label_com_docker_swarm_service_name":
		var names []string
		for _, svc := range ds.Services {
			names = append(names, svc.Spec.Name)
		}
		return names
	case "instance":
		var instances []string
		for _, n := range ds.Nodes {
			instances = append(instances, n.Description.Hostname+":9100")
		}
		return instances
	default:
		return []string{}
	}
}

func (ds *Dataset) buildTargets() map[string]any {
	var activeTargets []any
	for _, n := range ds.Nodes {
		activeTargets = append(activeTargets, map[string]any{
			"labels": map[string]string{
				"job":      "node-exporter",
				"instance": n.Description.Hostname + ":9100",
			},
			"health":    "up",
			"lastScrape": time.Now().Add(-15 * time.Second).Format(time.RFC3339),
		})
		activeTargets = append(activeTargets, map[string]any{
			"labels": map[string]string{
				"job":      "cadvisor",
				"instance": n.Description.Hostname + ":8080",
			},
			"health":    "up",
			"lastScrape": time.Now().Add(-15 * time.Second).Format(time.RFC3339),
		})
	}
	return map[string]any{
		"activeTargets":  activeTargets,
		"droppedTargets": []any{},
	}
}

func writePromResponse(w http.ResponseWriter, resultType string, result []any) {
	if result == nil {
		result = []any{}
	}
	writeJSON(w, map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": resultType,
			"result":     result,
		},
	})
}

func extractJob(query string) string {
	if strings.Contains(query, "node-exporter") || strings.Contains(query, "node_") {
		return "node-exporter"
	}
	if strings.Contains(query, "cadvisor") || strings.Contains(query, "container_") {
		return "cadvisor"
	}
	return "prometheus"
}

func parsePromTime(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	// Try unix timestamp (integer or float).
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec)
	}
	// Try RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now()
}

func parsePromStep(s string) time.Duration {
	if s == "" {
		return 60 * time.Second
	}
	// Try as plain seconds (integer or float).
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(f * float64(time.Second))
	}
	// Try as Go duration.
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return 60 * time.Second
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/demo/ -v -count=1
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/demo/prometheus.go internal/demo/timeseries.go internal/demo/prometheus_test.go
git commit -m "feat(demo): add Prometheus API mock with instant/range queries and time-series generator"
```

---

### Task 7: Integration Test — Full Round Trip

**Files:**
- Modify: `internal/demo/docker_test.go`

- [ ] **Step 1: Add integration-style tests that verify cross-references**

```go
// Add to docker_test.go

func TestTasksReferenceValidServices(t *testing.T) {
	ds := demo.BuildDataset()
	serviceIDs := make(map[string]bool)
	for _, s := range ds.Services {
		serviceIDs[s.ID] = true
	}
	for _, task := range ds.Tasks {
		if !serviceIDs[task.ServiceID] {
			t.Errorf("task %s references unknown service %s", task.ID, task.ServiceID)
		}
	}
}

func TestTasksReferenceValidNodes(t *testing.T) {
	ds := demo.BuildDataset()
	nodeIDs := make(map[string]bool)
	for _, n := range ds.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, task := range ds.Tasks {
		if task.NodeID != "" && !nodeIDs[task.NodeID] {
			t.Errorf("task %s references unknown node %s", task.ID, task.NodeID)
		}
	}
}

func TestServicesHaveStackLabels(t *testing.T) {
	ds := demo.BuildDataset()
	for _, svc := range ds.Services {
		stack := svc.Spec.Labels["com.docker.stack.namespace"]
		if stack == "" {
			t.Errorf("service %s (%s) has no stack label", svc.ID, svc.Spec.Name)
		}
	}
}

func TestConfigsReferencedByServices(t *testing.T) {
	ds := demo.BuildDataset()
	configIDs := make(map[string]bool)
	for _, c := range ds.Configs {
		configIDs[c.ID] = true
	}
	for _, svc := range ds.Services {
		for _, ref := range svc.Spec.TaskTemplate.ContainerSpec.Configs {
			if !configIDs[ref.ConfigID] {
				t.Errorf("service %s references unknown config %s", svc.Spec.Name, ref.ConfigID)
			}
		}
	}
}

func TestSecretsReferencedByServices(t *testing.T) {
	ds := demo.BuildDataset()
	secretIDs := make(map[string]bool)
	for _, s := range ds.Secrets {
		secretIDs[s.ID] = true
	}
	for _, svc := range ds.Services {
		for _, ref := range svc.Spec.TaskTemplate.ContainerSpec.Secrets {
			if !secretIDs[ref.SecretID] {
				t.Errorf("service %s references unknown secret %s", svc.Spec.Name, ref.SecretID)
			}
		}
	}
}

func TestUniqueIDs(t *testing.T) {
	ds := demo.BuildDataset()
	seen := make(map[string]string) // id -> description

	for _, n := range ds.Nodes {
		if prev, ok := seen[n.ID]; ok {
			t.Errorf("duplicate ID %s: node %s and %s", n.ID, n.Description.Hostname, prev)
		}
		seen[n.ID] = "node:" + n.Description.Hostname
	}
	for _, s := range ds.Services {
		if prev, ok := seen[s.ID]; ok {
			t.Errorf("duplicate ID %s: service %s and %s", s.ID, s.Spec.Name, prev)
		}
		seen[s.ID] = "service:" + s.Spec.Name
	}
	for _, task := range ds.Tasks {
		if prev, ok := seen[task.ID]; ok {
			t.Errorf("duplicate ID %s: task and %s", task.ID, prev)
		}
		seen[task.ID] = "task:" + task.ID
	}
}
```

- [ ] **Step 2: Run all tests**

```bash
go test ./internal/demo/ -v -count=1
```

Expected: all pass — the dataset is internally consistent.

- [ ] **Step 3: Commit**

```bash
git add internal/demo/docker_test.go
git commit -m "test(demo): add dataset consistency and cross-reference validation tests"
```

---

### Task 8: Makefile and Docker Compose for Demo

**Files:**
- Modify: `Makefile`
- Create: `compose.demo.yaml`

- [ ] **Step 1: Add build-demo target to Makefile**

Add after the existing `build` target:

```makefile
build-demo:
	go build -o cetacean-demo ./cmd/demo-server
```

- [ ] **Step 2: Create compose.demo.yaml**

```yaml
# compose.demo.yaml
# Run: docker compose -f compose.demo.yaml up
# Then open http://localhost:9000
#
# This starts the demo server (fake Docker + Prometheus APIs) alongside
# a real Cetacean instance pointed at it. No Docker swarm required.

services:
  demo-server:
    build:
      context: .
      dockerfile: Dockerfile
      target: builder
    command: ["/src/cetacean-demo", "-docker-addr=:2375", "-prom-addr=:9090"]
    expose:
      - "2375"
      - "9090"

  cetacean:
    build:
      context: .
    environment:
      CETACEAN_DOCKER_HOST: tcp://demo-server:2375
      CETACEAN_PROMETHEUS_URL: http://demo-server:9090
      CETACEAN_LISTEN_ADDR: ":9000"
      CETACEAN_OPERATIONS_LEVEL: "0"
      CETACEAN_RECOMMENDATIONS: "true"
    ports:
      - "9000:9000"
    depends_on:
      - demo-server
```

Note: This requires adding the demo binary build to the Dockerfile's builder stage. Add this line after the main binary build in the Dockerfile:

```dockerfile
RUN go build -o /src/cetacean-demo ./cmd/demo-server
```

- [ ] **Step 3: Verify Makefile target works**

```bash
make build-demo
```

Expected: produces `./cetacean-demo` binary.

- [ ] **Step 4: Commit**

```bash
git add Makefile compose.demo.yaml
git commit -m "feat(demo): add build-demo Makefile target and compose.demo.yaml"
```

---

### Task 9: Manual Smoke Test

This task verifies the entire chain works end-to-end.

- [ ] **Step 1: Build both binaries**

```bash
make build-demo
cd frontend && npm run build && cd ..
go build -o cetacean .
```

- [ ] **Step 2: Start the demo server**

```bash
./cetacean-demo &
```

Expected output:
```
level=INFO msg="docker API listening" addr=:2375
level=INFO msg="prometheus API listening" addr=:9090
```

- [ ] **Step 3: Start Cetacean pointed at the demo server**

```bash
CETACEAN_DOCKER_HOST=tcp://localhost:2375 \
CETACEAN_PROMETHEUS_URL=http://localhost:9090 \
CETACEAN_OPERATIONS_LEVEL=0 \
CETACEAN_RECOMMENDATIONS=true \
./cetacean &
```

Expected: Cetacean starts, performs full sync, reports nodes/services/tasks counts.

- [ ] **Step 4: Verify the dashboard**

Open `http://localhost:9000` in a browser and check:

1. **Nodes page** — 3 nodes visible (1 manager, 2 workers)
2. **Services page** — 11 services across 3 stacks
3. **Stacks page** — webshop, monitoring, infra stacks with correct service counts
4. **Service detail** — click a service, see tasks, cross-references
5. **Task detail** — click a task, see node reference, service reference
6. **Configs/Secrets** — present with stack labels and "used by" references
7. **Networks** — overlay networks with connected services
8. **Metrics** — charts render with fake data, monitoring status shows "healthy"
9. **Recommendations** — at least some recommendations appear (missing healthcheck, single replica)
10. **Activity feed** — events appear as the simulator emits them
11. **Search** — Cmd+K finds services/nodes by name

- [ ] **Step 5: Stop both processes and commit any fixes**

```bash
kill %1 %2
```

If any fixes were needed, commit them:
```bash
git add -A
git commit -m "fix(demo): address issues found during smoke test"
```

---

## Post-Implementation Notes

**Future enhancements (not in scope for v1):**
- Write operation support (scale, update image, env editing) — mutate the dataset in-memory
- More realistic event patterns (rolling updates, service failures with recovery)
- Canned log variety (nginx access logs, app error logs, different formats per service)
- Dockerfile demo stage for single-image deployment
- CI job that builds and tests the demo binary
