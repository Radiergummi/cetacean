package recommendations

import (
	"fmt"
	"math"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
)

// serviceSpec holds the resource configuration from a swarm service.
type serviceSpec struct {
	id                string
	name              string
	cpuLimit          int64 // NanoCPUs
	cpuReservation    int64 // NanoCPUs
	memoryLimit       int64 // bytes
	memoryReservation int64 // bytes
}

// serviceMetrics holds actual usage from Prometheus.
type serviceMetrics struct {
	cpu    float64 // percentage (e.g. 50 = 50%)
	memory float64 // bytes
}

// roundCPU rounds a NanoCPU value to the nearest 0.05 cores, with a minimum of 0.05 cores.
// Returns NanoCPUs.
func roundCPU(nanoCPUs float64) float64 {
	cores := nanoCPUs / 1e9
	rounded := math.Round(cores*20) / 20
	if rounded < 0.05 {
		rounded = 0.05
	}

	return rounded * 1e9
}

// roundMemory rounds a byte value up to the nearest 64MB, with a minimum of 64MB.
func roundMemory(bytes float64) float64 {
	const mb64 = 64 * 1024 * 1024
	rounded := math.Ceil(bytes/mb64) * mb64
	if rounded < mb64 {
		rounded = mb64
	}

	return rounded
}

// formatDuration converts a time.Duration to a human-readable string
// like "30 minutes", "24 hours", "7 days".
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}

		return fmt.Sprintf("%d minutes", minutes)
	}

	hours := int(d.Hours())
	if hours == 1 {
		return "1 hour"
	}

	if hours < 48 {
		return fmt.Sprintf("%d hours", hours)
	}

	return fmt.Sprintf("%d days", hours/24)
}

// evaluate computes sizing recommendations for a service.
// instant is used for at-limit and approaching-limit checks (current rate).
// p95 is used for over-provisioned checks (p95 over lookback window).
// Both may be nil independently.
func evaluate(
	spec serviceSpec,
	instant *serviceMetrics,
	p95 *serviceMetrics,
	cfg *config.SizingConfig,
) []Recommendation {
	var hints []Recommendation

	// --- Config-only checks (always run, even without metrics) ---

	noCPULimit := spec.cpuLimit == 0
	noMemLimit := spec.memoryLimit == 0

	if noCPULimit && noMemLimit {
		hints = append(hints, Recommendation{
			Category:   CategoryNoLimits,
			Severity:   SeverityWarning,
			Scope:      ScopeService,
			TargetID:   spec.id,
			TargetName: spec.name,
			Resource:   "cpu+memory",
			Message:    "Service has no CPU or memory limits set",
			Current:    0,
			Configured: 0,
		})
	} else {
		if noCPULimit {
			hints = append(hints, Recommendation{
				Category:   CategoryNoLimits,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   spec.id,
				TargetName: spec.name,
				Resource:   "cpu",
				Message:    "Service has no CPU limit set",
				Current:    0,
				Configured: 0,
			})
		}

		if noMemLimit {
			hints = append(hints, Recommendation{
				Category:   CategoryNoLimits,
				Severity:   SeverityWarning,
				Scope:      ScopeService,
				TargetID:   spec.id,
				TargetName: spec.name,
				Resource:   "memory",
				Message:    "Service has no memory limit set",
				Current:    0,
				Configured: 0,
			})
		}
	}

	// Only emit no-reservations hints when the service HAS limits but is missing reservations.
	noCPUReservation := spec.cpuReservation == 0
	noMemReservation := spec.memoryReservation == 0

	if !noCPULimit && noCPUReservation && !noMemLimit && noMemReservation {
		hints = append(hints, Recommendation{
			Category:   CategoryNoReservations,
			Severity:   SeverityInfo,
			Scope:      ScopeService,
			TargetID:   spec.id,
			TargetName: spec.name,
			Resource:   "cpu+memory",
			Message:    "Service has limits but no CPU or memory reservations set",
			Current:    0,
			Configured: 0,
		})
	} else {
		if !noCPULimit && noCPUReservation {
			hints = append(hints, Recommendation{
				Category:   CategoryNoReservations,
				Severity:   SeverityInfo,
				Scope:      ScopeService,
				TargetID:   spec.id,
				TargetName: spec.name,
				Resource:   "cpu",
				Message:    "Service has a CPU limit but no reservation set",
				Current:    0,
				Configured: 0,
			})
		}

		if !noMemLimit && noMemReservation {
			hints = append(hints, Recommendation{
				Category:   CategoryNoReservations,
				Severity:   SeverityInfo,
				Scope:      ScopeService,
				TargetID:   spec.id,
				TargetName: spec.name,
				Resource:   "memory",
				Message:    "Service has a memory limit but no reservation set",
				Current:    0,
				Configured: 0,
			})
		}
	}

	// --- At-limit / approaching-limit checks (instant metrics) ---

	resourcesFixAction := new(string)
	*resourcesFixAction = "PATCH /services/{id}/resources"

	if instant != nil {
		if !noCPULimit {
			cpuLimitPct := float64(spec.cpuLimit) / 1e9 * 100
			cpuRatio := instant.cpu / cpuLimitPct

			switch {
			case cpuRatio > cfg.AtLimit:
				suggested := roundCPU(instant.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryAtLimit,
					Severity:   SeverityCritical,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "cpu",
					Message:    fmt.Sprintf("CPU usage is at %.0f%% of limit", cpuRatio*100),
					Current:    instant.cpu,
					Configured: cpuLimitPct,
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})

			case cpuRatio > cfg.ApproachingLimit:
				suggested := roundCPU(instant.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryApproachingLimit,
					Severity:   SeverityWarning,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "cpu",
					Message:    fmt.Sprintf("CPU usage is at %.0f%% of limit", cpuRatio*100),
					Current:    instant.cpu,
					Configured: cpuLimitPct,
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})
			}
		}

		if !noMemLimit {
			memRatio := instant.memory / float64(spec.memoryLimit)

			switch {
			case memRatio > cfg.AtLimit:
				suggested := roundMemory(instant.memory * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryAtLimit,
					Severity:   SeverityCritical,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "memory",
					Message:    fmt.Sprintf("Memory usage is at %.0f%% of limit", memRatio*100),
					Current:    instant.memory,
					Configured: float64(spec.memoryLimit),
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})

			case memRatio > cfg.ApproachingLimit:
				suggested := roundMemory(instant.memory * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryApproachingLimit,
					Severity:   SeverityWarning,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "memory",
					Message:    fmt.Sprintf("Memory usage is at %.0f%% of limit", memRatio*100),
					Current:    instant.memory,
					Configured: float64(spec.memoryLimit),
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})
			}
		}
	}

	// --- Over-provisioned checks (p95 metrics over lookback window) ---

	if p95 != nil {
		if !noCPULimit && !noCPUReservation {
			cpuReservationPct := float64(spec.cpuReservation) / 1e9 * 100
			cpuResRatio := p95.cpu / cpuReservationPct

			if cpuResRatio < cfg.OverProvisioned {
				suggested := roundCPU(p95.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryOverProvisioned,
					Severity:   SeverityInfo,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "cpu",
					Message: fmt.Sprintf(
						"p95 CPU usage over the past %s is %.0f%% of reservation",
						formatDuration(cfg.Lookback),
						cpuResRatio*100,
					),
					Current:    p95.cpu,
					Configured: cpuReservationPct,
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})
			}
		}

		if !noMemLimit && !noMemReservation {
			memResRatio := p95.memory / float64(spec.memoryReservation)

			if memResRatio < cfg.OverProvisioned {
				suggested := roundMemory(p95.memory * cfg.HeadroomMultiplier)
				hints = append(hints, Recommendation{
					Category:   CategoryOverProvisioned,
					Severity:   SeverityInfo,
					Scope:      ScopeService,
					TargetID:   spec.id,
					TargetName: spec.name,
					Resource:   "memory",
					Message: fmt.Sprintf(
						"p95 memory usage over the past %s is %.0f%% of reservation",
						formatDuration(cfg.Lookback),
						memResRatio*100,
					),
					Current:    p95.memory,
					Configured: float64(spec.memoryReservation),
					Suggested:  &suggested,
					FixAction:  resourcesFixAction,
				})
			}
		}
	}

	return hints
}
