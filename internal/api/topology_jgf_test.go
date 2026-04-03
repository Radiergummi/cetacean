package api

import (
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/jgf"
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
