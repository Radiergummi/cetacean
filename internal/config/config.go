package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"
)

// OperationsLevel controls which write operations are available.
type OperationsLevel int

const (
	// OpsReadOnly disables all write operations.
	OpsReadOnly OperationsLevel = 0

	// OpsOperational allows reactive service actions: scale, image update,
	// rollback, restart.
	OpsOperational OperationsLevel = 1

	// OpsConfiguration allows service definition changes: env, labels,
	// resources, healthcheck, placement, ports, update-policy, rollback-policy,
	// log-driver.
	OpsConfiguration OperationsLevel = 2

	// OpsImpactful allows all operations including node availability/labels,
	// service mode/endpoint mode changes, task removal, and service deletion.
	OpsImpactful OperationsLevel = 3
)

type Config struct {
	DockerHost       string
	PrometheusURL    string
	ListenAddr       string
	BasePath         string          // CETACEAN_BASE_PATH, default ""
	LogLevel         string          // "debug", "info", "warn", "error"
	LogFormat        string          // "json", "text"
	DataDir          string          // CETACEAN_DATA_DIR, default "./data"
	Snapshot         bool            // CETACEAN_SNAPSHOT, default true
	SSEBatchInterval time.Duration   // CETACEAN_SSE_BATCH_INTERVAL, default 100ms
	Pprof            bool            // CETACEAN_PPROF, default false
	SelfMetrics      bool            // CETACEAN_SELF_METRICS, default true
	Recommendations  bool            // CETACEAN_RECOMMENDATIONS, default true
	OperationsLevel  OperationsLevel // CETACEAN_OPERATIONS_LEVEL
	CORSOrigins      []string        // CETACEAN_CORS_ORIGINS, default empty (disabled)
	TrustedProxies   []netip.Prefix  // CETACEAN_TRUSTED_PROXIES
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
		fListen          *string
		fPprof           *bool
		fSelfMetrics     *bool
		fRecommendations *bool
		fSSEBatch        *string
		fDockerHost      *string
		fPromURL         *string
		fLogLevel        *string
		fLogFormat       *string
		fDataDir         *string
		fSnapshot        *bool
		fOpsLevel        *int
		fBasePath        *string
		fCORSOrigins     []string
		fTrustedProxies  *string
	)
	if fc != nil {
		if fc.Server != nil {
			fListen = fc.Server.ListenAddr
			fPprof = fc.Server.Pprof
			fSelfMetrics = fc.Server.SelfMetrics
			fRecommendations = fc.Server.Recommendations
			fOpsLevel = fc.Server.OperationsLevel
			fBasePath = fc.Server.BasePath
			fTrustedProxies = fc.Server.TrustedProxies
			if fc.Server.SSE != nil {
				fSSEBatch = fc.Server.SSE.BatchInterval
			}
			if fc.Server.CORS != nil {
				fCORSOrigins = fc.Server.CORS.Origins
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

	batchInterval, err := resolveDuration(
		flags.SSEBatchInterval,
		"CETACEAN_SSE_BATCH_INTERVAL",
		fSSEBatch,
		100*time.Millisecond,
	)
	if err != nil {
		return nil, err
	}

	opsLevel, err := resolveInt(
		flags.OperationsLevel,
		"CETACEAN_OPERATIONS_LEVEL",
		fOpsLevel,
		int(OpsOperational),
		int(OpsReadOnly),
		int(OpsImpactful),
	)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DockerHost: resolve(
			flags.DockerHost,
			"CETACEAN_DOCKER_HOST",
			fDockerHost,
			"unix:///var/run/docker.sock",
		),
		PrometheusURL: resolve(flags.PrometheusURL, "CETACEAN_PROMETHEUS_URL", fPromURL, ""),
		ListenAddr:    resolve(flags.Listen, "CETACEAN_LISTEN_ADDR", fListen, ":9000"),
		BasePath: NormalizeBasePath(
			resolve(flags.BasePath, "CETACEAN_BASE_PATH", fBasePath, ""),
		),
		LogLevel:         resolve(flags.LogLevel, "CETACEAN_LOG_LEVEL", fLogLevel, "info"),
		LogFormat:        resolve(flags.LogFormat, "CETACEAN_LOG_FORMAT", fLogFormat, "json"),
		DataDir:          resolve(flags.DataDir, "CETACEAN_DATA_DIR", fDataDir, "./data"),
		Snapshot:         resolveBool(flags.Snapshot, "CETACEAN_SNAPSHOT", fSnapshot, true),
		SSEBatchInterval: batchInterval,
		Pprof:            resolveBool(flags.Pprof, "CETACEAN_PPROF", fPprof, false),
		SelfMetrics: resolveBool(
			flags.SelfMetrics,
			"CETACEAN_SELF_METRICS",
			fSelfMetrics,
			true,
		),
		Recommendations: resolveBool(
			flags.Recommendations,
			"CETACEAN_RECOMMENDATIONS",
			fRecommendations,
			true,
		),
		OperationsLevel: OperationsLevel(opsLevel),
		CORSOrigins: resolveStringSlice(
			flags.CORSOrigins,
			"CETACEAN_CORS_ORIGINS",
			fCORSOrigins,
		),
	}

	if err := ValidateBasePath(cfg.BasePath); err != nil {
		return nil, err
	}

	trustedProxiesRaw := resolve(
		flags.TrustedProxies,
		"CETACEAN_TRUSTED_PROXIES",
		fTrustedProxies,
		"",
	)
	if trustedProxiesRaw != "" {
		tp, err := parseTrustedProxies(trustedProxiesRaw)
		if err != nil {
			return nil, fmt.Errorf("server.trusted_proxies: %w", err)
		}
		cfg.TrustedProxies = tp
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
