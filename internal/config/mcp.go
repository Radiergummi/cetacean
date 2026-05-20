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

	// SigningKey is the HMAC key used to sign MCP tokens. If empty, main.go
	// auto-generates an ephemeral key on startup.
	// TODO: _FILE secret support via resolveSecret
	SigningKey string

	// AccessTokenTTL is how long MCP access tokens remain valid.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is how long MCP refresh tokens remain valid.
	RefreshTokenTTL time.Duration

	// SessionIdleTTL is the maximum idle time before a session is evicted.
	SessionIdleTTL time.Duration

	// MaxSessions is the maximum number of concurrent MCP sessions.
	MaxSessions int

	// RequireResourceIndicator requires RFC 8707 resource indicators in token requests.
	RequireResourceIndicator bool

	// DCREnabled enables Dynamic Client Registration (RFC 7591).
	DCREnabled bool

	// DCRRateLimit is the maximum number of DCR requests per minute.
	DCRRateLimit int

	// DCRMaxClients is the maximum number of dynamically registered clients.
	DCRMaxClients int

	// CIMDEnabled enables Client-Initiated Metadata Discovery.
	CIMDEnabled bool

	// AuthBypass is a list of client IDs that skip authentication checks.
	AuthBypass []string
}

// DefaultMCPConfig returns an MCPConfig populated with sensible defaults.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Enabled:                  false,
		OperationsLevel:          OpsInherit,
		SigningKey:               "",
		AccessTokenTTL:           time.Hour,
		RefreshTokenTTL:          720 * time.Hour,
		SessionIdleTTL:           30 * time.Minute,
		MaxSessions:              256,
		RequireResourceIndicator: true,
		DCREnabled:               true,
		DCRRateLimit:             10,
		DCRMaxClients:            1000,
		CIMDEnabled:              true,
		AuthBypass:               nil,
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
		fEnabled       *bool
		fSigningKey    *string
		fAccessTTL     *string
		fRefreshTTL    *string
		fIdleTTL       *string
		fMaxSessions   *int
		fOpsLevel      *int
		fRequireRI     *bool
		fDCREnabled    *bool
		fDCRRateLimit  *int
		fDCRMaxClients *int
		fCIMDEnabled   *bool
		fAuthBypass    []string
	)
	if fm != nil {
		fEnabled = fm.Enabled
		fSigningKey = fm.SigningKey
		fAccessTTL = fm.AccessTokenTTL
		fRefreshTTL = fm.RefreshTokenTTL
		fIdleTTL = fm.SessionIdleTTL
		fMaxSessions = fm.MaxSessions
		fOpsLevel = fm.OperationsLevel
		if fm.OAuth != nil {
			fRequireRI = fm.OAuth.RequireResourceIndicator
			fDCREnabled = fm.OAuth.DCREnabled
			fDCRRateLimit = fm.OAuth.DCRRateLimit
			fDCRMaxClients = fm.OAuth.DCRMaxClients
			fCIMDEnabled = fm.OAuth.CIMDEnabled
			fAuthBypass = fm.OAuth.AuthBypass
		}
	}

	accessTTL, err := resolveDuration(
		nil,
		"CETACEAN_MCP_ACCESS_TOKEN_TTL",
		fAccessTTL,
		def.AccessTokenTTL,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	refreshTTL, err := resolveDuration(
		nil,
		"CETACEAN_MCP_REFRESH_TOKEN_TTL",
		fRefreshTTL,
		def.RefreshTokenTTL,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	idleTTL, err := resolveDuration(
		nil,
		"CETACEAN_MCP_SESSION_IDLE_TTL",
		fIdleTTL,
		def.SessionIdleTTL,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	maxSessions, err := resolveInt(
		nil,
		"CETACEAN_MCP_MAX_SESSIONS",
		fMaxSessions,
		def.MaxSessions,
		1,
		1<<20,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	dcrRateLimit, err := resolveInt(
		nil,
		"CETACEAN_MCP_DCR_RATE_LIMIT",
		fDCRRateLimit,
		def.DCRRateLimit,
		1,
		1<<20,
	)
	if err != nil {
		return MCPConfig{}, err
	}

	dcrMaxClients, err := resolveInt(
		nil,
		"CETACEAN_MCP_DCR_MAX_CLIENTS",
		fDCRMaxClients,
		def.DCRMaxClients,
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
		Enabled:         resolveBool(nil, "CETACEAN_MCP", fEnabled, def.Enabled),
		OperationsLevel: opsLevel,
		SigningKey: resolve(
			nil,
			"CETACEAN_MCP_SIGNING_KEY",
			fSigningKey,
			def.SigningKey,
		),
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
		SessionIdleTTL:  idleTTL,
		MaxSessions:     maxSessions,
		RequireResourceIndicator: resolveBool(
			nil,
			"CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR",
			fRequireRI,
			def.RequireResourceIndicator,
		),
		DCREnabled: resolveBool(
			nil,
			"CETACEAN_MCP_DCR_ENABLED",
			fDCREnabled,
			def.DCREnabled,
		),
		DCRRateLimit:  dcrRateLimit,
		DCRMaxClients: dcrMaxClients,
		CIMDEnabled: resolveBool(
			nil,
			"CETACEAN_MCP_CIMD_ENABLED",
			fCIMDEnabled,
			def.CIMDEnabled,
		),
		AuthBypass: resolveStringSlice(nil, "CETACEAN_MCP_AUTH_BYPASS", fAuthBypass),
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
