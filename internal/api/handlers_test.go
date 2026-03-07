package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func closedReady() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestHandleHealth(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady())
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	h.HandleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandleReady_NotReady(t *testing.T) {
	ch := make(chan struct{}) // not closed = not ready
	h := NewHandlers(cache.New(nil), nil, ch)
	req := httptest.NewRequest("GET", "/api/ready", nil)
	w := httptest.NewRecorder()
	h.HandleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}

func TestHandleReady_Ready(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady())
	req := httptest.NewRequest("GET", "/api/ready", nil)
	w := httptest.NewRecorder()
	h.HandleReady(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandleCluster(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})
	h := NewHandlers(c, nil, closedReady())

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
	h := NewHandlers(c, nil, closedReady())

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(resp.Items))
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
}

func TestHandleGetNode_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c, nil, closedReady())

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
	h := NewHandlers(c, nil, closedReady())

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
	h := NewHandlers(c, nil, closedReady())

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
	h := NewHandlers(c, nil, closedReady())

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

func TestHandleListServices_Paginated(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		svc := swarm.Service{ID: name}
		svc.Spec.Name = name
		c.SetService(svc)
	}
	h := NewHandlers(c, nil, closedReady())

	req := httptest.NewRequest("GET", "/api/services?limit=2&sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[swarm.Service]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 3 {
		t.Fatalf("expected total 3, got %d", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("expected first item alpha, got %s", resp.Items[0].Spec.Name)
	}
	if resp.Items[1].Spec.Name != "bravo" {
		t.Errorf("expected second item bravo, got %s", resp.Items[1].Spec.Name)
	}
}

func TestHandleListNodes_Paginated(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1"}
	n1.Description.Hostname = "zulu"
	n2 := swarm.Node{ID: "n2"}
	n2.Description.Hostname = "alpha"
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady())

	req := httptest.NewRequest("GET", "/api/nodes?limit=1&sort=hostname", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Description.Hostname != "alpha" {
		t.Errorf("expected alpha, got %s", resp.Items[0].Description.Hostname)
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
	h := NewHandlers(c, nil, closedReady())

	req := httptest.NewRequest("GET", "/api/services?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[swarm.Service]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 service, got %d", len(resp.Items))
	}
	if resp.Items[0].Spec.Name != "web-frontend" {
		t.Errorf("expected web-frontend, got %s", resp.Items[0].Spec.Name)
	}
}

func TestHandleHistory(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	c.SetService(swarm.Service{ID: "s2", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "redis"}}})

	h := NewHandlers(c, nil, closedReady())
	req := httptest.NewRequest("GET", "/api/history?type=service", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var entries []cache.HistoryEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
	// Newest first
	if len(entries) > 0 && entries[0].Name != "redis" {
		t.Errorf("first entry name=%s, want redis", entries[0].Name)
	}
}

func TestHandleHistory_FilterByResource(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}})

	h := NewHandlers(c, nil, closedReady())
	req := httptest.NewRequest("GET", "/api/history?resourceId=s1", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var entries []cache.HistoryEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}
