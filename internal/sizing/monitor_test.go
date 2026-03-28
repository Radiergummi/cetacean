package sizing

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func mockQuery(cpuResults, memResults, cpuP95Results, memP95Results []PromResult) QueryFunc {
	return func(_ context.Context, query string) ([]PromResult, error) {
		isP95 := strings.Contains(query, "quantile")

		if strings.Contains(query, "cpu_usage") {
			if isP95 {
				return cpuP95Results, nil
			}

			return cpuResults, nil
		}

		if strings.Contains(query, "memory_usage") {
			if isP95 {
				return memP95Results, nil
			}

			return memResults, nil
		}

		return nil, nil
	}
}

func TestMonitor_NilSafe(t *testing.T) {
	var m *Monitor

	results := m.Results()

	if results == nil {
		t.Error("nil monitor should return non-nil empty slice")
	}

	if len(results) != 0 {
		t.Error("nil monitor should return empty results")
	}
}

func TestMonitor_SingleTick(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				Resources: &swarm.ResourceRequirements{
					Limits:       &swarm.Limit{NanoCPUs: 1e9, MemoryBytes: 1 << 30},
					Reservations: &swarm.Resources{NanoCPUs: 0.5e9, MemoryBytes: 512 << 20},
				},
			},
		},
	})

	instantCPU := []PromResult{{Labels: map[string]string{serviceLabelKey: "web"}, Value: 90}}
	instantMem := []PromResult{{Labels: map[string]string{serviceLabelKey: "web"}, Value: float64(400 << 20)}}
	p95CPU := []PromResult{{Labels: map[string]string{serviceLabelKey: "web"}, Value: 40}}
	p95Mem := []PromResult{{Labels: map[string]string{serviceLabelKey: "web"}, Value: float64(300 << 20)}}

	query := mockQuery(instantCPU, instantMem, p95CPU, p95Mem)

	cfg := &config.SizingConfig{
		Enabled: true, Interval: time.Second, HeadroomMultiplier: 2.0,
		OverProvisioned: 0.20, ApproachingLimit: 0.80, AtLimit: 0.95, Lookback: 168 * time.Hour,
	}

	m := New(query, c, cfg)
	m.tick(context.Background())

	results := m.Results()
	if len(results) == 0 {
		t.Fatal("expected at least one service sizing result")
	}

	for _, ss := range results {
		if ss.ServiceName == "web" {
			for _, h := range ss.Hints {
				if h.Resource == "cpu" && h.Category == CategoryApproachingLimit {
					return // success — CPU at 90% of 100% limit
				}
			}

			t.Errorf("expected approaching-limit CPU hint, got: %+v", ss.Hints)
			return
		}
	}

	t.Error("expected sizing result for service 'web'")
}

func TestMonitor_NewReturnsNilWhenDisabled(t *testing.T) {
	cfg := &config.SizingConfig{Enabled: false}
	m := New(func(context.Context, string) ([]PromResult, error) { return nil, nil }, cache.New(nil), cfg)

	if m != nil {
		t.Error("expected nil monitor when disabled")
	}
}

func TestMonitor_NewReturnsNilWhenNoQuery(t *testing.T) {
	cfg := &config.SizingConfig{Enabled: true}
	m := New(nil, cache.New(nil), cfg)

	if m != nil {
		t.Error("expected nil monitor when query is nil")
	}
}
