package sizing

import (
	"fmt"
	"math"

	"github.com/radiergummi/cetacean/internal/config"
)

// serviceSpec holds the resource configuration from a swarm service.
type serviceSpec struct {
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

// previousState tracks sustained-tick counters between evaluations.
type previousState struct {
	cpuLowTicks    int
	memoryLowTicks int
}

// evaluateResult holds both the recommendations and updated tick state.
type evaluateResult struct {
	hints    []Recommendation
	newState previousState
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

func ptr(v float64) *float64 {
	return &v
}

// evaluate computes sizing recommendations for a service.
// metrics and state may be nil (no Prometheus data / first tick).
func evaluate(spec serviceSpec, metrics *serviceMetrics, state *previousState, cfg *config.SizingConfig) evaluateResult {
	var hints []Recommendation

	newState := previousState{}
	if state != nil {
		newState = *state
	}

	// --- Config-only checks (always run, even without metrics) ---

	noCPULimit := spec.cpuLimit == 0
	noMemLimit := spec.memoryLimit == 0

	if noCPULimit && noMemLimit {
		hints = append(hints, Recommendation{
			Category:   CategoryNoLimits,
			Severity:   SeverityWarning,
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
				Resource:   "memory",
				Message:    "Service has a memory limit but no reservation set",
				Current:    0,
				Configured: 0,
			})
		}
	}

	// --- Metrics-based checks (only when metrics are available) ---

	if metrics == nil {
		return evaluateResult{hints: hints, newState: newState}
	}

	// CPU checks
	if !noCPULimit {
		cpuLimitPct := float64(spec.cpuLimit) / 1e9 * 100
		cpuRatio := metrics.cpu / cpuLimitPct

		switch {
		case cpuRatio > cfg.AtLimit:
			suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryAtLimit,
				Severity:   SeverityCritical,
				Resource:   "cpu",
				Message:    fmt.Sprintf("CPU usage is at %.0f%% of limit", cpuRatio*100),
				Current:    metrics.cpu,
				Configured: cpuLimitPct,
				Suggested:  ptr(suggested),
			})
			newState.cpuLowTicks = 0

		case cpuRatio > cfg.ApproachingLimit:
			suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryApproachingLimit,
				Severity:   SeverityWarning,
				Resource:   "cpu",
				Message:    fmt.Sprintf("CPU usage is at %.0f%% of limit", cpuRatio*100),
				Current:    metrics.cpu,
				Configured: cpuLimitPct,
				Suggested:  ptr(suggested),
			})
			newState.cpuLowTicks = 0
		}

		if !noCPUReservation && cpuRatio <= cfg.ApproachingLimit {
			cpuReservationPct := float64(spec.cpuReservation) / 1e9 * 100
			cpuResRatio := metrics.cpu / cpuReservationPct

			if cpuResRatio < cfg.OverProvisioned {
				newState.cpuLowTicks++
				if newState.cpuLowTicks >= cfg.SustainedTicks {
					suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
					hints = append(hints, Recommendation{
						Category:   CategoryOverProvisioned,
						Severity:   SeverityInfo,
						Resource:   "cpu",
						Message:    fmt.Sprintf("CPU usage has been below %.0f%% of reservation for %d ticks", cfg.OverProvisioned*100, newState.cpuLowTicks),
						Current:    metrics.cpu,
						Configured: cpuReservationPct,
						Suggested:  ptr(suggested),
					})
				}
			} else {
				newState.cpuLowTicks = 0
			}
		}
	}

	// Memory checks
	if !noMemLimit {
		memRatio := metrics.memory / float64(spec.memoryLimit)

		switch {
		case memRatio > cfg.AtLimit:
			suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryAtLimit,
				Severity:   SeverityCritical,
				Resource:   "memory",
				Message:    fmt.Sprintf("Memory usage is at %.0f%% of limit", memRatio*100),
				Current:    metrics.memory,
				Configured: float64(spec.memoryLimit),
				Suggested:  ptr(suggested),
			})
			newState.memoryLowTicks = 0

		case memRatio > cfg.ApproachingLimit:
			suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryApproachingLimit,
				Severity:   SeverityWarning,
				Resource:   "memory",
				Message:    fmt.Sprintf("Memory usage is at %.0f%% of limit", memRatio*100),
				Current:    metrics.memory,
				Configured: float64(spec.memoryLimit),
				Suggested:  ptr(suggested),
			})
			newState.memoryLowTicks = 0
		}

		if !noMemReservation && memRatio <= cfg.ApproachingLimit {
			memResRatio := metrics.memory / float64(spec.memoryReservation)

			if memResRatio < cfg.OverProvisioned {
				newState.memoryLowTicks++
				if newState.memoryLowTicks >= cfg.SustainedTicks {
					suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
					hints = append(hints, Recommendation{
						Category:   CategoryOverProvisioned,
						Severity:   SeverityInfo,
						Resource:   "memory",
						Message:    fmt.Sprintf("Memory usage has been below %.0f%% of reservation for %d ticks", cfg.OverProvisioned*100, newState.memoryLowTicks),
						Current:    metrics.memory,
						Configured: float64(spec.memoryReservation),
						Suggested:  ptr(suggested),
					})
				}
			} else {
				newState.memoryLowTicks = 0
			}
		}
	}

	return evaluateResult{hints: hints, newState: newState}
}
