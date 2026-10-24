package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// OperationsLevel controls which write operations are available.
type OperationsLevel int

const (
	// OpsInherit is a sentinel value (-1) used only in MCPConfig.OperationsLevel
	// to indicate that the MCP server should inherit the global operations level.
	// It is not a real tier and must not be used in permission checks directly;
	// call MCPConfig.EffectiveOperationsLevel to resolve the actual level.
	OpsInherit OperationsLevel = -1

	// OpsReadOnly disables all write operations.
	OpsReadOnly OperationsLevel = 0

	// OpsOperational allows reactive service actions: scale, image update,
	// rollback, restart.
	OpsOperational OperationsLevel = 1

	// OpsConfiguration allows service definition changes: env, labels,
	// resources, healthcheck, placement, ports, update-policy, rollback-policy,
	// log-driver.
	OpsConfiguration OperationsLevel = 2

	// OpsImpactful allows all operations including node availability/role,
	// service mode/endpoint mode changes, task removal, and service deletion.
	OpsImpactful OperationsLevel = 3
)

type Config struct {
	DockerHost string
	DockerTLS  DockerTLSConfig // [docker.tls] / CETACEAN_DOCKER_TLS_*

	// DockerTaskPollInterval is how often the watcher re-lists every task;
	// zero disables it. CETACEAN_DOCKER_TASK_POLL_INTERVAL, default 10s.
	DockerTaskPollInterval time.Duration

	PrometheusURL    string
	ListenAddr       string
	BasePath         string           // CETACEAN_BASE_PATH, default ""
	PublicURL        string           // CETACEAN_PUBLIC_URL, external origin, default ""
	LogLevel         string           // "debug", "info", "warn", "error"
	LogFormat        string           // "json", "text"
	DataDir          string           // CETACEAN_DATA_DIR, default "./data"
	Snapshot         bool             // CETACEAN_SNAPSHOT, default true
	SSEBatchInterval time.Duration    // CETACEAN_SSE_BATCH_INTERVAL, default 100ms
	Pprof            bool             // CETACEAN_PPROF, default false
	SelfMetrics      bool             // CETACEAN_SELF_METRICS, default true
	Recommendations  bool             // CETACEAN_RECOMMENDATIONS, default true
	VersionCheck     bool             // CETACEAN_VERSION_CHECK, default true
	OperationsLevel  OperationsLevel  // CETACEAN_OPERATIONS_LEVEL
	CORSOrigins      []string         // CETACEAN_CORS_ORIGINS, default empty (disabled)
	TrustedProxies   []netip.Prefix   // CETACEAN_TRUSTED_PROXIES
	ForwardedHeaders ForwardedHeaders // CETACEAN_FORWARDED_HEADERS
	MCP              MCPConfig        // [mcp] section / CETACEAN_MCP_* env vars
	OAuth            OAuthConfig      // [oauth] section / CETACEAN_OAUTH_* env vars

	// OTelEndpoint is the OTLP/HTTP collector to export traces to.
	// CETACEAN_OTEL_ENDPOINT / [tracing].endpoint; empty disables tracing.
	OTelEndpoint string
}

// LoadFlagged loads the file flags.Config names, or the one DiscoverConfigFile
// finds, and resolves the configuration from it. It returns the file and its
// path too, for the section loaders that read the file on their own.
func LoadFlagged(flags *Flags) (cfg *Config, fc *fileConfig, path string, err error) {
	path = flags.Config
	if path == "" {
		path = DiscoverConfigFile()
	}

	fc, err = LoadFile(path)
	if err != nil {
		return nil, nil, "", err
	}

	cfg, err = Load(fc, flags)
	if err != nil {
		return nil, nil, "", err
	}

	return cfg, fc, path, nil
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
		fListen           *string
		fPprof            *bool
		fSelfMetrics      *bool
		fRecommendations  *bool
		fVersionCheck     *bool
		fSSEBatch         *string
		fDockerHost       *string
		fDocker           *fileDocker
		fPromURL          *string
		fLogLevel         *string
		fLogFormat        *string
		fDataDir          *string
		fSnapshot         *bool
		fOpsLevel         *int
		fBasePath         *string
		fPublicURL        *string
		fCORSOrigins      []string
		fTrustedProxies   *string
		fForwardedHeaders *string
		fOTelEndpoint     *string
	)
	if fc != nil {
		if fc.Server != nil {
			fListen = fc.Server.ListenAddr
			fPprof = fc.Server.Pprof
			fSelfMetrics = fc.Server.SelfMetrics
			fRecommendations = fc.Server.Recommendations
			fVersionCheck = fc.Server.VersionCheck
			fOpsLevel = fc.Server.OperationsLevel
			fBasePath = fc.Server.BasePath
			fPublicURL = fc.Server.PublicURL
			fTrustedProxies = fc.Server.TrustedProxies
			fForwardedHeaders = fc.Server.ForwardedHeaders
			if fc.Server.SSE != nil {
				fSSEBatch = fc.Server.SSE.BatchInterval
			}
			if fc.Server.CORS != nil {
				fCORSOrigins = fc.Server.CORS.Origins
			}
		}
		if fc.Docker != nil {
			fDocker = fc.Docker
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
		if fc.Tracing != nil {
			fOTelEndpoint = fc.Tracing.Endpoint
		}
	}

	batchInterval, err := resolveDuration(
		flags.SSEBatchInterval,
		"CETACEAN_SSE_BATCH_INTERVAL",
		fSSEBatch,
		100*time.Millisecond,
		false,
	)
	if err != nil {
		return nil, err
	}

	var fTaskPoll *string
	if fDocker != nil {
		fTaskPoll = fDocker.TaskPollInterval
	}

	taskPollInterval, err := resolveDuration(
		flags.DockerTaskPoll,
		"CETACEAN_DOCKER_TASK_POLL_INTERVAL",
		fTaskPoll,
		10*time.Second,
		true,
	)
	if err != nil {
		return nil, err
	}

	// Each poll has the daemon decode and send the whole task list.
	if taskPollInterval > 0 && taskPollInterval < time.Second {
		return nil, fmt.Errorf(
			"docker.task_poll_interval %s: want at least 1s, or 0 to turn it off",
			taskPollInterval,
		)
	}

	opsLevel, err := resolveInt(
		flags.OperationsLevel,
		"CETACEAN_OPERATIONS_LEVEL",
		fOpsLevel,
		int(OpsReadOnly),
		int(OpsReadOnly),
		int(OpsImpactful),
	)
	if err != nil {
		return nil, err
	}

	snapshot, err := resolveBool(flags.Snapshot, "CETACEAN_SNAPSHOT", fSnapshot, true)
	if err != nil {
		return nil, err
	}

	pprof, err := resolveBool(flags.Pprof, "CETACEAN_PPROF", fPprof, false)
	if err != nil {
		return nil, err
	}

	selfMetrics, err := resolveBool(flags.SelfMetrics, "CETACEAN_SELF_METRICS", fSelfMetrics, true)
	if err != nil {
		return nil, err
	}

	recommendations, err := resolveBool(
		flags.Recommendations,
		"CETACEAN_RECOMMENDATIONS",
		fRecommendations,
		true,
	)
	if err != nil {
		return nil, err
	}

	versionCheck, err := resolveBool(
		flags.VersionCheck,
		"CETACEAN_VERSION_CHECK",
		fVersionCheck,
		true,
	)
	if err != nil {
		return nil, err
	}

	logLevel := strings.ToLower(resolve(flags.LogLevel, "CETACEAN_LOG_LEVEL", fLogLevel, "info"))
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, logLevel) {
		return nil, fmt.Errorf("logging.level %q: want debug, info, warn or error", logLevel)
	}

	logFormat := strings.ToLower(
		resolve(flags.LogFormat, "CETACEAN_LOG_FORMAT", fLogFormat, "json"),
	)
	if logFormat != "json" && logFormat != "text" {
		return nil, fmt.Errorf("logging.format %q: want json or text", logFormat)
	}

	cfg := &Config{
		DockerHost: resolve(
			flags.DockerHost,
			"CETACEAN_DOCKER_HOST",
			fDockerHost,
			"unix:///var/run/docker.sock",
		),
		PrometheusURL: resolve(flags.PrometheusURL, "CETACEAN_PROMETHEUS_URL", fPromURL, ""),
		OTelEndpoint:  resolve(nil, "CETACEAN_OTEL_ENDPOINT", fOTelEndpoint, ""),
		ListenAddr:    resolve(flags.Listen, "CETACEAN_LISTEN_ADDR", fListen, ":9000"),
		BasePath: NormalizeBasePath(
			resolve(flags.BasePath, "CETACEAN_BASE_PATH", fBasePath, ""),
		),
		PublicURL: strings.TrimRight(
			resolve(flags.PublicURL, "CETACEAN_PUBLIC_URL", fPublicURL, ""),
			"/",
		),
		LogLevel:         logLevel,
		LogFormat:        logFormat,
		DataDir:          resolve(flags.DataDir, "CETACEAN_DATA_DIR", fDataDir, "./data"),
		Snapshot:         snapshot,
		SSEBatchInterval: batchInterval,

		DockerTaskPollInterval: taskPollInterval,
		Pprof:                  pprof,
		SelfMetrics:            selfMetrics,
		Recommendations:        recommendations,
		VersionCheck:           versionCheck,
		OperationsLevel:        OperationsLevel(opsLevel),
		CORSOrigins: resolveStringSlice(
			flags.CORSOrigins,
			"CETACEAN_CORS_ORIGINS",
			fCORSOrigins,
		),
	}

	cfg.DockerTLS = loadDockerTLS(flags, fDocker)
	if err := validateDocker(cfg.DockerHost, cfg.DockerTLS); err != nil {
		return nil, err
	}

	if err := ValidateBasePath(cfg.BasePath); err != nil {
		return nil, err
	}

	if err := ValidatePublicURL(cfg.PublicURL); err != nil {
		return nil, err
	}

	var fMCP *fileMCP
	if fc != nil {
		fMCP = fc.MCP
	}
	mcpCfg, err := loadMCP(fMCP)
	if err != nil {
		return nil, err
	}
	cfg.MCP = mcpCfg

	var fOAuth *fileOAuth
	if fc != nil {
		fOAuth = fc.OAuth
	}
	oauthCfg, err := loadOAuth(fOAuth)
	if err != nil {
		return nil, err
	}
	cfg.OAuth = oauthCfg

	trustedProxiesRaw := resolve(
		flags.TrustedProxies,
		"CETACEAN_TRUSTED_PROXIES",
		fTrustedProxies,
		"",
	)
	if trustedProxiesRaw != "" {
		tp, err := parseTrustedProxies(trustedProxiesRaw)
		if err != nil {
			return nil, err
		}
		cfg.TrustedProxies = tp
	}

	cfg.ForwardedHeaders, err = parseForwardedHeaders(resolve(
		flags.ForwardedHeaders,
		"CETACEAN_FORWARDED_HEADERS",
		fForwardedHeaders,
		string(XForwardedHeaders),
	))
	if err != nil {
		return nil, err
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
