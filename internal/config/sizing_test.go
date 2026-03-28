package config

import (
	"testing"
	"time"
)

func TestLoadSizing_Defaults(t *testing.T) {
	for _, key := range []string{
		"CETACEAN_SIZING_ENABLED", "CETACEAN_SIZING_INTERVAL",
		"CETACEAN_SIZING_HEADROOM_MULTIPLIER", "CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED",
		"CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "CETACEAN_SIZING_THRESHOLD_AT_LIMIT",
		"CETACEAN_SIZING_LOOKBACK",
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
	if cfg.Lookback != 168*time.Hour {
		t.Errorf("lookback: got %v, want 168h", cfg.Lookback)
	}
}

func TestLoadSizing_EnvOverrides(t *testing.T) {
	t.Setenv("CETACEAN_SIZING_ENABLED", "false")
	t.Setenv("CETACEAN_SIZING_INTERVAL", "30s")
	t.Setenv("CETACEAN_SIZING_HEADROOM_MULTIPLIER", "1.5")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED", "0.10")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "0.70")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_AT_LIMIT", "0.90")
	t.Setenv("CETACEAN_SIZING_LOOKBACK", "24h")

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
	if cfg.Lookback != 24*time.Hour {
		t.Errorf("lookback: got %v, want 24h", cfg.Lookback)
	}
}

func TestLoadSizing_FileConfig(t *testing.T) {
	for _, key := range []string{
		"CETACEAN_SIZING_ENABLED", "CETACEAN_SIZING_INTERVAL",
		"CETACEAN_SIZING_HEADROOM_MULTIPLIER", "CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED",
		"CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "CETACEAN_SIZING_THRESHOLD_AT_LIMIT",
		"CETACEAN_SIZING_LOOKBACK",
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
	if cfg.ApproachingLimit != 0.80 {
		t.Errorf("approaching-limit should be default 0.80, got %f", cfg.ApproachingLimit)
	}
}

func TestLoadSizing_InvalidThreshold(t *testing.T) {
	t.Setenv("CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED", "1.5")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT", "")
	t.Setenv("CETACEAN_SIZING_THRESHOLD_AT_LIMIT", "")
	t.Setenv("CETACEAN_SIZING_ENABLED", "")
	t.Setenv("CETACEAN_SIZING_INTERVAL", "")
	t.Setenv("CETACEAN_SIZING_HEADROOM_MULTIPLIER", "")
	t.Setenv("CETACEAN_SIZING_LOOKBACK", "")

	_, err := LoadSizing(nil)
	if err == nil {
		t.Fatal("expected error for threshold > 1.0")
	}
}
