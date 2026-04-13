package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
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

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["@context"] == "" {
		t.Error("missing @context")
	}
	if resp["@id"] == "" {
		t.Error("missing @id")
	}
	if resp["@type"] != "NetworkTopology" {
		t.Errorf("@type=%v, want NetworkTopology", resp["@type"])
	}

	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 2 {
		t.Errorf("nodes=%d, want 2", len(nodes))
	}

	edges, _ := resp["edges"].([]any)
	if len(edges) != 1 {
		t.Errorf("edges=%d, want 1", len(edges))
	}

	networks, _ := resp["networks"].([]any)
	if len(networks) != 1 {
		t.Errorf("networks=%d, want 1", len(networks))
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
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "net1"}},
		},
	})
	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(nodes))
	}

	node, _ := nodes[0].(map[string]any)
	if node["replicas"] != float64(3) {
		t.Errorf("replicas=%v, want 3", node["replicas"])
	}
}

func TestHandlePlacementTopology(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(
		swarm.Node{
			ID:          "n1",
			Description: swarm.NodeDescription{Hostname: "worker-01"},
			Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		},
	)
	c.SetService(
		swarm.Service{
			ID:   "svc1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			NodeID:    "n1",
			Slot:      1,
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["@context"] == "" {
		t.Error("missing @context")
	}
	if resp["@id"] == "" {
		t.Error("missing @id")
	}
	if resp["@type"] != "PlacementTopology" {
		t.Errorf("@type=%v, want PlacementTopology", resp["@type"])
	}

	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 1 {
		t.Errorf("nodes=%d, want 1", len(nodes))
	}

	node, _ := nodes[0].(map[string]any)
	tasks, _ := node["tasks"].([]any)
	if len(tasks) != 1 {
		t.Errorf("tasks=%d, want 1", len(tasks))
	}

	task, _ := tasks[0].(map[string]any)
	if task["serviceName"] != "nginx" {
		t.Errorf("serviceName=%v, want nginx", task["serviceName"])
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

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d, want 1", len(nodes))
	}

	n, _ := nodes[0].(map[string]any)
	if n["image"] != "nginx:1.25" {
		t.Errorf("image=%q, want nginx:1.25", n["image"])
	}
	if n["mode"] != "replicated" {
		t.Errorf("mode=%q, want replicated", n["mode"])
	}

	ports, _ := n["ports"].([]any)
	if len(ports) != 1 || ports[0] != "80:8080/tcp" {
		t.Errorf("ports=%v, want [80:8080/tcp]", ports)
	}
	if n["updateStatus"] != "updating" {
		t.Errorf("updateStatus=%q, want updating", n["updateStatus"])
	}
}

func TestHandlePlacementTopology_EnrichedFields(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-01"},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
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

	h := newTestHandlers(t, withCache(c))
	req := httptest.NewRequest("GET", "/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	nodes, _ := resp["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("nodes is empty")
	}

	node, _ := nodes[0].(map[string]any)
	if node["availability"] != "active" {
		t.Errorf("availability=%q, want active", node["availability"])
	}

	tasks, _ := node["tasks"].([]any)
	if len(tasks) == 0 {
		t.Fatal("tasks is empty")
	}

	task, _ := tasks[0].(map[string]any)
	if task["image"] != "nginx:1.25" {
		t.Errorf("image=%q, want nginx:1.25", task["image"])
	}
}

func TestHandleNetworkTopology_DeprecationHeaders(t *testing.T) {
	h := newTestHandlers(t, withCache(cache.New(nil)))
	req := httptest.NewRequest("GET", "/topology/networks", nil)
	w := httptest.NewRecorder()
	h.HandleNetworkTopology(w, req)

	if w.Header().Get("Deprecation") != "true" {
		t.Error("expected Deprecation: true header")
	}
	link := w.Header().Get("Link")
	if !strings.Contains(link, `</topology>`) ||
		!strings.Contains(link, `rel="successor-version"`) {
		t.Errorf("expected Link successor-version header, got %q", link)
	}
}

func TestHandlePlacementTopology_DeprecationHeaders(t *testing.T) {
	h := newTestHandlers(t, withCache(cache.New(nil)))
	req := httptest.NewRequest("GET", "/topology/placement", nil)
	w := httptest.NewRecorder()
	h.HandlePlacementTopology(w, req)

	if w.Header().Get("Deprecation") != "true" {
		t.Error("expected Deprecation: true header")
	}
	link := w.Header().Get("Link")
	if !strings.Contains(link, `</topology>`) ||
		!strings.Contains(link, `rel="successor-version"`) {
		t.Errorf("expected Link successor-version header, got %q", link)
	}
}
