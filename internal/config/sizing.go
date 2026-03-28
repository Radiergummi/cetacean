package config

import "time"

// SizingConfig controls the resource right-sizing monitor.
type SizingConfig struct {
	Enabled            bool
	Interval           time.Duration
	HeadroomMultiplier float64
	OverProvisioned    float64       // below this fraction of reservation = over-provisioned
	ApproachingLimit   float64       // above this fraction of limit = approaching
	AtLimit            float64       // above this fraction of limit = at limit
	Lookback           time.Duration // p95 lookback window for over-provisioned checks
}

// LoadSizing resolves sizing configuration from file config, env vars, and defaults.
// Accepts *fileConfig (unexported) — callers in main.go pass the pointer through without naming the type.
func LoadSizing(fc *fileConfig) (*SizingConfig, error) {
	var (
		fEnabled  *bool
		fInterval *string
		fHeadroom *float64
		fOverProv *float64
		fApproach *float64
		fAtLimit  *float64
		fLookback *string
	)

	if fc != nil && fc.Sizing != nil {
		fEnabled = fc.Sizing.Enabled
		fInterval = fc.Sizing.Interval
		fHeadroom = fc.Sizing.Headroom
		if fc.Sizing.Thresholds != nil {
			fOverProv = fc.Sizing.Thresholds.OverProvisioned
			fApproach = fc.Sizing.Thresholds.ApproachingLimit
			fAtLimit = fc.Sizing.Thresholds.AtLimit
			fLookback = fc.Sizing.Thresholds.Lookback
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

	lookback, err := resolveDuration(nil, "CETACEAN_SIZING_LOOKBACK", fLookback, 168*time.Hour)
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
		Lookback:           lookback,
	}, nil
}
