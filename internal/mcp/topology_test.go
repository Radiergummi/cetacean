package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// seedTopology builds a two-node cluster running two services on one overlay
// network, which is enough for both views to have vertices and edges.
func seedTopology(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)

	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "backend",
		Driver: "overlay",
		Scope:  "swarm",
	})

	for _, spec := range []struct{ id, name string }{{"svc1", "api"}, {"svc2", "worker"}} {
		replicas := uint64(1)
		svc := swarm.Service{
			ID: spec.id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: spec.name},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: spec.name + ":1"},
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: "net1", Aliases: []string{spec.name}},
					},
				},
			},
		}
		c.SetService(svc)
	}

	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleManager},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		NodeID:    "node1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	return c
}

// callTopology drives get_topology through the real HTTP transport and decodes
// its structured content. Anything short of that misses the wiring — output
// schema validation, tier gating and argument decoding all live there.
func callTopology(t *testing.T, handler http.Handler, args string) cluster.TopologyGraph {
	t.Helper()

	_, env := mcpModern(t, handler, 1, "tools/call",
		`{"name":"get_topology","arguments":`+args+`}`)
	if env.Error != nil {
		t.Fatalf("tools/call transport error: %+v", env.Error)
	}

	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if result.IsError {
		t.Fatalf("get_topology returned a tool error: %s", env.Result)
	}

	var graph cluster.TopologyGraph
	if err := json.Unmarshal(result.StructuredContent, &graph); err != nil {
		t.Fatalf("decode structured content: %v (raw %s)", err, result.StructuredContent)
	}

	return graph
}

// TestGetTopologyDefaultsToTheNetworkView — the tool is what a host calls to
// render the widget, so calling it with no arguments has to produce a graph
// rather than an error.
func TestGetTopologyDefaultsToTheNetworkView(t *testing.T) {
	srv := newToolTestServer(t, seedTopology(t), &fakeWriteClient{}, config.OpsReadOnly)

	graph := callTopology(t, srv.Handler(), `{}`)

	if graph.View != cluster.TopologyViewNetwork {
		t.Errorf("view = %q, want %q", graph.View, cluster.TopologyViewNetwork)
	}

	var services, networks int
	for _, n := range graph.Nodes {
		switch n.Type {
		case "service":
			services++
		case "network":
			networks++
		}
	}

	if services != 2 || networks != 1 {
		t.Errorf("got %d services and %d networks, want 2 and 1: %+v",
			services, networks, graph.Nodes)
	}

	if len(graph.Edges) != 2 {
		t.Errorf("edges = %+v, want one per service attachment", graph.Edges)
	}
}

// TestGetTopologyServesThePlacementView — the widget switches views by calling
// the tool again with the other argument, so the argument has to reach the
// builder.
func TestGetTopologyServesThePlacementView(t *testing.T) {
	srv := newToolTestServer(t, seedTopology(t), &fakeWriteClient{}, config.OpsReadOnly)

	graph := callTopology(t, srv.Handler(), `{"view":"placement"}`)

	if graph.View != cluster.TopologyViewPlacement {
		t.Errorf("view = %q, want %q", graph.View, cluster.TopologyViewPlacement)
	}

	found := false
	for _, e := range graph.Edges {
		if e.Source == "node1" && e.Target == "svc1" {
			found = true
		}
	}

	if !found {
		t.Errorf("no edge placing svc1 on node1: %+v", graph.Edges)
	}
}

// TestGetTopologyRejectsAnUnknownView — the view is a closed set, and a typo
// must say so rather than silently returning the default graph under a label
// the caller did not ask for.
func TestGetTopologyRejectsAnUnknownView(t *testing.T) {
	srv := newToolTestServer(t, seedTopology(t), &fakeWriteClient{}, config.OpsReadOnly)

	_, env := mcpModern(t, srv.Handler(), 1, "tools/call",
		`{"name":"get_topology","arguments":{"view":"physical"}}`)
	if env.Error != nil {
		t.Fatalf("tools/call transport error: %+v", env.Error)
	}

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if !result.IsError {
		t.Fatalf("unknown view was accepted: %s", env.Result)
	}
}

// TestGetTopologyAppliesACL — a graph is a disclosure surface like any other
// read. A caller who cannot read a service must not learn it exists, nor that
// something is attached to a network.
func TestGetTopologyAppliesACL(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:api", "node:*", "network:*"))

	srv := newToolTestServer(
		t,
		seedTopology(t),
		&fakeWriteClient{},
		config.OpsReadOnly,
		func(o *Options) { o.ACL = evaluator },
	)

	graph := callTopology(t, srv.Handler(), `{}`)

	for _, n := range graph.Nodes {
		if n.ID == "svc2" {
			t.Errorf("worker is not readable by this caller but appears as a vertex: %+v", n)
		}
	}

	for _, e := range graph.Edges {
		if e.Source == "svc2" {
			t.Errorf("edge discloses an unreadable service: %+v", e)
		}
	}
}

// TestGetTopologyCarriesWidgetMeta — a host finds the graph widget from the
// tool's _meta, so a tool that does not name it renders as JSON.
func TestGetTopologyCarriesWidgetMeta(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("get_topology")
	if !ok {
		t.Fatal("get_topology is not registered at OpsReadOnly")
	}

	if td.widget != "topology" {
		t.Errorf("widget = %q, want topology", td.widget)
	}

	if td.tool.Meta == nil {
		t.Fatal("get_topology carries no _meta; hosts cannot find its widget")
	}

	uri := uiResourceURI("topology")
	if got := td.tool.Meta.AdditionalFields[uiResourceURIMetaKey]; got != uri {
		t.Errorf("_meta[%q] = %v, want %q", uiResourceURIMetaKey, got, uri)
	}
}
