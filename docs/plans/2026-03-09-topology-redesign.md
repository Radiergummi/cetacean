# Topology Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the D3 force-directed topology page with a card-based React Flow + dagre layout, with two views: logical (stacks) and physical (nodes).

**Architecture:** React Flow renders custom card nodes inside group containers. Dagre computes LR layout with compound nodes. Logical view shows service cards grouped by stack with colored network edges. Physical view shows task cards grouped by swarm node with hover-based service highlighting.

**Tech Stack:** React Flow (`@xyflow/react`), dagre, existing shadcn/Tailwind components.

---

### Task 1: Install dependencies

**Files:**
- Modify: `frontend/package.json`

**Step 1: Add new packages**

```bash
cd frontend && npm install @xyflow/react dagre && npm install -D @types/dagre
```

**Step 2: Remove D3 packages**

```bash
cd frontend && npm uninstall d3-force d3-drag d3-selection d3-zoom @types/d3-force @types/d3-drag @types/d3-selection @types/d3-zoom
```

**Step 3: Verify builds**

Run: `cd frontend && npx tsc --noEmit`
Expected: Only errors from Topology.tsx (which imports the removed D3 modules). This is expected — we'll rewrite it next.

**Step 4: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "deps: swap d3-force/drag/selection/zoom for @xyflow/react + dagre"
```

---

### Task 2: Enrich backend topology API

**Files:**
- Modify: `internal/api/topology.go:15-52` (struct definitions)
- Modify: `internal/api/topology.go:54-122` (HandleNetworkTopology)
- Modify: `internal/api/topology.go:124-157` (HandlePlacementTopology)
- Modify: `internal/api/topology_test.go`

**Step 1: Write failing tests for enriched fields**

Add to `internal/api/topology_test.go`:

```go
func TestHandleNetworkTopology_EnrichedFields(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "web_default", Driver: "overlay"})
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web",
				Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "nginx:1.25@sha256:abc123",
				},
			},
			EndpointSpec: &swarm.EndpointSpec{
				Ports: []swarm.PortConfig{
					{PublishedPort: 80, TargetPort: 8080, Protocol: "tcp"},
				},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
		UpdateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
	})

	h := NewHandlers(c, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/api/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp NetworkTopology
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(resp.Nodes))
	}
	n := resp.Nodes[0]
	if n.Image != "nginx:1.25" {
		t.Errorf("image=%q, want nginx:1.25", n.Image)
	}
	if n.Mode != "replicated" {
		t.Errorf("mode=%q, want replicated", n.Mode)
	}
	if len(n.Ports) != 1 || n.Ports[0] != "80:8080/tcp" {
		t.Errorf("ports=%v, want [80:8080/tcp]", n.Ports)
	}
	if n.UpdateStatus != "updating" {
		t.Errorf("updateStatus=%q, want updating", n.UpdateStatus)
	}
}

func TestHandlePlacementTopology_EnrichedFields(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-01"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker, Availability: swarm.NodeAvailabilityActive},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "nginx"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25@sha256:abc"},
			},
		},
	})
	c.SetTask(swarm.Task{
		ID: "t1", ServiceID: "svc1", NodeID: "n1", Slot: 1,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		Spec:   swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25@sha256:abc"}},
	})

	h := NewHandlers(c, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/api/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	var resp PlacementTopology
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Nodes[0].Availability != "active" {
		t.Errorf("availability=%q, want active", resp.Nodes[0].Availability)
	}
	if resp.Nodes[0].Tasks[0].Image != "nginx:1.25" {
		t.Errorf("image=%q, want nginx:1.25", resp.Nodes[0].Tasks[0].Image)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run "TestHandle.*Enriched" -v`
Expected: FAIL — fields don't exist on structs yet.

**Step 3: Update structs and handlers**

Update `TopoServiceNode`:

```go
type TopoServiceNode struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Stack        string   `json:"stack,omitempty"`
	Replicas     int      `json:"replicas"`
	Image        string   `json:"image"`
	Ports        []string `json:"ports,omitempty"`
	Mode         string   `json:"mode"`
	UpdateStatus string   `json:"updateStatus,omitempty"`
}
```

Update `TopoClusterNode` — add Availability:

```go
type TopoClusterNode struct {
	ID           string     `json:"id"`
	Hostname     string     `json:"hostname"`
	Role         string     `json:"role"`
	State        string     `json:"state"`
	Availability string     `json:"availability"`
	Tasks        []TopoTask `json:"tasks"`
}
```

Update `TopoTask` — add Image:

```go
type TopoTask struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	ServiceName string `json:"serviceName"`
	State       string `json:"state"`
	Slot        int    `json:"slot"`
	Image       string `json:"image"`
}
```

In `HandleNetworkTopology`, update the node construction loop to populate the new fields. Add a helper `stripImageDigest(image string) string` that strips the `@sha256:...` suffix. Add a helper `formatPorts(ports []swarm.PortConfig) []string` that formats as `"published:target/protocol"`. Derive mode as `"replicated"` or `"global"` from `svc.Spec.Mode`. Get update status from `svc.UpdateStatus.State` if non-nil.

In `HandlePlacementTopology`, populate `Availability` from `n.Spec.Availability` and task `Image` from `t.Spec.ContainerSpec.Image` (stripped of digest).

**Step 4: Run tests**

Run: `go test ./internal/api/ -run "TestHandle" -v`
Expected: All topology tests PASS (both old and new).

**Step 5: Commit**

```bash
git add internal/api/topology.go internal/api/topology_test.go
git commit -m "feat: enrich topology API with image, ports, mode, updateStatus"
```

---

### Task 3: Update TypeScript types and API client

**Files:**
- Modify: `frontend/src/api/types.ts:239-276`

**Step 1: Update `TopoServiceNode`**

```typescript
export interface TopoServiceNode {
  id: string;
  name: string;
  stack?: string;
  replicas: number;
  image: string;
  ports?: string[];
  mode: string;
  updateStatus?: string;
}
```

**Step 2: Update `TopoClusterNode`**

```typescript
export interface TopoClusterNode {
  id: string;
  hostname: string;
  role: string;
  state: string;
  availability: string;
  tasks: TopoTask[];
}
```

**Step 3: Update `TopoTask`**

```typescript
export interface TopoTask {
  id: string;
  serviceId: string;
  serviceName: string;
  state: string;
  slot: number;
  image: string;
}
```

**Step 4: Verify types compile**

Run: `cd frontend && npx tsc --noEmit`
Expected: Errors only from Topology.tsx (D3 imports removed in task 1). Types file itself compiles.

**Step 5: Commit**

```bash
git add frontend/src/api/types.ts
git commit -m "feat: update topology TypeScript types with enriched fields"
```

---

### Task 4: Build dagre layout utility

**Files:**
- Create: `frontend/src/lib/layoutDagre.ts`
- Create: `frontend/src/lib/layoutDagre.test.ts`

**Step 1: Write test**

```typescript
import { describe, it, expect } from "vitest";
import { computeLayout } from "./layoutDagre";
import type { Node, Edge } from "@xyflow/react";

describe("computeLayout", () => {
  it("assigns positions to nodes", () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = computeLayout(nodes, edges);

    expect(result.length).toBe(2);
    // In LR layout, first node should be left of second
    const posA = result.find((n) => n.id === "a")!.position;
    const posB = result.find((n) => n.id === "b")!.position;
    expect(posA.x).toBeLessThan(posB.x);
  });

  it("handles group nodes with children", () => {
    const nodes: Node[] = [
      { id: "group", position: { x: 0, y: 0 }, data: { label: "Stack" }, type: "group" },
      { id: "child", position: { x: 0, y: 0 }, data: {}, parentId: "group" },
    ];
    const result = computeLayout(nodes, []);
    expect(result.length).toBe(2);
    // Both should have valid positions
    for (const n of result) {
      expect(typeof n.position.x).toBe("number");
      expect(typeof n.position.y).toBe("number");
    }
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/lib/layoutDagre.test.ts`
Expected: FAIL — module doesn't exist.

**Step 3: Implement**

```typescript
import dagre from "dagre";
import type { Node, Edge } from "@xyflow/react";

const NODE_WIDTH = 240;
const NODE_HEIGHT = 120;
const GROUP_PADDING = 40;

export function computeLayout(
  nodes: Node[],
  edges: Edge[],
  direction: "LR" | "TB" = "LR",
): Node[] {
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({ rankdir: direction, ranksep: 80, nodesep: 40 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const node of nodes) {
    if (node.type === "group") {
      g.setNode(node.id, { width: NODE_WIDTH + GROUP_PADDING * 2, height: NODE_HEIGHT });
    } else {
      g.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    }
    if (node.parentId) {
      g.setParent(node.id, node.parentId);
    }
  }

  for (const edge of edges) {
    g.setEdge(edge.source, edge.target);
  }

  dagre.layout(g);

  return nodes.map((node) => {
    const pos = g.node(node.id);
    return {
      ...node,
      position: {
        x: pos.x - (node.type === "group" ? (NODE_WIDTH + GROUP_PADDING * 2) / 2 : NODE_WIDTH / 2),
        y: pos.y - NODE_HEIGHT / 2,
      },
    };
  });
}
```

**Step 4: Run test**

Run: `cd frontend && npx vitest run src/lib/layoutDagre.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/lib/layoutDagre.ts frontend/src/lib/layoutDagre.test.ts
git commit -m "feat: add dagre layout utility for React Flow"
```

---

### Task 5: Build custom React Flow node components

**Files:**
- Create: `frontend/src/components/topology/ServiceCardNode.tsx`
- Create: `frontend/src/components/topology/TaskCardNode.tsx`
- Create: `frontend/src/components/topology/GroupNode.tsx`
- Create: `frontend/src/components/topology/NetworkEdge.tsx`

These are React Flow custom node/edge components. Each receives data via the `data` prop.

**Step 1: Create ServiceCardNode**

A React Flow custom node. Renders a shadcn-style card with:
- Service name as header
- Mode badge (Replicated/Global) — use existing Badge component or a simple `<span>` with Tailwind
- Image tag
- Replicas with status dot (green if running === desired, yellow if partial, red if 0)
- Published ports
- Update status line if present
- `<Handle>` components on left and right for edge connections
- Click navigates to `/services/{id}`

**Step 2: Create TaskCardNode**

Compact card for physical view:
- `serviceName.slot` as title
- State with colored dot
- Image tag
- `<Handle>` components (not connected to edges, but needed for React Flow)
- `data.highlighted` prop controls a ring/glow CSS class
- `onMouseEnter`/`onMouseLeave` callbacks passed via data to trigger cross-card highlighting

**Step 3: Create GroupNode**

A container node for stacks (logical) and swarm nodes (physical):
- Renders a labeled bordered container
- For stack groups: just the stack name as header
- For node groups: hostname + role badge + state dot + availability
- Uses React Flow's built-in group node sizing (expands to fit children)

**Step 4: Create NetworkEdge**

A custom edge component:
- Colored line (color from `data.color`)
- On hover: shows tooltip with network name, driver, scope via `data.networkInfo`
- Uses `getBezierPath` or `getSmoothStepPath` from `@xyflow/react`

**Step 5: Verify all compile**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS (or only Topology.tsx errors from the old D3 imports being gone)

**Step 6: Commit**

```bash
git add frontend/src/components/topology/
git commit -m "feat: add custom React Flow nodes for topology (ServiceCard, TaskCard, Group, NetworkEdge)"
```

---

### Task 6: Build data transformation helpers

**Files:**
- Create: `frontend/src/lib/topologyTransform.ts`
- Create: `frontend/src/lib/topologyTransform.test.ts`

These transform API responses (`NetworkTopology`, `PlacementTopology`) into React Flow `Node[]` and `Edge[]` arrays.

**Step 1: Write tests**

```typescript
import { describe, it, expect } from "vitest";
import { buildLogicalFlow, buildPhysicalFlow } from "./topologyTransform";
import type { NetworkTopology, PlacementTopology } from "@/api/types";

describe("buildLogicalFlow", () => {
  it("creates group nodes for stacks and service nodes as children", () => {
    const data: NetworkTopology = {
      nodes: [
        { id: "s1", name: "web", stack: "app", replicas: 3, image: "nginx:1.25", mode: "replicated", ports: ["80:8080/tcp"] },
        { id: "s2", name: "api", stack: "app", replicas: 2, image: "node:20", mode: "replicated" },
        { id: "s3", name: "monitor", replicas: 1, image: "prom:latest", mode: "replicated" },
      ],
      edges: [{ source: "s1", target: "s2", networks: ["net1"] }],
      networks: [{ id: "net1", name: "app_net", driver: "overlay" }],
    };
    const { nodes, edges } = buildLogicalFlow(data);

    // Should have 1 group (app) + 3 service nodes
    const groups = nodes.filter((n) => n.type === "stackGroup");
    const services = nodes.filter((n) => n.type === "serviceCard");
    expect(groups.length).toBe(1);
    expect(services.length).toBe(3);
    expect(services.filter((n) => n.parentId === groups[0].id).length).toBe(2);
    expect(services.find((n) => n.id === "s3")?.parentId).toBeUndefined();

    // One edge per network per service pair
    expect(edges.length).toBe(1);
  });
});

describe("buildPhysicalFlow", () => {
  it("creates node groups with task children", () => {
    const data: PlacementTopology = {
      nodes: [
        {
          id: "n1", hostname: "worker-01", role: "worker", state: "ready", availability: "active",
          tasks: [
            { id: "t1", serviceId: "svc1", serviceName: "web", state: "running", slot: 1, image: "nginx:1.25" },
            { id: "t2", serviceId: "svc1", serviceName: "web", state: "running", slot: 2, image: "nginx:1.25" },
          ],
        },
        {
          id: "n2", hostname: "worker-02", role: "worker", state: "ready", availability: "active",
          tasks: [
            { id: "t3", serviceId: "svc1", serviceName: "web", state: "running", slot: 3, image: "nginx:1.25" },
          ],
        },
      ],
    };
    const { nodes } = buildPhysicalFlow(data);

    const groups = nodes.filter((n) => n.type === "nodeGroup");
    const tasks = nodes.filter((n) => n.type === "taskCard");
    expect(groups.length).toBe(2);
    expect(tasks.length).toBe(3);
    expect(tasks.filter((n) => n.parentId === "n1").length).toBe(2);
  });
});
```

**Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/topologyTransform.test.ts`
Expected: FAIL

**Step 3: Implement**

`buildLogicalFlow(data: NetworkTopology)` → `{ nodes: Node[], edges: Edge[] }`:
- Group services by `stack`. For each stack, create a group node with `type: "stackGroup"`.
- For each service, create a node with `type: "serviceCard"`, `parentId` set to the stack group (if any).
- For each edge in `data.edges`, for each network in `edge.networks`, create a React Flow edge with `type: "networkEdge"`, a unique ID, color from palette, and network metadata in `data`.
- Assign colors to networks from the 12-color palette using the same `hashString` approach.

`buildPhysicalFlow(data: PlacementTopology)` → `{ nodes: Node[], edges: Edge[] }`:
- For each cluster node, create a group node with `type: "nodeGroup"`.
- For each task, create a node with `type: "taskCard"`, `parentId` set to the node group.
- No edges.

**Step 4: Run tests**

Run: `cd frontend && npx vitest run src/lib/topologyTransform.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/lib/topologyTransform.ts frontend/src/lib/topologyTransform.test.ts
git commit -m "feat: add topology data transformation (API → React Flow nodes/edges)"
```

---

### Task 7: Rewrite Topology.tsx

**Files:**
- Modify: `frontend/src/pages/Topology.tsx` (full rewrite)

**Step 1: Rewrite the page**

Replace the entire file. The new structure:

```
Topology (default export)
├── State: view toggle ("logical" | "physical"), fetched data, loading, error
├── SSE subscription (same debounced refetch pattern)
├── SegmentedControl for view toggle (reuse existing component)
├── LogicalView
│   └── ReactFlow with:
│       - nodes/edges from buildLogicalFlow()
│       - positions from computeLayout()
│       - nodeTypes: { stackGroup: GroupNode, serviceCard: ServiceCardNode }
│       - edgeTypes: { networkEdge: NetworkEdge }
│       - NetworkLegend overlay panel
│       - fitView on initial load
└── PhysicalView
    └── ReactFlow with:
        - nodes from buildPhysicalFlow()
        - positions from computeLayout()
        - nodeTypes: { nodeGroup: GroupNode, taskCard: TaskCardNode }
        - Hover state: hoveredServiceId, passed to TaskCardNode via data
        - fitView on initial load
```

Key implementation details:
- Import `ReactFlow, ReactFlowProvider, useNodesState, useEdgesState, Background` from `@xyflow/react`
- Import `@xyflow/react/dist/style.css` for base styles
- Register `nodeTypes` and `edgeTypes` as stable objects outside the component (React Flow requirement)
- Both views share the same data-fetching logic from the parent `Topology` component
- `fitView` prop on `<ReactFlow>` for auto-zoom on load
- Use `proOptions={{ hideAttribution: true }}` (React Flow allows this for open-source)
- Physical view hover: store `hoveredServiceId` in state, pass to each TaskCardNode via data, TaskCardNode applies a highlight class when its serviceId matches

**Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

**Step 3: Verify it renders**

Run: `cd frontend && npm run dev`
Open browser, navigate to `/topology`. Both views should render with cards and edges. (Manual verification.)

**Step 4: Run all frontend tests**

Run: `cd frontend && npx vitest run`
Expected: All tests PASS. (The old Topology.tsx had no tests — only the new layout/transform tests matter.)

**Step 5: Run lint and format**

Run: `cd frontend && npm run lint && npm run fmt`

**Step 6: Commit**

```bash
git add frontend/src/pages/Topology.tsx
git commit -m "feat: rewrite topology page with React Flow card-based layout"
```

---

### Task 8: Final cleanup and verify

**Files:**
- Verify: no remaining d3 imports anywhere
- Run: full backend + frontend test suite

**Step 1: Verify no D3 remnants**

Search for any remaining d3 imports in the frontend. There should be none.

**Step 2: Run full test suite**

Run: `go test ./... && cd frontend && npx vitest run`
Expected: All PASS.

**Step 3: Run lint + format check**

Run: `make check`
Expected: PASS

**Step 4: Commit any cleanup**

```bash
git add -A
git commit -m "chore: remove unused d3 dependencies and clean up"
```
