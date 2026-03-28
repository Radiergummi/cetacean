package sizing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

const (
	serviceLabelKey = "container_label_com_docker_swarm_service_name"
	serviceFilter   = `container_label_com_docker_swarm_service_id!=""`

	cpuInstantQuery    = `sum by (` + serviceLabelKey + `)(rate(container_cpu_usage_seconds_total{` + serviceFilter + `}[5m])) * 100`
	memoryInstantQuery = `avg_over_time(sum by (` + serviceLabelKey + `)(container_memory_usage_bytes{` + serviceFilter + `})[1h:])`
)

// QueryFunc executes a Prometheus instant query and returns results.
type QueryFunc func(ctx context.Context, query string) ([]prom.Result, error)

// Monitor periodically evaluates service resource sizing.
type Monitor struct {
	query QueryFunc
	cache *cache.Cache
	cfg   *config.SizingConfig

	mu      sync.RWMutex
	results []ServiceSizing
}

// New creates a new sizing monitor. Returns nil if query is nil or sizing is disabled.
func New(query QueryFunc, c *cache.Cache, cfg *config.SizingConfig) *Monitor {
	if query == nil || cfg == nil || !cfg.Enabled {
		return nil
	}

	return &Monitor{
		query: query,
		cache: c,
		cfg:   cfg,
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

// formatPromDuration formats a time.Duration as a Prometheus duration string.
func formatPromDuration(d time.Duration) string {
	hours := int(d.Hours())
	if hours%24 == 0 && hours >= 24 {
		return fmt.Sprintf("%dd", hours/24)
	}

	return fmt.Sprintf("%dh", hours)
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
	cpuP95Ch := make(chan queryResult, 1)
	memP95Ch := make(chan queryResult, 1)

	lookbackStr := formatPromDuration(m.cfg.Lookback)

	go func() {
		data, err := m.queryByService(tickCtx, cpuInstantQuery)
		cpuCh <- queryResult{data, err}
	}()

	go func() {
		data, err := m.queryByService(tickCtx, memoryInstantQuery)
		memCh <- queryResult{data, err}
	}()

	go func() {
		query := fmt.Sprintf(
			`quantile by (%s)(0.95, sum by (%s)(rate(container_cpu_usage_seconds_total{%s}[5m]))[%s:]) * 100`,
			serviceLabelKey, serviceLabelKey, serviceFilter, lookbackStr,
		)
		data, err := m.queryByService(tickCtx, query)
		cpuP95Ch <- queryResult{data, err}
	}()

	go func() {
		query := fmt.Sprintf(
			`quantile by (%s)(0.95, sum by (%s)(container_memory_usage_bytes{%s})[%s:])`,
			serviceLabelKey, serviceLabelKey, serviceFilter, lookbackStr,
		)
		data, err := m.queryByService(tickCtx, query)
		memP95Ch <- queryResult{data, err}
	}()

	cpuResult := <-cpuCh
	memResult := <-memCh
	cpuP95Result := <-cpuP95Ch
	memP95Result := <-memP95Ch

	if cpuResult.err != nil {
		slog.Warn("sizing: CPU query failed", "error", cpuResult.err)
	}

	if memResult.err != nil {
		slog.Warn("sizing: memory query failed", "error", memResult.err)
	}

	if cpuP95Result.err != nil {
		slog.Warn("sizing: p95 CPU query failed", "error", cpuP95Result.err)
	}

	if memP95Result.err != nil {
		slog.Warn("sizing: p95 memory query failed", "error", memP95Result.err)
	}

	services := m.cache.ListServices()
	now := time.Now()

	var newResults []ServiceSizing

	for _, svc := range services {
		spec := extractSpec(svc)

		var instant *serviceMetrics
		if cpuResult.err == nil && memResult.err == nil {
			cpu := cpuResult.data[spec.name]
			mem := memResult.data[spec.name]
			instant = &serviceMetrics{cpu: cpu, memory: mem}
		}

		var p95 *serviceMetrics
		if cpuP95Result.err == nil && memP95Result.err == nil {
			cpu := cpuP95Result.data[spec.name]
			mem := memP95Result.data[spec.name]
			p95 = &serviceMetrics{cpu: cpu, memory: mem}
		}

		hints := evaluate(spec, instant, p95, m.cfg)

		if len(hints) == 0 {
			continue
		}

		newResults = append(newResults, ServiceSizing{
			ServiceID:   svc.ID,
			ServiceName: spec.name,
			Hints:       hints,
			ComputedAt:  now,
		})
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
