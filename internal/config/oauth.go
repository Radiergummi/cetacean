package config

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// OAuthConfig holds configuration for the OAuth 2.1 authorization server.
type OAuthConfig struct {
	// Enabled controls whether the authorization server runs. It is opt-in:
	// a deployment that does not ask for one issues no tokens at all.
	Enabled bool

	// Issuer is the canonical external URL of this Cetacean instance, used as
	// the OAuth 2.1 issuer identifier and as the base for the resource
	// audience. Empty means "derive from the listen address" — only correct
	// when no reverse proxy sits in front. Behind a proxy, set this to the
	// public URL (e.g. "https://cetacean.example.com").
	Issuer string

	// SigningKey is the root the token and CSRF keys derive from. Empty means
	// a fresh root each start, which invalidates every issued token.
	SigningKey string

	// AccessTokenTTL is how long access tokens remain valid.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is how long refresh tokens remain valid.
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
}

// DefaultOAuthConfig returns an OAuthConfig populated with sensible defaults.
func DefaultOAuthConfig() OAuthConfig {
	return OAuthConfig{
		Enabled:         false,
		Issuer:          "",
		SigningKey:      "",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 720 * time.Hour,
		ConsentTTL:      2160 * time.Hour, // 90d, well past the refresh token's 30d

		RequireResourceIndicator: true,
		DCREnabled:               true,
		DCRRateLimit:             10,
		DCRMaxClients:            1000,
		CIMDEnabled:              true,
	}
}

// ValidateOAuth settles whether the authorization server, MCP and the auth mode
// make a coherent deployment. Both failures are refusals rather than warnings:
// one would leave the cluster's write surface open, and the other describes a
// server that cannot answer the question it exists to answer.
//
// A method rather than a function of both flags, because the two are booleans
// and adjacent: transposed, they would compile and report the wrong conflict.
func (c *Config) ValidateOAuth(authMode string) error {
	anonymous := authMode == "none"

	// /mcp is exempt from the auth middleware and authenticates itself, so it
	// needs something of its own: a bearer check against an authorization
	// server, or the upstream provider for a mode named in mcp.auth_bypass.
	// With neither it would serve the cluster's write surface unauthenticated.
	// internal/mcp refuses the same combination — this one names the settings.
	bypassed := slices.Contains(c.MCP.AuthBypass, authMode)
	if c.MCP.Enabled && !anonymous && !c.OAuth.Enabled && !bypassed {
		return fmt.Errorf(
			"mcp.enabled with auth.mode=%q needs either oauth.enabled or %q in "+
				"mcp.auth_bypass: /mcp authenticates itself, so with neither it "+
				"would serve unauthenticated",
			authMode,
			authMode,
		)
	}

	if c.OAuth.Enabled && anonymous {
		return fmt.Errorf(
			"oauth.enabled needs an auth.mode other than %q: the authorization server "+
				"establishes no identity of its own, so consent would ask an anonymous "+
				"user to approve a client",
			authMode,
		)
	}

	return nil
}

// movedOAuthEnv maps each removed variable to what replaced it. Nothing reads
// the old names, so a deployment still setting one gets the default instead of
// what it wrote — and for the three that switch a capability off, the default
// is on. The file needs no list: LoadFile refuses any key the schema lacks.
var movedOAuthEnv = map[string]string{ //nolint:gosec // G101: names, not values
	"CETACEAN_MCP_ISSUER":                     "CETACEAN_OAUTH_ISSUER",
	"CETACEAN_MCP_SIGNING_KEY":                "CETACEAN_OAUTH_SIGNING_KEY",
	"CETACEAN_MCP_SIGNING_KEY_FILE":           "CETACEAN_OAUTH_SIGNING_KEY_FILE",
	"CETACEAN_MCP_ACCESS_TOKEN_TTL":           "CETACEAN_OAUTH_ACCESS_TOKEN_TTL",
	"CETACEAN_MCP_REFRESH_TOKEN_TTL":          "CETACEAN_OAUTH_REFRESH_TOKEN_TTL",
	"CETACEAN_MCP_CONSENT_TTL":                "CETACEAN_OAUTH_CONSENT_TTL",
	"CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR": "CETACEAN_OAUTH_REQUIRE_RESOURCE_INDICATOR",
	"CETACEAN_MCP_DCR_ENABLED":                "CETACEAN_OAUTH_DCR_ENABLED",
	"CETACEAN_MCP_DCR_RATE_LIMIT":             "CETACEAN_OAUTH_DCR_RATE_LIMIT",
	"CETACEAN_MCP_DCR_MAX_CLIENTS":            "CETACEAN_OAUTH_DCR_MAX_CLIENTS",
	"CETACEAN_MCP_CIMD_ENABLED":               "CETACEAN_OAUTH_CIMD_ENABLED",
}

// checkMovedOAuthEnv refuses startup while any removed variable is still set,
// naming every one of them at once so an operator fixes the deployment in one
// pass rather than one restart per variable.
func checkMovedOAuthEnv() error {
	var moved []string
	for old, replacement := range movedOAuthEnv {
		if _, ok := os.LookupEnv(old); ok {
			moved = append(moved, old+" is now "+replacement)
		}
	}
	if len(moved) == 0 {
		return nil
	}

	slices.Sort(moved)

	return fmt.Errorf(
		"the authorization server's settings moved out of CETACEAN_MCP_*, and the "+
			"old variables are no longer read: %s. Unset them once the deployment "+
			"carries the new names",
		strings.Join(moved, "; "),
	)
}

// loadOAuth builds an OAuthConfig from a file section and env vars, applying the
// standard resolve helpers. It is called from Load() and is also directly
// testable.
func loadOAuth(fo *fileOAuth) (OAuthConfig, error) {
	if err := checkMovedOAuthEnv(); err != nil {
		return OAuthConfig{}, err
	}

	def := DefaultOAuthConfig()

	// Extract file-level pointers (safely handle nil sub-struct).
	var (
		fEnabled       *bool
		fIssuer        *string
		fSigningKey    *string
		fAccessTTL     *string
		fRefreshTTL    *string
		fConsentTTL    *string
		fRequireRI     *bool
		fDCREnabled    *bool
		fDCRRateLimit  *int
		fDCRMaxClients *int
		fCIMDEnabled   *bool
	)
	if fo != nil {
		fEnabled = fo.Enabled
		fIssuer = fo.Issuer
		fSigningKey = fo.SigningKey
		fAccessTTL = fo.AccessTokenTTL
		fRefreshTTL = fo.RefreshTokenTTL
		fConsentTTL = fo.ConsentTTL
		fRequireRI = fo.RequireResourceIndicator
		fDCREnabled = fo.DCREnabled
		fDCRRateLimit = fo.DCRRateLimit
		fDCRMaxClients = fo.DCRMaxClients
		fCIMDEnabled = fo.CIMDEnabled
	}

	accessTTL, err := resolveDuration(
		nil,
		"CETACEAN_OAUTH_ACCESS_TOKEN_TTL",
		fAccessTTL,
		def.AccessTokenTTL,
	)
	if err != nil {
		return OAuthConfig{}, err
	}

	refreshTTL, err := resolveDuration(
		nil,
		"CETACEAN_OAUTH_REFRESH_TOKEN_TTL",
		fRefreshTTL,
		def.RefreshTokenTTL,
	)
	if err != nil {
		return OAuthConfig{}, err
	}

	// Non-negative rather than positive: zero is the documented way to turn
	// remembered approvals off, and ConsentStore.Enabled() honours it.
	consentTTL, err := resolveNonNegativeDuration(
		nil,
		"CETACEAN_OAUTH_CONSENT_TTL",
		fConsentTTL,
		def.ConsentTTL,
	)
	if err != nil {
		return OAuthConfig{}, err
	}

	dcrRateLimit, err := resolveInt(
		nil,
		"CETACEAN_OAUTH_DCR_RATE_LIMIT",
		fDCRRateLimit,
		def.DCRRateLimit,
		1,
		1<<20,
	)
	if err != nil {
		return OAuthConfig{}, err
	}

	dcrMaxClients, err := resolveInt(
		nil,
		"CETACEAN_OAUTH_DCR_MAX_CLIENTS",
		fDCRMaxClients,
		def.DCRMaxClients,
		1,
		1<<20,
	)
	if err != nil {
		return OAuthConfig{}, err
	}

	issuer, err := resolveOAuthIssuer(fIssuer)
	if err != nil {
		return OAuthConfig{}, err
	}

	signingKey, err := resolveSecret(
		nil,
		"CETACEAN_OAUTH_SIGNING_KEY",
		fSigningKey,
		def.SigningKey,
	)
	if err != nil {
		return OAuthConfig{}, err
	}
	if err := checkSigningKey(signingKey); err != nil {
		return OAuthConfig{}, err
	}

	return OAuthConfig{
		Enabled:         resolveBool(nil, "CETACEAN_OAUTH_ENABLED", fEnabled, def.Enabled),
		Issuer:          issuer,
		SigningKey:      signingKey,
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
		ConsentTTL:      consentTTL,
		RequireResourceIndicator: resolveBool(
			nil,
			"CETACEAN_OAUTH_REQUIRE_RESOURCE_INDICATOR",
			fRequireRI,
			def.RequireResourceIndicator,
		),
		DCREnabled: resolveBool(
			nil,
			"CETACEAN_OAUTH_DCR_ENABLED",
			fDCREnabled,
			def.DCREnabled,
		),
		DCRRateLimit:  dcrRateLimit,
		DCRMaxClients: dcrMaxClients,
		CIMDEnabled: resolveBool(
			nil,
			"CETACEAN_OAUTH_CIMD_ENABLED",
			fCIMDEnabled,
			def.CIMDEnabled,
		),
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
		"oauth.signing_key must be %d bytes of hex or base64 — generate one with "+
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

// resolveOAuthIssuer reads CETACEAN_OAUTH_ISSUER and the file value, validates
// the result as an http(s) URL with a host, and strips trailing slashes.
// Empty input is allowed and means "derive from listen address" at startup.
func resolveOAuthIssuer(file *string) (string, error) {
	const envKey = "CETACEAN_OAUTH_ISSUER"

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

// OAuthIssuer returns the canonical external base URL clients reach this
// deployment at: oauth.issuer, then server.public_url, then a derivation from
// server.listen_addr and whether TLS terminates here.
//
// The second return is false when that derivation reaches nothing: the
// default ":9000" has an empty host, and a wildcard bind ("0.0.0.0", "::")
// parses but resolves nowhere. The string is returned either way, since only
// the authorization server truly breaks on it, which is the caller's to decide.
func (c *Config) OAuthIssuer(tlsEnabled bool) (string, bool) {
	if c.OAuth.Issuer != "" {
		return c.OAuth.Issuer, true
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
