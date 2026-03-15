package config

import (
	"testing"
)

func TestParseFlags_NoArgs(t *testing.T) {
	flags, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "" {
		t.Errorf("Config=%s, want empty", flags.Config)
	}
	if flags.Listen != nil {
		t.Error("Listen should be nil when not set")
	}
	if flags.Version {
		t.Error("Version should be false")
	}
}

func TestParseFlags_AllFlags(t *testing.T) {
	args := []string{
		"-config", "cetacean.toml",
		"-listen", ":8080",
		"-docker-host", "tcp://remote:2375",
		"-prometheus-url", "http://prom:9090",
		"-log-level", "debug",
		"-log-format", "text",
		"-auth-mode", "oidc",
		"-pprof",
		"-version",
	}

	flags, err := ParseFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "cetacean.toml" {
		t.Errorf("Config=%s", flags.Config)
	}
	if flags.Listen == nil || *flags.Listen != ":8080" {
		t.Error("Listen not set correctly")
	}
	if flags.DockerHost == nil || *flags.DockerHost != "tcp://remote:2375" {
		t.Error("DockerHost not set correctly")
	}
	if flags.PrometheusURL == nil || *flags.PrometheusURL != "http://prom:9090" {
		t.Error("PrometheusURL not set correctly")
	}
	if flags.LogLevel == nil || *flags.LogLevel != "debug" {
		t.Error("LogLevel not set correctly")
	}
	if flags.LogFormat == nil || *flags.LogFormat != "text" {
		t.Error("LogFormat not set correctly")
	}
	if flags.AuthMode == nil || *flags.AuthMode != "oidc" {
		t.Error("AuthMode not set correctly")
	}
	if flags.Pprof == nil || *flags.Pprof != true {
		t.Error("Pprof not set correctly")
	}
	if !flags.Version {
		t.Error("Version should be true")
	}
}

func TestParseFlags_UnsetFlagsAreNil(t *testing.T) {
	flags, err := ParseFlags([]string{"-listen", ":8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Listen == nil {
		t.Error("Listen should be set")
	}
	if flags.DockerHost != nil {
		t.Error("DockerHost should be nil")
	}
	if flags.PrometheusURL != nil {
		t.Error("PrometheusURL should be nil")
	}
	if flags.Pprof != nil {
		t.Error("Pprof should be nil")
	}
}

func TestParseFlags_ConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_CONFIG", "/etc/cetacean.toml")
	flags, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "/etc/cetacean.toml" {
		t.Errorf("Config=%s, want /etc/cetacean.toml", flags.Config)
	}
}

func TestParseFlags_ConfigFlagOverridesEnv(t *testing.T) {
	t.Setenv("CETACEAN_CONFIG", "/etc/cetacean.toml")
	flags, err := ParseFlags([]string{"-config", "/opt/cetacean.toml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "/opt/cetacean.toml" {
		t.Errorf("Config=%s, want /opt/cetacean.toml", flags.Config)
	}
}

func TestParseFlags_InvalidFlag(t *testing.T) {
	_, err := ParseFlags([]string{"-nonexistent"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}
