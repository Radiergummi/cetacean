package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/jgf"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestBuildNetworkJGF(t *testing.T) {
	replicas := uint64(2)
	services := []swarm.Service{
		{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   "web",
					Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
				},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25@sha256:abc"},
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: "net1", Aliases: []string{"web"}},
					},
				},
			},
			Endpoint: swarm.Endpoint{
				VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
			},
		},
		{
			ID: "svc2",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   "api",
					Labels: map[string]string{"com.docker.stack.namespace": "mystack"},
				},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: "api:latest"},
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: "net1", Aliases: []string{"api"}},
					},
				},
			},
			Endpoint: swarm.Endpoint{
				VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
			},
		},
	}
	networks := []network.Summary{
		{ID: "net1", Name: "mystack_default", Driver: "overlay", Scope: "swarm"},
	}

	g := buildNetworkJGF(services, networks, "/api/context.jsonld")

	// 2 service nodes
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%d, want 2", len(g.Nodes))
	}

	svc1URN := jgf.URN("service", "svc1")
	svc2URN := jgf.URN("service", "svc2")

	if _, ok := g.Nodes[svc1URN]; !ok {
		t.Errorf("missing node %s", svc1URN)
	}

	if _, ok := g.Nodes[svc2URN]; !ok {
		t.Errorf("missing node %s", svc2URN)
	}

	// 1 edge between the two services sharing net1
	if len(g.Edges) != 1 {
		t.Fatalf("edges=%d, want 1", len(g.Edges))
	}

	// Round-trip through JSON to test the actual wire format.
	edgeBytes, err := json.Marshal(g.Edges[0])
	if err != nil {
		t.Fatalf("marshal edge: %v", err)
	}

	var edge jgf.Edge
	if err := json.Unmarshal(edgeBytes, &edge); err != nil {
		t.Fatalf("unmarshal edge: %v", err)
	}

	if edge.Source >= edge.Target {
		t.Errorf("edge source %q should be < target %q", edge.Source, edge.Target)
	}

	nets, ok := edge.Metadata["networks"].([]any)
	if !ok {
		t.Fatalf("edge metadata 'networks' missing or wrong type")
	}

	if len(nets) != 1 {
		t.Fatalf("edge networks=%d, want 1", len(nets))
	}

	netEntry, ok := nets[0].(map[string]any)
	if !ok {
		t.Fatal("network entry not a map")
	}

	if netEntry["name"] != "mystack_default" {
		t.Errorf("network name=%v, want mystack_default", netEntry["name"])
	}

	// Aliases should be present on the network entry
	aliases, ok := netEntry["aliases"].(map[string]any)
	if !ok {
		t.Fatal("aliases missing or wrong type")
	}

	if len(aliases) != 2 {
		t.Errorf("aliases count=%d, want 2", len(aliases))
	}

	// 1 stack hyperedge
	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}

	he := g.Hyperedges[0]
	if he.Metadata["kind"] != "stack" {
		t.Errorf("hyperedge kind=%v, want stack", he.Metadata["kind"])
	}

	if he.Metadata["name"] != "mystack" {
		t.Errorf("hyperedge name=%v, want mystack", he.Metadata["name"])
	}

	if len(he.Nodes) != 2 {
		t.Errorf("hyperedge nodes=%d, want 2", len(he.Nodes))
	}

	// Graph-level metadata
	if g.Metadata["@context"] != jsonLDContext {
		t.Errorf("graph @context=%v, want %s", g.Metadata["@context"], jsonLDContext)
	}
}

func TestBuildPlacementJGF(t *testing.T) {
	c := cache.New(nil)
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
		ID:        "t1",
		ServiceID: "svc1",
		NodeID:    "n1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		Spec: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25@sha256:abc"},
		},
	})

	clusterNodes := []swarm.Node{
		{
			ID:          "n1",
			Description: swarm.NodeDescription{Hostname: "worker-01"},
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		},
	}

	svcNames := map[string]string{"svc1": "nginx"}
	svcImages := map[string]string{"svc1": "nginx:1.25"}
	readableServiceIDs := map[string]bool{"svc1": true}

	g := buildPlacementJGF(
		clusterNodes,
		c,
		svcNames,
		svcImages,
		readableServiceIDs,
		"/api/context.jsonld",
	)

	// 2 graph nodes: 1 cluster node + 1 service
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes=%d, want 2", len(g.Nodes))
	}

	nodeURN := jgf.URN("node", "n1")
	svcURN := jgf.URN("service", "svc1")

	if _, ok := g.Nodes[nodeURN]; !ok {
		t.Errorf("missing node %s", nodeURN)
	}

	if _, ok := g.Nodes[svcURN]; !ok {
		t.Errorf("missing node %s", svcURN)
	}

	// No edges
	if len(g.Edges) != 0 {
		t.Errorf("edges=%d, want 0", len(g.Edges))
	}

	// 1 hyperedge for the service
	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}

	he := g.Hyperedges[0]

	if he.Metadata["kind"] != "placement" {
		t.Errorf("hyperedge kind=%v, want placement", he.Metadata["kind"])
	}

	// Service URN should be first
	if he.Nodes[0] != svcURN {
		t.Errorf("hyperedge nodes[0]=%s, want %s", he.Nodes[0], svcURN)
	}

	if he.Nodes[1] != nodeURN {
		t.Errorf("hyperedge nodes[1]=%s, want %s", he.Nodes[1], nodeURN)
	}

	tasksRaw, ok := he.Metadata["tasks"]
	if !ok {
		t.Fatal("tasks missing from hyperedge metadata")
	}
	tasks, ok := tasksRaw.([]map[string]any)
	if !ok {
		t.Fatalf("tasks type=%T, want []map[string]any", tasksRaw)
	}

	if len(tasks) != 1 {
		t.Fatalf("tasks=%d, want 1", len(tasks))
	}

	task := tasks[0]
	if task["id"] != jgf.URN("task", "t1") {
		t.Errorf("task id=%v, want %s", task["id"], jgf.URN("task", "t1"))
	}

	if task["state"] != "running" {
		t.Errorf("task state=%v, want running", task["state"])
	}
}

func TestHandleTopology_JGF(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(
		network.Summary{ID: "net1", Name: "web_default", Driver: "overlay", Scope: "swarm"},
	)
	c.SetNode(swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-01"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "nginx"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		NodeID:    "n1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopology(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.jgf+json" {
		t.Errorf("Content-Type=%q, want application/vnd.jgf+json", ct)
	}

	var doc jgf.Document
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(doc.Graphs) != 2 {
		t.Fatalf("graphs=%d, want 2", len(doc.Graphs))
	}

	if doc.Graphs[0].ID != "network" {
		t.Errorf("graph[0].id=%q, want network", doc.Graphs[0].ID)
	}

	if doc.Graphs[1].ID != "placement" {
		t.Errorf("graph[1].id=%q, want placement", doc.Graphs[1].ID)
	}
}

func TestBuildNetworkJGF_EmptyInput(t *testing.T) {
	g := buildNetworkJGF(nil, nil, "/api/context.jsonld")

	if g.ID != "network" {
		t.Errorf("id=%q, want network", g.ID)
	}
	if len(g.Nodes) != 0 {
		t.Errorf("nodes=%d, want 0", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("edges=%d, want 0", len(g.Edges))
	}
	if len(g.Hyperedges) != 0 {
		t.Errorf("hyperedges=%d, want 0", len(g.Hyperedges))
	}
}

func TestBuildPlacementJGF_EmptyInput(t *testing.T) {
	c := cache.New(nil)
	g := buildPlacementJGF(nil, c, nil, nil, nil, "/api/context.jsonld")

	if g.ID != "placement" {
		t.Errorf("id=%q, want placement", g.ID)
	}
	if len(g.Nodes) != 0 {
		t.Errorf("nodes=%d, want 0", len(g.Nodes))
	}
	if len(g.Hyperedges) != 0 {
		t.Errorf("hyperedges=%d, want 0", len(g.Hyperedges))
	}
}

func TestHandleTopology_ETag304(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(
		network.Summary{ID: "net1", Name: "web_default", Driver: "overlay", Scope: "swarm"},
	)
	c.SetNode(swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-01"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "nginx"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		NodeID:    "n1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	h := newTestHandlers(t, withCache(c))

	// First request — get ETag.
	req1 := httptest.NewRequest("GET", "/topology", nil)
	w1 := httptest.NewRecorder()
	h.HandleTopology(w1, req1)

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header")
	}
	if w1.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache")
	}

	// Second request with If-None-Match.
	req2 := httptest.NewRequest("GET", "/topology", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.HandleTopology(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("expected empty body on 304, got %d bytes", w2.Body.Len())
	}
}

func TestBuildNetworkJGF_IsolatedService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "standalone"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "app:latest"}},
		},
		// No VIPs.
	})

	g := buildNetworkJGF(c.ListServices(), c.ListNetworks(), jsonLDContext)

	if len(g.Nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("edges=%d, want 0 (no overlay networks)", len(g.Edges))
	}
	if len(g.Hyperedges) != 0 {
		t.Errorf("hyperedges=%d, want 0 (no stack)", len(g.Hyperedges))
	}
}

func TestBuildPlacementJGF_TaskImageFallback(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations:  swarm.Annotations{Name: "webapp"},
			TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "webapp:v2"}},
		},
	})
	// Task with no ContainerSpec — should fall back to service image.
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		NodeID:    "node1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		Spec:      swarm.TaskSpec{}, // No ContainerSpec.
	})

	svcNames := map[string]string{"svc1": "webapp"}
	svcImages := map[string]string{"svc1": "webapp:v2"}
	readable := map[string]bool{"svc1": true}

	g := buildPlacementJGF(c.ListNodes(), c, svcNames, svcImages, readable, jsonLDContext)

	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}

	// Round-trip to check task image.
	heBytes, err := json.Marshal(g.Hyperedges[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var he jgf.Hyperedge
	if err := json.Unmarshal(heBytes, &he); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	tasks, ok := he.Metadata["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("expected 1 task in hyperedge metadata")
	}
	task, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatal("task is not a map")
	}
	if task["image"] != "webapp:v2" {
		t.Errorf("expected task image fallback to 'webapp:v2', got %v", task["image"])
	}
}

func TestHandleTopologyGraphML(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopologyGraphML(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/graphml+xml" {
		t.Errorf("Content-Type=%q, want application/graphml+xml", ct)
	}
	if !strings.Contains(w.Body.String(), "<graphml") {
		t.Error("response should contain graphml")
	}
	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header")
	}
}

func TestHandleTopologyDOT(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopologyDOT(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/vnd.graphviz" {
		t.Errorf("Content-Type=%q, want text/vnd.graphviz", ct)
	}
	if !strings.HasPrefix(w.Body.String(), "graph") {
		t.Error("response should start with 'graph'")
	}
	if w.Header().Get("ETag") == "" {
		t.Error("expected ETag header")
	}
}

func TestHandleTopologyGraphML_WithEdges(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopologyGraphML(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<edge") {
		t.Error("expected edge element in GraphML output")
	}
	if !strings.Contains(body, "frontend") {
		t.Error("expected network name 'frontend' in GraphML edge data")
	}
}

func TestHandleTopologyDOT_WithEdges(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "frontend", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology", nil)
	w := httptest.NewRecorder()
	h.HandleTopologyDOT(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, " -- ") {
		t.Error("expected edge in DOT output")
	}
	if !strings.Contains(body, "frontend") {
		t.Error("expected network name 'frontend' in DOT edge label")
	}
}

func TestHandleTopologyGraphML_ETag304(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	h := newTestHandlers(t, withCache(c))

	req1 := httptest.NewRequest("GET", "/topology", nil)
	w1 := httptest.NewRecorder()
	h.HandleTopologyGraphML(w1, req1)

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on first request")
	}

	req2 := httptest.NewRequest("GET", "/topology", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.HandleTopologyGraphML(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
}

func TestHandleTopologyDOT_ETag304(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "webapp"}},
	})

	h := newTestHandlers(t, withCache(c))

	req1 := httptest.NewRequest("GET", "/topology", nil)
	w1 := httptest.NewRecorder()
	h.HandleTopologyDOT(w1, req1)

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on first request")
	}

	req2 := httptest.NewRequest("GET", "/topology", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.HandleTopologyDOT(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
}

// A service published with endpoint mode dnsrr has no virtual IP, so deriving
// network membership from Endpoint.VirtualIPs alone drops it from the graph.
// cluster.NetworkGraph unions the VIPs with the task template's networks; the
// JGF projection must join on the same rule or the two topologies disagree.
func TestBuildNetworkJGFSeesDNSRRAttachments(t *testing.T) {
	services := []swarm.Service{
		{
			ID: "svc1",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					Networks: []swarm.NetworkAttachmentConfig{{Target: "net1"}},
				},
			},
			Endpoint: swarm.Endpoint{
				VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
			},
		},
		{
			// dnsrr: attached in the spec, but never given a virtual IP.
			ID: "svc2",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "api"},
				TaskTemplate: swarm.TaskSpec{
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: "net1", Aliases: []string{"api"}},
					},
				},
				EndpointSpec: &swarm.EndpointSpec{Mode: swarm.ResolutionModeDNSRR},
			},
		},
	}
	networks := []network.Summary{{ID: "net1", Name: "app_default", Driver: "overlay"}}

	g := buildNetworkJGF(services, networks, "/api/context.jsonld")

	if len(g.Edges) != 1 {
		t.Fatalf("edges=%d, want 1 — the dnsrr service must still share net1", len(g.Edges))
	}

	source, target := g.Edges[0].Source, g.Edges[0].Target
	if source != jgf.URN("service", "svc1") || target != jgf.URN("service", "svc2") {
		t.Errorf("edge %s→%s, want svc1→svc2", source, target)
	}
}

// After a rolling update the node still holds the shutdown task alongside its
// replacement. cluster.PlacementGraph drops it — the orchestrator no longer
// intends it to run — and the JGF projection must too, or the dashboard counts
// the same replica twice and a node holding only a replaced task is reported
// as running the service.
func TestBuildPlacementJGFIgnoresReplacedTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{
		ID:           "task-old",
		ServiceID:    "svc1",
		NodeID:       "node1",
		Slot:         1,
		DesiredState: swarm.TaskStateShutdown,
		Status:       swarm.TaskStatus{State: swarm.TaskStateShutdown},
	})
	c.SetTask(swarm.Task{
		ID:           "task-new",
		ServiceID:    "svc1",
		NodeID:       "node1",
		Slot:         1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	clusterNodes := []swarm.Node{
		{ID: "node1", Description: swarm.NodeDescription{Hostname: "worker-1"}},
	}

	g := buildPlacementJGF(
		clusterNodes,
		c,
		map[string]string{"svc1": "web"},
		map[string]string{"svc1": "nginx"},
		map[string]bool{"svc1": true},
		"/api/context.jsonld",
	)

	if len(g.Hyperedges) != 1 {
		t.Fatalf("hyperedges=%d, want 1", len(g.Hyperedges))
	}

	tasks, _ := g.Hyperedges[0].Metadata["tasks"].([]map[string]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d, want 1 — the replaced task must not be counted", len(tasks))
	}

	if id, _ := tasks[0]["id"].(string); id != jgf.URN("task", "task-new") {
		t.Errorf("task=%v, want the live replacement", id)
	}
}

// Two services sharing several overlays produce one edge carrying all of them,
// and that array was filled by ranging a map — so two calls over identical
// state serialised differently and flipped the response's ETag, turning every
// conditional request into a 200. The edges themselves are sorted for exactly
// this reason; the networks inside them must be too.
func TestBuildNetworkJGFOrdersSharedNetworksDeterministically(t *testing.T) {
	attach := func(id, name string, targets ...string) swarm.Service {
		configs := make([]swarm.NetworkAttachmentConfig, 0, len(targets))
		vips := make([]swarm.EndpointVirtualIP, 0, len(targets))

		for _, target := range targets {
			configs = append(configs, swarm.NetworkAttachmentConfig{Target: target})
			vips = append(vips, swarm.EndpointVirtualIP{NetworkID: target})
		}

		return swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations:  swarm.Annotations{Name: name},
				TaskTemplate: swarm.TaskSpec{Networks: configs},
			},
			Endpoint: swarm.Endpoint{VirtualIPs: vips},
		}
	}

	shared := []string{"net-d", "net-a", "net-c", "net-b"}
	services := []swarm.Service{
		attach("svc1", "web", shared...),
		attach("svc2", "api", shared...),
	}

	networks := make([]network.Summary, 0, len(shared))
	for _, id := range shared {
		networks = append(networks, network.Summary{ID: id, Name: id, Driver: "overlay"})
	}

	names := func() []string {
		g := buildNetworkJGF(services, networks, "/api/context.jsonld")
		if len(g.Edges) != 1 {
			t.Fatalf("edges=%d, want 1", len(g.Edges))
		}

		entries, ok := g.Edges[0].Metadata["networks"].([]any)
		if !ok {
			t.Fatalf("networks metadata = %T, want a slice", g.Edges[0].Metadata["networks"])
		}

		got := make([]string, 0, len(entries))
		for _, entry := range entries {
			got = append(got, entry.(map[string]any)["id"].(string))
		}

		return got
	}

	first := names()

	want := make([]string, 0, len(shared))
	for _, id := range slices.Sorted(slices.Values(shared)) {
		want = append(want, jgf.URN("network", id))
	}

	if !slices.Equal(first, want) {
		t.Errorf("networks = %v, want them sorted: %v", first, want)
	}

	// Ranging a map is not reliably out of order on any single call, so repeat:
	// what matters is that every call agrees.
	for range 20 {
		if got := names(); !slices.Equal(got, first) {
			t.Fatalf("networks = %v on a later call, %v on the first", got, first)
		}
	}
}

// The removed projections need routes of their own. Without them the SPA
// catch-all answers both paths with 200 and an HTML page, so a JSON client of
// the old endpoint receives a rendered dashboard rather than an error — and
// the e2e test asserting the endpoint no longer serves a graph passes only
// because it never ran against a real server.
func TestRemovedTopologyProjectionsAreGone(t *testing.T) {
	c := cache.New(nil)
	h := newTestHandlers(t, withCache(c))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	defer b.Close()

	srv := httptest.NewServer(newTestRouter(t, h, b, nil))
	defer srv.Close()

	for _, path := range []string{"/topology/networks", "/topology/placement"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Accept", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("get %s: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusGone {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusGone)
			}

			if ct := resp.Header.Get(
				"Content-Type",
			); !strings.HasPrefix(
				ct,
				"application/problem+json",
			) {
				t.Errorf("content-type = %q, want a problem document", ct)
			}

			var problem struct {
				Status int    `json:"status"`
				Title  string `json:"title"`
				Detail string `json:"detail"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}

			if problem.Status != http.StatusGone {
				t.Errorf("problem status = %d, want %d", problem.Status, http.StatusGone)
			}
			if !strings.Contains(problem.Detail, "/topology") {
				t.Errorf("detail = %q, want it to point at the replacement", problem.Detail)
			}
		})
	}
}
