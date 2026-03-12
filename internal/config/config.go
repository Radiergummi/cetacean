package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	DockerHost    string
	PrometheusURL string
	ListenAddr    string
	LogLevel      string // "debug", "info", "warn", "error"
	LogFormat     string // "json", "text"
	DataDir       string // CETACEAN_DATA_DIR, default "./data"
	Snapshot         bool          // CETACEAN_SNAPSHOT, default true
	SSEBatchInterval time.Duration // CETACEAN_SSE_BATCH_INTERVAL, default 100ms
	Pprof             bool          // CETACEAN_PPROF, default false
}

func Load() (*Config, error) {
	batchInterval, err := envDuration("CETACEAN_SSE_BATCH_INTERVAL", 100*time.Millisecond)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DockerHost:        envOr("CETACEAN_DOCKER_HOST", "unix:///var/run/docker.sock"),
		PrometheusURL:     os.Getenv("CETACEAN_PROMETHEUS_URL"),
		ListenAddr:        envOr("CETACEAN_LISTEN_ADDR", ":9000"),
		LogLevel:          envOr("CETACEAN_LOG_LEVEL", "info"),
		LogFormat:         envOr("CETACEAN_LOG_FORMAT", "json"),
		DataDir:           envOr("CETACEAN_DATA_DIR", "./data"),
		Snapshot:          envBool("CETACEAN_SNAPSHOT", true),
		SSEBatchInterval: batchInterval,
		Pprof:             envBool("CETACEAN_PPROF", false),
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

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", key, v)
	}
	return d, nil
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	switch strings.ToLower(v) {
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		return fallback
	}
}
