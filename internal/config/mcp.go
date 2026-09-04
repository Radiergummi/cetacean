package config

import (
	"fmt"
	"net/url"
	"os"
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

	// Issuer is the canonical external URL of this Cetacean instance, used as
	// the OAuth 2.1 issuer identifier and as the base for the MCP resource
	// audience. Empty means "derive from the listen address" — only correct
	// when no reverse proxy sits in front. Behind a proxy, set this to the
	// public URL (e.g. "https://cetacean.example.com").
	Issuer string

	// SigningKey is the HMAC key used to sign MCP tokens. If empty, main.go
	// auto-generates an ephemeral key on startup.
	// TODO: _FILE secret support via resolveSecret
	SigningKey string

	// AccessTokenTTL is how long MCP access tokens remain valid.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is how long MCP refresh tokens remain valid.
	RefreshTokenTTL time.Duration

	// ConsentTTL is how long a remembered approval keeps letting a client skip
	// the consent screen. It must outlive RefreshTokenTTL to be useful at all —
	// the point of remembering is that an expired refresh token does not cost
	// the operator a second prompt — but it cannot be unbounded: an approval is
	// only revocable by presenting a token from its grant family, so once that
	// family lapses an unbounded record would keep authorizing silently with no
	// way left to withdraw it. Zero or negative disables remembering entirely,
	// so every authorization is prompted.
	ConsentTTL time.Duration

	// RequireResourceIndicator requires RFC 8707 resource indicators in token requests.
	RequireResourceIndicator bool

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

	// DCREnabled enables Dynamic Client Registration (RFC 7591).
	DCREnabled bool

	// DCRRateLimit is the maximum number of DCR requests per IP per hour.
	DCRRateLimit int

	// DCRMaxClients is the maximum number of dynamically registered clients.
	DCRMaxClients int

	// CIMDEnabled enables Client ID Metadata Documents: an https:// client_id
	// that Cetacean fetches and verifies. Disabling it stops the server making
	// outbound requests on a client's behalf.
	CIMDEnabled bool

	// AuthBypass lists upstream Cetacean auth modes (e.g. "cert") whose
	// authenticated identity is accepted at /mcp without an OAuth bearer
	// token. When a request reaches /mcp and the active auth mode is in this
	// list, the MCP server derives identity from the upstream provider
	// (e.g. the mTLS client certificate) instead of validating a JWT.
	// Modes that would issue redirects (e.g. "oidc") are unsafe to list.
	AuthBypass []string
}

// DefaultMCPConfig returns an MCPConfig populated with sensible defaults.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Enabled:         false,
		OperationsLevel: OpsInherit,
		Issuer:          "",
		SigningKey:      "",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 720 * time.Hour,
		ConsentTTL:      2160 * time.Hour, // 90d, well past the refresh token's 30d

		RequireResourceIndicator: true,
		MaxConcurrentTasks:       32,
		// 15m covers the 5m convergence timeout with a collection window well
		// clear of it, since the TTL runs from creation.
		TaskTTL:       15 * time.Minute,
		MaxTaskTTL:    time.Hour,
		DCREnabled:    true,
		DCRRateLimit:  10,
		DCRMaxClients: 1000,
		CIMDEnabled:   true,
		AuthBypass:    nil,
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
		fIssuer        *string
		fSigningKey    *string
		fAccessTTL     *string
		fRefreshTTL    *string
		fOpsLevel      *int
		fRequireRI     *bool
		fConsentTTL    *string
		fTaskTTL       *string
		fMaxTaskTTL    *string
		fMaxTasks      *int
		fDCREnabled    *bool
		fDCRRateLimit  *int
		fDCRMaxClients *int
		fCIMDEnabled   *bool
		fAuthBypass    []string
	)
	if fm != nil {
		fEnabled = fm.Enabled
		fIssuer = fm.Issuer
		fSigningKey = fm.SigningKey
		fAccessTTL = fm.AccessTokenTTL
		fRefreshTTL = fm.RefreshTokenTTL
		fConsentTTL = fm.ConsentTTL
		fOpsLevel = fm.OperationsLevel
		fMaxTasks = fm.MaxConcurrentTasks
		fTaskTTL = fm.TaskTTL
		fMaxTaskTTL = fm.MaxTaskTTL
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

	consentTTL, err := resolveDuration(
		nil,
		"CETACEAN_MCP_CONSENT_TTL",
		fConsentTTL,
		def.ConsentTTL,
	)
	if err != nil {
		return MCPConfig{}, err
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

	issuer, err := resolveMCPIssuer(fIssuer)
	if err != nil {
		return MCPConfig{}, err
	}

	return MCPConfig{
		Enabled:         resolveBool(nil, "CETACEAN_MCP", fEnabled, def.Enabled),
		OperationsLevel: opsLevel,
		Issuer:          issuer,
		SigningKey: resolve(
			nil,
			"CETACEAN_MCP_SIGNING_KEY",
			fSigningKey,
			def.SigningKey,
		),
		AccessTokenTTL:     accessTTL,
		RefreshTokenTTL:    refreshTTL,
		ConsentTTL:         consentTTL,
		MaxConcurrentTasks: maxConcurrentTasks,
		TaskTTL:            taskTTL,
		MaxTaskTTL:         maxTaskTTL,
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

// resolveMCPIssuer reads CETACEAN_MCP_ISSUER and the file value, validates
// the result as an http(s) URL with a host, and strips trailing slashes.
// Empty input is allowed and means "derive from listen address" at startup.
func resolveMCPIssuer(file *string) (string, error) {
	const envKey = "CETACEAN_MCP_ISSUER"

	raw := os.Getenv(envKey)
	source := envKey
	if raw == "" && file != nil {
		raw = *file
		source = "config file"
	}
	if raw == "" {
		return "", nil
	}

	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL from %s %q: %w", source, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%s must use http or https scheme, got %q", source, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s must include a host, got %q", source, raw)
	}
	if u.Fragment != "" || u.RawQuery != "" {
		return "", fmt.Errorf("%s must not contain a fragment or query, got %q", source, raw)
	}

	return raw, nil
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
