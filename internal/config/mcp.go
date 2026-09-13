package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// MCPConfig holds configuration for the MCP (Model Context Protocol) server.
type MCPConfig struct {
	// Enabled controls whether the MCP server is started.
	Enabled bool

	// OperationsLevel overrides the global operations level for MCP clients.
	// OpsInherit (-1) means fall back to the global CETACEAN_OPERATIONS_LEVEL.
	OperationsLevel OperationsLevel

	// MaxConcurrentTasks caps how many task-augmented tool calls may run at
	// once. Each holds a goroutine polling the cache until the cluster
	// converges, so the cap bounds what a client can pin down by firing off
	// mutations it never collects.
	MaxConcurrentTasks int

	// TaskTTL is the retention mcp-go is given for a task whose client did not
	// ask for one. mcp-go schedules cleanup only for a task carrying a TTL, so
	// without this a client that omits the field pins its result for the life
	// of the process. Zero disables the fill-in.
	//
	// The clock starts when the task is created, not when it finishes, so this
	// must comfortably exceed the convergence timeout or a result expires
	// before the client that asked for it can collect it.
	TaskTTL time.Duration

	// MaxTaskTTL caps the retention a client may ask for. Without it the fill-
	// in above is not a bound at all: a client naming a large TTL pins a result
	// for exactly as long as it likes. A request above this is served with the
	// ceiling, not refused. Zero disables the cap.
	MaxTaskTTL time.Duration

	// AuthBypass lists upstream Cetacean auth modes (e.g. "cert") whose
	// authenticated identity is accepted at /mcp without a bearer token: the
	// MCP server derives identity from the upstream provider instead of
	// validating a JWT. Modes that issue redirects (e.g. "oidc") are unsafe to
	// list. It does not remove the need for an authorization server — the
	// bypass runs inside the bearer middleware an absent one never installs.
	AuthBypass []string
}

// DefaultMCPConfig returns an MCPConfig populated with sensible defaults.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Enabled:            false,
		OperationsLevel:    OpsInherit,
		MaxConcurrentTasks: 32,
		// 15m covers the 5m convergence timeout with a collection window well
		// clear of it, since the TTL runs from creation.
		TaskTTL:    15 * time.Minute,
		MaxTaskTTL: time.Hour,
		AuthBypass: nil,
	}
}

// EffectiveOperationsLevel returns the operations level to apply to MCP clients.
// When OperationsLevel is OpsInherit, the supplied global level is returned.
func (m MCPConfig) EffectiveOperationsLevel(global OperationsLevel) OperationsLevel {
	if m.OperationsLevel == OpsInherit {
		return global
	}

	return m.OperationsLevel
}

// loadMCP builds an MCPConfig from a file section and env vars, applying the
// standard resolve helpers. It is called from Load() and is also directly
// testable.
func loadMCP(fm *fileMCP) (MCPConfig, error) {
	def := DefaultMCPConfig()

	// Extract file-level pointers (safely handle nil sub-struct).
	var (
		fEnabled    *bool
		fOpsLevel   *int
		fTaskTTL    *string
		fMaxTaskTTL *string
		fMaxTasks   *int
		fAuthBypass []string
	)
	if fm != nil {
		fEnabled = fm.Enabled
		fOpsLevel = fm.OperationsLevel
		fMaxTasks = fm.MaxConcurrentTasks
		fTaskTTL = fm.TaskTTL
		fMaxTaskTTL = fm.MaxTaskTTL
		fAuthBypass = fm.AuthBypass
	}

	taskTTL, err := resolveNonNegativeDuration(
		nil,
		"CETACEAN_MCP_TASK_TTL",
		fTaskTTL,
		def.TaskTTL,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	maxTaskTTL, err := resolveNonNegativeDuration(
		nil,
		"CETACEAN_MCP_MAX_TASK_TTL",
		fMaxTaskTTL,
		def.MaxTaskTTL,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	maxConcurrentTasks, err := resolveInt(
		nil,
		"CETACEAN_MCP_MAX_CONCURRENT_TASKS",
		fMaxTasks,
		def.MaxConcurrentTasks,
		1,
		1<<20,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	// OpsInherit (-1) is a sentinel that cannot be expressed in the [0,3] range
	// accepted by resolveInt, so we handle it manually.
	opsLevel, err := resolveMCPOpsLevel(fOpsLevel)
	if err != nil {
		return MCPConfig{}, err
	}

	return MCPConfig{
		Enabled:            resolveBool(nil, "CETACEAN_MCP", fEnabled, def.Enabled),
		OperationsLevel:    opsLevel,
		MaxConcurrentTasks: maxConcurrentTasks,
		TaskTTL:            taskTTL,
		MaxTaskTTL:         maxTaskTTL,
		AuthBypass:         resolveStringSlice(nil, "CETACEAN_MCP_AUTH_BYPASS", fAuthBypass),
	}, nil
}

// resolveMCPOpsLevel reads CETACEAN_MCP_OPERATIONS_LEVEL and the file value,
// returning OpsInherit when neither is set. Unlike the global ops level,
// OpsInherit (-1) is a valid result here.
func resolveMCPOpsLevel(file *int) (OperationsLevel, error) {
	const envKey = "CETACEAN_MCP_OPERATIONS_LEVEL"
	const min, max = int(OpsReadOnly), int(OpsImpactful)

	if raw := os.Getenv(envKey); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return OpsInherit, fmt.Errorf("invalid integer from %s %q: %w", envKey, raw, err)
		}

		result, err := checkIntRange(v, min, max, envKey)
		if err != nil {
			return OpsInherit, err
		}

		return OperationsLevel(result), nil
	}

	if file != nil {
		result, err := checkIntRange(*file, min, max, "config file")
		if err != nil {
			return OpsInherit, err
		}

		return OperationsLevel(result), nil
	}

	return OpsInherit, nil
}
