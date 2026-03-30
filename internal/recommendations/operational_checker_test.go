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

func mockOperationalQuery(flakyResults, diskResults, memResults []prom.Result) QueryFunc {
	return func(_ context.Context, query string) ([]prom.Result, error) {
		if strings.Contains(query, "container_last_seen") {
			return flakyResults, nil
		}

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

func TestOperationalChecker_FlakyServiceAboveThreshold(t *testing.T) {
	c := newOperationalCache([]swarm.Service{
		{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
	}, nil)

	flakyResults := []prom.Result{
		{Labels: map[string]string{serviceLabelKey: "web"}, Value: 10},
	}
	query := mockOperationalQuery(flakyResults, nil, nil)
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
}

func TestOperationalChecker_FlakyServiceBelowThreshold(t *testing.T) {
	c := newOperationalCache([]swarm.Service{
		{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}},
	}, nil)

	flakyResults := []prom.Result{
		{Labels: map[string]string{serviceLabelKey: "web"}, Value: 3},
	}
	query := mockOperationalQuery(flakyResults, nil, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
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
		{Labels: map[string]string{"instance": "192.168.1.10"}, Value: 95},
	}
	query := mockOperationalQuery(nil, diskResults, nil)
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
		{Labels: map[string]string{"instance": "192.168.1.10"}, Value: 85},
	}
	query := mockOperationalQuery(nil, diskResults, nil)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
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
		{Labels: map[string]string{"instance": "192.168.1.10"}, Value: 95},
	}
	query := mockOperationalQuery(nil, nil, memResults)
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

	flakyResults := []prom.Result{
		{Labels: map[string]string{serviceLabelKey: "web"}, Value: 1},
	}
	diskResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10"}, Value: 50},
	}
	memResults := []prom.Result{
		{Labels: map[string]string{"instance": "192.168.1.10"}, Value: 60},
	}
	query := mockOperationalQuery(flakyResults, diskResults, memResults)
	oc := NewOperationalChecker(query, c, 24*time.Hour)

	recs := oc.Check(context.Background())

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d: %+v", len(recs), recs)
	}
}
