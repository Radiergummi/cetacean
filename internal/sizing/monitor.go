package sizing

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

const serviceLabelKey = "container_label_com_docker_swarm_service_name"

// PromResult holds a single Prometheus query result.
// Defined locally to avoid circular imports with the api package.
type PromResult struct {
	Labels map[string]string
	Value  float64
}

// QueryFunc executes a Prometheus instant query and returns results.
type QueryFunc func(ctx context.Context, query string) ([]PromResult, error)

// Monitor periodically evaluates service resource sizing.
type Monitor struct {
	query QueryFunc
	cache *cache.Cache
	cfg   *config.SizingConfig

	mu       sync.RWMutex
	results  []ServiceSizing
	previous map[string]*previousState
}

// New creates a new sizing monitor. Returns nil if query is nil or sizing is disabled.
func New(query QueryFunc, c *cache.Cache, cfg *config.SizingConfig) *Monitor {
	if query == nil || cfg == nil || !cfg.Enabled {
		return nil
	}

	return &Monitor{
		query:    query,
		cache:    c,
		cfg:      cfg,
		previous: make(map[string]*previousState),
	}
}

// Run starts the periodic evaluation loop. Blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	if m == nil {
		return
	}

	m.tick(ctx)

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// Results returns the latest sizing results. Safe to call on nil receiver.
func (m *Monitor) Results() []ServiceSizing {
	if m == nil {
		return []ServiceSizing{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ServiceSizing, len(m.results))
	copy(out, m.results)

	return out
}

func (m *Monitor) tick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	type queryResult struct {
		data map[string]float64
		err  error
	}

	cpuCh := make(chan queryResult, 1)
	memCh := make(chan queryResult, 1)

	go func() {
		data, err := m.queryByService(tickCtx, `sum by (container_label_com_docker_swarm_service_name)(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_id!=""}[5m])) * 100`)
		cpuCh <- queryResult{data, err}
	}()

	go func() {
		data, err := m.queryByService(tickCtx, `avg_over_time(sum by (container_label_com_docker_swarm_service_name)(container_memory_usage_bytes{container_label_com_docker_swarm_service_id!=""})[1h:])`)
		memCh <- queryResult{data, err}
	}()

	cpuResult := <-cpuCh
	memResult := <-memCh

	if cpuResult.err != nil {
		slog.Warn("sizing: CPU query failed", "error", cpuResult.err)
	}

	if memResult.err != nil {
		slog.Warn("sizing: memory query failed", "error", memResult.err)
	}

	services := m.cache.ListServices()
	now := time.Now()

	var newResults []ServiceSizing
	activeIDs := make(map[string]struct{}, len(services))

	for _, svc := range services {
		activeIDs[svc.ID] = struct{}{}
		spec := extractSpec(svc)

		var metrics *serviceMetrics
		if cpuResult.err == nil && memResult.err == nil {
			cpu := cpuResult.data[spec.name]
			mem := memResult.data[spec.name]
			metrics = &serviceMetrics{cpu: cpu, memory: mem}
		}

		prev := m.previous[svc.ID]
		result := evaluate(spec, metrics, prev, m.cfg)

		state := result.newState
		m.previous[svc.ID] = &state

		if len(result.hints) == 0 {
			continue
		}

		newResults = append(newResults, ServiceSizing{
			ServiceID:   svc.ID,
			ServiceName: spec.name,
			Hints:       result.hints,
			ComputedAt:  now,
		})
	}

	// Clean up tick state for removed services.
	for id := range m.previous {
		if _, ok := activeIDs[id]; !ok {
			delete(m.previous, id)
		}
	}

	m.mu.Lock()
	m.results = newResults
	m.mu.Unlock()
}

func extractSpec(svc swarm.Service) serviceSpec {
	s := serviceSpec{name: svc.Spec.Name}

	if res := svc.Spec.TaskTemplate.Resources; res != nil {
		if res.Limits != nil {
			s.cpuLimit = res.Limits.NanoCPUs
			s.memoryLimit = res.Limits.MemoryBytes
		}

		if res.Reservations != nil {
			s.cpuReservation = res.Reservations.NanoCPUs
			s.memoryReservation = res.Reservations.MemoryBytes
		}
	}

	return s
}

func (m *Monitor) queryByService(ctx context.Context, query string) (map[string]float64, error) {
	results, err := m.query(ctx, query)
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(results))
	for _, r := range results {
		if name := r.Labels[serviceLabelKey]; name != "" {
			out[name] = r.Value
		}
	}

	return out, nil
}
