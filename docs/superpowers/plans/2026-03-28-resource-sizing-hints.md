# Resource Right-Sizing Hints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface proactive right-sizing hints comparing actual Prometheus metrics against configured service limits/reservations, with suggested values that can be applied directly from the editor.

**Architecture:** New `internal/sizing` package with a background `Monitor` goroutine that periodically queries Prometheus, evaluates per-service resource usage against thresholds, and stores results in memory. Exposed via `GET /services/sizing`. Frontend conditionally shows a "Sizing" column on the service list and hint banners on the service detail page.

**Tech Stack:** Go (backend monitor + config + handler), React/TypeScript (frontend hooks + UI), Prometheus PromQL (metrics queries)

---

### Task 1: Add `resolveFloat` Helper

**Files:**
- Modify: `internal/config/resolve.go`
- Test: `internal/config/resolve_test.go`

- [ ] **Step 1: Write failing test for resolveFloat**

In `internal/config/resolve_test.go`, add:

```go
func TestResolveFloat(t *testing.T) {
	tests := []struct {
		name    string
		flag    *float64
		envKey  string
		envVal  string
		file    *float64
		def     float64
		min     float64
		max     float64
		want    float64
		wantErr bool
	}{
		{name: "default", def: 0.20, min: 0, max: 1, want: 0.20},
		{name: "flag wins", flag: ptr(0.5), def: 0.20, min: 0, max: 1, want: 0.5},
		{name: "env wins over file", envKey: "TEST_FLOAT", envVal: "0.75", file: ptr(0.3), def: 0.20, min: 0, max: 1, want: 0.75},
		{name: "file wins over default", file: ptr(0.4), def: 0.20, min: 0, max: 1, want: 0.4},
		{name: "below min", flag: ptr(-0.1), min: 0, max: 1, wantErr: true},
		{name: "above max", flag: ptr(1.5), min: 0, max: 1, wantErr: true},
		{name: "invalid env", envKey: "TEST_FLOAT", envVal: "notanumber", min: 0, max: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}

			got, err := resolveFloat(tt.flag, tt.envKey, tt.file, tt.def, tt.min, tt.max)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestResolveFloat -v`
Expected: FAIL — `resolveFloat` undefined

- [ ] **Step 3: Implement resolveFloat**

In `internal/config/resolve.go`, add after `checkIntRange`:

```go
// resolveFloat returns the first set value in precedence order:
// flag > env > file > hardcoded default. Returns an error if any
// explicitly set value is not a valid float or is out of [min, max].
func resolveFloat(flag *float64, envKey string, file *float64, def, min, max float64) (float64, error) {
	var raw string
	var source string
	switch envVal := os.Getenv(envKey); {
	case flag != nil:
		return checkFloatRange(*flag, min, max, "flag")
	case envVal != "":
		raw, source = envVal, envKey
	case file != nil:
		return checkFloatRange(*file, min, max, "config file")
	default:
		return def, nil
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid float from %s %q: %w", source, raw, err)
	}
	return checkFloatRange(v, min, max, source)
}

func checkFloatRange(v, min, max float64, source string) (float64, error) {
	if v < min || v > max {
		return 0, fmt.Errorf("value %f from %s out of range [%f, %f]", v, source, min, max)
	}
	return v, nil
}
```

Add `"strconv"` to the imports if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestResolveFloat -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/resolve.go internal/config/resolve_test.go
git commit -m "feat(config): add resolveFloat helper for float config values"
```

---

### Task 2: Add Sizing Configuration

**Files:**
- Create: `internal/config/sizing.go`
- Create: `internal/config/sizing_test.go`
- Modify: `internal/config/file.go` (add `fileSizing` struct to `fileConfig`)

- [ ] **Step 1: Write failing test for LoadSizing**

Create `internal/config/sizing_test.go`:

```go
package config

import (
	"testing"
	"time"
)

func TestLoadSizing_Defaults(t *testing.T) {
	// Clear any env vars that could affect the test
	for _, key := range []string{
		"CETACEAN_SIZING_ENABLED",
		"CETACEAN_SIZING_INTERVAL",
		"CETACEAN_SIZING_HEADROOM_MULTIPLIER",
		"CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED",
		"CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT",
		"CETACEAN_SIZING_THRESHOLD_AT_LIMIT",
		"CETACEAN_SIZING_SUSTAINED_TICKS",
	} {
		t.Setenv(key, "")
	}

	cfg, err := LoadSizing(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("interval: got %v, want 60s", cfg.Interval)
	}
	if cfg.HeadroomMultiplier != 2.0 {
		t.Errorf("headroom: got %f, want 2.0", cfg.HeadroomMultiplier)
	}
	if cfg.OverProvisioned != 0.20 {
		t.Errorf("over-provisioned: got %f, want 0.20", cfg.OverProvisioned)
	}
	if cfg.ApproachingLimit != 0.80 {
		t.Errorf("approaching-limit: got %f, want 0.80", cfg.ApproachingLimit)
	}
	if cfg.AtLimit != 0.95 {
		t.Errorf("at-limit: got %f, want 0.95", cfg.AtLimit)
	}
	if cfg.SustainedTicks != 3 {
		t.Errorf("sustained-ticks: got %d, want 3", cfg.SustainedTicks)
	}
}

func TestLoadSizing_EnvOverrides(t *testing.T) {
	t.Setenv("CETACEAN_SIZING_ENABLED", "false")
	t.Setenv("CETACEAN_SIZING_INTERVAL", "30s")
	t.Setenv("CETACEAN_SIZING_HEADROOM_MULTIPLIER", "1.5")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED", "0.10")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "0.70")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_AT_LIMIT", "0.90")
	t.Setenv("CETACEAN_SIZING_SUSTAINED_TICKS", "5")

	cfg, err := LoadSizing(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Enabled {
		t.Error("expected disabled")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval: got %v, want 30s", cfg.Interval)
	}
	if cfg.HeadroomMultiplier != 1.5 {
		t.Errorf("headroom: got %f, want 1.5", cfg.HeadroomMultiplier)
	}
	if cfg.OverProvisioned != 0.10 {
		t.Errorf("over-provisioned: got %f, want 0.10", cfg.OverProvisioned)
	}
	if cfg.ApproachingLimit != 0.70 {
		t.Errorf("approaching-limit: got %f, want 0.70", cfg.ApproachingLimit)
	}
	if cfg.AtLimit != 0.90 {
		t.Errorf("at-limit: got %f, want 0.90", cfg.AtLimit)
	}
	if cfg.SustainedTicks != 5 {
		t.Errorf("sustained-ticks: got %d, want 5", cfg.SustainedTicks)
	}
}

func TestLoadSizing_FileConfig(t *testing.T) {
	// Clear env vars
	for _, key := range []string{
		"CETACEAN_SIZING_ENABLED",
		"CETACEAN_SIZING_INTERVAL",
		"CETACEAN_SIZING_HEADROOM_MULTIPLIER",
		"CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED",
		"CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT",
		"CETACEAN_SIZING_THRESHOLD_AT_LIMIT",
		"CETACEAN_SIZING_SUSTAINED_TICKS",
	} {
		t.Setenv(key, "")
	}

	interval := "45s"
	multiplier := 3.0
	overProv := 0.15
	fc := &fileConfig{
		Sizing: &fileSizing{
			Interval: &interval,
			Headroom: &multiplier,
			Thresholds: &fileSizingThresholds{
				OverProvisioned: &overProv,
			},
		},
	}

	cfg, err := LoadSizing(fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Interval != 45*time.Second {
		t.Errorf("interval: got %v, want 45s", cfg.Interval)
	}
	if cfg.HeadroomMultiplier != 3.0 {
		t.Errorf("headroom: got %f, want 3.0", cfg.HeadroomMultiplier)
	}
	if cfg.OverProvisioned != 0.15 {
		t.Errorf("over-provisioned: got %f, want 0.15", cfg.OverProvisioned)
	}
	// Others should be defaults
	if cfg.ApproachingLimit != 0.80 {
		t.Errorf("approaching-limit: got %f, want 0.80 (default)", cfg.ApproachingLimit)
	}
}

func TestLoadSizing_InvalidThreshold(t *testing.T) {
	t.Setenv("CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED", "1.5")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_AT_LIMIT", "")
	t.Setenv("CETACEAN_SIZING_ENABLED", "")
	t.Setenv("CETACEAN_SIZING_INTERVAL", "")
	t.Setenv("CETACEAN_SIZING_HEADROOM_MULTIPLIER", "")
	t.Setenv("CETACEAN_SIZING_SUSTAINED_TICKS", "")

	_, err := LoadSizing(nil)
	if err == nil {
		t.Fatal("expected error for threshold > 1.0")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadSizing -v`
Expected: FAIL — `LoadSizing` undefined

- [ ] **Step 3: Add fileSizing to fileConfig**

In `internal/config/file.go`, add the TOML structs before `fileAuthOIDC` (around line 93) and add the field to `fileConfig`:

```go
type fileSizing struct {
	Enabled    *bool                  `toml:"enabled"`
	Interval   *string                `toml:"interval"`
	Headroom   *float64               `toml:"headroom_multiplier"`
	Thresholds *fileSizingThresholds  `toml:"thresholds"`
}

type fileSizingThresholds struct {
	OverProvisioned *float64 `toml:"over_provisioned"`
	ApproachingLimit *float64 `toml:"approaching_limit"`
	AtLimit          *float64 `toml:"at_limit"`
	SustainedTicks   *int     `toml:"sustained_ticks"`
}
```

Add to `fileConfig` struct (line ~57):
```go
Sizing  *fileSizing  `toml:"sizing"`
```

- [ ] **Step 4: Implement LoadSizing**

Create `internal/config/sizing.go`:

```go
package config

import "time"

// SizingConfig controls the resource right-sizing monitor.
type SizingConfig struct {
	Enabled            bool
	Interval           time.Duration
	HeadroomMultiplier float64
	OverProvisioned    float64 // below this fraction of reservation = over-provisioned
	ApproachingLimit   float64 // above this fraction of limit = approaching
	AtLimit            float64 // above this fraction of limit = at limit
	SustainedTicks     int     // consecutive ticks required for over-provisioned
}

// LoadSizing resolves sizing configuration from file config, env vars, and defaults.
// Accepts *fileConfig (unexported) — callers in main.go pass the pointer through without naming the type.
func LoadSizing(fc *fileConfig) (*SizingConfig, error) {
	var (
		fEnabled    *bool
		fInterval   *string
		fHeadroom   *float64
		fOverProv   *float64
		fApproach   *float64
		fAtLimit    *float64
		fSustained  *int
	)

	if fc != nil && fc.Sizing != nil {
		fEnabled = fc.Sizing.Enabled
		fInterval = fc.Sizing.Interval
		fHeadroom = fc.Sizing.Headroom
		if fc.Sizing.Thresholds != nil {
			fOverProv = fc.Sizing.Thresholds.OverProvisioned
			fApproach = fc.Sizing.Thresholds.ApproachingLimit
			fAtLimit = fc.Sizing.Thresholds.AtLimit
			fSustained = fc.Sizing.Thresholds.SustainedTicks
		}
	}

	interval, err := resolveDuration(nil, "CETACEAN_SIZING_INTERVAL", fInterval, 60*time.Second)
	if err != nil {
		return nil, err
	}

	headroom, err := resolveFloat(nil, "CETACEAN_SIZING_HEADROOM_MULTIPLIER", fHeadroom, 2.0, 1.0, 10.0)
	if err != nil {
		return nil, err
	}

	overProv, err := resolveFloat(nil, "CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED", fOverProv, 0.20, 0.0, 1.0)
	if err != nil {
		return nil, err
	}

	approach, err := resolveFloat(nil, "CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", fApproach, 0.80, 0.0, 1.0)
	if err != nil {
		return nil, err
	}

	atLimit, err := resolveFloat(nil, "CETACEAN_SIZING_THRESHOLD_AT_LIMIT", fAtLimit, 0.95, 0.0, 1.0)
	if err != nil {
		return nil, err
	}

	sustained, err := resolveInt(nil, "CETACEAN_SIZING_SUSTAINED_TICKS", fSustained, 3, 1, 100)
	if err != nil {
		return nil, err
	}

	return &SizingConfig{
		Enabled:            resolveBool(nil, "CETACEAN_SIZING_ENABLED", fEnabled, true),
		Interval:           interval,
		HeadroomMultiplier: headroom,
		OverProvisioned:    overProv,
		ApproachingLimit:   approach,
		AtLimit:            atLimit,
		SustainedTicks:     sustained,
	}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestLoadSizing -v`
Expected: all PASS

- [ ] **Step 6: Run full config test suite**

Run: `go test ./internal/config/ -v`
Expected: all PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add internal/config/sizing.go internal/config/sizing_test.go internal/config/file.go
git commit -m "feat(config): add sizing configuration with thresholds"
```

---

### Task 3: Implement Evaluate Logic

**Files:**
- Create: `internal/sizing/sizing.go` (types)
- Create: `internal/sizing/evaluate.go`
- Create: `internal/sizing/evaluate_test.go`

- [ ] **Step 1: Create types**

Create `internal/sizing/sizing.go`:

```go
package sizing

import "time"

// Category of recommendation.
type Category string

const (
	CategoryOverProvisioned  Category = "over-provisioned"
	CategoryApproachingLimit Category = "approaching-limit"
	CategoryAtLimit          Category = "at-limit"
	CategoryNoLimits         Category = "no-limits"
	CategoryNoReservations   Category = "no-reservations"
)

// Severity for visual treatment.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Recommendation is a single right-sizing hint for a resource.
type Recommendation struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Resource   string   `json:"resource"`
	Message    string   `json:"message"`
	Current    float64  `json:"current"`
	Configured float64  `json:"configured"`
	Suggested  *float64 `json:"suggested,omitempty"`
}

// ServiceSizing holds all recommendations for a single service.
type ServiceSizing struct {
	ServiceID   string           `json:"serviceId"`
	ServiceName string           `json:"serviceName"`
	Hints       []Recommendation `json:"hints"`
	ComputedAt  time.Time        `json:"computedAt"`
}
```

- [ ] **Step 2: Write failing tests for evaluate**

Create `internal/sizing/evaluate_test.go`:

```go
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

func TestEvaluate_NoLimits(t *testing.T) {
	result := evaluate(
		serviceSpec{name: "test-svc"},
		&serviceMetrics{cpu: 50, memory: 1024},
		nil,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryNoLimits {
			found = true
			if h.Severity != SeverityWarning {
				t.Errorf("expected warning severity, got %s", h.Severity)
			}
		}
	}
	if !found {
		t.Error("expected no-limits hint")
	}
}

func TestEvaluate_NoReservations(t *testing.T) {
	result := evaluate(
		serviceSpec{
			name:        "test-svc",
			cpuLimit:    1e9,     // 1 core
			memoryLimit: 1 << 30, // 1GB
		},
		&serviceMetrics{cpu: 50, memory: 500 << 20},
		nil,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryNoReservations {
			found = true
			if h.Severity != SeverityInfo {
				t.Errorf("expected info severity, got %s", h.Severity)
			}
		}
	}
	if !found {
		t.Error("expected no-reservations hint")
	}
}

func TestEvaluate_AtLimit(t *testing.T) {
	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    0.5e9,
			memoryLimit:       1 << 30,
			memoryReservation: 512 << 20,
		},
		&serviceMetrics{
			cpu:    98,  // 98% — above 95% threshold
			memory: 500 << 20,
		},
		nil,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryAtLimit && h.Resource == "cpu" {
			found = true
			if h.Severity != SeverityCritical {
				t.Errorf("expected critical severity, got %s", h.Severity)
			}
			if h.Suggested == nil {
				t.Error("expected suggested value")
			}
		}
	}
	if !found {
		t.Error("expected at-limit hint for CPU")
	}
}

func TestEvaluate_ApproachingLimit(t *testing.T) {
	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    0.5e9,
			memoryLimit:       1 << 30,
			memoryReservation: 512 << 20,
		},
		&serviceMetrics{
			cpu:    85, // 85% — between 80-95%
			memory: 500 << 20,
		},
		nil,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryApproachingLimit && h.Resource == "cpu" {
			found = true
			if h.Severity != SeverityWarning {
				t.Errorf("expected warning severity, got %s", h.Severity)
			}
		}
	}
	if !found {
		t.Error("expected approaching-limit hint for CPU")
	}
}

func TestEvaluate_OverProvisioned_NotSustained(t *testing.T) {
	// Only 1 previous tick below threshold — not sustained yet (need 3)
	prev := &previousState{
		cpuLowTicks:    1,
		memoryLowTicks: 0,
	}

	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    1e9,
			memoryLimit:       1 << 30,
			memoryReservation: 1 << 30,
		},
		&serviceMetrics{
			cpu:    10, // 10% of 100% = below 20% threshold
			memory: 800 << 20,
		},
		prev,
		defaultConfig(),
	)

	for _, h := range result.hints {
		if h.Category == CategoryOverProvisioned && h.Resource == "cpu" {
			t.Error("should not flag over-provisioned before sustained ticks")
		}
	}
}

func TestEvaluate_OverProvisioned_Sustained(t *testing.T) {
	// 2 previous ticks + current = 3 = sustained
	prev := &previousState{
		cpuLowTicks:    2,
		memoryLowTicks: 0,
	}

	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    1e9,
			memoryLimit:       1 << 30,
			memoryReservation: 1 << 30,
		},
		&serviceMetrics{
			cpu:    10,
			memory: 800 << 20,
		},
		prev,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryOverProvisioned && h.Resource == "cpu" {
			found = true
			if h.Severity != SeverityInfo {
				t.Errorf("expected info severity, got %s", h.Severity)
			}
			if h.Suggested == nil {
				t.Error("expected suggested value")
			}
		}
	}
	if !found {
		t.Error("expected over-provisioned hint for CPU after sustained ticks")
	}
}

func TestEvaluate_Healthy(t *testing.T) {
	prev := &previousState{}
	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    0.5e9,
			memoryLimit:       1 << 30,
			memoryReservation: 512 << 20,
		},
		&serviceMetrics{
			cpu:    50,
			memory: 600 << 20,
		},
		prev,
		defaultConfig(),
	)

	if len(result.hints) != 0 {
		t.Errorf("expected no hints for healthy service, got %d: %+v", len(result.hints), result.hints)
	}
}

func TestEvaluate_NoMetrics_ConfigOnlyHints(t *testing.T) {
	result := evaluate(
		serviceSpec{name: "test-svc"},
		nil,
		nil,
		defaultConfig(),
	)

	found := false
	for _, h := range result.hints {
		if h.Category == CategoryNoLimits {
			found = true
		}
		if h.Category == CategoryOverProvisioned || h.Category == CategoryApproachingLimit || h.Category == CategoryAtLimit {
			t.Errorf("should not produce metrics-based hint without metrics: %s", h.Category)
		}
	}
	if !found {
		t.Error("expected no-limits config-only hint even without metrics")
	}
}

func TestEvaluate_SuggestedValueRounding(t *testing.T) {
	result := evaluate(
		serviceSpec{
			name:              "test-svc",
			cpuLimit:          1e9,
			cpuReservation:    1e9,
			memoryLimit:       1 << 30,
			memoryReservation: 1 << 30,
		},
		&serviceMetrics{
			cpu:    10,
			memory: 100 << 20, // 100MB — well below 1GB reservation
		},
		&previousState{cpuLowTicks: 2, memoryLowTicks: 2},
		defaultConfig(),
	)

	for _, h := range result.hints {
		if h.Suggested == nil {
			continue
		}
		if h.Resource == "memory" {
			// Should be rounded to nearest 64MB
			mb := *h.Suggested / (1 << 20)
			remainder := int(mb) % 64
			if remainder != 0 {
				t.Errorf("memory suggestion %f bytes not rounded to 64MB: %fMB", *h.Suggested, mb)
			}
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/sizing/ -v`
Expected: FAIL — `evaluate` undefined

- [ ] **Step 4: Implement evaluate**

Create `internal/sizing/evaluate.go`:

```go
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

// evaluate compares actual metrics against spec and returns recommendations
// plus updated tick counters for sustained-low tracking.
// metrics may be nil if Prometheus data is unavailable — only config-only hints are returned.
// prev may be nil on first evaluation.
func evaluate(
	spec serviceSpec,
	metrics *serviceMetrics,
	prev *previousState,
	cfg *config.SizingConfig,
) evaluateResult {
	var hints []Recommendation
	var newState previousState

	hasCPULimit := spec.cpuLimit > 0
	hasCPURes := spec.cpuReservation > 0
	hasMemLimit := spec.memoryLimit > 0
	hasMemRes := spec.memoryReservation > 0

	// Config-only checks
	if !hasCPULimit && !hasMemLimit {
		hints = append(hints, Recommendation{
			Category: CategoryNoLimits,
			Severity: SeverityWarning,
			Resource: "cpu+memory",
			Message:  "No resource limits configured",
		})
	} else {
		if !hasCPULimit {
			hints = append(hints, Recommendation{
				Category: CategoryNoLimits,
				Severity: SeverityWarning,
				Resource: "cpu",
				Message:  "No CPU limit configured",
			})
		}
		if !hasMemLimit {
			hints = append(hints, Recommendation{
				Category: CategoryNoLimits,
				Severity: SeverityWarning,
				Resource: "memory",
				Message:  "No memory limit configured",
			})
		}
	}

	if (hasCPULimit || hasMemLimit) && !hasCPURes && !hasMemRes {
		hints = append(hints, Recommendation{
			Category: CategoryNoReservations,
			Severity: SeverityInfo,
			Resource: "cpu+memory",
			Message:  "No resource reservations configured",
		})
	} else {
		if hasCPULimit && !hasCPURes {
			hints = append(hints, Recommendation{
				Category: CategoryNoReservations,
				Severity: SeverityInfo,
				Resource: "cpu",
				Message:  "No CPU reservation configured",
			})
		}
		if hasMemLimit && !hasMemRes {
			hints = append(hints, Recommendation{
				Category: CategoryNoReservations,
				Severity: SeverityInfo,
				Resource: "memory",
				Message:  "No memory reservation configured",
			})
		}
	}

	if metrics == nil {
		return evaluateResult{hints: hints}
	}

	// Metrics-based checks: CPU
	if hasCPULimit {
		cpuLimitPct := float64(spec.cpuLimit) / 1e9 * 100
		ratio := metrics.cpu / cpuLimitPct

		if ratio >= cfg.AtLimit {
			suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryAtLimit,
				Severity:   SeverityCritical,
				Resource:   "cpu",
				Message:    fmt.Sprintf("CPU usage at %.0f%% of limit", ratio*100),
				Current:    metrics.cpu,
				Configured: cpuLimitPct,
				Suggested:  &suggested,
			})
		} else if ratio >= cfg.ApproachingLimit {
			suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryApproachingLimit,
				Severity:   SeverityWarning,
				Resource:   "cpu",
				Message:    fmt.Sprintf("CPU usage at %.0f%% of limit", ratio*100),
				Current:    metrics.cpu,
				Configured: cpuLimitPct,
				Suggested:  &suggested,
			})
		}
	}

	if hasCPURes {
		cpuResPct := float64(spec.cpuReservation) / 1e9 * 100
		ratio := metrics.cpu / cpuResPct
		lowTicks := 0
		if prev != nil {
			lowTicks = prev.cpuLowTicks
		}

		if ratio < cfg.OverProvisioned {
			lowTicks++
		} else {
			lowTicks = 0
		}
		newState.cpuLowTicks = lowTicks

		if lowTicks >= cfg.SustainedTicks {
			suggested := roundCPU(metrics.cpu / 100 * 1e9 * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryOverProvisioned,
				Severity:   SeverityInfo,
				Resource:   "cpu",
				Message:    fmt.Sprintf("CPU using %.0f%% of reservation", ratio*100),
				Current:    metrics.cpu,
				Configured: cpuResPct,
				Suggested:  &suggested,
			})
		}
	}

	// Metrics-based checks: Memory
	if hasMemLimit {
		ratio := metrics.memory / float64(spec.memoryLimit)

		if ratio >= cfg.AtLimit {
			suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryAtLimit,
				Severity:   SeverityCritical,
				Resource:   "memory",
				Message:    fmt.Sprintf("Memory usage at %.0f%% of limit", ratio*100),
				Current:    metrics.memory,
				Configured: float64(spec.memoryLimit),
				Suggested:  &suggested,
			})
		} else if ratio >= cfg.ApproachingLimit {
			suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryApproachingLimit,
				Severity:   SeverityWarning,
				Resource:   "memory",
				Message:    fmt.Sprintf("Memory usage at %.0f%% of limit", ratio*100),
				Current:    metrics.memory,
				Configured: float64(spec.memoryLimit),
				Suggested:  &suggested,
			})
		}
	}

	if hasMemRes {
		ratio := metrics.memory / float64(spec.memoryReservation)
		lowTicks := 0
		if prev != nil {
			lowTicks = prev.memoryLowTicks
		}

		if ratio < cfg.OverProvisioned {
			lowTicks++
		} else {
			lowTicks = 0
		}
		newState.memoryLowTicks = lowTicks

		if lowTicks >= cfg.SustainedTicks {
			suggested := roundMemory(metrics.memory * cfg.HeadroomMultiplier)
			hints = append(hints, Recommendation{
				Category:   CategoryOverProvisioned,
				Severity:   SeverityInfo,
				Resource:   "memory",
				Message:    fmt.Sprintf("Memory using %.0f%% of reservation", ratio*100),
				Current:    metrics.memory,
				Configured: float64(spec.memoryReservation),
				Suggested:  &suggested,
			})
		}
	}

	return evaluateResult{hints: hints, newState: newState}
}

// roundCPU rounds NanoCPUs to nearest 0.05 cores.
func roundCPU(nanoCPUs float64) float64 {
	cores := nanoCPUs / 1e9
	rounded := math.Round(cores*20) / 20 // nearest 0.05
	if rounded < 0.05 {
		rounded = 0.05
	}
	return rounded * 1e9
}

// roundMemory rounds bytes to nearest 64MB.
func roundMemory(bytes float64) float64 {
	const unit = 64 << 20 // 64MB
	rounded := math.Ceil(bytes/float64(unit)) * float64(unit)
	if rounded < float64(unit) {
		rounded = float64(unit)
	}
	return rounded
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/sizing/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/sizing/
git commit -m "feat(sizing): add evaluate logic with threshold comparisons"
```

---

### Task 4: Implement Sizing Monitor

**Files:**
- Create: `internal/sizing/monitor.go`
- Create: `internal/sizing/monitor_test.go`

- [ ] **Step 1: Write failing test for Monitor**

Create `internal/sizing/monitor_test.go`:

```go
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

// mockQuery returns a QueryFunc that dispatches based on query content.
func mockQuery(cpuResults, memResults []PromResult) QueryFunc {
	return func(_ context.Context, query string) ([]PromResult, error) {
		if strings.Contains(query, "cpu_usage") {
			return cpuResults, nil
		}
		if strings.Contains(query, "memory_usage") {
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

	query := mockQuery(
		[]PromResult{{Labels: map[string]string{"container_label_com_docker_swarm_service_name": "web"}, Value: 90}},
		[]PromResult{{Labels: map[string]string{"container_label_com_docker_swarm_service_name": "web"}, Value: float64(400 << 20)}},
	)

	cfg := &config.SizingConfig{
		Enabled:            true,
		Interval:           time.Second,
		HeadroomMultiplier: 2.0,
		OverProvisioned:    0.20,
		ApproachingLimit:   0.80,
		AtLimit:            0.95,
		SustainedTicks:     3,
	}

	m := New(query, c, cfg)
	m.tick(context.Background())

	results := m.Results()
	if len(results) == 0 {
		t.Fatal("expected at least one service sizing result")
	}

	found := false
	for _, ss := range results {
		if ss.ServiceName == "web" {
			found = true
			// CPU at 90% of 100% limit = approaching limit
			for _, h := range ss.Hints {
				if h.Resource == "cpu" && h.Category == CategoryApproachingLimit {
					return // success
				}
			}
			t.Errorf("expected approaching-limit CPU hint, got hints: %+v", ss.Hints)
		}
	}
	if !found {
		t.Error("expected sizing result for service 'web'")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sizing/ -run TestMonitor -v`
Expected: FAIL — `Monitor`, `New` undefined

- [ ] **Step 3: Implement Monitor**

Create `internal/sizing/monitor.go`:

```go
package sizing

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

const (
	serviceLabelKey = "container_label_com_docker_swarm_service_name"
	cpuQuery        = `sum by (` + serviceLabelKey + `)(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_id!=""}[5m])) * 100`
	memoryQuery     = `avg_over_time(sum by (` + serviceLabelKey + `)(container_memory_usage_bytes{container_label_com_docker_swarm_service_id!=""})[1h:])`
)

// PromResult holds a single Prometheus query result.
// Defined locally to avoid circular imports with the api package.
type PromResult struct {
	Labels map[string]string
	Value  float64
}

// QueryFunc executes a Prometheus instant query and returns results.
// In production, wrap api.PromClient: func(ctx, q) -> convert []api.PromResult to []sizing.PromResult.
type QueryFunc func(ctx context.Context, query string) ([]PromResult, error)

// Monitor periodically evaluates service resource sizing.
type Monitor struct {
	query QueryFunc
	cache *cache.Cache
	cfg   *config.SizingConfig

	mu       sync.RWMutex
	results  []ServiceSizing
	previous map[string]*previousState // keyed by service ID
}

// New creates a new sizing monitor. Returns nil if query is nil.
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

	// Run immediately on startup
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Query Prometheus for CPU and memory in parallel
	type queryResult struct {
		data map[string]float64 // service name → value
		err  error
	}
	cpuCh := make(chan queryResult, 1)
	memCh := make(chan queryResult, 1)

	go func() {
		data, err := m.queryByService(ctx, cpuQuery)
		cpuCh <- queryResult{data, err}
	}()
	go func() {
		data, err := m.queryByService(ctx, memoryQuery)
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

	// Build service specs from cache
	services := m.cache.ListServices()
	now := time.Now()
	var results []ServiceSizing
	newPrevious := make(map[string]*previousState, len(services))

	for _, svc := range services {
		spec := extractSpec(svc)
		prev := m.previous[svc.ID]

		var metrics *serviceMetrics
		cpuVal, hasCPU := cpuResult.data[spec.name]
		memVal, hasMem := memResult.data[spec.name]
		if hasCPU || hasMem {
			metrics = &serviceMetrics{cpu: cpuVal, memory: memVal}
		}

		result := evaluate(spec, metrics, prev, m.cfg)
		newPrevious[svc.ID] = &result.newState

		if len(result.hints) > 0 {
			results = append(results, ServiceSizing{
				ServiceID:   svc.ID,
				ServiceName: spec.name,
				Hints:       result.hints,
				ComputedAt:  now,
			})
		}
	}

	m.mu.Lock()
	m.results = results
	m.previous = newPrevious
	m.mu.Unlock()

	slog.Debug("sizing: evaluation complete", "services", len(services), "hints", len(results))
}

func (m *Monitor) queryByService(ctx context.Context, query string) (map[string]float64, error) {
	results, err := m.query(ctx, query)
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(results))
	for _, r := range results {
		name := r.Labels[serviceLabelKey]
		if name != "" {
			out[name] = r.Value
		}
	}
	return out, nil
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
```

Add `"github.com/docker/docker/api/types/swarm"` to imports. The `sizing` package does NOT import `internal/api` — it defines its own `PromResult` and `QueryFunc` types to avoid circular imports (`api` imports `sizing` via handlers).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sizing/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sizing/monitor.go internal/sizing/monitor_test.go
git commit -m "feat(sizing): add background monitor with Prometheus queries"
```

---

### Task 5: Add API Endpoint

**Files:**
- Modify: `internal/api/handlers.go` (add `sizingMonitor` field to `Handlers`, add `HandleServicesSizing` handler)
- Modify: `internal/api/router.go` (register `GET /services/sizing`)
- Modify: `main.go` (create and start the monitor)

- [ ] **Step 1: Add sizingMonitor to Handlers struct**

In `internal/api/handlers.go`, add to `Handlers` struct (around line 194):

```go
sizingMonitor *sizing.Monitor
```

Update `NewHandlers` to accept the monitor parameter. Add it as the last parameter:

```go
func NewHandlers(
	c *cache.Cache,
	b *Broadcaster,
	dc DockerLogStreamer,
	sc DockerSystemClient,
	wc DockerWriteClient,
	pc DockerPluginClient,
	ready <-chan struct{},
	promClient *PromClient,
	operationsLevel config.OperationsLevel,
	sizingMonitor *sizing.Monitor,
) *Handlers {
```

And set it in the returned struct:
```go
sizingMonitor: sizingMonitor,
```

Add import: `"github.com/radiergummi/cetacean/internal/sizing"`

- [ ] **Step 2: Implement HandleServicesSizing**

Add to `internal/api/handlers.go` (or a new file `internal/api/sizing_handler.go` if preferred for clarity):

```go
func (h *Handlers) HandleServicesSizing(w http.ResponseWriter, r *http.Request) {
	results := h.sizingMonitor.Results()
	writeJSONWithETag(w, r, results)
}
```

- [ ] **Step 3: Register the route**

In `internal/api/router.go`, add after the cluster routes (around line 57):

```go
// Sizing hints
mux.HandleFunc("GET /services/sizing", contentNegotiated(h.HandleServicesSizing, spa))
```

- [ ] **Step 4: Wire up in main.go**

In `main.go`, after creating `promClient` (around line 196), add:

```go
var sizingMonitor *sizing.Monitor
if promClient != nil {
	sizingCfg, err := config.LoadSizing(fc)
	if err != nil {
		slog.Error("failed to load sizing config", "error", err)
		os.Exit(1)
	}
	// Wrap api.PromClient into sizing.QueryFunc to avoid circular imports
	queryFunc := sizing.QueryFunc(func(ctx context.Context, query string) ([]sizing.PromResult, error) {
		results, err := promClient.InstantQuery(ctx, query)
		if err != nil {
			return nil, err
		}
		out := make([]sizing.PromResult, len(results))
		for i, r := range results {
			out[i] = sizing.PromResult{Labels: r.Labels, Value: r.Value}
		}
		return out, nil
	})
	sizingMonitor = sizing.New(queryFunc, stateCache, sizingCfg)
	if sizingMonitor != nil {
		go sizingMonitor.Run(ctx)
		slog.Info("sizing monitor started", "interval", sizingCfg.Interval)
	}
}
```

`LoadSizing` accepts `*fileConfig` (unexported) — `main.go` just passes the `fc` pointer through without naming the type, same as it does for `Load`, `LoadAuth`, and `LoadTLS`.

Pass the sizing monitor to `NewHandlers`:
```go
handlers := api.NewHandlers(
	stateCache,
	broadcaster,
	dockerClient,
	dockerClient,
	dockerClient,
	dockerClient,
	watcher.Ready(),
	promClient,
	cfg.OperationsLevel,
	sizingMonitor,
)
```

- [ ] **Step 5: Run full test suite to check for compilation errors**

Run: `go build ./...`
Expected: compilation will fail — existing `NewHandlers` call sites are missing the new `sizingMonitor` parameter.

- [ ] **Step 5b: Fix NewHandlers call sites in test files**

Add `nil` as the last argument to every `NewHandlers(...)` call in these test files:
- `internal/api/handlers_test.go`
- `internal/api/write_handlers_test.go`
- `internal/api/integration_test.go`
- `internal/api/handlers_bench_test.go`
- Any other file that calls `NewHandlers` (search with `grep -r "NewHandlers(" internal/api/`)

- [ ] **Step 5c: Verify build and tests pass**

Run: `go build ./...`
Expected: compiles successfully

Run: `go test ./...`
Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/router.go main.go internal/api/*_test.go
git commit -m "feat(api): add GET /services/sizing endpoint"
```

---

### Task 6: Frontend Types and API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add TypeScript types**

In `frontend/src/api/types.ts`, add at the end of the file:

```typescript
export type SizingCategory =
  | "over-provisioned"
  | "approaching-limit"
  | "at-limit"
  | "no-limits"
  | "no-reservations";

export type SizingSeverity = "info" | "warning" | "critical";

export interface SizingRecommendation {
  category: SizingCategory;
  severity: SizingSeverity;
  resource: string;
  message: string;
  current: number;
  configured: number;
  suggested?: number;
}

export interface ServiceSizing {
  serviceId: string;
  serviceName: string;
  hints: SizingRecommendation[];
  computedAt: string;
}
```

- [ ] **Step 2: Add API client method**

In `frontend/src/api/client.ts`, add to the `api` object (near the other service-related methods):

```typescript
serviceSizing: () => fetchJSON<ServiceSizing[]>("/services/sizing"),
```

Add `ServiceSizing` to the imports from `types.ts`.

- [ ] **Step 3: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat(frontend): add sizing types and API client method"
```

---

### Task 7: Frontend `useSizingHints` Hook

**Files:**
- Create: `frontend/src/hooks/useSizingHints.ts`

- [ ] **Step 1: Create the hook**

Create `frontend/src/hooks/useSizingHints.ts`:

```typescript
import { api } from "@/api/client";
import type { ServiceSizing } from "@/api/types";
import { useMonitoringStatus } from "./useMonitoringStatus";
import { useEffect, useState } from "react";

interface SizingHints {
  byServiceId: Map<string, ServiceSizing>;
  hasData: boolean;
}

const emptySizingHints: SizingHints = {
  byServiceId: new Map(),
  hasData: false,
};

export function useSizingHints(): SizingHints {
  const monitoring = useMonitoringStatus();
  const enabled =
    monitoring?.prometheusConfigured &&
    monitoring?.prometheusReachable;

  const [hints, setHints] = useState<SizingHints>(emptySizingHints);

  useEffect(() => {
    if (!enabled) {
      setHints(emptySizingHints);
      return;
    }

    let cancelled = false;

    api.serviceSizing().then((results) => {
      if (cancelled) {
        return;
      }

      const byServiceId = new Map<string, ServiceSizing>();

      for (const sizing of results) {
        byServiceId.set(sizing.serviceId, sizing);
      }

      setHints({
        byServiceId,
        hasData: true,
      });
    }).catch(() => {
      // Sizing hints are non-critical — fail silently
    });

    return () => {
      cancelled = true;
    };
  }, [enabled]);

  return hints;
}
```

- [ ] **Step 2: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSizingHints.ts
git commit -m "feat(frontend): add useSizingHints hook"
```

---

### Task 8: Service List — Sizing Column

**Files:**
- Modify: `frontend/src/pages/ServiceList.tsx`
- Create: `frontend/src/components/SizingBadge.tsx`

- [ ] **Step 1: Create SizingBadge component**

Create `frontend/src/components/SizingBadge.tsx`:

```typescript
import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const severityOrder: Record<SizingSeverity, number> = {
  critical: 3,
  warning: 2,
  info: 1,
};

const severityStyles: Record<SizingSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

const categoryIcons: Record<string, string> = {
  "at-limit": "\u25b2",
  "approaching-limit": "\u25b2",
  "over-provisioned": "\u25bc",
  "no-limits": "\u2610",
  "no-reservations": "\u2610",
};

/**
 * Picks the highest-severity hint and formats it for display.
 */
export function highestSeverityHint(
  hints: SizingRecommendation[],
): { label: string; severity: SizingSeverity; allHints: SizingRecommendation[] } | null {
  if (hints.length === 0) {
    return null;
  }

  const sorted = [...hints].sort(
    (a, b) => severityOrder[b.severity] - severityOrder[a.severity],
  );
  const top = sorted[0];

  const icon = categoryIcons[top.category] ?? "";
  const resource = top.resource === "cpu+memory" ? "" : top.resource.toUpperCase();

  let pct = "";

  if (top.configured > 0 && top.current > 0) {
    pct = ` ${Math.round((top.current / top.configured) * 100)}%`;
  }

  const label = top.category === "no-limits" || top.category === "no-reservations"
    ? `${icon} ${top.message}`
    : `${icon} ${resource}${pct}`;

  return { label: label.trim(), severity: top.severity, allHints: sorted };
}

export function SizingBadge({ hints }: { hints: SizingRecommendation[] }) {
  const top = highestSeverityHint(hints);

  if (!top) {
    return (
      <span className="text-xs text-green-600 dark:text-green-400">{"\u2713"} OK</span>
    );
  }

  const content = (
    <span className={`text-xs font-medium ${severityStyles[top.severity]}`}>
      {top.label}
    </span>
  );

  if (top.allHints.length <= 1) {
    return content;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {content}
      </TooltipTrigger>
      <TooltipContent>
        <ul className="space-y-1 text-xs">
          {top.allHints.map((hint, index) => (
            <li key={index}>{hint.message}</li>
          ))}
        </ul>
      </TooltipContent>
    </Tooltip>
  );
}
```

- [ ] **Step 2: Add sizing column to ServiceList**

In `frontend/src/pages/ServiceList.tsx`:

Add imports:
```typescript
import { useSizingHints } from "../hooks/useSizingHints";
import { SizingBadge } from "../components/SizingBadge";
```

After `const { getForService } = useServiceMetrics();` (line 71), add:
```typescript
const sizing = useSizingHints();
```

After the `metricsColumns` definition (around line 180), add:
```typescript
const sizingColumns: Column<ServiceListItem>[] = sizing.hasData
  ? [
      {
        header: "Sizing",
        cell: ({ ID }) => {
          const serviceSizing = sizing.byServiceId.get(ID);
          return <SizingBadge hints={serviceSizing?.hints ?? []} />;
        },
      },
    ]
  : [];
```

Update the columns array (line 182):
```typescript
const columns: Column<ServiceListItem>[] = [...baseColumns, ...metricsColumns, ...sizingColumns];
```

- [ ] **Step 3: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/SizingBadge.tsx frontend/src/pages/ServiceList.tsx
git commit -m "feat(frontend): add sizing column to service list"
```

---

### Task 9: Service Detail — PageHeader Badge

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add sizing badge to service detail header**

In `frontend/src/pages/ServiceDetail.tsx`:

Add imports:
```typescript
import { useSizingHints } from "../hooks/useSizingHints";
import { highestSeverityHint } from "../components/SizingBadge";
```

Inside the component, after existing hook calls, add:
```typescript
const sizing = useSizingHints();
const serviceSizing = id ? sizing.byServiceId.get(id) : undefined;
```

Find where `PageHeader` is rendered and add a sizing badge to the `actions` prop (or alongside the title). Create a small inline component that shows the badge and scrolls to the resources section on click:

```typescript
const sizingHint = serviceSizing ? highestSeverityHint(serviceSizing.hints) : null;
```

In the JSX where `PageHeader` is rendered, add a sizing pill. The exact integration depends on where `actions` are placed — add a clickable badge that scrolls to and expands the resources collapsible section. Use a ref or DOM query to scroll:

```typescript
{sizingHint && (
  <button
    type="button"
    className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${
      sizingHint.severity === "critical"
        ? "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400"
        : sizingHint.severity === "warning"
          ? "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
          : "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400"
    }`}
    onClick={() => {
      const resourcesSection = document.getElementById("resources-section");
      resourcesSection?.scrollIntoView({ behavior: "smooth" });
    }}
  >
    {sizingHint.label}
  </button>
)}
```

Add `id="resources-section"` to the Resources `CollapsibleSection` wrapper in the JSX.

- [ ] **Step 2: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat(frontend): add sizing badge to service detail header"
```

---

### Task 10: Service Detail — ResourcesEditor Callout

**Files:**
- Modify: `frontend/src/components/service-detail/ResourcesEditor.tsx`

- [ ] **Step 1: Add hints prop and callout banner**

In `frontend/src/components/service-detail/ResourcesEditor.tsx`:

Add to the component props (around line 56):
```typescript
hints?: SizingRecommendation[];
```

Add import:
```typescript
import type { SizingRecommendation } from "@/api/types";
```

Inside the component, before the allocation bars (or at the top of the display mode), render the callout when hints are present:

```typescript
{hints && hints.length > 0 && !editing && (
  <div className={`rounded-md border p-3 text-sm ${
    hints.some(({ severity }) => severity === "critical")
      ? "border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950/30 dark:text-red-300"
      : hints.some(({ severity }) => severity === "warning")
        ? "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-300"
        : "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300"
  }`}>
    <ul className="space-y-1">
      {hints.map((hint, index) => (
        <li key={index} className="flex items-center justify-between gap-2">
          <span>{hint.message}{hint.suggested != null && ` \u2014 consider ${
            hint.resource === "memory"
              ? formatBytes(hint.suggested)
              : formatCores(hint.suggested / 1e9)
          }`}</span>
          {hint.suggested != null && canEditConfig && (
            <button
              type="button"
              className="shrink-0 text-xs font-medium underline"
              onClick={() => {
                openEdit();
                // Pre-fill will happen via the suggested values being passed to edit state
              }}
            >
              Apply
            </button>
          )}
        </li>
      ))}
    </ul>
  </div>
)}
```

The "Apply" button enters edit mode. To pre-fill suggested values, modify `openEdit()` to accept optional initial values:

```typescript
function openEdit(prefill?: { cpuReservation?: number; cpuLimit?: number; memReservation?: number; memLimit?: number }) {
  // ... existing capacity fetch logic ...
  // If prefill provided, use those values as initial slider positions
  if (prefill) {
    if (prefill.cpuLimit != null) setCpuLimitCores(prefill.cpuLimit / 1e9);
    // ... etc for other fields
  }
}
```

Then the "Apply" button passes the hint's suggested value:
```typescript
onClick={() => openEdit({
  [hint.resource === "cpu" ? "cpuLimit" : "memLimit"]: hint.suggested,
})}
```

- [ ] **Step 2: Pass hints from ServiceDetail**

In `frontend/src/pages/ServiceDetail.tsx`, update the `ResourcesEditor` usage to pass hints:

```typescript
<ResourcesEditor
  serviceId={id!}
  resources={serviceResources}
  onSaved={setServiceResources}
  pids={taskTemplate.Resources?.Limits?.Pids}
  allocation={{
    cpuReserved,
    cpuLimit,
    cpuActual,
    memReserved,
    memLimit,
    memActual,
  }}
  hints={serviceSizing?.hints}
/>
```

- [ ] **Step 3: Verify types compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/service-detail/ResourcesEditor.tsx frontend/src/pages/ServiceDetail.tsx
git commit -m "feat(frontend): add sizing callout banner to resources editor"
```

---

### Task 11: Update CLAUDE.md and Documentation

**Files:**
- Modify: `CLAUDE.md` (add sizing env vars to the table, add sizing endpoint to architecture docs)
- Modify: `docs/configuration.md` (add sizing configuration section)

- [ ] **Step 1: Update CLAUDE.md environment variables table**

Add the sizing env vars to the table in `CLAUDE.md`:

```markdown
| `CETACEAN_SIZING_ENABLED` | `true` | No (disable sizing monitor) |
| `CETACEAN_SIZING_INTERVAL` | `60s` | No |
| `CETACEAN_SIZING_HEADROOM_MULTIPLIER` | `2.0` | No |
| `CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED` | `0.20` | No |
| `CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT` | `0.80` | No |
| `CETACEAN_SIZING_THRESHOLD_AT_LIMIT` | `0.95` | No |
| `CETACEAN_SIZING_SUSTAINED_TICKS` | `3` | No |
```

- [ ] **Step 2: Update CLAUDE.md architecture section**

Add to the Backend section:
```markdown
- **`sizing/`** — Resource right-sizing monitor. `Monitor` goroutine periodically queries Prometheus for per-service CPU/memory usage, compares against configured limits/reservations, and produces `[]ServiceSizing` recommendations. Nil-safe (disabled when Prometheus unconfigured). Categories: over-provisioned, approaching-limit, at-limit, no-limits, no-reservations. Configurable thresholds via `[sizing]` TOML section.
```

Add the endpoint to the router description:
```markdown
`GET /services/sizing` returns per-service sizing recommendations.
```

- [ ] **Step 3: Update docs/configuration.md**

Add a sizing configuration section with the TOML example and env var descriptions.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/configuration.md
git commit -m "docs: add sizing monitor configuration and architecture"
```

---

### Task 12: Integration Test and Final Verification

**Files:**
- Run existing tests
- Manual verification checklist

- [ ] **Step 1: Run full backend test suite**

Run: `go test ./...`
Expected: all PASS

- [ ] **Step 2: Run frontend type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Run frontend lint**

Run: `cd frontend && npm run lint`
Expected: no errors

- [ ] **Step 4: Run full build**

Run: `cd frontend && npm run build && cd .. && go build -o cetacean .`
Expected: builds successfully

- [ ] **Step 5: Run make check**

Run: `make check`
Expected: all checks pass

- [ ] **Step 6: Commit any fixups**

If any lint or formatting issues were found, fix and commit them.
