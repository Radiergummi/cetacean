package recommendations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/prom"
)

func mockOperationalQuery(diskResults, memResults []prom.Result) QueryFunc {
	return func(_ context.Context, query string) ([]prom.Result, error) {
		if strings.Contains(query, "filesystem") {
			return diskResults, nil
		}

		if strings.Contains(query, "MemAvailable") {
			return memResults, nil
		}

		return nil, nil
	}
}

func newOperationalCache(services []swarm.Service, nodes []swarm.Node) *cache.Cache {
	c := cache.New(nil)
	for _, svc := range services {
		c.SetService(svc)
	}

	for _, node := range nodes {
		c.SetNode(node)
	}

	return c
}

// recordRestarts plays n failure events for the given service through the
// cache so the RestartTracker increments naturally.
func recordRestarts(c *cache.Cache, serviceID string, n int) {
	now := time.Now()
	for i := range n {
		taskID := serviceID + "-task-" + time.Duration(i).String()
		c.SetTask(swarm.Task{
			ID:        taskID,
			ServiceID: serviceID,
			Status: swarm.TaskStatus{
				State:     swarm.TaskStateFailed,
				Timestamp: now.Add(-time.Duration(i) * time.Minute),
			},
		})
	}
}

func TestOperationalChecker_FlakyServiceAboveThreshold(t *testing.T) {
	c := newOperationalCache([]swarm.Service{
		{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
	}, nil)
	recordRestarts(c, "svc1", 10)

	query := mockOperationalQuery(nil, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	r := recs[0]
	if r.Category != CategoryFlakyService {
		t.Errorf("expected category %q, got %q", CategoryFlakyService, r.Category)
	}

	if r.Severity != SeverityWarning {
		t.Errorf("expected severity %q, got %q", SeverityWarning, r.Severity)
	}

	if r.Scope != ScopeService {
		t.Errorf("expected scope %q, got %q", ScopeService, r.Scope)
	}

	if r.TargetID != "svc1" {
		t.Errorf("expected targetId %q, got %q", "svc1", r.TargetID)
	}

	if r.TargetName != "web" {
		t.Errorf("expected targetName %q, got %q", "web", r.TargetName)
	}

	if !strings.Contains(r.Message, "10 task failures") {
		t.Errorf("expected message to mention '10 task failures', got %q", r.Message)
	}
}

func TestOperationalChecker_FlakyServiceBelowThreshold(t *testing.T) {
	c := newOperationalCache([]swarm.Service{
		{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
	}, nil)
	recordRestarts(c, "svc1", 3)

	query := mockOperationalQuery(nil, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
	}
}

func TestOperationalChecker_FlakyService_RunsWithoutPrometheus(t *testing.T) {
	c := newOperationalCache([]swarm.Service{
		{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
	}, nil)
	recordRestarts(c, "svc1", 10)

	oc := NewOperationalChecker(nil, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation with nil query, got %d", len(recs))
	}

	if recs[0].Category != CategoryFlakyService {
		t.Errorf("expected flaky service rec, got %q", recs[0].Category)
	}
}

func TestOperationalChecker_NodeDiskAboveThreshold(t *testing.T) {
	c := newOperationalCache(nil, []swarm.Node{
		{
			ID:          "node1",
			Description: swarm.NodeDescription{Hostname: "worker1"},
			Status:      swarm.NodeStatus{Addr: "192.168.1.10"},
		},
	})

	diskResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10:9100"}, Value: 95},
	}
	query := mockOperationalQuery(diskResults, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	r := recs[0]
	if r.Category != CategoryNodeDiskFull {
		t.Errorf("expected category %q, got %q", CategoryNodeDiskFull, r.Category)
	}

	if r.Severity != SeverityCritical {
		t.Errorf("expected severity %q, got %q", SeverityCritical, r.Severity)
	}

	if r.Scope != ScopeNode {
		t.Errorf("expected scope %q, got %q", ScopeNode, r.Scope)
	}

	if r.TargetID != "node1" {
		t.Errorf("expected targetId %q, got %q", "node1", r.TargetID)
	}
}

func TestOperationalChecker_NodeDiskBelowThreshold(t *testing.T) {
	c := newOperationalCache(nil, []swarm.Node{
		{
			ID:          "node1",
			Description: swarm.NodeDescription{Hostname: "worker1"},
			Status:      swarm.NodeStatus{Addr: "192.168.1.10"},
		},
	})

	diskResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10:9100"}, Value: 85},
	}
	query := mockOperationalQuery(diskResults, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
	}
}

func TestOperationalChecker_NodeDiskHostnameInstance(t *testing.T) {
	c := newOperationalCache(nil, []swarm.Node{
		{
			ID:          "node1",
			Description: swarm.NodeDescription{Hostname: "worker1"},
			Status:      swarm.NodeStatus{Addr: "192.168.1.10"},
		},
	})

	diskResults := []prom.Result{
		{Labels: map[string]string{"instance": "worker1:9100"}, Value: 95},
	}
	query := mockOperationalQuery(diskResults, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	if recs[0].TargetID != "node1" {
		t.Errorf("expected targetId %q, got %q", "node1", recs[0].TargetID)
	}

	if recs[0].TargetName != "worker1" {
		t.Errorf("expected targetName %q, got %q", "worker1", recs[0].TargetName)
	}
}

func TestOperationalChecker_NodeMemoryAboveThreshold(t *testing.T) {
	c := newOperationalCache(nil, []swarm.Node{
		{
			ID:          "node1",
			Description: swarm.NodeDescription{Hostname: "worker1"},
			Status:      swarm.NodeStatus{Addr: "192.168.1.10"},
		},
	})

	memResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10:9100"}, Value: 95},
	}
	query := mockOperationalQuery(nil, memResults)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	r := recs[0]
	if r.Category != CategoryNodeMemPressure {
		t.Errorf("expected category %q, got %q", CategoryNodeMemPressure, r.Category)
	}

	if r.Severity != SeverityCritical {
		t.Errorf("expected severity %q, got %q", SeverityCritical, r.Severity)
	}
}

func TestOperationalChecker_AllHealthy(t *testing.T) {
	c := newOperationalCache(
		[]swarm.Service{
			{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
		},
		[]swarm.Node{
			{
				ID:          "node1",
				Description: swarm.NodeDescription{Hostname: "worker1"},
				Status:      swarm.NodeStatus{Addr: "192.168.1.10"},
			},
		},
	)
	recordRestarts(c, "svc1", 1)

	diskResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10:9100"}, Value: 50},
	}
	memResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10:9100"}, Value: 60},
	}
	query := mockOperationalQuery(diskResults, memResults)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
	}
}
