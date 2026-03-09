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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
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
	h := NewHandlers(c, nil, closedReady(), nil, nil)
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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
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
