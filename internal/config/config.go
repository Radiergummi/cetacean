package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	DockerHost    string
	PrometheusURL string
	ListenAddr    string
	LogLevel      string // "debug", "info", "warn", "error"
	LogFormat     string // "json", "text"
}

func Load() (*Config, error) {
	cfg := &Config{
		DockerHost:    envOr("CETACEAN_DOCKER_HOST", "unix:///var/run/docker.sock"),
		PrometheusURL: os.Getenv("CETACEAN_PROMETHEUS_URL"),
		ListenAddr:    envOr("CETACEAN_LISTEN_ADDR", ":9000"),
		LogLevel:      envOr("CETACEAN_LOG_LEVEL", "info"),
		LogFormat:     envOr("CETACEAN_LOG_FORMAT", "json"),
	}

	if cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("CETACEAN_PROMETHEUS_URL is required")
	}

	return cfg, nil
}

func (c *Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
