package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

func replicated(name string, replicas uint64) swarm.Service {
	return swarm.Service{
		ID: "svc-" + name,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: name + ":1@sha256:abc"},
			},
		},
	}
}

func overlay(id, name string) network.Summary {
	return network.Summary{ID: id, Name: name, Driver: "overlay", Scope: "swarm"}
}

func findNode(t *testing.T, g TopologyGraph, id string) TopologyNode {
	t.Helper()

	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}

	t.Fatalf("no node %q in graph; have %+v", id, g.Nodes)

	return TopologyNode{}
}

func hasEdge(g TopologyGraph, source, target string) bool {
	for _, e := range g.Edges {
		if e.Source == source && e.Target == target {
			return true
		}
	}

	return false
}

// TestNetworkGraphIsBipartite — the network view is services joined to the
// overlay networks they attach to, one edge per attachment, so the graph stays
// linear in attachments rather than quadratic in co-attached services.
func TestNetworkGraphIsBipartite(t *testing.T) {
	api := replicated("api", 2)
	api.Spec.Labels = map[string]string{"com.docker.stack.namespace": "shop"}
	api.Endpoint.VirtualIPs = []swarm.EndpointVirtualIP{{NetworkID: "net-1"}}

	worker := replicated("worker", 1)
	worker.Endpoint.VirtualIPs = []swarm.EndpointVirtualIP{{NetworkID: "net-1"}}

	graph := NetworkGraph(
		[]swarm.Service{api, worker},
		[]network.Summary{overlay("net-1", "backend"), overlay("net-2", "unused")},
	)

	if graph.View != TopologyViewNetwork {
		t.Errorf("view = %q, want %q", graph.View, TopologyViewNetwork)
	}

	svc := findNode(t, graph, "svc-api")
	if svc.Type != "service" || svc.Label != "api" {
		t.Errorf("service node = %+v, want type service labelled api", svc)
	}
	if svc.Group != "shop" {
		t.Errorf("service node group = %q, want the stack namespace", svc.Group)
	}
	if svc.Detail != "api:1" {
		t.Errorf("service node detail = %q, want the image without its digest", svc.Detail)
	}

	net := findNode(t, graph, "net-1")
	if net.Type != "network" || net.Label != "backend" {
		t.Errorf("network node = %+v, want type network labelled backend", net)
	}

	if !hasEdge(graph, "svc-api", "net-1") || !hasEdge(graph, "svc-worker", "net-1") {
		t.Errorf("missing service→network edges: %+v", graph.Edges)
	}
	if len(graph.Edges) != 2 {
		t.Errorf("edges = %d, want one per attachment", len(graph.Edges))
	}

	// A network nothing attaches to is noise on a graph, and a bridge network
	// is not swarm connectivity at all.
	for _, n := range graph.Nodes {
		if n.ID == "net-2" {
			t.Error("net-2 has no attached service and must not be a node")
		}
	}
}

// TestNetworkGraphSeesDNSRRAttachments — a service published with endpoint mode
// dnsrr has no virtual IP, so reading attachments from VirtualIPs alone loses
// it entirely. The desired attachment on the task template is the other half of
// the same fact.
func TestNetworkGraphSeesDNSRRAttachments(t *testing.T) {
	svc := replicated("dns", 1)
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
		{Target: "net-1", Aliases: []string{"dns-internal"}},
	}

	graph := NetworkGraph([]swarm.Service{svc}, []network.Summary{overlay("net-1", "backend")})

	if !hasEdge(graph, "svc-dns", "net-1") {
		t.Fatalf("attachment declared only on the task template was dropped: %+v", graph.Edges)
	}

	for _, e := range graph.Edges {
		if e.Source == "svc-dns" && e.Label != "dns-internal" {
			t.Errorf("edge label = %q, want the network alias", e.Label)
		}
	}
}

// TestNetworkGraphIgnoresNonOverlayNetworks — only overlay networks carry
// swarm service traffic; bridge, host and null attachments say nothing about
// how services reach each other.
func TestNetworkGraphIgnoresNonOverlayNetworks(t *testing.T) {
	svc := replicated("api", 1)
	svc.Endpoint.VirtualIPs = []swarm.EndpointVirtualIP{{NetworkID: "br-1"}}

	graph := NetworkGraph(
		[]swarm.Service{svc},
		[]network.Summary{{ID: "br-1", Name: "bridge", Driver: "bridge"}},
	)

	if len(graph.Edges) != 0 {
		t.Errorf("bridge attachment produced edges: %+v", graph.Edges)
	}
}

// TestPlacementGraphJoinsNodesToServices — the placement view answers "what
// runs where", so an edge carries how many of a service's tasks a cluster node
// is actually running.
func TestPlacementGraphJoinsNodesToServices(t *testing.T) {
	clusterNode := swarm.Node{
		ID: "node-1",
		Description: swarm.NodeDescription{
			Hostname: "worker-a",
		},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	}

	tasks := []swarm.Task{
		{
			ID:        "t1",
			NodeID:    "node-1",
			ServiceID: "svc-api",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
		{
			ID:        "t2",
			NodeID:    "node-1",
			ServiceID: "svc-api",
			Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
		},
		{
			ID:        "t3",
			NodeID:    "node-2",
			ServiceID: "svc-api",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	}

	graph := PlacementGraph(
		[]swarm.Node{clusterNode},
		tasks,
		[]swarm.Service{replicated("api", 2)},
	)

	if graph.View != TopologyViewPlacement {
		t.Errorf("view = %q, want %q", graph.View, TopologyViewPlacement)
	}

	host := findNode(t, graph, "node-1")
	if host.Type != "node" || host.Label != "worker-a" {
		t.Errorf("cluster node = %+v, want type node labelled worker-a", host)
	}
	if host.State != "ready" {
		t.Errorf("cluster node state = %q, want ready", host.State)
	}

	// One of two desired replicas is running on a node this caller can see, so
	// the shared rule says pending. The third task is on node-2, which is not
	// in the filtered slice and must not count towards the total.
	svc := findNode(t, graph, "svc-api")
	if svc.State != "pending" {
		t.Errorf("service state = %q, want pending for 1 of 2 desired replicas", svc.State)
	}

	if len(graph.Edges) != 1 {
		t.Fatalf("edges = %+v, want one per (node, service) pair with tasks", graph.Edges)
	}

	edge := graph.Edges[0]
	if edge.Source != "node-1" || edge.Target != "svc-api" {
		t.Errorf("edge = %+v, want node-1 → svc-api", edge)
	}
	if edge.Label != "1/2 running" {
		t.Errorf("edge label = %q, want the running count out of the tasks placed here", edge.Label)
	}
}

// TestPlacementGraphDropsTasksOnUnreadableResources — the caller's ACL filter
// is applied to the node and service slices before they get here, so a task
// referencing something outside them must not leak its existence back as an
// edge or a vertex.
func TestPlacementGraphDropsTasksOnUnreadableResources(t *testing.T) {
	graph := PlacementGraph(
		[]swarm.Node{{ID: "node-1", Description: swarm.NodeDescription{Hostname: "a"}}},
		[]swarm.Task{
			{ID: "t1", NodeID: "node-hidden", ServiceID: "svc-api"},
			{ID: "t2", NodeID: "node-1", ServiceID: "svc-hidden"},
		},
		[]swarm.Service{replicated("api", 1)},
	)

	if len(graph.Edges) != 0 {
		t.Errorf("edges = %+v, want none: neither task joins two visible resources", graph.Edges)
	}

	for _, n := range graph.Nodes {
		if n.ID == "node-hidden" || n.ID == "svc-hidden" {
			t.Errorf("filtered-out resource %q appears as a vertex", n.ID)
		}
	}
}

// TestPlacementGraphKeepsUnscheduledServices — a service with no running task
// is the one an operator most needs to see, so it stays as an isolated vertex
// rather than disappearing with its tasks.
func TestPlacementGraphKeepsUnscheduledServices(t *testing.T) {
	graph := PlacementGraph(nil, nil, []swarm.Service{replicated("api", 3)})

	svc := findNode(t, graph, "svc-api")
	if svc.State != "failed" {
		t.Errorf("state = %q, want the derived state for 0 of 3 replicas", svc.State)
	}
}

// TestGraphsAreDeterministic — the result is marshalled into an ETag'd MCP
// result, and map iteration order would change the bytes on every call.
func TestGraphsAreDeterministic(t *testing.T) {
	services := []swarm.Service{replicated("a", 1), replicated("b", 1), replicated("c", 1)}
	for i := range services {
		services[i].Endpoint.VirtualIPs = []swarm.EndpointVirtualIP{
			{NetworkID: "net-2"}, {NetworkID: "net-1"},
		}
	}

	networks := []network.Summary{overlay("net-1", "one"), overlay("net-2", "two")}

	first := NetworkGraph(services, networks)
	for range 10 {
		if got := NetworkGraph(services, networks); !equalGraphs(first, got) {
			t.Fatalf("network graph is not stable across calls:\n%+v\n%+v", first, got)
		}
	}
}

// TestGraphsNeverMarshalNullSlices — output-schema validation is on server-wide
// and the schema declares arrays; a nil slice marshals to null and fails it.
func TestGraphsNeverMarshalNullSlices(t *testing.T) {
	for name, graph := range map[string]TopologyGraph{
		"network":   NetworkGraph(nil, nil),
		"placement": PlacementGraph(nil, nil, nil),
	} {
		if graph.Nodes == nil {
			t.Errorf("%s: Nodes is nil and would marshal to null", name)
		}
		if graph.Edges == nil {
			t.Errorf("%s: Edges is nil and would marshal to null", name)
		}
	}
}

func equalGraphs(a, b TopologyGraph) bool {
	if a.View != b.View || len(a.Nodes) != len(b.Nodes) || len(a.Edges) != len(b.Edges) {
		return false
	}

	for i := range a.Nodes {
		if a.Nodes[i] != b.Nodes[i] {
			return false
		}
	}

	for i := range a.Edges {
		if a.Edges[i] != b.Edges[i] {
			return false
		}
	}

	return true
}

func TestStripImageDigest(t *testing.T) {
	for input, want := range map[string]string{
		"nginx:1.27":                  "nginx:1.27",
		"nginx:1.27@sha256:deadbeef":  "nginx:1.27",
		"registry.io/ns/img@sha256:0": "registry.io/ns/img",
		"":                            "",
	} {
		if got := StripImageDigest(input); got != want {
			t.Errorf("StripImageDigest(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReplicaCount(t *testing.T) {
	if got := ReplicaCount(replicated("api", 4)); got != 4 {
		t.Errorf("ReplicaCount = %d, want 4", got)
	}

	global := swarm.Service{
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	}
	if got := ReplicaCount(global); got != 0 {
		t.Errorf("ReplicaCount(global) = %d, want 0", got)
	}
}
