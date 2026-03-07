package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func TestHandleCluster(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/cluster", nil)
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var snap cache.ClusterSnapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if snap.NodeCount != 1 || snap.ServiceCount != 1 {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
}

func TestHandleListNodes(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var nodes []swarm.Node
	json.NewDecoder(w.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestHandleGetNode_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/nodes/n1", nil)
	req.SetPathValue("id", "n1")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/nodes/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleServiceTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	t1 := swarm.Task{ID: "t1", ServiceID: "svc1"}
	t2 := swarm.Task{ID: "t2", ServiceID: "svc2"}
	c.SetTask(t1)
	c.SetTask(t2)
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/services/svc1/tasks", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var tasks []swarm.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", tasks[0].ID)
	}
}

func TestHandleNodeTasks(t *testing.T) {
	c := cache.New(nil)
	n := swarm.Node{ID: "node1"}
	c.SetNode(n)
	t1 := swarm.Task{ID: "t1", NodeID: "node1"}
	t2 := swarm.Task{ID: "t2", NodeID: "node2"}
	c.SetTask(t1)
	c.SetTask(t2)
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/nodes/node1/tasks", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleNodeTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var tasks []swarm.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", tasks[0].ID)
	}
}

func TestHandleListServices_Search(t *testing.T) {
	c := cache.New(nil)
	svc1 := swarm.Service{ID: "s1"}
	svc1.Spec.Name = "web-frontend"
	svc2 := swarm.Service{ID: "s2"}
	svc2.Spec.Name = "api-backend"
	c.SetService(svc1)
	c.SetService(svc2)
	h := NewHandlers(c, nil)

	req := httptest.NewRequest("GET", "/api/services?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var services []swarm.Service
	json.NewDecoder(w.Body).Decode(&services)
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Spec.Name != "web-frontend" {
		t.Errorf("expected web-frontend, got %s", services[0].Spec.Name)
	}
}
