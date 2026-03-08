package api

import (
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"cetacean/internal/cache"
)

func TestHandleNetworkTopology(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "web_default", Driver: "overlay"})
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
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

	h := NewHandlers(c, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/api/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp NetworkTopology
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Nodes) != 2 {
		t.Errorf("nodes=%d, want 2", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Errorf("edges=%d, want 1", len(resp.Edges))
	}
	if len(resp.Networks) != 1 {
		t.Errorf("networks=%d, want 1", len(resp.Networks))
	}
}

func TestHandleNetworkTopology_WithReplicatedService(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "app_net", Driver: "overlay"})
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
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
	if resp.Nodes[0].Replicas != 3 {
		t.Errorf("replicas=%d, want 3", resp.Nodes[0].Replicas)
	}
}

func TestHandlePlacementTopology(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}, Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}, Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", NodeID: "n1", Slot: 1, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	h := NewHandlers(c, nil, closedReady(), nil)
	req := httptest.NewRequest("GET", "/api/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	var resp PlacementTopology
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Nodes) != 1 {
		t.Errorf("nodes=%d, want 1", len(resp.Nodes))
	}
	if len(resp.Nodes[0].Tasks) != 1 {
		t.Errorf("tasks=%d, want 1", len(resp.Nodes[0].Tasks))
	}
	if resp.Nodes[0].Tasks[0].ServiceName != "nginx" {
		t.Errorf("serviceName=%s, want nginx", resp.Nodes[0].Tasks[0].ServiceName)
	}
}
