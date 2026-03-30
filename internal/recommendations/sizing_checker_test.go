package recommendations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

func mockQuery(cpuResults, memResults, cpuP95Results, memP95Results []prom.Result) QueryFunc {
	return func(_ context.Context, query string) ([]prom.Result, error) {
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

func TestSizingChecker_Check(t *testing.T) {
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

	instantCPU := []prom.Result{{Labels: map[string]string{serviceLabelKey: "web"}, Value: 90}}
	instantMem := []prom.Result{
		{Labels: map[string]string{serviceLabelKey: "web"}, Value: float64(400 << 20)},
	}
	p95CPU := []prom.Result{{Labels: map[string]string{serviceLabelKey: "web"}, Value: 40}}
	p95Mem := []prom.Result{
		{Labels: map[string]string{serviceLabelKey: "web"}, Value: float64(300 << 20)},
	}

	query := mockQuery(instantCPU, instantMem, p95CPU, p95Mem)

	cfg := &config.SizingConfig{
		HeadroomMultiplier: 2.0,
		OverProvisioned:    0.20,
		ApproachingLimit:   0.80,
		AtLimit:            0.95,
		Lookback:           168 * time.Hour,
	}

	sc := NewSizingChecker(query, c, cfg)
	recs := sc.Check(context.Background())

	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	for _, r := range recs {
		if r.Resource == "cpu" && r.Category == CategoryApproachingLimit {
			if r.Scope != ScopeService {
				t.Errorf("expected scope %q, got %q", ScopeService, r.Scope)
			}

			if r.TargetID != "svc1" {
				t.Errorf("expected targetId %q, got %q", "svc1", r.TargetID)
			}

			return // success — CPU at 90% of 100% limit
		}
	}

	t.Errorf("expected approaching-limit CPU hint, got: %+v", recs)
}

func TestSizingChecker_Name(t *testing.T) {
	sc := NewSizingChecker(nil, nil, nil)
	if sc.Name() != "sizing" {
		t.Errorf("expected name %q, got %q", "sizing", sc.Name())
	}
}

func TestSizingChecker_Interval(t *testing.T) {
	sc := NewSizingChecker(nil, nil, nil)
	if sc.Interval() != 5*time.Minute {
		t.Errorf("expected interval %v, got %v", 5*time.Minute, sc.Interval())
	}
}
