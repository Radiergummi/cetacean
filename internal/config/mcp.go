package config

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
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

	// TaskTTL is the retention mcp-go is given for a task whose client asked
	// for none, which it would otherwise pin for the life of the process; zero
	// disables the fill-in. The clock starts at creation, not completion, so
	// this must exceed the convergence timeout or a result expires uncollected.
	TaskTTL time.Duration

	// MaxTaskTTL caps the retention a client may ask for. Without it the fill-
	// in above is not a bound at all: a client naming a large TTL pins a result
	// for exactly as long as it likes. A request above this is served with the
	// ceiling, not refused. Zero disables the cap.
	MaxTaskTTL time.Duration

	// AuthBypass lists upstream Cetacean auth modes (e.g. "cert") whose
	// authenticated identity is accepted at /mcp without a bearer token: the
	// MCP server derives identity from the upstream provider instead of
	// validating a JWT. Only bypassableAuthModes may appear, and a listed mode
	// authenticates /mcp on its own, so no authorization server is required.
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

// EffectiveOperationsLevel returns the operations level to apply to MCP clients:
// the global one, narrowed by this transport's own. A transport override is a
// ceiling and never a second dial, so one set above the deployment's tier grants
// nothing the deployment refused.
func (m MCPConfig) EffectiveOperationsLevel(global OperationsLevel) OperationsLevel {
	return capLevel(m.OperationsLevel, global)
}

// capLevel narrows global by level, which may be OpsInherit for "no ceiling of
// its own".
func capLevel(level, global OperationsLevel) OperationsLevel {
	if level == OpsInherit {
		return global
	}

	return min(level, global)
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
	opsLevel, err := resolveOpsCeiling("CETACEAN_MCP_OPERATIONS_LEVEL", fOpsLevel)
	if err != nil {
		return MCPConfig{}, err
	}

	authBypass := resolveStringSlice(nil, "CETACEAN_MCP_AUTH_BYPASS", fAuthBypass)
	if err := checkAuthBypass(authBypass); err != nil {
		return MCPConfig{}, err
	}

	return MCPConfig{
		Enabled:            resolveBool(nil, "CETACEAN_MCP", fEnabled, def.Enabled),
		OperationsLevel:    opsLevel,
		MaxConcurrentTasks: maxConcurrentTasks,
		TaskTTL:            taskTTL,
		MaxTaskTTL:         maxTaskTTL,
		AuthBypass:         authBypass,
	}, nil
}

// bypassableAuthModes are the upstream modes whose provider establishes an
// identity from the transport rather than from an ambient credential. oidc is
// absent on purpose: it authenticates from a session cookie, and /mcp is exempt
// from cross-origin protection precisely because it should carry no such thing.
var bypassableAuthModes = []string{"cert", "headers", "tailscale"}

// checkAuthBypass refuses a mode the bypass cannot safely accept, so the rule
// the documentation already states is enforced rather than trusted.
func checkAuthBypass(modes []string) error {
	for _, mode := range modes {
		if slices.Contains(bypassableAuthModes, mode) {
			continue
		}

		return fmt.Errorf(
			"mcp.auth_bypass accepts only %s, got %q: those carry identity in the "+
				"transport, where oidc authenticates from a session cookie that "+
				"/mcp's exemption from cross-origin protection assumes absent",
			strings.Join(bypassableAuthModes, ", "),
			mode,
		)
	}

	return nil
}

// resolveOpsCeiling reads a transport's own operations level from envKey and
// the file value, returning OpsInherit when neither is set. Unlike the global
// ops level, OpsInherit (-1) is a valid result here.
func resolveOpsCeiling(envKey string, file *int) (OperationsLevel, error) {
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
