package recommendations

import (
	"context"
	"fmt"
	"log/slog"
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

// SizingChecker implements Checker for resource right-sizing recommendations.
type SizingChecker struct {
	query QueryFunc
	cache *cache.Cache
	cfg   *config.SizingConfig
}

// NewSizingChecker creates a new sizing checker.
func NewSizingChecker(query QueryFunc, c *cache.Cache, cfg *config.SizingConfig) *SizingChecker {
	return &SizingChecker{
		query: query,
		cache: c,
		cfg:   cfg,
	}
}

func (sc *SizingChecker) Name() string             { return "sizing" }
func (sc *SizingChecker) Interval() time.Duration   { return 5 * time.Minute }

// Check runs all sizing queries and evaluates every service.
func (sc *SizingChecker) Check(ctx context.Context) []Recommendation {
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

	lookbackStr := formatPromDuration(sc.cfg.Lookback)

	go func() {
		data, err := queryByService(tickCtx, sc.query, cpuInstantQuery)
		cpuCh <- queryResult{data, err}
	}()

	go func() {
		data, err := queryByService(tickCtx, sc.query, memoryInstantQuery)
		memCh <- queryResult{data, err}
	}()

	go func() {
		query := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (%s)(rate(container_cpu_usage_seconds_total{%s}[5m])))[%s:5m]) * 100`,
			serviceLabelKey, serviceFilter, lookbackStr,
		)
		data, err := queryByService(tickCtx, sc.query, query)
		cpuP95Ch <- queryResult{data, err}
	}()

	go func() {
		query := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (%s)(container_memory_usage_bytes{%s}))[%s:5m])`,
			serviceLabelKey, serviceFilter, lookbackStr,
		)
		data, err := queryByService(tickCtx, sc.query, query)
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

	services := sc.cache.ListServices()

	var recs []Recommendation

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

		hints := evaluate(spec, instant, p95, sc.cfg)
		recs = append(recs, hints...)
	}

	return recs
}

// formatPromDuration formats a time.Duration as a Prometheus duration string.
func formatPromDuration(d time.Duration) string {
	hours := int(d.Hours())
	if hours%24 == 0 && hours >= 24 {
		return fmt.Sprintf("%dd", hours/24)
	}

	return fmt.Sprintf("%dh", hours)
}

func extractSpec(svc swarm.Service) serviceSpec {
	s := serviceSpec{
		id:   svc.ID,
		name: svc.Spec.Name,
	}

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

func queryByService(ctx context.Context, query QueryFunc, promQuery string) (map[string]float64, error) {
	results, err := query(ctx, promQuery)
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
