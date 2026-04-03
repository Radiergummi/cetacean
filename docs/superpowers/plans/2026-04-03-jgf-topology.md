# JGF Topology Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve topology data as JSON Graph Format hypergraphs with `urn:cetacean:` URNs, a unified `/topology` endpoint, and a frontend that consumes JGF directly.

**Architecture:** New `internal/api/jgf/` package defines JGF types and serialization. `topology.go` gains a new `HandleTopology` handler producing a multi-graph JGF document. The old handlers add deprecation headers and optionally serve JGF via content negotiation. Content negotiation is extended with a new `ContentTypeJGF` type. The frontend replaces its custom topology types and transform functions with JGF-aware equivalents.

**Tech Stack:** Go stdlib, `github.com/goccy/go-json`, React 19, TypeScript, ReactFlow, ELK

---

## File Structure

### Backend
- **Create:** `internal/api/jgf/jgf.go` — JGF types (`Document`, `Graph`, `Node`, `Edge`, `Hyperedge`, `Metadata`) and `URN(typ, id)` helper
- **Create:** `internal/api/jgf/jgf_test.go` — Unit tests for JGF serialization and URN formatting
- **Modify:** `internal/api/topology.go` — Add `HandleTopology` (unified endpoint), add `buildNetworkJGF` and `buildPlacementJGF` serialization functions
- **Create:** `internal/api/topology_jgf_test.go` — Tests for JGF topology handlers
- **Modify:** `internal/api/negotiate.go` — Add `ContentTypeJGF` and `application/vnd.jgf+json` to supported types, `.jgf` extension suffix
- **Modify:** `internal/api/dispatch.go` — Add `contentNegotiatedJGF` dispatch helper
- **Modify:** `internal/api/router.go` — Register unified `/topology` endpoint, add deprecation to old endpoints
- **Modify:** `internal/api/context.go` — Extend JSON-LD context with topology vocabulary terms

### Frontend
- **Modify:** `frontend/src/api/types.ts` — Add JGF types, keep old types for deprecated endpoint compatibility
- **Modify:** `frontend/src/api/client.ts` — Add `api.topology()` method with JGF accept header
- **Modify:** `frontend/src/lib/topologyTransform.ts` — Replace `buildLogicalFlow`/`buildPhysicalFlow` with JGF consumers
- **Modify:** `frontend/src/lib/topologyTransform.test.ts` — Update tests for new transform functions
- **Modify:** `frontend/src/pages/Topology.tsx` — Fetch from unified endpoint, pass JGF graphs to transforms

---

### Task 1: JGF Types Package

**Files:**
- Create: `internal/api/jgf/jgf.go`
- Create: `internal/api/jgf/jgf_test.go`

- [ ] **Step 1: Write failing tests for JGF types and URN helper**

Create `internal/api/jgf/jgf_test.go`:

```go
package jgf

import (
	"testing"

	json "github.com/goccy/go-json"
)

func TestURN(t *testing.T) {
	tests := []struct {
		typ, id, want string
	}{
		{"service", "abc123", "urn:cetacean:service:abc123"},
		{"node", "def456", "urn:cetacean:node:def456"},
		{"task", "ghi789", "urn:cetacean:task:ghi789"},
		{"network", "jkl012", "urn:cetacean:network:jkl012"},
	}
	for _, tt := range tests {
		if got := URN(tt.typ, tt.id); got != tt.want {
			t.Errorf("URN(%q, %q) = %q, want %q", tt.typ, tt.id, got, tt.want)
		}
	}
}

func TestDocument_MarshalJSON(t *testing.T) {
	doc := Document{
		Graphs: []Graph{
			{
				ID:       "test",
				Type:     "test-graph",
				Label:    "Test Graph",
				Directed: false,
				Metadata: Metadata{"@context": "/api/context.jsonld"},
				Nodes: map[string]Node{
					"urn:cetacean:service:svc1": {
						Label:    "webapp",
						Metadata: Metadata{"@context": "/api/context.jsonld", "kind": "service"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip Document
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(roundtrip.Graphs) != 1 {
		t.Fatalf("graphs=%d, want 1", len(roundtrip.Graphs))
	}
	g := roundtrip.Graphs[0]
	if g.ID != "test" {
		t.Errorf("id=%q, want %q", g.ID, "test")
	}
	if len(g.Nodes) != 1 {
		t.Errorf("nodes=%d, want 1", len(g.Nodes))
	}
	node, ok := g.Nodes["urn:cetacean:service:svc1"]
	if !ok {
		t.Fatal("missing node urn:cetacean:service:svc1")
	}
	if node.Label != "webapp" {
		t.Errorf("label=%q, want %q", node.Label, "webapp")
	}
}

func TestGraph_WithEdgesAndHyperedges(t *testing.T) {
	g := Graph{
		ID:       "network",
		Type:     "network-topology",
		Label:    "Network Topology",
		Directed: false,
		Metadata: Metadata{"@context": "/api/context.jsonld"},
		Nodes: map[string]Node{
			"urn:cetacean:service:svc1": {Label: "a", Metadata: Metadata{"@context": "/api/context.jsonld"}},
			"urn:cetacean:service:svc2": {Label: "b", Metadata: Metadata{"@context": "/api/context.jsonld"}},
		},
		Edges: []Edge{
			{
				Source:   "urn:cetacean:service:svc1",
				Target:   "urn:cetacean:service:svc2",
				Metadata: Metadata{"@context": "/api/context.jsonld"},
			},
		},
		Hyperedges: []Hyperedge{
			{
				Nodes:    []string{"urn:cetacean:service:svc1", "urn:cetacean:service:svc2"},
				Metadata: Metadata{"@context": "/api/context.jsonld", "kind": "stack", "name": "webapp"},
			},
		},
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip Graph
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(roundtrip.Edges) != 1 {
		t.Errorf("edges=%d, want 1", len(roundtrip.Edges))
	}
	if len(roundtrip.Hyperedges) != 1 {
		t.Errorf("hyperedges=%d, want 1", len(roundtrip.Hyperedges))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/jgf/ -v`
Expected: Compilation error — package doesn't exist.

- [ ] **Step 3: Implement JGF types**

Create `internal/api/jgf/jgf.go`:

```go
// Package jgf defines types for the JSON Graph Format (https://jsongraphformat.info/).
package jgf

// Document is a multi-graph JGF document.
type Document struct {
	Graphs []Graph `json:"graphs"`
}

// Graph is a single JGF graph or hypergraph.
type Graph struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Label      string          `json:"label"`
	Directed   bool            `json:"directed"`
	Metadata   Metadata        `json:"metadata"`
	Nodes      map[string]Node `json:"nodes"`
	Edges      []Edge          `json:"edges,omitempty"`
	Hyperedges []Hyperedge     `json:"hyperedges,omitempty"`
}

// Node is a JGF graph node.
type Node struct {
	Label    string   `json:"label"`
	Metadata Metadata `json:"metadata"`
}

// Edge is a pairwise relationship between two nodes.
type Edge struct {
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Metadata Metadata `json:"metadata"`
}

// Hyperedge is a group relationship connecting multiple nodes.
type Hyperedge struct {
	Nodes    []string `json:"nodes"`
	Metadata Metadata `json:"metadata"`
}

// Metadata is a JSON-LD annotated metadata object.
type Metadata map[string]any

// URN returns a cetacean URN for the given entity type and ID.
// Format: urn:cetacean:<type>:<id>
func URN(typ, id string) string {
	return "urn:cetacean:" + typ + ":" + id
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/jgf/ -v`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/jgf/
git commit -m "feat(jgf): add JSON Graph Format types package"
```

---

### Task 2: Content Negotiation — `ContentTypeJGF`

**Files:**
- Modify: `internal/api/negotiate.go`
- Modify: `internal/api/negotiate_test.go` (if exists, otherwise `internal/api/dispatch.go`)

- [ ] **Step 1: Write failing test**

Find where content negotiation tests live (grep for `TestParseAccept` or `TestNegotiate`). Add a test:

```go
func TestParseAccept_JGF(t *testing.T) {
	ct := parseAccept("application/vnd.jgf+json")
	if ct != ContentTypeJGF {
		t.Errorf("got %v, want ContentTypeJGF", ct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Expected: `ContentTypeJGF` undefined.

- [ ] **Step 3: Implement**

In `internal/api/negotiate.go`:

Add `ContentTypeJGF` to the `ContentType` enum:

```go
const (
	ContentTypeJSON ContentType = iota
	ContentTypeHTML
	ContentTypeSSE
	ContentTypeAtom
	ContentTypeJGF

	ContentTypeUnsupported ContentType = -1
)
```

Add the String case:

```go
case ContentTypeJGF:
	return "JGF"
```

Add to `supportedTypes`:

```go
{"application", "vnd.jgf+json", ContentTypeJGF},
```

Add `.jgf` extension suffix handling in the `negotiate` function, alongside `.json`, `.html`, `.atom`:

```go
} else if strings.HasSuffix(path, ".jgf") {
	ct = ContentTypeJGF
	path = strings.TrimSuffix(path, ".jgf")
	r.URL.Path = path
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run TestParseAccept -v`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/negotiate.go internal/api/negotiate_test.go
git commit -m "feat(api): add ContentTypeJGF for application/vnd.jgf+json"
```

---

### Task 3: JGF Topology Serialization Functions

**Files:**
- Modify: `internal/api/topology.go`
- Create: `internal/api/topology_jgf_test.go`

This task adds `buildNetworkJGF` and `buildPlacementJGF` functions that convert cache data into `jgf.Graph` instances. These are pure functions (no HTTP, no handlers) — they take filtered data and return a graph.

- [ ] **Step 1: Write failing tests**

Create `internal/api/topology_jgf_test.go`:

```go
package api

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/jgf"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestBuildNetworkJGF(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})

	replicas := uint64(2)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "webapp_api",
				Labels: map[string]string{"com.docker.stack.namespace": "webapp"},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "api:latest"},
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "net1", Aliases: []string{"api", "webapp_api"}},
				},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetService(swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "webapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "webapp"},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "web:latest"},
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "net1", Aliases: []string{"web"}},
				},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	services := c.ListServices()
	networks := c.ListNetworks()

	g := buildNetworkJGF(services, networks)

	if g.ID != "network" {
		t.Errorf("id=%q, want %q", g.ID, "network")
	}
	if g.Type != "network-topology" {
		t.Errorf("type=%q, want %q", g.Type, "network-topology")
	}

	// 2 service nodes
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%d, want 2", len(g.Nodes))
	}
	svc1Node, ok := g.Nodes[jgf.URN("service", "svc1")]
	if !ok {
		t.Fatal("missing node for svc1")
	}
	if svc1Node.Label != "webapp_api" {
		t.Errorf("label=%q, want %q", svc1Node.Label, "webapp_api")
	}

	// 1 edge (svc1 ↔ svc2 via net1)
	if len(g.Edges) != 1 {
		t.Fatalf("edges=%d, want 1", len(g.Edges))
	}
	edge := g.Edges[0]
	edgeNetworks, ok := edge.Metadata["networks"].([]any)
	if !ok {
		t.Fatal("edge missing networks metadata")
	}
	if len(edgeNetworks) != 1 {
		t.Fatalf("edge networks=%d, want 1", len(edgeNetworks))
	}
	// Check aliases are on the edge, not on nodes
	netMeta, ok := edgeNetworks[0].(map[string]any)
	if !ok {
		t.Fatal("network metadata not a map")
	}
	aliases, ok := netMeta["aliases"].(map[string]any)
	if !ok {
		t.Fatal("missing aliases in network edge metadata")
	}
	if _, ok := aliases[jgf.URN("service", "svc1")]; !ok {
		t.Error("missing aliases for svc1")
	}

	// 1 stack hyperedge
	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}
	he := g.Hyperedges[0]
	if he.Metadata["kind"] != "stack" {
		t.Errorf("hyperedge kind=%v, want stack", he.Metadata["kind"])
	}
	if he.Metadata["name"] != "webapp" {
		t.Errorf("hyperedge name=%v, want webapp", he.Metadata["name"])
	}
	if len(he.Nodes) != 2 {
		t.Errorf("hyperedge nodes=%d, want 2", len(he.Nodes))
	}
}

func TestBuildPlacementJGF(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "webapp"},
			Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptrUint64(2)}},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "webapp:latest"}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		NodeID:    "node1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	clusterNodes := c.ListNodes()
	services := c.ListServices()

	// Build lookup maps (same as handler does)
	svcNames := make(map[string]string)
	svcImages := make(map[string]string)
	for _, svc := range services {
		svcNames[svc.ID] = svc.Spec.Name
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			svcImages[svc.ID] = svc.Spec.TaskTemplate.ContainerSpec.Image
		}
	}
	readableServiceIDs := make(map[string]bool)
	for _, svc := range services {
		readableServiceIDs[svc.ID] = true
	}

	g := buildPlacementJGF(clusterNodes, c, svcNames, svcImages, readableServiceIDs)

	if g.ID != "placement" {
		t.Errorf("id=%q, want %q", g.ID, "placement")
	}

	// 1 node + 1 service = 2 graph nodes
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%d, want 2", len(g.Nodes))
	}

	nodeNode, ok := g.Nodes[jgf.URN("node", "node1")]
	if !ok {
		t.Fatal("missing cluster node")
	}
	if nodeNode.Label != "worker-1" {
		t.Errorf("label=%q, want %q", nodeNode.Label, "worker-1")
	}

	// 1 hyperedge (svc1 placed on node1)
	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}
	he := g.Hyperedges[0]
	if len(he.Nodes) != 2 {
		t.Errorf("hyperedge nodes=%d, want 2 (service + node)", len(he.Nodes))
	}
	tasks, ok := he.Metadata["tasks"].([]any)
	if !ok {
		t.Fatal("missing tasks in hyperedge metadata")
	}
	if len(tasks) != 1 {
		t.Errorf("tasks=%d, want 1", len(tasks))
	}
}

func ptrUint64(v uint64) *uint64 { return &v }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestBuildNetworkJGF|TestBuildPlacementJGF" -v`
Expected: Compilation error — `buildNetworkJGF` and `buildPlacementJGF` undefined.

- [ ] **Step 3: Implement the serialization functions**

Add to `internal/api/topology.go` (after the existing functions):

```go
func buildNetworkJGF(services []swarm.Service, networks []network.Summary) jgf.Graph {
	ctx := jgf.Metadata{"@context": jsonLDContext}

	// Build overlay set for filtering.
	overlaySet := make(map[string]struct{}, len(networks))
	for _, n := range networks {
		if n.Driver == "overlay" {
			overlaySet[n.ID] = struct{}{}
		}
	}

	// Build nodes, netServices (network→service list), aliases, and stacks.
	nodes := make(map[string]jgf.Node, len(services))
	netServices := make(map[string][]string)
	svcAliases := make(map[string]map[string][]string) // svcID → netID → aliases
	stacks := make(map[string][]string)                 // stackName → []svcURN

	for _, svc := range services {
		urn := jgf.URN("service", svc.ID)

		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
		var mode string
		if svc.Spec.Mode.Replicated != nil {
			mode = "replicated"
		} else if svc.Spec.Mode.Global != nil {
			mode = "global"
		}
		var ports []string
		if svc.Spec.EndpointSpec != nil {
			ports = formatPorts(svc.Spec.EndpointSpec.Ports)
		}
		var updateStatus string
		if svc.UpdateStatus != nil {
			updateStatus = string(svc.UpdateStatus.State)
		}

		meta := jgf.Metadata{
			"@context":  jsonLDContext,
			"kind":      "service",
			"replicas":  replicaCount(svc),
			"image":     image,
			"mode":      mode,
		}
		if len(ports) > 0 {
			meta["ports"] = ports
		}
		if updateStatus != "" {
			meta["updateStatus"] = updateStatus
		}

		nodes[urn] = jgf.Node{Label: svc.Spec.Name, Metadata: meta}

		// Collect network aliases per service.
		for _, na := range svc.Spec.TaskTemplate.Networks {
			if _, ok := overlaySet[na.Target]; ok && len(na.Aliases) > 0 {
				if svcAliases[svc.ID] == nil {
					svcAliases[svc.ID] = make(map[string][]string)
				}
				svcAliases[svc.ID][na.Target] = na.Aliases
			}
		}

		// Build netServices.
		for _, vip := range svc.Endpoint.VirtualIPs {
			if _, ok := overlaySet[vip.NetworkID]; ok {
				netServices[vip.NetworkID] = append(netServices[vip.NetworkID], svc.ID)
			}
		}

		// Collect stacks.
		if stack := svc.Spec.Labels["com.docker.stack.namespace"]; stack != "" {
			stacks[stack] = append(stacks[stack], urn)
		}
	}

	// Build network lookup.
	networkMap := make(map[string]network.Summary, len(networks))
	for _, n := range networks {
		networkMap[n.ID] = n
	}

	// Build edges.
	type edgeKey struct{ a, b string }
	edgeMap := make(map[edgeKey][]string)
	for netID, svcs := range netServices {
		for i := range svcs {
			for j := i + 1; j < len(svcs); j++ {
				a, b := svcs[i], svcs[j]
				if a > b {
					a, b = b, a
				}
				edgeMap[edgeKey{a, b}] = append(edgeMap[edgeKey{a, b}], netID)
			}
		}
	}

	edges := make([]jgf.Edge, 0, len(edgeMap))
	for k, netIDs := range edgeMap {
		srcURN := jgf.URN("service", k.a)
		tgtURN := jgf.URN("service", k.b)

		networkMetas := make([]any, 0, len(netIDs))
		for _, netID := range netIDs {
			n := networkMap[netID]
			netMeta := map[string]any{
				"id":     jgf.URN("network", netID),
				"name":   n.Name,
				"driver": n.Driver,
				"scope":  n.Scope,
			}

			// Per-endpoint aliases.
			aliases := make(map[string]any)
			if a := svcAliases[k.a][netID]; len(a) > 0 {
				aliases[srcURN] = a
			}
			if a := svcAliases[k.b][netID]; len(a) > 0 {
				aliases[tgtURN] = a
			}
			if len(aliases) > 0 {
				netMeta["aliases"] = aliases
			}

			networkMetas = append(networkMetas, netMeta)
		}

		edges = append(edges, jgf.Edge{
			Source: srcURN,
			Target: tgtURN,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"networks": networkMetas,
			},
		})
	}

	// Build stack hyperedges.
	hyperedges := make([]jgf.Hyperedge, 0, len(stacks))
	for name, members := range stacks {
		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: members,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"kind":     "stack",
				"name":     name,
			},
		})
	}

	return jgf.Graph{
		ID:         "network",
		Type:       "network-topology",
		Label:      "Network Topology",
		Directed:   false,
		Metadata:   ctx,
		Nodes:      nodes,
		Edges:      edges,
		Hyperedges: hyperedges,
	}
}

func buildPlacementJGF(
	clusterNodes []swarm.Node,
	c *cache.Cache,
	svcNames, svcImages map[string]string,
	readableServiceIDs map[string]bool,
) jgf.Graph {
	ctx := jgf.Metadata{"@context": jsonLDContext}

	nodes := make(map[string]jgf.Node)

	// Add cluster node entries.
	for _, n := range clusterNodes {
		nodes[jgf.URN("node", n.ID)] = jgf.Node{
			Label: n.Description.Hostname,
			Metadata: jgf.Metadata{
				"@context":     jsonLDContext,
				"kind":         "node",
				"role":         string(n.Spec.Role),
				"state":        string(n.Status.State),
				"availability": string(n.Spec.Availability),
			},
		}
	}

	// Build hyperedges: one per service, connecting service to all nodes with tasks.
	type svcPlacement struct {
		nodeURNs []string
		tasks    []map[string]any
	}
	placements := make(map[string]*svcPlacement)

	for _, n := range clusterNodes {
		nodeURN := jgf.URN("node", n.ID)
		for _, t := range c.ListTasksByNode(n.ID) {
			if !readableServiceIDs[t.ServiceID] {
				continue
			}

			svcURN := jgf.URN("service", t.ServiceID)
			p, ok := placements[svcURN]
			if !ok {
				p = &svcPlacement{}
				placements[svcURN] = p

				// Add service node (once).
				var image string
				if t.Spec.ContainerSpec != nil {
					image = stripImageDigest(t.Spec.ContainerSpec.Image)
				}
				if image == "" {
					image = svcImages[t.ServiceID]
				}
				nodes[svcURN] = jgf.Node{
					Label: svcNames[t.ServiceID],
					Metadata: jgf.Metadata{
						"@context": jsonLDContext,
						"kind":     "service",
						"image":    image,
					},
				}
			}

			// Track unique nodes in the hyperedge.
			found := false
			for _, existing := range p.nodeURNs {
				if existing == nodeURN {
					found = true
					break
				}
			}
			if !found {
				p.nodeURNs = append(p.nodeURNs, nodeURN)
			}

			taskImage := ""
			if t.Spec.ContainerSpec != nil {
				taskImage = stripImageDigest(t.Spec.ContainerSpec.Image)
			}
			if taskImage == "" {
				taskImage = svcImages[t.ServiceID]
			}

			p.tasks = append(p.tasks, map[string]any{
				"id":    jgf.URN("task", t.ID),
				"node":  nodeURN,
				"state": string(t.Status.State),
				"slot":  t.Slot,
				"image": taskImage,
			})
		}
	}

	hyperedges := make([]jgf.Hyperedge, 0, len(placements))
	for svcURN, p := range placements {
		heNodes := append([]string{svcURN}, p.nodeURNs...)
		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: heNodes,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"tasks":    p.tasks,
			},
		})
	}

	return jgf.Graph{
		ID:         "placement",
		Type:       "placement-topology",
		Label:      "Placement Topology",
		Directed:   false,
		Metadata:   ctx,
		Nodes:      nodes,
		Hyperedges: hyperedges,
	}
}
```

Add `"github.com/docker/docker/api/types/network"` and `"github.com/radiergummi/cetacean/internal/api/jgf"` to the imports of `topology.go`. Also add `"github.com/radiergummi/cetacean/internal/cache"`.

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestBuildNetworkJGF|TestBuildPlacementJGF" -v`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/topology.go internal/api/topology_jgf_test.go
git commit -m "feat(api): add JGF serialization functions for topology"
```

---

### Task 4: Unified `/topology` Handler and Router Registration

**Files:**
- Modify: `internal/api/topology.go`
- Modify: `internal/api/dispatch.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/topology_jgf_test.go`

- [ ] **Step 1: Write failing test for the unified handler**

Add to `internal/api/topology_jgf_test.go`:

```go
func TestHandleTopology_JGF(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker, Availability: swarm.NodeAvailabilityActive},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetTask(swarm.Task{
		ID: "task1", ServiceID: "svc1", NodeID: "node1",
		Slot:   1,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopology(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var doc jgf.Document
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc.Graphs) != 2 {
		t.Fatalf("graphs=%d, want 2", len(doc.Graphs))
	}
	if doc.Graphs[0].ID != "network" {
		t.Errorf("graph[0].id=%q, want %q", doc.Graphs[0].ID, "network")
	}
	if doc.Graphs[1].ID != "placement" {
		t.Errorf("graph[1].id=%q, want %q", doc.Graphs[1].ID, "placement")
	}
}
```

- [ ] **Step 2: Implement `HandleTopology`**

Add to `internal/api/topology.go`:

```go
func (h *Handlers) HandleTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	identity := auth.IdentityFromContext(r.Context())

	// Build network graph.
	services := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListServices(),
		func(s swarm.Service) string { return "service:" + s.Spec.Name },
	)
	networks := h.cache.ListNetworks()
	networkGraph := buildNetworkJGF(services, networks)

	// Build placement graph.
	clusterNodes := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListNodes(),
		nodeResource,
	)
	allServices := h.cache.ListServices()
	svcNames := make(map[string]string, len(allServices))
	svcImages := make(map[string]string, len(allServices))
	for _, svc := range allServices {
		svcNames[svc.ID] = svc.Spec.Name
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			svcImages[svc.ID] = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
	}
	readableServiceIDs := make(map[string]bool, len(services))
	for _, svc := range services {
		readableServiceIDs[svc.ID] = true
	}
	placementGraph := buildPlacementJGF(clusterNodes, h.cache, svcNames, svcImages, readableServiceIDs)

	doc := jgf.Document{
		Graphs: []jgf.Graph{networkGraph, placementGraph},
	}

	w.Header().Set("Content-Type", "application/vnd.jgf+json")
	writeCachedJSON(w, r, doc)
}
```

- [ ] **Step 3: Add deprecation headers to old handlers**

At the top of `HandleNetworkTopology` and `HandlePlacementTopology`, after the `requireAnyGrant` check, add:

```go
w.Header().Set("Deprecation", "true")
w.Header().Add("Link", `</topology>; rel="successor-version"`)
```

- [ ] **Step 4: Register unified endpoint in router**

In `internal/api/router.go`, add before the existing topology routes:

```go
// Unified topology (JGF only)
mux.HandleFunc("GET /topology", contentNegotiated(h.HandleTopology, nil, spa))
```

The dispatch via `contentNegotiated` means HTML gets SPA, and the default (JSON/JGF) path calls `HandleTopology` which sets its own `Content-Type`. Since JGF *is* the JSON representation for this endpoint, this works.

- [ ] **Step 5: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandleTopology|TestHandleNetworkTopology|TestHandlePlacementTopology" -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/topology.go internal/api/topology_jgf_test.go internal/api/router.go internal/api/dispatch.go
git commit -m "feat(api): add unified /topology endpoint serving JGF hypergraphs"
```

---

### Task 5: Extend JSON-LD Context

**Files:**
- Modify: `internal/api/context.go`

- [ ] **Step 1: Add topology vocabulary terms**

Extend the `jsonLDContextDoc` constant to include topology terms:

```go
const jsonLDContextDoc = `{
  "@context": {
    "@vocab": "urn:cetacean:",
    "items": {"@container": "@set"},
    "type": "urn:ietf:rfc:9457#type",
    "title": "urn:ietf:rfc:9457#title",
    "status": "urn:ietf:rfc:9457#status",
    "detail": "urn:ietf:rfc:9457#detail",
    "instance": "urn:ietf:rfc:9457#instance",
    "kind": "urn:cetacean:kind",
    "name": "urn:cetacean:name",
    "replicas": "urn:cetacean:replicas",
    "mode": "urn:cetacean:mode",
    "role": "urn:cetacean:role",
    "state": "urn:cetacean:state",
    "availability": "urn:cetacean:availability",
    "ports": {"@container": "@list"},
    "aliases": "urn:cetacean:aliases",
    "tasks": {"@container": "@list"},
    "slot": "urn:cetacean:slot",
    "image": "urn:cetacean:image",
    "updateStatus": "urn:cetacean:updateStatus",
    "driver": "urn:cetacean:driver",
    "scope": "urn:cetacean:scope",
    "networks": {"@container": "@list"}
  }
}`
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -v`
Expected: All pass (context document is only served, not parsed by tests, but check nothing breaks).

- [ ] **Step 3: Commit**

```bash
git add internal/api/context.go
git commit -m "feat(api): extend JSON-LD context with topology vocabulary terms"
```

---

### Task 6: Frontend JGF Types and API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add JGF types to `types.ts`**

```typescript
export interface JGFDocument {
  graphs: JGFGraph[];
}

export interface JGFGraph {
  id: string;
  type: string;
  label: string;
  directed: boolean;
  metadata: JGFMetadata;
  nodes: Record<string, JGFNode>;
  edges?: JGFEdge[];
  hyperedges?: JGFHyperedge[];
}

export type JGFMetadata = Record<string, unknown> & { "@context": string };

export interface JGFNode {
  label: string;
  metadata: JGFMetadata;
}

export interface JGFEdge {
  source: string;
  target: string;
  metadata: JGFMetadata;
}

export interface JGFHyperedge {
  nodes: string[];
  metadata: JGFMetadata;
}
```

- [ ] **Step 2: Add `api.topology()` to client**

Add a new `fetchJGF` helper (or reuse `fetchJSON` with a custom Accept header):

```typescript
async function fetchJGF<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(apiPath(path), {
    headers: { Accept: "application/vnd.jgf+json" },
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!res.ok) {
    if (res.status === 401 && res.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLogin();
    }
    await throwResponseError(res);
  }

  return res.json();
}
```

Add to the `api` object:

```typescript
topology: () => fetchJGF<JGFDocument>("/topology"),
```

Keep the old methods but mark them with comments:

```typescript
/** @deprecated Use api.topology() instead. */
topologyNetworks: () => fetchJSON<NetworkTopology>("/topology/networks").then(({ data }) => data),
/** @deprecated Use api.topology() instead. */
topologyPlacement: () => fetchJSON<PlacementTopology>("/topology/placement").then(({ data }) => data),
```

- [ ] **Step 3: Run TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat(frontend): add JGF types and unified topology API method"
```

---

### Task 7: Frontend Transform Functions

**Files:**
- Modify: `frontend/src/lib/topologyTransform.ts`
- Modify: `frontend/src/lib/topologyTransform.test.ts`

Replace `buildLogicalFlow(data: NetworkTopology)` with `networkGraphToReactFlow(graph: JGFGraph)` and `buildPhysicalFlow(data: PlacementTopology)` with `placementGraphToReactFlow(graph: JGFGraph)`.

- [ ] **Step 1: Write failing tests for the new transform functions**

Update `frontend/src/lib/topologyTransform.test.ts` to test `networkGraphToReactFlow` and `placementGraphToReactFlow` with JGF input data. The tests should verify:

For `networkGraphToReactFlow`:
- Service nodes are created from JGF nodes with `kind === "service"`
- Stack group nodes are created from hyperedges with `kind === "stack"`
- Edges are created from JGF edges with network metadata
- Stack parent-child relationships are set correctly

For `placementGraphToReactFlow`:
- Cluster node entries are created from JGF nodes with `kind === "node"`
- Tasks are extracted from hyperedge metadata
- Services are aggregated per node

Read the existing test file first to match its patterns, then rewrite the tests with JGF input structures.

- [ ] **Step 2: Implement the new transforms**

Rewrite `buildLogicalFlow` → `networkGraphToReactFlow` and `buildPhysicalFlow` → `placementGraphToReactFlow`. Keep the exported `hashColor` and `stripStackPrefix` helpers unchanged.

Key differences in the new transforms:
- Input is `JGFGraph` instead of `NetworkTopology`/`PlacementTopology`
- Nodes come from `graph.nodes` (Record keyed by URN)
- Stack membership comes from `graph.hyperedges` filtered by `kind === "stack"`
- Network aliases come from edge metadata `networks[].aliases[urn]` instead of node metadata `networkAliases`
- Service IDs are URNs (`urn:cetacean:service:svc1`)
- Placement tasks come from hyperedge metadata instead of nested `TopoClusterNode.Tasks`

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/lib/topologyTransform.test.ts`
Expected: All pass.

- [ ] **Step 4: Run TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/topologyTransform.ts frontend/src/lib/topologyTransform.test.ts
git commit -m "feat(frontend): rewrite topology transforms for JGF consumption"
```

---

### Task 8: Topology Page — Switch to Unified Endpoint

**Files:**
- Modify: `frontend/src/pages/Topology.tsx`

- [ ] **Step 1: Update the topology page**

Replace the dual `api.topologyNetworks()` / `api.topologyPlacement()` fetch with a single `api.topology()` call. Extract graphs by `id`:

```typescript
const fetchData = useCallback(async () => {
  if (initialLoadRef.current) {
    setLoading(true);
  }
  setError(null);
  try {
    const document = await api.topology();
    const networkGraph = document.graphs.find((g) => g.id === "network") ?? null;
    const placementGraph = document.graphs.find((g) => g.id === "placement") ?? null;
    setNetworkData(networkGraph);
    setPlacementData(placementGraph);
  } catch (error) {
    setError(getErrorMessage(error, "Failed to load topology"));
  } finally {
    setLoading(false);
    initialLoadRef.current = false;
  }
}, []);
```

Update state types from `NetworkTopology | null` and `PlacementTopology | null` to `JGFGraph | null`.

Update the view components to call `networkGraphToReactFlow(networkData)` and `placementGraphToReactFlow(placementData)` instead of `buildLogicalFlow` and `buildPhysicalFlow`.

Remove unused imports of `NetworkTopology`, `PlacementTopology`, `buildLogicalFlow`, `buildPhysicalFlow`.

- [ ] **Step 2: Run TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 3: Run all frontend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/Topology.tsx
git commit -m "feat(frontend): switch topology page to unified JGF endpoint"
```

---

### Task 9: Full Stack Verification

**Files:** None new.

- [ ] **Step 1: Run all Go tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`
Expected: All pass.

- [ ] **Step 2: Run all frontend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

- [ ] **Step 3: Lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: Clean (or pre-existing issues only).

- [ ] **Step 4: Format check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make fmt-check`
Expected: Clean.

- [ ] **Step 5: Build**

Run: `cd /Users/moritz/GolandProjects/cetacean && make build`
Expected: Builds successfully.
