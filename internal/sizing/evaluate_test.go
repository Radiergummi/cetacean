package sizing

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

func defaultConfig() *config.SizingConfig {
	return &config.SizingConfig{
		Enabled:            true,
		OverProvisioned:    0.20,
		ApproachingLimit:   0.80,
		AtLimit:            0.95,
		HeadroomMultiplier: 2.0,
		SustainedTicks:     3,
	}
}

// specWithLimits returns a spec with 2 CPU cores and 512MB limits, 1 CPU / 256MB reservations.
func specWithLimits() serviceSpec {
	return serviceSpec{
		name:              "test-service",
		cpuLimit:          2_000_000_000, // 2 cores
		cpuReservation:    1_000_000_000, // 1 core
		memoryLimit:       512 * 1024 * 1024,
		memoryReservation: 256 * 1024 * 1024,
	}
}

func TestEvaluate_NoLimits(t *testing.T) {
	spec := serviceSpec{name: "test"}
	result := evaluate(spec, nil, nil, defaultConfig())

	if len(result.hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %+v", len(result.hints), result.hints)
	}

	hint := result.hints[0]

	if hint.Category != CategoryNoLimits {
		t.Errorf("expected category %q, got %q", CategoryNoLimits, hint.Category)
	}

	if hint.Severity != SeverityWarning {
		t.Errorf("expected severity %q, got %q", SeverityWarning, hint.Severity)
	}

	if hint.Resource != "cpu+memory" {
		t.Errorf("expected resource %q, got %q", "cpu+memory", hint.Resource)
	}
}

func TestEvaluate_NoLimits_OnlyMemory(t *testing.T) {
	spec := serviceSpec{
		name:     "test",
		cpuLimit: 1_000_000_000, // has CPU limit
	}
	result := evaluate(spec, nil, nil, defaultConfig())

	if len(result.hints) != 2 {
		t.Fatalf("expected 2 hints (no-memory-limit + no-cpu-reservation), got %d: %+v", len(result.hints), result.hints)
	}

	categories := map[Category]bool{}
	for _, h := range result.hints {
		categories[h.Category] = true
	}

	if !categories[CategoryNoLimits] {
		t.Error("expected a no-limits hint")
	}

	if !categories[CategoryNoReservations] {
		t.Error("expected a no-reservations hint for cpu")
	}
}

func TestEvaluate_NoReservations(t *testing.T) {
	spec := serviceSpec{
		name:        "test",
		cpuLimit:    2_000_000_000,
		memoryLimit: 512 * 1024 * 1024,
		// no reservations
	}
	result := evaluate(spec, nil, nil, defaultConfig())

	if len(result.hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %+v", len(result.hints), result.hints)
	}

	hint := result.hints[0]

	if hint.Category != CategoryNoReservations {
		t.Errorf("expected category %q, got %q", CategoryNoReservations, hint.Category)
	}

	if hint.Severity != SeverityInfo {
		t.Errorf("expected severity %q, got %q", SeverityInfo, hint.Severity)
	}

	if hint.Resource != "cpu+memory" {
		t.Errorf("expected resource %q, got %q", "cpu+memory", hint.Resource)
	}
}

func TestEvaluate_AtLimit(t *testing.T) {
	spec := specWithLimits()
	// 2 core limit → 200%. 98% of 200% = 196%.
	metrics := &serviceMetrics{cpu: 196}

	result := evaluate(spec, metrics, nil, defaultConfig())

	var atLimitHint *Recommendation
	for i := range result.hints {
		if result.hints[i].Category == CategoryAtLimit && result.hints[i].Resource == "cpu" {
			atLimitHint = &result.hints[i]
		}
	}

	if atLimitHint == nil {
		t.Fatalf("expected at-limit hint for cpu, got: %+v", result.hints)
	}

	if atLimitHint.Severity != SeverityCritical {
		t.Errorf("expected severity %q, got %q", SeverityCritical, atLimitHint.Severity)
	}

	if atLimitHint.Suggested == nil {
		t.Fatal("expected suggested value to be present")
	}
}

func TestEvaluate_ApproachingLimit(t *testing.T) {
	spec := specWithLimits()
	// 2 core limit → 200%. 85% of 200% = 170%.
	metrics := &serviceMetrics{cpu: 170}

	result := evaluate(spec, metrics, nil, defaultConfig())

	var approachingHint *Recommendation
	for i := range result.hints {
		if result.hints[i].Category == CategoryApproachingLimit && result.hints[i].Resource == "cpu" {
			approachingHint = &result.hints[i]
		}
	}

	if approachingHint == nil {
		t.Fatalf("expected approaching-limit hint for cpu, got: %+v", result.hints)
	}

	if approachingHint.Severity != SeverityWarning {
		t.Errorf("expected severity %q, got %q", SeverityWarning, approachingHint.Severity)
	}

	if approachingHint.Suggested == nil {
		t.Fatal("expected suggested value to be present")
	}
}

func TestEvaluate_OverProvisioned_NotSustained(t *testing.T) {
	spec := specWithLimits()
	// 1 core reservation → 100%. 5% is well below 20% threshold.
	metrics := &serviceMetrics{cpu: 5}
	prev := &previousState{cpuLowTicks: 1}

	result := evaluate(spec, metrics, prev, defaultConfig())

	for _, h := range result.hints {
		if h.Category == CategoryOverProvisioned && h.Resource == "cpu" {
			t.Errorf("did not expect over-provisioned hint after only 2 ticks (1 previous + 1 current)")
		}
	}

	// Tick counter should have incremented.
	if result.newState.cpuLowTicks != 2 {
		t.Errorf("expected cpuLowTicks=2, got %d", result.newState.cpuLowTicks)
	}
}

func TestEvaluate_OverProvisioned_Sustained(t *testing.T) {
	spec := specWithLimits()
	// 1 core reservation → 100%. 5% is well below 20% threshold.
	metrics := &serviceMetrics{cpu: 5}
	// 2 previous ticks + this tick = 3 total, which meets SustainedTicks=3.
	prev := &previousState{cpuLowTicks: 2}

	result := evaluate(spec, metrics, prev, defaultConfig())

	var overProvHint *Recommendation
	for i := range result.hints {
		if result.hints[i].Category == CategoryOverProvisioned && result.hints[i].Resource == "cpu" {
			overProvHint = &result.hints[i]
		}
	}

	if overProvHint == nil {
		t.Fatalf("expected over-provisioned hint for cpu, got: %+v", result.hints)
	}

	if overProvHint.Severity != SeverityInfo {
		t.Errorf("expected severity %q, got %q", SeverityInfo, overProvHint.Severity)
	}

	if overProvHint.Suggested == nil {
		t.Fatal("expected suggested value to be present")
	}
}

func TestEvaluate_Healthy(t *testing.T) {
	spec := specWithLimits()
	// 2 core limit → 200%. 50% usage = 25% of limit (between 20% and 80%).
	metrics := &serviceMetrics{
		cpu:    50,
		memory: 128 * 1024 * 1024, // 128MB, well within 512MB limit and above 20% of 256MB reservation
	}

	result := evaluate(spec, metrics, nil, defaultConfig())

	for _, h := range result.hints {
		if h.Category == CategoryAtLimit || h.Category == CategoryApproachingLimit || h.Category == CategoryOverProvisioned {
			t.Errorf("unexpected hint for healthy service: %+v", h)
		}
	}
}

func TestEvaluate_NoMetrics_ConfigOnlyHints(t *testing.T) {
	// Service has no limits set.
	spec := serviceSpec{name: "test"}

	result := evaluate(spec, nil, nil, defaultConfig())

	for _, h := range result.hints {
		if h.Category == CategoryAtLimit || h.Category == CategoryApproachingLimit || h.Category == CategoryOverProvisioned {
			t.Errorf("unexpected metrics-based hint when metrics are nil: %+v", h)
		}
	}

	// Should still have the config-only no-limits hint.
	found := false
	for _, h := range result.hints {
		if h.Category == CategoryNoLimits {
			found = true
		}
	}

	if !found {
		t.Error("expected no-limits hint even without metrics")
	}
}

func TestEvaluate_SuggestedValueRounding(t *testing.T) {
	// 512MB limit; 98% usage = ~501MB → over AtLimit.
	// Suggested = 501MB * 2.0 = ~1002MB → rounds up to 1024MB (nearest 64MB).
	spec := serviceSpec{
		name:              "test",
		memoryLimit:       512 * 1024 * 1024,
		memoryReservation: 256 * 1024 * 1024,
	}
	usageBytes := float64(spec.memoryLimit) * 0.98

	metrics := &serviceMetrics{memory: usageBytes}

	result := evaluate(spec, metrics, nil, defaultConfig())

	var atLimitHint *Recommendation
	for i := range result.hints {
		if result.hints[i].Category == CategoryAtLimit && result.hints[i].Resource == "memory" {
			atLimitHint = &result.hints[i]
		}
	}

	if atLimitHint == nil {
		t.Fatalf("expected at-limit hint for memory, got: %+v", result.hints)
	}

	if atLimitHint.Suggested == nil {
		t.Fatal("expected suggested value")
	}

	const mb64 = 64 * 1024 * 1024
	suggested := *atLimitHint.Suggested

	if int64(suggested)%mb64 != 0 {
		t.Errorf("suggested memory %v is not a multiple of 64MB", suggested)
	}
}

// TestEvaluate_OverProvisioned_ReservationNotLimit verifies that the over-provisioned
// check compares usage against the reservation, not the limit.
// With a 10x ratio between limit and reservation, the two would give different results.
func TestEvaluate_OverProvisioned_ReservationNotLimit(t *testing.T) {
	spec := serviceSpec{
		name:              "test-service",
		cpuLimit:          10_000_000_000, // 10 cores limit
		cpuReservation:    1_000_000_000,  // 1 core reservation
		memoryLimit:       1024 * 1024 * 1024,
		memoryReservation: 128 * 1024 * 1024,
	}
	// 5% CPU usage: well below 20% of reservation (1 core = 100%), but only 0.5% of limit.
	// If we used limit ratio, 0.5% < 20% would also be true, but the Configured field
	// should be the reservation, not the limit.
	metrics := &serviceMetrics{
		cpu:    5,
		memory: float64(10 * 1024 * 1024), // 10MB: below 20% of 128MB reservation
	}
	prev := &previousState{cpuLowTicks: 2, memoryLowTicks: 2}

	result := evaluate(spec, metrics, prev, defaultConfig())

	var cpuHint, memHint *Recommendation
	for i := range result.hints {
		if result.hints[i].Category == CategoryOverProvisioned && result.hints[i].Resource == "cpu" {
			cpuHint = &result.hints[i]
		}

		if result.hints[i].Category == CategoryOverProvisioned && result.hints[i].Resource == "memory" {
			memHint = &result.hints[i]
		}
	}

	if cpuHint == nil {
		t.Fatalf("expected over-provisioned hint for cpu, got: %+v", result.hints)
	}

	// Configured must be the reservation percentage (100%), not the limit percentage (1000%).
	cpuReservationPct := float64(spec.cpuReservation) / 1e9 * 100 // 100%
	if cpuHint.Configured != cpuReservationPct {
		t.Errorf("cpu Configured = %v, want reservation pct %v (not limit pct %v)",
			cpuHint.Configured, cpuReservationPct, float64(spec.cpuLimit)/1e9*100)
	}

	if memHint == nil {
		t.Fatalf("expected over-provisioned hint for memory, got: %+v", result.hints)
	}

	// Configured must be the reservation bytes, not the limit bytes.
	if memHint.Configured != float64(spec.memoryReservation) {
		t.Errorf("memory Configured = %v, want reservation %v (not limit %v)",
			memHint.Configured, float64(spec.memoryReservation), float64(spec.memoryLimit))
	}
}

func TestRoundCPU(t *testing.T) {
	tests := []struct {
		input    float64 // NanoCPUs
		expected float64 // NanoCPUs
	}{
		{0, 50_000_000},                // min 0.05 cores
		{10_000_000, 50_000_000},       // 0.01 → rounds to 0.05
		{120_000_000, 100_000_000},     // 0.12 → rounds to 0.10
		{1_000_000_000, 1_000_000_000}, // 1.0 → stays 1.0
		{1_050_000_000, 1_050_000_000}, // 1.05 → stays 1.05
		{1_060_000_000, 1_050_000_000}, // 1.06 → rounds to 1.05
		{1_500_000_000, 1_500_000_000}, // 1.5 → stays 1.5
	}

	for _, tc := range tests {
		got := roundCPU(tc.input)
		if got != tc.expected {
			t.Errorf("roundCPU(%v) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestRoundMemory(t *testing.T) {
	const mb64 = 64 * 1024 * 1024

	tests := []struct {
		input    float64
		expected float64
	}{
		{0, mb64},            // min 64MB
		{1, mb64},            // rounds up to 64MB
		{mb64, mb64},         // exact
		{mb64 + 1, 2 * mb64}, // just over → rounds up
		{2 * mb64, 2 * mb64}, // exact
	}

	for _, tc := range tests {
		got := roundMemory(tc.input)
		if got != tc.expected {
			t.Errorf("roundMemory(%v) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}
