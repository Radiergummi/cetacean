package config

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
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

	// Issuer is the canonical external URL of this instance, used as the OAuth
	// 2.1 issuer identifier and the base for the MCP resource audience. Empty
	// derives it from the listen address, which is only correct when no reverse
	// proxy sits in front.
	Issuer string

	// SigningKey is the root the token and CSRF keys derive from. Empty means
	// a fresh root each start, which invalidates every issued token.
	SigningKey string

	// AccessTokenTTL is how long MCP access tokens remain valid.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is how long MCP refresh tokens remain valid.
	RefreshTokenTTL time.Duration

	// ConsentTTL is how long a remembered approval keeps letting a client skip
	// the consent screen. It must outlive RefreshTokenTTL to be useful, but
	// cannot be unbounded: an approval is revocable only through its grant
	// family, so a record outliving that one authorizes silently forever.
	// Zero or negative disables remembering, prompting every authorization.
	ConsentTTL time.Duration

	// RequireResourceIndicator requires RFC 8707 resource indicators in token requests.
	RequireResourceIndicator bool

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

	// AuthBypass lists upstream auth modes whose authenticated identity is
	// accepted at /mcp without an OAuth bearer. The MCP server then derives
	// identity from that provider — the mTLS certificate, say — rather than
	// validating a JWT. A mode that issues redirects is unsafe to list.
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

	// Non-negative rather than positive: zero is the documented way to turn
	// remembered approvals off, and ConsentStore.Enabled() honours it.
	consentTTL, err := resolveNonNegativeDuration(
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

	signingKey, err := resolveSecret(
		nil,
		"CETACEAN_MCP_SIGNING_KEY",
		fSigningKey,
		def.SigningKey,
	)
	if err != nil {
		return MCPConfig{}, err
	}
	if err := checkSigningKey(signingKey); err != nil {
		return MCPConfig{}, err
	}

	return MCPConfig{
		Enabled:            resolveBool(nil, "CETACEAN_MCP", fEnabled, def.Enabled),
		OperationsLevel:    opsLevel,
		Issuer:             issuer,
		SigningKey:         signingKey,
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

const signingKeyBytes = 32

// The published public key is a deterministic function of the root and needs no
// authentication to fetch, so anything but real key material can be ground
// offline from it. An empty key is not a failure: it means none was configured,
// and one is generated instead.
func checkSigningKey(key string) error {
	if key == "" {
		return nil
	}

	if _, decoded := SigningKeyBytes(key); decoded {
		return nil
	}

	return fmt.Errorf(
		"mcp.signing_key must be %d bytes of hex or base64 — generate one with "+
			"`openssl rand -hex 32`, or leave it unset to have one generated",
		signingKeyBytes,
	)
}

// A value that decodes as hex or base64 to exactly signingKeyBytes is key
// material; anything else is not, and decoded reports which.
func SigningKeyBytes(key string) (root []byte, decoded bool) {
	decoders := []func(string) ([]byte, error){
		hex.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}

	for _, decode := range decoders {
		if b, err := decode(key); err == nil && len(b) == signingKeyBytes {
			return b, true
		}
	}

	return []byte(key), false
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

// MCPIssuer returns the canonical external base URL clients reach this
// deployment at: mcp.issuer, then server.public_url, then a derivation from
// server.listen_addr. The second return is false when that derivation reaches
// nothing — an empty or wildcard host. The string comes back either way, since
// only OAuth truly breaks on it.
func (c *Config) MCPIssuer(tlsEnabled bool) (string, bool) {
	if c.MCP.Issuer != "" {
		return c.MCP.Issuer, true
	}

	if c.PublicURL != "" {
		return c.PublicURL, true
	}

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	issuer := scheme + "://" + c.ListenAddr

	u, err := url.Parse(issuer)
	if err != nil {
		return issuer, false
	}

	switch u.Hostname() {
	case "", "0.0.0.0", "::":
		return issuer, false
	}

	return issuer, true
}

// MCPIssuerRequired reports whether /mcp needs a reachable issuer rather than
// one that only feeds cosmetic tool-icon URLs. OAuth is in play unless the auth
// mode is "none" or listed in mcp.oauth.auth_bypass, since a bypassed mode
// never drives the authorize/token flow.
func (c *Config) MCPIssuerRequired(authMode string) bool {
	return authMode != "none" && !slices.Contains(c.MCP.AuthBypass, authMode)
}
