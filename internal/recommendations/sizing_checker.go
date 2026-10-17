package recommendations

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
	serviceLabelKey = prom.SwarmServiceLabel
	serviceFilter   = `container_label_com_docker_swarm_service_id!=""`

	cpuInstantQuery    = `avg by (` + serviceLabelKey + `)(rate(container_cpu_usage_seconds_total{` + serviceFilter + `}[5m])) * 100`
	memoryInstantQuery = `avg_over_time(avg by (` + serviceLabelKey + `)(container_memory_usage_bytes{` + serviceFilter + `})[1h:])`
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

func (sc *SizingChecker) Name() string            { return "sizing" }
func (sc *SizingChecker) Interval() time.Duration { return 5 * time.Minute }

// Check runs all sizing queries and evaluates every service.
func (sc *SizingChecker) Check(ctx context.Context) []Recommendation {
	tickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	lookbackStr := formatPromDuration(sc.cfg.Lookback)

	results := runQueries(
		tickCtx,
		sc.query,
		cpuInstantQuery,
		memoryInstantQuery,
		fmt.Sprintf(
			`quantile_over_time(0.95, (avg by (%s)(rate(container_cpu_usage_seconds_total{%s}[5m])))[%s:5m]) * 100`,
			serviceLabelKey,
			serviceFilter,
			lookbackStr,
		),
		fmt.Sprintf(
			`quantile_over_time(0.95, (avg by (%s)(container_memory_usage_bytes{%s}))[%s:5m])`,
			serviceLabelKey, serviceFilter, lookbackStr,
		),
	)
	cpuResult, memResult := results[0], results[1]
	cpuP95Result, memP95Result := results[2], results[3]

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

	cpu, mem := byService(cpuResult.results), byService(memResult.results)
	cpuP95, memP95 := byService(cpuP95Result.results), byService(memP95Result.results)

	services := sc.cache.ListServices()

	var recs []Recommendation

	for _, svc := range services {
		spec := extractSpec(svc)

		var instant *serviceMetrics
		if cpuResult.err == nil && memResult.err == nil {
			instant = measured(cpu, mem, spec.name)
		}

		var p95 *serviceMetrics
		if cpuP95Result.err == nil && memP95Result.err == nil {
			p95 = measured(cpuP95, memP95, spec.name)
		}

		hints := evaluate(spec, instant, p95, sc.cfg)
		recs = append(recs, hints...)
	}

	return recs
}

// measured pairs a service's CPU and memory results, reporting nil unless both
// are present. A query that succeeds but returns no series is silence, not a
// measurement, and 0 sits under every over-provisioned threshold. An exporter
// emits the two families together, so one alone means wrong, not idle.
func measured(cpuData, memData map[string]float64, service string) *serviceMetrics {
	cpu, hasCPU := cpuData[service]
	mem, hasMem := memData[service]

	if !hasCPU || !hasMem {
		return nil
	}

	return &serviceMetrics{cpu: cpu, memory: mem}
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

// byService keys results by service name; a later series wins a repeated name.
func byService(results []prom.Result) map[string]float64 {
	entries := entriesBy(results, serviceLabelKey)
	out := make(map[string]float64, len(entries))
	for _, e := range entries {
		out[e.key] = e.value
	}

	return out
}

type queryResult struct {
	results []prom.Result
	err     error
}

// runQueries runs the queries concurrently and returns their outcomes in the
// order they were given.
func runQueries(ctx context.Context, query QueryFunc, promQueries ...string) []queryResult {
	out := make([]queryResult, len(promQueries))

	var wg sync.WaitGroup
	for i, promQuery := range promQueries {
		wg.Go(func() {
			results, err := query(ctx, promQuery)
			out[i] = queryResult{results, err}
		})
	}
	wg.Wait()

	return out
}
