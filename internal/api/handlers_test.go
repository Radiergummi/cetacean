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

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func closedReady() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestHandleHealth(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)
	req := httptest.NewRequest("GET", "/-/health", nil)
	w := httptest.NewRecorder()
	h.HandleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "version", "commit", "buildDate", "operationsLevel"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in health response", key)
		}
	}
}

func TestHandleReady_NotReady(t *testing.T) {
	ch := make(chan struct{}) // not closed = not ready
	h := NewHandlers(cache.New(nil), nil, nil, nil, nil, nil, ch, nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/-/ready", nil)
	w := httptest.NewRecorder()
	h.HandleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}

func TestHandleReady_Ready(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)
	req := httptest.NewRequest("GET", "/-/ready", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/cluster", nil)
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["@context"] == nil {
		t.Error("response missing @context")
	}
	if body["@type"] != "Cluster" {
		t.Errorf("@type=%v, want Cluster", body["@type"])
	}
	if body["nodeCount"].(float64) != 1 || body["serviceCount"].(float64) != 1 {
		t.Errorf(
			"unexpected snapshot: nodeCount=%v serviceCount=%v",
			body["nodeCount"],
			body["serviceCount"],
		)
	}
}

func TestHandleClusterMetrics_NoPrometheus(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/cluster/metrics", nil)
	w := httptest.NewRecorder()
	h.HandleClusterMetrics(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleClusterCapacity(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "n1",
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    4000000000,
				MemoryBytes: 8589934592,
			},
		},
	})
	c.SetNode(swarm.Node{
		ID: "n2",
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{
				NanoCPUs:    8000000000,
				MemoryBytes: 4294967296,
			},
		},
	})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/cluster/capacity", nil)
	w := httptest.NewRecorder()
	h.HandleClusterCapacity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["@type"] != "ClusterCapacity" {
		t.Errorf("@type=%v, want ClusterCapacity", body["@type"])
	}
	if body["maxNodeCPU"].(float64) != 8 {
		t.Errorf("maxNodeCPU=%v, want 8", body["maxNodeCPU"])
	}
	if body["maxNodeMemory"].(float64) != 8589934592 {
		t.Errorf("maxNodeMemory=%v, want 8589934592", body["maxNodeMemory"])
	}
	if body["nodeCount"].(float64) != 2 {
		t.Errorf("nodeCount=%v, want 2", body["nodeCount"])
	}
}

func TestHandleClusterMetrics_WithPrometheus(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var val string
		switch {
		case strings.Contains(query, "cpu_seconds"):
			val = "0.62"
		case strings.Contains(query, "MemTotal"):
			val = "47400000000"
		case strings.Contains(query, "filesystem_size"):
			val = "500000000000"
		case strings.Contains(query, "filesystem_avail"):
			val = "295000000000"
		default:
			val = "0"
		}
		body := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"` + val + `"]}]}}`
		fmt.Fprint(w, body)
	}))
	defer prom.Close()

	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:     "n1",
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{
			Resources: swarm.Resources{NanoCPUs: 4_000_000_000, MemoryBytes: 64_000_000_000},
		},
	})
	h := NewHandlers(
		c,
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		NewPromClient(prom.URL),
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/cluster/metrics", nil)
	w := httptest.NewRecorder()
	h.HandleClusterMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		CPU struct {
			Percent float64 `json:"percent"`
		} `json:"cpu"`
		Memory struct {
			Percent float64 `json:"percent"`
		} `json:"memory"`
		Disk struct {
			Percent float64 `json:"percent"`
		} `json:"disk"`
	}
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck // test code
	if resp.CPU.Percent == 0 {
		t.Error("expected non-zero CPU percent")
	}
	if resp.Memory.Percent == 0 {
		t.Error("expected non-zero Memory percent")
	}
	if resp.Disk.Percent == 0 {
		t.Error("expected non-zero Disk percent")
	}
}

func TestHandleMonitoringStatus_NoPrometheus(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/-/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp MonitoringStatus
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=false")
	}
	if resp.PrometheusReachable {
		t.Error("expected prometheusReachable=false")
	}
	if resp.NodeExporter != nil {
		t.Error("expected nodeExporter=nil")
	}
	if resp.Cadvisor != nil {
		t.Error("expected cadvisor=nil")
	}
}

func TestHandleMonitoringStatus_WithPrometheus(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var body string
		switch {
		case strings.Contains(query, "node-exporter"):
			body = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"host1:9100"},"value":[1,"1"]},{"metric":{"instance":"host2:9100"},"value":[1,"1"]}]}}`
		case strings.Contains(query, "cadvisor"):
			body = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"host1:8080"},"value":[1,"1"]},{"metric":{"instance":"host2:8080"},"value":[1,"1"]}]}}`
		default:
			body = `{"status":"success","data":{"resultType":"vector","result":[]}}`
		}
		fmt.Fprint(w, body)
	}))
	defer prom.Close()

	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	c.SetNode(swarm.Node{ID: "n2", Status: swarm.NodeStatus{State: swarm.NodeStateReady}})
	h := NewHandlers(
		c,
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		NewPromClient(prom.URL),
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/-/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MonitoringStatus
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=true")
	}
	if !resp.PrometheusReachable {
		t.Error("expected prometheusReachable=true")
	}
	if resp.NodeExporter == nil {
		t.Fatal("expected nodeExporter to be non-nil")
	}
	if resp.NodeExporter.Targets != 2 {
		t.Errorf("expected nodeExporter.targets=2, got %d", resp.NodeExporter.Targets)
	}
	if resp.NodeExporter.Nodes != 2 {
		t.Errorf("expected nodeExporter.nodes=2, got %d", resp.NodeExporter.Nodes)
	}
	if resp.Cadvisor == nil {
		t.Fatal("expected cadvisor to be non-nil")
	}
	if resp.Cadvisor.Targets != 2 {
		t.Errorf("expected cadvisor.targets=2, got %d", resp.Cadvisor.Targets)
	}
	if resp.Cadvisor.Nodes != 2 {
		t.Errorf("expected cadvisor.nodes=2, got %d", resp.Cadvisor.Nodes)
	}
}

func TestHandleMonitoringStatus_PrometheusUnreachable(t *testing.T) {
	// Point to a URL that will refuse connections
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		NewPromClient("http://127.0.0.1:19999"),
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/-/metrics/status", nil)
	w := httptest.NewRecorder()
	h.HandleMonitoringStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp MonitoringStatus
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.PrometheusConfigured {
		t.Error("expected prometheusConfigured=true")
	}
	if resp.PrometheusReachable {
		t.Error("expected prometheusReachable=false")
	}
	if resp.NodeExporter != nil {
		t.Error("expected nodeExporter=nil")
	}
	if resp.Cadvisor != nil {
		t.Error("expected cadvisor=nil")
	}
}

func TestHandleListNodes(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes/n1", nil)
	req.SetPathValue("id", "n1")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes/missing", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services/svc1/tasks", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[EnrichedTask]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", resp.Items[0].ID)
	}
	if resp.Total != 1 {
		t.Errorf("expected total 1, got %d", resp.Total)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes/node1/tasks", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleNodeTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[EnrichedTask]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "t1" {
		t.Errorf("expected task t1, got %s", resp.Items[0].ID)
	}
	if resp.Total != 1 {
		t.Errorf("expected total 1, got %d", resp.Total)
	}
}

func TestHandleListServices_Paginated(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		svc := swarm.Service{ID: name}
		svc.Spec.Name = name
		c.SetService(svc)
	}
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services?limit=2&sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[ServiceListItem]
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
	svc.Spec.Mode.Replicated = &swarm.ReplicatedService{
		Replicas: func() *uint64 { v := uint64(3); return &v }(),
	}
	c.SetService(svc)
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t3",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp CollectionResponse[ServiceListItem]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes?limit=1&sort=hostname", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[ServiceListItem]
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
	c.SetService(
		swarm.Service{
			ID:   "s1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
		},
	)
	c.SetService(
		swarm.Service{
			ID:   "s2",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "redis"}},
		},
	)

	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/history?type=service", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var resp CollectionResponse[cache.HistoryEntry]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Errorf("got %d entries, want 2", len(resp.Items))
	}
	// Newest first
	if len(resp.Items) > 0 && resp.Items[0].Name != "redis" {
		t.Errorf("first entry name=%s, want redis", resp.Items[0].Name)
	}
}

func TestHandleHistory_FilterByResource(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "s1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
		},
	)
	c.SetNode(swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-01"}})

	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/history?resourceId=s1", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var resp CollectionResponse[cache.HistoryEntry]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Errorf("got %d entries, want 1", len(resp.Items))
	}
}

func TestHandleGetService_Found(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "nginx"
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetService_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/services/missing", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/tasks/t1", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetTask_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/tasks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleListTasks(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "svc2",
			Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/tasks?sort=state", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Errorf("total=%d, want 2", resp.Total)
	}
}

func TestHandleServiceTasks_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/services/missing/tasks", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleServiceTasks(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleNodeTasks_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/nodes/missing/tasks", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/stacks", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[cache.Stack]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/stacks?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	var resp CollectionResponse[cache.Stack]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Name)
	}
}

func TestHandleListConfigs_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(
		swarm.Config{
			ID:   "c1",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "zebra"}},
		},
	)
	c.SetConfig(
		swarm.Config{
			ID:   "c2",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "alpha"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/configs?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	var resp CollectionResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Spec.Name)
	}
}

func TestHandleListSecrets_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(
		swarm.Secret{
			ID:   "s1",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "zebra"}},
		},
	)
	c.SetSecret(
		swarm.Secret{
			ID:   "s2",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "alpha"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/secrets?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	var resp CollectionResponse[swarm.Secret]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Spec.Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Spec.Name)
	}
}

func TestHandleListNetworks_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "zebra"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "alpha"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/networks?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	var resp CollectionResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Name != "alpha" {
		t.Errorf("first=%s, want alpha", resp.Items[0].Name)
	}
}

func TestHandleListVolumes_SortByName(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "zebra"})
	c.SetVolume(volume.Volume{Name: "alpha"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/volumes?sort=name", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	var resp CollectionResponse[volume.Volume]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/stacks?search=front", nil)
	w := httptest.NewRecorder()
	h.HandleListStacks(w, req)

	var resp CollectionResponse[cache.Stack]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/stacks/mystack", nil)
	req.SetPathValue("name", "mystack")
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var wrapper struct {
		Context string            `json:"@context"`
		ID      string            `json:"@id"`
		Type    string            `json:"@type"`
		Stack   cache.StackDetail `json:"stack"`
	}
	json.NewDecoder(w.Body).Decode(&wrapper)
	if wrapper.Stack.Name != "mystack" {
		t.Errorf("name=%s, want mystack", wrapper.Stack.Name)
	}
	if wrapper.Type != "Stack" {
		t.Errorf("@type=%s, want Stack", wrapper.Type)
	}
}

func TestHandleGetStack_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/stacks/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleGetStack(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleListConfigs(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(
		swarm.Config{
			ID:   "c1",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
		},
	)
	c.SetConfig(
		swarm.Config{
			ID:   "c2",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "db-config"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/configs?search=app", nil)
	w := httptest.NewRecorder()
	h.HandleListConfigs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 config, got %d", len(resp.Items))
	}
}

func TestHandleListSecrets(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(
		swarm.Secret{
			ID:   "s1",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}},
		},
	)
	c.SetSecret(
		swarm.Secret{
			ID:   "s2",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "api-key"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/secrets?search=tls", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Secret]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(resp.Items))
	}
}

func TestHandleListNetworks(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "web_overlay", Driver: "overlay"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "db_bridge", Driver: "bridge"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/networks?search=web", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 network, got %d", len(resp.Items))
	}
}

func TestHandleListVolumes(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol", Driver: "local"})
	c.SetVolume(volume.Volume{Name: "cache-vol", Driver: "local"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/volumes?search=data", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[volume.Volume]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(resp.Items))
	}
}

func TestHandleTaskLogs_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/tasks/missing/logs", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleTaskLogs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleHistory_InvalidLimit(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "s1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "nginx"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	// invalid limit should fall back to default (50)
	req := httptest.NewRequest("GET", "/history?limit=abc", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/history?limit=2", nil)
	w := httptest.NewRecorder()
	h.HandleHistory(w, req)

	var resp CollectionResponse[cache.HistoryEntry]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 2 {
		t.Errorf("got %d entries, want 2", len(resp.Items))
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes?sort=role", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes?sort=status", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes?sort=availability", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services?sort=mode", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp CollectionResponse[ServiceListItem]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/tasks?sort=service", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	var resp CollectionResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].ServiceID != "svc-a" {
		t.Errorf("first serviceID=%s, want svc-a", resp.Items[0].ServiceID)
	}
}

func TestHandleListTasks_SortByNode(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", NodeID: "node-b"})
	c.SetTask(swarm.Task{ID: "t2", NodeID: "node-a"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/tasks?sort=node", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	var resp CollectionResponse[swarm.Task]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].NodeID != "node-a" {
		t.Errorf("first nodeID=%s, want node-a", resp.Items[0].NodeID)
	}
}

func TestHandleListNetworks_SortByDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "n1", Name: "net1", Driver: "overlay"})
	c.SetNetwork(network.Summary{ID: "n2", Name: "net2", Driver: "bridge"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/networks?sort=driver", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	var resp CollectionResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items[0].Driver != "bridge" {
		t.Errorf("first driver=%s, want bridge", resp.Items[0].Driver)
	}
}

func TestHandleListVolumes_SortByDriver(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "vol1", Driver: "nfs"})
	c.SetVolume(volume.Volume{Name: "vol2", Driver: "local"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/volumes?sort=driver", nil)
	w := httptest.NewRecorder()
	h.HandleListVolumes(w, req)

	var resp CollectionResponse[volume.Volume]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", `role == "manager"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Node]
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
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "svc2",
			Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t3",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateFailed},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListTasks(w, filterReq("/tasks", `state == "failed" && service == "svc1"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Task]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListServices(w, filterReq("/services", `image contains "nginx"`))

	var resp CollectionResponse[ServiceListItem]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListNetworks(w, filterReq("/networks", `driver == "overlay"`))

	var resp CollectionResponse[network.Summary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 network, got %d", len(resp.Items))
	}
}

func TestHandleList_FilterInvalid(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", "invalid ==="))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleList_FilterTooLong(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)
	long := strings.Repeat("a", 513)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", long))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleList_FilterEmpty(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	// Empty filter should return all items
	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", ""))

	var resp CollectionResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("expected 1 node with empty filter, got %d", resp.Total)
	}
}

func TestHandleList_FilterNoMatch(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1", Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", `role == "manager"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 0 {
		t.Errorf("expected 0 nodes, got %d", resp.Total)
	}
}

func TestHandleListVolumes_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "data-vol", Driver: "local"})
	c.SetVolume(volume.Volume{Name: "cache-vol", Driver: "nfs"})
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListVolumes(w, filterReq("/volumes", `driver == "nfs"`))

	var resp CollectionResponse[volume.Volume]
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
	c.SetConfig(
		swarm.Config{
			ID:   "c1",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app-config"}},
		},
	)
	c.SetConfig(
		swarm.Config{
			ID:   "c2",
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "db-config"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListConfigs(w, filterReq("/configs", `name contains "app"`))

	var resp CollectionResponse[swarm.Config]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 config, got %d", len(resp.Items))
	}
}

func TestHandleListSecrets_Filter(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(
		swarm.Secret{
			ID:   "s1",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tls-cert"}},
		},
	)
	c.SetSecret(
		swarm.Secret{
			ID:   "s2",
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "api-key"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListSecrets(w, filterReq("/secrets", `name startsWith "tls"`))

	var resp CollectionResponse[swarm.Secret]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	w := httptest.NewRecorder()
	h.HandleListStacks(w, filterReq("/stacks", `services > 0`))

	var resp CollectionResponse[cache.Stack]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	// search narrows to "web-*", filter narrows to just one, pagination limits
	req := filterReq("/nodes", `name endsWith "-prod"`)
	q := req.URL.Query()
	q.Set("search", "web")
	q.Set("limit", "10")
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	// expr treats missing env keys as nil, so this evaluates to false (no match)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, filterReq("/nodes", `nonexistent == "value"`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Node]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/nodes?search=worker", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	var resp CollectionResponse[swarm.Node]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Items))
	}
	if resp.Items[0].Description.Hostname != "worker-01" {
		t.Errorf("expected worker-01, got %s", resp.Items[0].Description.Hostname)
	}
}

//go:fix inline
func uint64Ptr(v uint64) *uint64 { return new(v) }

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
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)

	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, "memory") {
			w.Write(
				[]byte(
					`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"104857600"]}]}}`,
				),
			)
		} else {
			w.Write(
				[]byte(
					`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"container_label_com_docker_stack_namespace":"myapp"},"value":[1234567890,"45.2"]}]}}`,
				),
			)
		}
	}))
	defer prom.Close()

	h := NewHandlers(
		c,
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		NewPromClient(prom.URL),
		config.OpsImpactful,
		nil,
	)
	req := httptest.NewRequest("GET", "/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[cache.StackSummary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(resp.Items))
	}
	if resp.Items[0].Name != "myapp" {
		t.Errorf("name=%q, want myapp", resp.Items[0].Name)
	}
	if resp.Items[0].TasksByState["running"] != 2 {
		t.Errorf("running=%d, want 2", resp.Items[0].TasksByState["running"])
	}
	if resp.Items[0].MemoryUsageBytes != 104857600 {
		t.Errorf("memoryUsageBytes=%d, want 104857600", resp.Items[0].MemoryUsageBytes)
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
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "svc1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		},
	)

	h := NewHandlers(
		c,
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		NewPromClient("http://127.0.0.1:1"),
		config.OpsImpactful,
		nil,
	)
	req := httptest.NewRequest("GET", "/stacks/summary", nil)
	w := httptest.NewRecorder()
	h.HandleStackSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CollectionResponse[cache.StackSummary]
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(resp.Items))
	}
	if resp.Items[0].MemoryUsageBytes != 0 {
		t.Errorf(
			"expected 0 memory usage when prometheus is down, got %d",
			resp.Items[0].MemoryUsageBytes,
		)
	}
}

// --- Secret data redaction tests ---

func TestHandleGetSecret_DataIsRedacted(t *testing.T) {
	c := cache.New(nil)
	sec := swarm.Secret{ID: "sec1"}
	sec.Spec.Name = "my-secret"
	sec.Spec.Data = []byte("super-secret")
	c.SetSecret(sec)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/secrets/sec1", nil)
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
		Context  string             `json:"@context"`
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/secrets", nil)
	w := httptest.NewRecorder()
	h.HandleListSecrets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Error("response body contains secret data")
	}
	var resp CollectionResponse[swarm.Secret]
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/configs/cfg1", nil)
	req.SetPathValue("id", "cfg1")
	w := httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Context  string             `json:"@context"`
		ID       string             `json:"@id"`
		Type     string             `json:"@type"`
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
	if resp.Type != "Config" {
		t.Errorf("@type=%s, want Config", resp.Type)
	}
}

func TestHandleGetConfig_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/configs/missing", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/secrets/sec1", nil)
	req.SetPathValue("id", "sec1")
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Context  string             `json:"@context"`
		ID       string             `json:"@id"`
		Type     string             `json:"@type"`
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
	if resp.Type != "Secret" {
		t.Errorf("@type=%s, want Secret", resp.Type)
	}
}

func TestHandleGetSecret_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/secrets/missing", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/networks/net1", nil)
	req.SetPathValue("id", "net1")
	w := httptest.NewRecorder()
	h.HandleGetNetwork(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Context  string             `json:"@context"`
		ID       string             `json:"@id"`
		Type     string             `json:"@type"`
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
	if resp.Type != "Network" {
		t.Errorf("@type=%s, want Network", resp.Type)
	}
}

func TestHandleGetNetwork_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/networks/missing", nil)
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
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/volumes/data-vol", nil)
	req.SetPathValue("name", "data-vol")
	w := httptest.NewRecorder()
	h.HandleGetVolume(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Context  string             `json:"@context"`
		ID       string             `json:"@id"`
		Type     string             `json:"@type"`
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
	if resp.Type != "Volume" {
		t.Errorf("@type=%s, want Volume", resp.Type)
	}
}

func TestHandleGetVolume_NotFound(t *testing.T) {
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)

	req := httptest.NewRequest("GET", "/volumes/missing", nil)
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

	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/search?q=nginx", nil)
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

	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/search?q=nginx", nil)
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
	h := NewHandlers(
		cache.New(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		closedReady(),
		nil,
		config.OpsImpactful,
		nil,
	)
	req := httptest.NewRequest("GET", "/search", nil)
	w := httptest.NewRecorder()
	h.HandleSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleSearch_CapsAtThreePerType(t *testing.T) {
	c := cache.New(nil)
	for i := range 5 {
		c.SetService(swarm.Service{
			ID: fmt.Sprintf("svc%d", i),
			Spec: swarm.ServiceSpec{
				Annotations:  swarm.Annotations{Name: fmt.Sprintf("web-%d", i)},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}},
			},
		})
	}

	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)
	req := httptest.NewRequest("GET", "/search?q=web", nil)
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
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "svc2",
			Spec:      swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx"}},
		},
	)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/tasks", nil)
	w := httptest.NewRecorder()
	h.HandleListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CollectionResponse[swarm.Task]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 tasks, got %d", resp.Total)
	}
}

func TestSegmentPrefixMatch(t *testing.T) {
	tests := []struct {
		query, target string
		want          bool
	}{
		// Exact prefix
		{"go_gc", "go_gc_cleanups_total", true},
		// Abbreviated segments with underscores
		{"ggclext", "go_gc_cleanups_executed_cleanups_total", true},
		// Skipping segments
		{"gotot", "go_gc_cleanups_executed_cleanups_total", true},
		// Single char per segment
		{"ggt", "go_gc_total", true},
		// Out of order — should fail
		{"tg", "go_total", false},
		// Characters not in any segment
		{"gx", "go_gc_total", false},
		// Single-segment targets are skipped (covered by substring match)
		{"up", "up", false},
		// Hyphen-separated names (Docker convention)
		{"mws", "my-web-server", true},
		{"monpro", "monitoring-prometheus", true},
		// Mixed separators
		{"swn", "stack_web-node", true},
		// Empty query matches everything
		{"", "anything", true},
		// Backtracking required (greedy would fail)
		{"gcl", "go_gc_cleanups", true},
		// Multi-char prefix across segments
		{"contcpu", "container_cpu_usage_seconds_total", true},
		// Substring that's not a segment prefix
		{"lean", "go_gc_cleanups", false},
		// Query longer than target
		{"abcdef", "ab_cd", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_in_%s", tt.query, tt.target), func(t *testing.T) {
			got := segmentPrefixMatch(tt.target, tt.query)
			if got != tt.want {
				t.Errorf(
					"segmentPrefixMatch(%q, %q) = %v, want %v",
					tt.target,
					tt.query,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestHandleGetService_WithTraefikIntegration(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.Labels = map[string]string{
		"traefik.enable":                                     "true",
		"traefik.http.routers.web.rule":                      "Host(`example.com`)",
		"traefik.http.services.web.loadbalancer.server.port": "8080",
		"app.version":                                        "1.0",
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["integrations"]; !ok {
		t.Error("expected integrations field in response")
	}
}

func TestHandleGetService_NoIntegrationsField(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "plain"
	svc.Spec.Labels = map[string]string{
		"app.version": "1.0",
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful, nil)

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["integrations"]; ok {
		t.Error("expected no integrations field when none detected")
	}
}
