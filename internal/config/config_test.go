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
}
