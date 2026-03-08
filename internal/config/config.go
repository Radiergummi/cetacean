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
	Snapshot          bool          // CETACEAN_SNAPSHOT, default true
	NotificationsFile string        // CETACEAN_NOTIFICATIONS_FILE, optional
	SSEBatchInterval  time.Duration // CETACEAN_SSE_BATCH_INTERVAL, default 100ms
}

func Load() (*Config, error) {
	cfg := &Config{
		DockerHost:    envOr("CETACEAN_DOCKER_HOST", "unix:///var/run/docker.sock"),
		PrometheusURL: os.Getenv("CETACEAN_PROMETHEUS_URL"),
		ListenAddr:    envOr("CETACEAN_LISTEN_ADDR", ":9000"),
		LogLevel:      envOr("CETACEAN_LOG_LEVEL", "info"),
		LogFormat:     envOr("CETACEAN_LOG_FORMAT", "json"),
		DataDir:       envOr("CETACEAN_DATA_DIR", "./data"),
		Snapshot:          envBool("CETACEAN_SNAPSHOT", true),
		NotificationsFile: os.Getenv("CETACEAN_NOTIFICATIONS_FILE"),
		SSEBatchInterval:  envDuration("CETACEAN_SSE_BATCH_INTERVAL", 100*time.Millisecond),
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

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil && d > 0 {
			return d
		}
	}
	return fallback
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
