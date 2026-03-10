package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
	"cetacean/internal/notify"
)

func closedReady() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestHandleHealth(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	h.HandleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "version", "commit", "buildDate"} {
		if body[key] == "" {
			t.Errorf("missing key %q in health response", key)
		}
	}
}

func TestHandleReady_NotReady(t *testing.T) {
	ch := make(chan struct{}) // not closed = not ready
	h := NewHandlers(cache.New(nil), nil, ch, nil, nil)
	req := httptest.NewRequest("GET", "/api/ready", nil)
	w := httptest.NewRecorder()
	h.HandleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}

func TestHandleReady_Ready(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)
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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services?limit=2&sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[ServiceListItem]
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

func TestHandleListServices_RunningTasks(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: func() *uint64 { v := uint64(3); return &v }()}
	c.SetService(svc)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp PagedResponse[ServiceListItem]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 service, got %d", len(resp.Items))
	}
	if resp.Items[0].RunningTasks != 2 {
		t.Errorf("RunningTasks = %d, want 2", resp.Items[0].RunningTasks)
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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

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
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp PagedResponse[ServiceListItem]
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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
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

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/history?resourceId=s1", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var entries []cache.HistoryEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

func TestHandleNotificationRules_Empty(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, make(chan struct{}), nil, nil)
	req := httptest.NewRequest("GET", "/api/notifications/rules", nil)
	w := httptest.NewRecorder()
	h.HandleNotificationRules(w, req)
	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestHandleNotificationRules_WithNotifier(t *testing.T) {
	c := cache.New(nil)
	n := notify.New([]notify.Rule{{ID: "r1", Name: "test-rule", Enabled: true}})
	h := NewHandlers(c, nil, closedReady(), n, nil)

	req := httptest.NewRequest("GET", "/api/notifications/rules", nil)
	w := httptest.NewRecorder()
	h.HandleNotificationRules(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var statuses []notify.RuleStatus
	json.NewDecoder(w.Body).Decode(&statuses)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(statuses))
	}
	if statuses[0].Name != "test-rule" {
		t.Errorf("name=%s, want test-rule", statuses[0].Name)
	}
}

func TestHandleGetService_Found(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "nginx"
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetService_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetTask_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks/t1", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetTask_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleListTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks?sort=state", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Errorf("total=%d, want 2", resp.Total)
	}
}

func TestHandleServiceTasks_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services/missing/tasks", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleNodeTasks_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/nodes/missing/tasks", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleNodeTasks(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleListStacks(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.Labels = map[string]string{"com.docker.stack.namespace": "mystack"}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/stacks", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[cache.Stack]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Fatalf("total=%d, want 1", resp.Total)
	}
	if resp.Items[0].Name != "mystack" {
		t.Errorf("name=%s, want mystack", resp.Items[0].Name)
	}
}

func TestHandleListStacks_SortByName(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"beta", "alpha"} {
		svc := swarm.Service{ID: name}
		svc.Spec.Name = name + "-svc"
		svc.Spec.Labels = map[string]string{"com.docker.stack.namespace": name}
		c.SetService(svc)
	}
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/stacks?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	var resp PagedResponse[cache.Stack]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Name)
	}
}

func TestHandleListConfigs_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "zebra"}}})
	c.SetConfig(swarm.Config{ID: "c2", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "alpha"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/configs?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	var resp PagedResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Spec.Name)
	}
}

func TestHandleListSecrets_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "zebra"}}})
	c.SetSecret(swarm.Secret{ID: "s2", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "alpha"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	var resp PagedResponse[swarm.Secret]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Spec.Name)
	}
}

func TestHandleListNetworks_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "zebra"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "alpha"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/networks?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	var resp PagedResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Name)
	}
}

func TestHandleListVolumes_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "zebra"})
	c.SetVolume(volume.Volume{Name: "alpha"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/volumes?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	var resp PagedResponse[volume.Volume]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Name)
	}
}

func TestHandleListStacks_Search(t *testing.T) {
	c := cache.New(nil)
	svc1 := swarm.Service{ID: "svc1"}
	svc1.Spec.Name = "web"
	svc1.Spec.Labels = map[string]string{"com.docker.stack.namespace": "frontend"}
	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "api"
	svc2.Spec.Labels = map[string]string{"com.docker.stack.namespace": "backend"}
	c.SetService(svc1)
	c.SetService(svc2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/stacks?search=front", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	var resp PagedResponse[cache.Stack]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(resp.Items))
	}
}

func TestHandleGetStack_Found(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.Labels = map[string]string{"com.docker.stack.namespace": "mystack"}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/stacks/mystack", nil)
	req.SetPathValue("name", "mystack")
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var detail cache.StackDetail
	json.NewDecoder(w.Body).Decode(&detail)
	if detail.Name != "mystack" {
		t.Errorf("name=%s, want mystack", detail.Name)
	}
}

func TestHandleGetStack_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/stacks/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleListConfigs(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}}})
	c.SetConfig(swarm.Config{ID: "c2", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "db-config"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/configs?search=app", nil)
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 config, got %d", len(resp.Items))
	}
}

func TestHandleListSecrets(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}}})
	c.SetSecret(swarm.Secret{ID: "s2", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "api-key"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets?search=tls", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Secret]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(resp.Items))
	}
}

func TestHandleListNetworks(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "web_overlay", Driver: "overlay"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "db_bridge", Driver: "bridge"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/networks?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 network, got %d", len(resp.Items))
	}
}

func TestHandleListVolumes(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol", Driver: "local"})
	c.SetVolume(volume.Volume{Name: "cache-vol", Driver: "local"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/volumes?search=data", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[volume.Volume]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(resp.Items))
	}
}

func TestHandleTaskLogs_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks/missing/logs", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleTaskLogs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleHistory_InvalidLimit(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	// invalid limit should fall back to default (50)
	req := httptest.NewRequest("GET", "/api/history?limit=abc", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleHistory_CustomLimit(t *testing.T) {
	c := cache.New(nil)
	for i := range 5 {
		svc := swarm.Service{ID: "s" + string(rune('a'+i))}
		svc.Spec.Name = "svc-" + string(rune('a'+i))
		c.SetService(svc)
	}
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/history?limit=2", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var entries []cache.HistoryEntry
	json.NewDecoder(w.Body).Decode(&entries)
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestHandleListNodes_SortByRole(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}}
	n1.Description.Hostname = "worker"
	n2 := swarm.Node{ID: "n2", Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager}}
	n2.Description.Hostname = "manager"
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/nodes?sort=role", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2, got %d", len(resp.Items))
	}
	// "manager" < "worker" alphabetically
	if string(resp.Items[0].Spec.Role) != "manager" {
		t.Errorf("first=%s, want manager", resp.Items[0].Spec.Role)
	}
}

func TestHandleListNodes_SortByStatus(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1", Status: swarm.NodeStatus{State: swarm.NodeStateReady}}
	n2 := swarm.Node{ID: "n2", Status: swarm.NodeStatus{State: swarm.NodeStateDown}}
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/nodes?sort=status", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if string(resp.Items[0].Status.State) != "down" {
		t.Errorf("first=%s, want down", resp.Items[0].Status.State)
	}
}

func TestHandleListNodes_SortByAvailability(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Availability: swarm.NodeAvailabilityPause}}
	n2 := swarm.Node{ID: "n2", Spec: swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive}}
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/nodes?sort=availability", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if string(resp.Items[0].Spec.Availability) != "active" {
		t.Errorf("first=%s, want active", resp.Items[0].Spec.Availability)
	}
}

func TestHandleListServices_SortByMode(t *testing.T) {
	c := cache.New(nil)
	svc1 := swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "global-svc"},
		Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
	}}
	svc2 := swarm.Service{ID: "s2", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "replicated-svc"},
		Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{}},
	}}
	c.SetService(svc1)
	c.SetService(svc2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/services?sort=mode", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp PagedResponse[ServiceListItem]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2, got %d", len(resp.Items))
	}
	// "Global" < "Replicated"
	if resp.Items[0].Spec.Mode.Global == nil {
		t.Error("expected Global first")
	}
}

func TestHandleListTasks_SortByService(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc-b"})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc-a"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks?sort=service", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	var resp PagedResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].ServiceID != "svc-a" {
		t.Errorf("first serviceID=%s, want svc-a", resp.Items[0].ServiceID)
	}
}

func TestHandleListTasks_SortByNode(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", NodeID: "node-b"})
	c.SetTask(swarm.Task{ID: "t2", NodeID: "node-a"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks?sort=node", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	var resp PagedResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].NodeID != "node-a" {
		t.Errorf("first nodeID=%s, want node-a", resp.Items[0].NodeID)
	}
}

func TestHandleListNetworks_SortByDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "net1", Driver: "overlay"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "net2", Driver: "bridge"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/networks?sort=driver", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	var resp PagedResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Driver != "bridge" {
		t.Errorf("first driver=%s, want bridge", resp.Items[0].Driver)
	}
}

func TestHandleListVolumes_SortByDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "vol1", Driver: "nfs"})
	c.SetVolume(volume.Volume{Name: "vol2", Driver: "local"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/volumes?sort=driver", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	var resp PagedResponse[volume.Volume]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Driver != "local" {
		t.Errorf("first driver=%s, want local", resp.Items[0].Driver)
	}
}

// --- Filter tests ---

func filterReq(path, filter string) *http.Request {
	req := httptest.NewRequest("GET", path, nil)
	q := req.URL.Query()
	q.Set("filter", filter)
	req.URL.RawQuery = q.Encode()
	return req
}

func TestHandleListNodes_Filter(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager}}
	n1.Description.Hostname = "manager-01"
	n2 := swarm.Node{ID: "n2", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}}
	n2.Description.Hostname = "worker-01"
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", `role == "manager"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Items))
	}
	if resp.Items[0].Description.Hostname != "manager-01" {
		t.Errorf("expected manager-01, got %s", resp.Items[0].Description.Hostname)
	}
}

func TestHandleListTasks_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc2", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})
	c.SetTask(swarm.Task{ID: "t3", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateFailed}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListTasks(w, filterReq("/api/tasks", `state == "failed" && service == "svc1"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "t3" {
		t.Errorf("expected t3, got %s", resp.Items[0].ID)
	}
}

func TestHandleListServices_Filter(t *testing.T) {
	c := cache.New(nil)
	svc1 := swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: "web"},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"}},
	}}
	svc2 := swarm.Service{ID: "s2", Spec: swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: "api"},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "golang:1.22"}},
	}}
	c.SetService(svc1)
	c.SetService(svc2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListServices(w, filterReq("/api/services", `image contains "nginx"`))

	var resp PagedResponse[ServiceListItem]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 service, got %d", len(resp.Items))
	}
	if resp.Items[0].Spec.Name != "web" {
		t.Errorf("expected web, got %s", resp.Items[0].Spec.Name)
	}
}

func TestHandleListNetworks_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "web_overlay", Driver: "overlay"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "db_bridge", Driver: "bridge"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListNetworks(w, filterReq("/api/networks", `driver == "overlay"`))

	var resp PagedResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 network, got %d", len(resp.Items))
	}
}

func TestHandleList_FilterInvalid(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", "invalid ==="))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleList_FilterTooLong(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)
	long := strings.Repeat("a", 513)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", long))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleList_FilterEmpty(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	// Empty filter should return all items
	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", ""))

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("expected 1 node with empty filter, got %d", resp.Total)
	}
}

func TestHandleList_FilterNoMatch(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", `role == "manager"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 0 {
		t.Errorf("expected 0 nodes, got %d", resp.Total)
	}
}

func TestHandleListVolumes_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol", Driver: "local"})
	c.SetVolume(volume.Volume{Name: "cache-vol", Driver: "nfs"})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListVolumes(w, filterReq("/api/volumes", `driver == "nfs"`))

	var resp PagedResponse[volume.Volume]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(resp.Items))
	}
	if resp.Items[0].Name != "cache-vol" {
		t.Errorf("expected cache-vol, got %s", resp.Items[0].Name)
	}
}

func TestHandleListConfigs_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}}})
	c.SetConfig(swarm.Config{ID: "c2", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "db-config"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListConfigs(w, filterReq("/api/configs", `name contains "app"`))

	var resp PagedResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 config, got %d", len(resp.Items))
	}
}

func TestHandleListSecrets_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}}})
	c.SetSecret(swarm.Secret{ID: "s2", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "api-key"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListSecrets(w, filterReq("/api/secrets", `name startsWith "tls"`))

	var resp PagedResponse[swarm.Secret]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(resp.Items))
	}
}

func TestHandleListStacks_Filter(t *testing.T) {
	c := cache.New(nil)
	svc1 := swarm.Service{ID: "svc1"}
	svc1.Spec.Name = "web"
	svc1.Spec.Labels = map[string]string{"com.docker.stack.namespace": "frontend"}
	svc2 := swarm.Service{ID: "svc2"}
	svc2.Spec.Name = "api"
	svc2.Spec.Labels = map[string]string{"com.docker.stack.namespace": "backend"}
	c.SetService(svc1)
	c.SetService(svc2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	w := httptest.NewRecorder()
	h.HandleListStacks(w, filterReq("/api/stacks", `services > 0`))

	var resp PagedResponse[cache.Stack]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(resp.Items))
	}
}

func TestHandleList_FilterWithSearchAndPagination(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web-prod", "web-staging", "api-prod", "api-staging"} {
		n := swarm.Node{ID: name, Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}}
		n.Description.Hostname = name
		c.SetNode(n)
	}
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	// search narrows to "web-*", filter narrows to just one, pagination limits
	req := filterReq("/api/nodes", `name endsWith "-prod"`)
	q := req.URL.Query()
	q.Set("search", "web")
	q.Set("limit", "10")
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Fatalf("expected 1, got %d", resp.Total)
	}
	if resp.Items[0].Description.Hostname != "web-prod" {
		t.Errorf("expected web-prod, got %s", resp.Items[0].Description.Hostname)
	}
}

func TestHandleList_FilterMissingField(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	// expr treats missing env keys as nil, so this evaluates to false (no match)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/api/nodes", `nonexistent == "value"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 0 {
		t.Errorf("expected 0 nodes for missing field filter, got %d", resp.Total)
	}
}

func TestHandleListNodes_Search(t *testing.T) {
	c := cache.New(nil)
	n1 := swarm.Node{ID: "n1"}
	n1.Description.Hostname = "worker-01"
	n2 := swarm.Node{ID: "n2"}
	n2.Description.Hostname = "manager-01"
	c.SetNode(n1)
	c.SetNode(n2)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/nodes?search=worker", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp PagedResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Items))
	}
	if resp.Items[0].Description.Hostname != "worker-01" {
		t.Errorf("expected worker-01, got %s", resp.Items[0].Description.Hostname)
	}
}

func uint64Ptr(v uint64) *uint64 { return &v }

func TestHandleStackSummary(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(2)},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, "memory") {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"104857600"]}]}}`))
		} else {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"45.2"]}]}}`))
		}
	}))
	defer prom.Close()

	h := NewHandlers(c, nil, closedReady(), nil, NewPromClient(prom.URL))
	req := httptest.NewRequest("GET", "/api/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []cache.StackSummary
	json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Name != "myapp" {
		t.Errorf("name=%q, want myapp", summaries[0].Name)
	}
	if summaries[0].TasksByState["running"] != 2 {
		t.Errorf("running=%d, want 2", summaries[0].TasksByState["running"])
	}
	if summaries[0].MemoryUsageBytes != 104857600 {
		t.Errorf("memoryUsageBytes=%d, want 104857600", summaries[0].MemoryUsageBytes)
	}
}

func TestHandleStackSummary_PrometheusDown(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "myapp_web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: uint64Ptr(1)},
			},
		},
	})
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Status: swarm.TaskStatus{State: swarm.TaskStateRunning}})

	h := NewHandlers(c, nil, closedReady(), nil, NewPromClient("http://127.0.0.1:1"))
	req := httptest.NewRequest("GET", "/api/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var summaries []cache.StackSummary
	json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].MemoryUsageBytes != 0 {
		t.Errorf("expected 0 memory usage when prometheus is down, got %d", summaries[0].MemoryUsageBytes)
	}
}

// --- Secret data redaction tests ---

func TestHandleGetSecret_DataIsRedacted(t *testing.T) {
	c := cache.New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Name = "my-secret"
	sec.Spec.Data = []byte("super-secret")
	c.SetSecret(sec)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Error("response body contains secret data")
	}
	var resp struct {
		Secret   swarm.Secret       `json:"secret"`
		Services []cache.ServiceRef `json:"services"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Secret.Spec.Data != nil {
		t.Errorf("expected Spec.Data to be nil, got %v", resp.Secret.Spec.Data)
	}
}

func TestHandleListSecrets_DataIsRedacted(t *testing.T) {
	c := cache.New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Name = "my-secret"
	sec.Spec.Data = []byte("super-secret")
	c.SetSecret(sec)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Error("response body contains secret data")
	}
	var resp PagedResponse[swarm.Secret]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(resp.Items))
	}
	if resp.Items[0].Spec.Data != nil {
		t.Errorf("expected Spec.Data to be nil, got %v", resp.Items[0].Spec.Data)
	}
}

// --- Detail endpoint tests for config/secret/network/volume ---

func TestHandleGetConfig_Found(t *testing.T) {
	c := cache.New(nil)
	cfg := swarm.Config{ID: "cfg1"}
	cfg.Spec.Name = "app-config"
	c.SetConfig(cfg)

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "app-config"}},
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Config   swarm.Config       `json:"config"`
		Services []cache.ServiceRef `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Config.ID != "cfg1" {
		t.Errorf("config ID=%s, want cfg1", resp.Config.ID)
	}
	if len(resp.Services) != 1 || resp.Services[0].ID != "svc1" {
		t.Errorf("expected 1 service ref (svc1), got %v", resp.Services)
	}
}

func TestHandleGetConfig_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/configs/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetSecret_Found(t *testing.T) {
	c := cache.New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Name = "tls-cert"
	c.SetSecret(sec)

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Secrets: []*swarm.SecretReference{{SecretID: "sec1", SecretName: "tls-cert"}},
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Secret   swarm.Secret       `json:"secret"`
		Services []cache.ServiceRef `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Secret.ID != "sec1" {
		t.Errorf("secret ID=%s, want sec1", resp.Secret.ID)
	}
	if len(resp.Services) != 1 || resp.Services[0].ID != "svc1" {
		t.Errorf("expected 1 service ref (svc1), got %v", resp.Services)
	}
}

func TestHandleGetSecret_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/secrets/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetNetwork_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "web_overlay", Driver: "overlay"})

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "net1"}}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleGetNetwork(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Network  network.Summary    `json:"network"`
		Services []cache.ServiceRef `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Network.ID != "net1" {
		t.Errorf("network ID=%s, want net1", resp.Network.ID)
	}
	if len(resp.Services) != 1 || resp.Services[0].ID != "svc1" {
		t.Errorf("expected 1 service ref (svc1), got %v", resp.Services)
	}
}

func TestHandleGetNetwork_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/networks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNetwork(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleGetVolume_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol", Driver: "local"})

	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "db"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "data-vol"}},
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/volumes/data-vol", nil)
	req.SetPathValue("name", "data-vol")
	w := httptest.NewRecorder()
	h.HandleGetVolume(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Volume   volume.Volume      `json:"volume"`
		Services []cache.ServiceRef `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Volume.Name != "data-vol" {
		t.Errorf("volume name=%s, want data-vol", resp.Volume.Name)
	}
	if len(resp.Services) != 1 || resp.Services[0].ID != "svc1" {
		t.Errorf("expected 1 service ref (svc1), got %v", resp.Services)
	}
}

func TestHandleGetVolume_NotFound(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/volumes/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleGetVolume(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Task list with nil ContainerSpec ---

func TestHandleSearch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "nginx-web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25-alpine"},
			},
		},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx.conf"}},
	})
	c.SetNetwork(network.Summary{ID: "net1", Name: "nginx-net", Driver: "overlay"})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Spec:        swarm.NodeSpec{Annotations: swarm.Annotations{Name: "worker-1"}},
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=nginx", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body struct {
		Query   string                       `json:"query"`
		Results map[string][]json.RawMessage `json:"results"`
		Counts  map[string]int               `json:"counts"`
		Total   int                          `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Query != "nginx" {
		t.Errorf("query=%q, want %q", body.Query, "nginx")
	}
	if body.Total != 3 {
		t.Errorf("total=%d, want 3", body.Total)
	}
	// Should match service, config, network — not the node (hostname is "worker-1")
	if len(body.Results["services"]) != 1 {
		t.Errorf("services results=%d, want 1", len(body.Results["services"]))
	}
	if len(body.Results["configs"]) != 1 {
		t.Errorf("configs results=%d, want 1", len(body.Results["configs"]))
	}
	if len(body.Results["networks"]) != 1 {
		t.Errorf("networks results=%d, want 1", len(body.Results["networks"]))
	}
	if len(body.Results["nodes"]) != 0 {
		t.Errorf("nodes results=%d, want 0", len(body.Results["nodes"]))
	}
}

func TestHandleSearch_MatchesLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{
		Name:   "data-vol",
		Labels: map[string]string{"team": "nginx-platform"},
	})

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=nginx", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body struct {
		Total   int                          `json:"total"`
		Results map[string][]json.RawMessage `json:"results"`
		Counts  map[string]int               `json:"counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Counts["volumes"] != 1 {
		t.Errorf("volumes count=%d, want 1", body.Counts["volumes"])
	}
	if body.Total != 1 {
		t.Errorf("total=%d, want 1", body.Total)
	}
}

func TestHandleSearch_EmptyQuery(t *testing.T) {
	h := NewHandlers(cache.New(nil), nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleSearch_CapsAtThreePerType(t *testing.T) {
	c := cache.New(nil)
	for i := 0; i < 5; i++ {
		c.SetService(swarm.Service{
			ID: fmt.Sprintf("svc%d", i),
			Spec: swarm.ServiceSpec{
				Annotations:  swarm.Annotations{Name: fmt.Sprintf("web-%d", i)},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
			},
		})
	}

	h := NewHandlers(c, nil, closedReady(), nil, nil)
	req := httptest.NewRequest("GET", "/api/search?q=web", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body struct {
		Results map[string][]json.RawMessage `json:"results"`
		Counts  map[string]int               `json:"counts"`
		Total   int                          `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Results["services"]) != 3 {
		t.Errorf("services results=%d, want 3", len(body.Results["services"]))
	}
	if body.Counts["services"] != 5 {
		t.Errorf("services count=%d, want 5", body.Counts["services"])
	}
	if body.Total != 5 {
		t.Errorf("total=%d, want 5", body.Total)
	}
}

func TestHandleListTasks_NilContainerSpec(t *testing.T) {
	c := cache.New(nil)
	// Task with nil ContainerSpec should not cause a panic
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc1", Spec: swarm.TaskSpec{ContainerSpec: nil}})
	c.SetTask(swarm.Task{ID: "t2", ServiceID: "svc2", Spec: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx"}}})
	h := NewHandlers(c, nil, closedReady(), nil, nil)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp PagedResponse[swarm.Task]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 tasks, got %d", resp.Total)
	}
}
