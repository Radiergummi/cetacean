package config

import (
	"fmt"
	"os"
)

type Config struct {
	DockerHost    string
	PrometheusURL string
	ListenAddr    string
}

func Load() (*Config, error) {
	cfg := &Config{
		DockerHost:    envOr("CETACEAN_DOCKER_HOST", "unix:///var/run/docker.sock"),
		PrometheusURL: os.Getenv("CETACEAN_PROMETHEUS_URL"),
		ListenAddr:    envOr("CETACEAN_LISTEN_ADDR", ":9000"),
	}

	if cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("CETACEAN_PROMETHEUS_URL is required")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
