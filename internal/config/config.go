package config

import (
	"log/slog"
	"strings"
	"time"
)

// OperationsLevel controls which write operations are available.
type OperationsLevel int

const (
	// OpsReadOnly disables all write operations.
	OpsReadOnly OperationsLevel = 0

	// OpsOperational allows routine service actions: scale, image update,
	// rollback, restart, and service env/labels/resources/healthcheck/placement/
	// ports/update-policy/rollback-policy/log-driver patches.
	OpsOperational OperationsLevel = 1

	// OpsImpactful allows all operations including node availability/labels,
	// service mode/endpoint mode changes, and task removal.
	OpsImpactful OperationsLevel = 2
)

type Config struct {
	DockerHost       string
	PrometheusURL    string
	ListenAddr       string
	LogLevel         string          // "debug", "info", "warn", "error"
	LogFormat        string          // "json", "text"
	DataDir          string          // CETACEAN_DATA_DIR, default "./data"
	Snapshot         bool            // CETACEAN_SNAPSHOT, default true
	SSEBatchInterval time.Duration   // CETACEAN_SSE_BATCH_INTERVAL, default 100ms
	Pprof            bool            // CETACEAN_PPROF, default false
	OperationsLevel  OperationsLevel // CETACEAN_OPERATIONS_LEVEL
}

// Load merges configuration from flags, environment variables, a TOML
// config file, and hardcoded defaults (in that precedence order).
// Pass nil for fc and/or flags to skip those layers.
func Load(fc *fileConfig, flags *Flags) (*Config, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// Extract file-level pointers (safely handle nil sub-structs).
	var (
		fListen     *string
		fPprof      *bool
		fSSEBatch   *string
		fDockerHost *string
		fPromURL    *string
		fLogLevel   *string
		fLogFormat  *string
		fDataDir    *string
		fSnapshot   *bool
		fOpsLevel   *int
	)
	if fc != nil {
		if fc.Server != nil {
			fListen = fc.Server.ListenAddr
			fPprof = fc.Server.Pprof
			fOpsLevel = fc.Server.OperationsLevel
			if fc.Server.SSE != nil {
				fSSEBatch = fc.Server.SSE.BatchInterval
			}
		}
		if fc.Docker != nil {
			fDockerHost = fc.Docker.Host
		}
		if fc.Prom != nil {
			fPromURL = fc.Prom.URL
		}
		if fc.Logging != nil {
			fLogLevel = fc.Logging.Level
			fLogFormat = fc.Logging.Format
		}
		if fc.Storage != nil {
			fDataDir = fc.Storage.DataDir
			fSnapshot = fc.Storage.Snapshot
		}
	}

	batchInterval, err := resolveDuration(nil, "CETACEAN_SSE_BATCH_INTERVAL", fSSEBatch, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}

	opsLevel, err := resolveInt(nil, "CETACEAN_OPERATIONS_LEVEL", fOpsLevel, int(OpsOperational), int(OpsReadOnly), int(OpsImpactful))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DockerHost:       resolve(flags.DockerHost, "CETACEAN_DOCKER_HOST", fDockerHost, "unix:///var/run/docker.sock"),
		PrometheusURL:    resolve(flags.PrometheusURL, "CETACEAN_PROMETHEUS_URL", fPromURL, ""),
		ListenAddr:       resolve(flags.Listen, "CETACEAN_LISTEN_ADDR", fListen, ":9000"),
		LogLevel:         resolve(flags.LogLevel, "CETACEAN_LOG_LEVEL", fLogLevel, "info"),
		LogFormat:        resolve(flags.LogFormat, "CETACEAN_LOG_FORMAT", fLogFormat, "json"),
		DataDir:          resolve(nil, "CETACEAN_DATA_DIR", fDataDir, "./data"),
		Snapshot:         resolveBool(nil, "CETACEAN_SNAPSHOT", fSnapshot, true),
		SSEBatchInterval: batchInterval,
		Pprof:            resolveBool(flags.Pprof, "CETACEAN_PPROF", fPprof, false),
		OperationsLevel:  OperationsLevel(opsLevel),
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
