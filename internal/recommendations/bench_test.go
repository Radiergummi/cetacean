package recommendations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

func benchSizingConfig() *config.SizingConfig {
	return &config.SizingConfig{
		OverProvisioned:    0.20,
		ApproachingLimit:   0.80,
		AtLimit:            0.95,
		HeadroomMultiplier: 2.0,
		Lookback:           168 * time.Hour,
	}
}

func populateCache(c *cache.Cache, n int) {
	replicas := uint64(1)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("service-%d", i)
		id := fmt.Sprintf("svc-id-%d", i)
		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: name},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{},
					Resources: &swarm.ResourceRequirements{
						Limits:       &swarm.Limit{NanoCPUs: 1e9, MemoryBytes: 512 << 20},
						Reservations: &swarm.Resources{NanoCPUs: 0.5e9, MemoryBytes: 256 << 20},
					},
				},
				Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			},
		})
		c.SetNode(swarm.Node{
			ID: fmt.Sprintf("node-id-%d", i),
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Description: swarm.NodeDescription{Hostname: fmt.Sprintf("worker-%d", i)},
		})
		c.SetTask(swarm.Task{
			ID:     fmt.Sprintf("task-id-%d", i),
			NodeID: fmt.Sprintf("node-id-%d", i),
			Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		})
	}
}

func BenchmarkEngineTick(b *testing.B) {
	c := cache.New(nil)
	populateCache(c, 100)

	cfg := benchSizingConfig()
	noopQuery := func(_ context.Context, _ string) ([]prom.Result, error) {
		return nil, nil
	}

	sizingChecker := NewSizingChecker(noopQuery, c, cfg)
	configChecker := NewConfigChecker(c)
	clusterChecker := NewClusterChecker(c)
	operationalChecker := NewOperationalChecker(noopQuery, c, 168*time.Hour)

	engine := NewEngine(sizingChecker, configChecker, clusterChecker, operationalChecker)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		engine.tick(context.Background(), true)
	}
}

func BenchmarkEvaluate(b *testing.B) {
	cfg := benchSizingConfig()

	cases := []struct {
		name    string
		spec    serviceSpec
		instant *serviceMetrics
		p95     *serviceMetrics
	}{
		{
			name: "no-limits",
			spec: serviceSpec{id: "svc", name: "web"},
		},
		{
			name: "approaching-limit",
			spec: serviceSpec{
				id: "svc", name: "web",
				cpuLimit: 1e9, cpuReservation: 0.5e9,
				memoryLimit: 512 << 20, memoryReservation: 256 << 20,
			},
			instant: &serviceMetrics{cpu: 85, memory: float64(430 << 20)},
			p95:     &serviceMetrics{cpu: 80, memory: float64(400 << 20)},
		},
		{
			name: "at-limit",
			spec: serviceSpec{
				id: "svc", name: "web",
				cpuLimit: 1e9, cpuReservation: 0.5e9,
				memoryLimit: 512 << 20, memoryReservation: 256 << 20,
			},
			instant: &serviceMetrics{cpu: 97, memory: float64(500 << 20)},
			p95:     &serviceMetrics{cpu: 95, memory: float64(490 << 20)},
		},
		{
			name: "over-provisioned",
			spec: serviceSpec{
				id: "svc", name: "web",
				cpuLimit: 1e9, cpuReservation: 0.5e9,
				memoryLimit: 512 << 20, memoryReservation: 256 << 20,
			},
			instant: &serviceMetrics{cpu: 10, memory: float64(30 << 20)},
			p95:     &serviceMetrics{cpu: 5, memory: float64(20 << 20)},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = evaluate(tc.spec, tc.instant, tc.p95, cfg)
			}
		})
	}
}

func BenchmarkConfigChecker(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d-services", n), func(b *testing.B) {
			c := cache.New(nil)
			populateCache(c, n)
			checker := NewConfigChecker(c)

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				_ = checker.Check(context.Background())
			}
		})
	}
}

func BenchmarkClusterChecker(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d-services", n), func(b *testing.B) {
			c := cache.New(nil)
			populateCache(c, n)
			checker := NewClusterChecker(c)

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				_ = checker.Check(context.Background())
			}
		})
	}
}

func BenchmarkComputeSummary(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("%d-recommendations", n), func(b *testing.B) {
			recs := make([]Recommendation, n)
			severities := []Severity{SeverityCritical, SeverityWarning, SeverityInfo}
			for i := range recs {
				recs[i] = Recommendation{Severity: severities[i%3]}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				_ = ComputeSummary(recs)
			}
		})
	}
}

func BenchmarkSortBySeverity(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("%d-recommendations", n), func(b *testing.B) {
			template := make([]Recommendation, n)
			severities := []Severity{SeverityInfo, SeverityWarning, SeverityCritical}
			for i := range template {
				template[i] = Recommendation{Severity: severities[i%3]}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				// Copy to avoid sorting an already-sorted slice.
				recs := make([]Recommendation, n)
				copy(recs, template)
				sortBySeverity(recs)
			}
		})
	}
}
