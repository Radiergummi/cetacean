package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("CETACEAN_DOCKER_HOST")
	os.Unsetenv("CETACEAN_PROMETHEUS_URL")
	os.Unsetenv("CETACEAN_LISTEN_ADDR")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when CETACEAN_PROMETHEUS_URL is not set")
	}
}

func TestLoad_WithRequiredEnv(t *testing.T) {
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prometheus:9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("expected default docker host, got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prometheus:9090" {
		t.Errorf("expected prometheus URL, got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":9000" {
		t.Errorf("expected default listen addr, got %s", cfg.ListenAddr)
	}
}

func TestLoad_AllEnvVars(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "tcp://remote:2375")
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prom:9090")
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")
	t.Setenv("CETACEAN_LOG_LEVEL", "debug")
	t.Setenv("CETACEAN_LOG_FORMAT", "text")
	t.Setenv("CETACEAN_DATA_DIR", "/tmp/data")
	t.Setenv("CETACEAN_SNAPSHOT", "false")
	t.Setenv("CETACEAN_NOTIFICATIONS_FILE", "/tmp/rules.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "tcp://remote:2375" {
		t.Errorf("got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prom:9090" {
		t.Errorf("got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("got %s", cfg.ListenAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel=%s, want debug", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat=%s, want text", cfg.LogFormat)
	}
	if cfg.DataDir != "/tmp/data" {
		t.Errorf("DataDir=%s, want /tmp/data", cfg.DataDir)
	}
	if cfg.Snapshot != false {
		t.Errorf("Snapshot=%v, want false", cfg.Snapshot)
	}
	if cfg.NotificationsFile != "/tmp/rules.json" {
		t.Errorf("NotificationsFile=%s, want /tmp/rules.json", cfg.NotificationsFile)
	}
}

func TestSlogLevel(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"debug", "DEBUG"},
		{"warn", "WARN"},
		{"error", "ERROR"},
		{"info", "INFO"},
		{"", "INFO"},        // default
		{"WARN", "WARN"},    // case insensitive
		{"unknown", "INFO"}, // fallback
	}
	for _, tt := range tests {
		cfg := &Config{LogLevel: tt.level}
		got := cfg.SlogLevel().String()
		if got != tt.want {
			t.Errorf("SlogLevel(%q)=%s, want %s", tt.level, got, tt.want)
		}
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		value    string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"TRUE", false, true},
		{"false", true, false},
		{"0", true, false},
		{"FALSE", true, false},
		{"", true, true},        // empty → fallback
		{"", false, false},      // empty → fallback
		{"maybe", true, true},   // unknown → fallback
		{"maybe", false, false}, // unknown → fallback
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			key := "TEST_ENVBOOL_" + tt.value
			if tt.value != "" {
				t.Setenv(key, tt.value)
			}
			got := envBool(key, tt.fallback)
			if got != tt.want {
				t.Errorf("envBool(%q, %v)=%v, want %v", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}
