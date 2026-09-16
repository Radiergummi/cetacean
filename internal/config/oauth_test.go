package config

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every configured key must decode to 32 bytes, so these are hex.
const (
	testSigningKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	envSigningKey  = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	fileSigningKey = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	envBeatsFile   = "ebebebebebebebebebebebebebebebebebebebebebebebebebebebebebebebeb"
)

// clearOAuthEnv unsets every variable loadOAuth reads, so a test asserting the
// file or default layer cannot be swayed by the ambient environment. The list
// tracks loadOAuth: add a setting there and add it here. _SIGNING_KEY_FILE is
// the one worth naming twice — left out, resolveSecret reads a path from the
// developer's shell and the failure points nowhere near the cause.
func clearOAuthEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"CETACEAN_OAUTH_ENABLED",
		"CETACEAN_OAUTH_ISSUER",
		"CETACEAN_OAUTH_SIGNING_KEY",
		"CETACEAN_OAUTH_SIGNING_KEY_FILE",
		"CETACEAN_OAUTH_ACCESS_TOKEN_TTL",
		"CETACEAN_OAUTH_REFRESH_TOKEN_TTL",
		"CETACEAN_OAUTH_CONSENT_TTL",
		"CETACEAN_OAUTH_REQUIRE_RESOURCE_INDICATOR",
		"CETACEAN_OAUTH_DCR_ENABLED",
		"CETACEAN_OAUTH_DCR_RATE_LIMIT",
		"CETACEAN_OAUTH_DCR_MAX_CLIENTS",
		"CETACEAN_OAUTH_CIMD_ENABLED",
	} {
		t.Setenv(key, "")
	}
}

func TestOAuthConfigDefaults(t *testing.T) {
	cfg := DefaultOAuthConfig()

	// The server issues tokens, so it is the last thing that should arrive
	// without being asked for.
	if cfg.Enabled {
		t.Error("the authorization server should be disabled by default")
	}
	if cfg.AccessTokenTTL != time.Hour {
		t.Errorf("access token TTL = %v, want 1h", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("refresh token TTL = %v, want 720h", cfg.RefreshTokenTTL)
	}

	// A remembered approval must outlive the refresh token, or remembering
	// buys nothing: skipping the prompt once the token expires is the point.
	if cfg.ConsentTTL <= cfg.RefreshTokenTTL {
		t.Errorf(
			"consent TTL = %v, want longer than the refresh token TTL %v",
			cfg.ConsentTTL,
			cfg.RefreshTokenTTL,
		)
	}
	if !cfg.DCREnabled {
		t.Error("DCR should be enabled by default")
	}
	if !cfg.CIMDEnabled {
		t.Error("CIMD should be enabled by default")
	}
	if !cfg.RequireResourceIndicator {
		t.Error("RFC 8707 resource indicator should be required by default")
	}
	if cfg.DCRRateLimit != 10 {
		t.Errorf("DCR rate limit = %d, want 10", cfg.DCRRateLimit)
	}
	if cfg.DCRMaxClients != 1000 {
		t.Errorf("DCR max clients = %d, want 1000", cfg.DCRMaxClients)
	}
}

func TestOAuthConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_ENABLED", "true")
	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY", envSigningKey)
	t.Setenv("CETACEAN_OAUTH_ACCESS_TOKEN_TTL", "2h")
	t.Setenv("CETACEAN_OAUTH_REFRESH_TOKEN_TTL", "48h")
	t.Setenv("CETACEAN_OAUTH_CONSENT_TTL", "96h")
	t.Setenv("CETACEAN_OAUTH_REQUIRE_RESOURCE_INDICATOR", "false")
	t.Setenv("CETACEAN_OAUTH_DCR_ENABLED", "false")
	t.Setenv("CETACEAN_OAUTH_DCR_RATE_LIMIT", "25")
	t.Setenv("CETACEAN_OAUTH_DCR_MAX_CLIENTS", "500")
	t.Setenv("CETACEAN_OAUTH_CIMD_ENABLED", "false")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.OAuth.Enabled {
		t.Error("the authorization server should be enabled")
	}
	if cfg.OAuth.SigningKey != envSigningKey {
		t.Errorf("signing key = %q, want %q", cfg.OAuth.SigningKey, envSigningKey)
	}
	if cfg.OAuth.AccessTokenTTL != 2*time.Hour {
		t.Errorf("access token TTL = %v, want 2h", cfg.OAuth.AccessTokenTTL)
	}
	if cfg.OAuth.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("refresh TTL = %v, want 48h", cfg.OAuth.RefreshTokenTTL)
	}
	if cfg.OAuth.ConsentTTL != 96*time.Hour {
		t.Errorf("consent TTL = %v, want 96h", cfg.OAuth.ConsentTTL)
	}
	if cfg.OAuth.RequireResourceIndicator {
		t.Error("RequireResourceIndicator should be false")
	}
	if cfg.OAuth.DCREnabled {
		t.Error("DCREnabled should be false")
	}
	if cfg.OAuth.DCRRateLimit != 25 {
		t.Errorf("DCR rate limit = %d, want 25", cfg.OAuth.DCRRateLimit)
	}
	if cfg.OAuth.DCRMaxClients != 500 {
		t.Errorf("DCR max clients = %d, want 500", cfg.OAuth.DCRMaxClients)
	}
	if cfg.OAuth.CIMDEnabled {
		t.Error("CIMDEnabled should be false")
	}
}

// Asserts every field, so a missing entry in clearOAuthEnv fails here rather
// than only on the machine that exports it.
func TestOAuthConfigFromFile(t *testing.T) {
	clearOAuthEnv(t)

	enabled := true
	issuer := "https://cetacean.example.com"
	signingKey := "fafafafafafafafafafafafafafafafafafafafafafafafafafafafafafafafa"
	accessTTL := "2h"
	refreshTTL := "48h"
	consentTTL := "96h"
	requireRI := false
	dcrEnabled := false
	dcrRate := 5
	dcrMax := 500
	cimdEnabled := false

	fc := &fileConfig{
		OAuth: &fileOAuth{
			Enabled:                  &enabled,
			Issuer:                   &issuer,
			SigningKey:               &signingKey,
			AccessTokenTTL:           &accessTTL,
			RefreshTokenTTL:          &refreshTTL,
			ConsentTTL:               &consentTTL,
			RequireResourceIndicator: &requireRI,
			DCREnabled:               &dcrEnabled,
			DCRRateLimit:             &dcrRate,
			DCRMaxClients:            &dcrMax,
			CIMDEnabled:              &cimdEnabled,
		},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.OAuth.Enabled {
		t.Error("Enabled should be true from file")
	}
	if cfg.OAuth.Issuer != issuer {
		t.Errorf("Issuer = %q, want %q", cfg.OAuth.Issuer, issuer)
	}
	if cfg.OAuth.SigningKey != signingKey {
		t.Errorf("SigningKey = %q, want %q", cfg.OAuth.SigningKey, signingKey)
	}
	if cfg.OAuth.AccessTokenTTL != 2*time.Hour {
		t.Errorf("AccessTokenTTL = %v, want 2h", cfg.OAuth.AccessTokenTTL)
	}
	if cfg.OAuth.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want 48h", cfg.OAuth.RefreshTokenTTL)
	}
	if cfg.OAuth.ConsentTTL != 96*time.Hour {
		t.Errorf("ConsentTTL = %v, want 96h", cfg.OAuth.ConsentTTL)
	}
	if cfg.OAuth.RequireResourceIndicator {
		t.Error("RequireResourceIndicator should be false from file")
	}
	if cfg.OAuth.DCREnabled {
		t.Error("DCREnabled should be false from file")
	}
	if cfg.OAuth.DCRRateLimit != 5 {
		t.Errorf("DCRRateLimit = %d, want 5", cfg.OAuth.DCRRateLimit)
	}
	if cfg.OAuth.DCRMaxClients != 500 {
		t.Errorf("DCRMaxClients = %d, want 500", cfg.OAuth.DCRMaxClients)
	}
	if cfg.OAuth.CIMDEnabled {
		t.Error("CIMDEnabled should be false from file")
	}
}

func TestOAuthConfigEnvWinsOverFile(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_ACCESS_TOKEN_TTL", "3h")

	fileTTL := "2h"
	fc := &fileConfig{OAuth: &fileOAuth{AccessTokenTTL: &fileTTL}}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OAuth.AccessTokenTTL != 3*time.Hour {
		t.Errorf(
			"AccessTokenTTL = %v, want 3h (env should win over file)",
			cfg.OAuth.AccessTokenTTL,
		)
	}
}

func TestOAuthConfigIssuerOverride(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_ISSUER", "https://cetacean.example.com/")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OAuth.Issuer != "https://cetacean.example.com" {
		t.Errorf("Issuer = %q, want trailing slash trimmed", cfg.OAuth.Issuer)
	}
}

func TestOAuthConfigIssuerInvalid(t *testing.T) {
	cases := map[string]string{
		"bad scheme": "ftp://cetacean.example.com",
		"no host":    "https://",
		"has query":  "https://cetacean.example.com?foo=bar",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CETACEAN_OAUTH_ISSUER", raw)
			if _, err := Load(nil, nil); err == nil {
				t.Errorf("expected error for %q", raw)
			}
		})
	}
}

func TestOAuthConfigIssuerFromFile(t *testing.T) {
	clearOAuthEnv(t)

	want := "https://cetacean.example.com"
	fc := &fileConfig{OAuth: &fileOAuth{Issuer: &want}}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OAuth.Issuer != want {
		t.Errorf("Issuer = %q, want %q", cfg.OAuth.Issuer, want)
	}
}

func TestOAuthConfigDCRRateLimitValidation(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_DCR_RATE_LIMIT", "0")

	if _, err := Load(nil, nil); err == nil {
		t.Error("expected error for DCRRateLimit=0")
	}
}

func TestOAuthConfigDCRMaxClientsValidation(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_DCR_MAX_CLIENTS", "-1")

	if _, err := Load(nil, nil); err == nil {
		t.Error("expected error for DCRMaxClients=-1")
	}
}

func TestLoadOAuth_ConsentTTL_ZeroDisables(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CETACEAN_OAUTH_CONSENT_TTL", "0")

	cfg, err := loadOAuth(nil)
	if err != nil {
		t.Fatalf("loadOAuth rejected the documented way to disable consent: %v", err)
	}

	if cfg.ConsentTTL != 0 {
		t.Errorf("ConsentTTL = %v, want 0", cfg.ConsentTTL)
	}
}

func TestLoadOAuth_ConsentTTL_RejectsNegative(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_CONSENT_TTL", "-1h")

	if _, err := loadOAuth(nil); err == nil {
		t.Error("loadOAuth accepted a negative consent TTL, want an error")
	}
}

func TestLoadOAuth_SigningKeyFromFile(t *testing.T) {
	clearOAuthEnv(t)

	keyPath := filepath.Join(t.TempDir(), "oauth_signing_key")
	if err := os.WriteFile(keyPath, append([]byte(fileSigningKey), '\n'), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY_FILE", keyPath)

	cfg, err := loadOAuth(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SigningKey != fileSigningKey {
		t.Errorf("signing key = %q, want %q", cfg.SigningKey, fileSigningKey)
	}
}

func TestLoadOAuth_SigningKeyEnvBeatsFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "oauth_signing_key")
	if err := os.WriteFile(keyPath, []byte(fileSigningKey), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY", envBeatsFile)
	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY_FILE", keyPath)

	cfg, err := loadOAuth(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SigningKey != envBeatsFile {
		t.Errorf("signing key = %q, want %q", cfg.SigningKey, envBeatsFile)
	}
}

func TestLoadOAuth_SigningKeyFileMissing(t *testing.T) {
	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY_FILE", filepath.Join(t.TempDir(), "absent"))

	if _, err := loadOAuth(nil); err == nil {
		t.Fatal("expected an error for an unreadable _FILE path, got nil")
	}
}

func TestLoadOAuth_SigningKeyThatIsNotKeyMaterialIsRejected(t *testing.T) {
	refused := map[string]string{
		"too short":                  "short",
		"a passphrase":               "correct-horse-battery-staple-abc",
		"hex of the wrong length":    "0123456789abcdef0123456789abcdef",
		"base64 of the wrong length": "bm90LXRoaXJ0eS10d28tYnl0ZXMtbG9uZw==",
	}

	for name, key := range refused {
		t.Run(name, func(t *testing.T) {
			clearOAuthEnv(t)
			t.Setenv("CETACEAN_OAUTH_SIGNING_KEY", key)

			if _, err := loadOAuth(nil); err == nil {
				t.Fatalf("%q was accepted; only 32 bytes of hex or base64 may be", key)
			}
		})
	}
}

func TestLoadOAuth_SigningKeyFromFileIsCheckedToo(t *testing.T) {
	clearOAuthEnv(t)

	keyPath := filepath.Join(t.TempDir(), "oauth_signing_key")
	if err := os.WriteFile(keyPath, []byte("too-short"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY_FILE", keyPath)

	if _, err := loadOAuth(nil); err == nil {
		t.Fatal("expected an error for a short signing key read from a file, got nil")
	}
}

func TestLoadOAuth_SigningKeyOfKeyMaterialIsAccepted(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CETACEAN_OAUTH_SIGNING_KEY", testSigningKey)

	cfg, err := loadOAuth(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SigningKey != testSigningKey {
		t.Errorf("signing key = %q, want the test key", cfg.SigningKey)
	}
}

// An unset key is not a bad key: one is generated instead.
func TestLoadOAuth_UnsetSigningKeyIsStillAllowed(t *testing.T) {
	clearOAuthEnv(t)

	cfg, err := loadOAuth(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SigningKey != "" {
		t.Errorf("signing key = %q, want empty", cfg.SigningKey)
	}
}

func TestOAuthIssuer(t *testing.T) {
	tests := []struct {
		name       string
		issuer     string
		publicURL  string
		listenAddr string
		tlsEnabled bool
		want       string
		wantOK     bool
	}{
		{
			name:       "explicit issuer wins",
			issuer:     "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://cetacean.example.com",
			wantOK:     true,
		},
		{
			name:       "public_url is used when oauth.issuer is unset",
			publicURL:  "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://cetacean.example.com",
			wantOK:     true,
		},
		{
			name:       "oauth.issuer overrides public_url",
			issuer:     "https://auth.example.com",
			publicURL:  "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://auth.example.com",
			wantOK:     true,
		},
		{
			name:       "default listen address has no host",
			listenAddr: ":9000",
			want:       "http://:9000",
			wantOK:     false,
		},
		{
			name:       "wildcard bind is not reachable",
			listenAddr: "0.0.0.0:9000",
			want:       "http://0.0.0.0:9000",
			wantOK:     false,
		},
		{
			name:       "unspecified IPv6 bind is not reachable",
			listenAddr: "[::]:9000",
			want:       "http://[::]:9000",
			wantOK:     false,
		},
		{
			name:       "explicit host derives",
			listenAddr: "cetacean.internal:9000",
			want:       "http://cetacean.internal:9000",
			wantOK:     true,
		},
		{
			name:       "TLS derives https",
			listenAddr: "cetacean.internal:9000",
			tlsEnabled: true,
			want:       "https://cetacean.internal:9000",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ListenAddr: tt.listenAddr,
				PublicURL:  tt.publicURL,
				OAuth:      OAuthConfig{Issuer: tt.issuer},
			}

			got, ok := cfg.OAuthIssuer(tt.tlsEnabled)

			if got != tt.want {
				t.Errorf("issuer = %q, want %q", got, tt.want)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestValidateOAuth(t *testing.T) {
	tests := []struct {
		name         string
		oauthEnabled bool
		mcpEnabled   bool
		authMode     string
		authBypass   []string
		wantErr      string
	}{
		{
			name:       "MCP under real auth without the server would serve unauthenticated",
			mcpEnabled: true,
			authMode:   "oidc",
			wantErr:    "needs either oauth.enabled",
		},
		{
			// The mTLS deployment: clients cannot drive a browser consent
			// screen, so the upstream provider authenticates /mcp and no
			// authorization server is built at all.
			name:       "a bypassed mode needs no server",
			mcpEnabled: true,
			authMode:   "cert",
			authBypass: []string{"cert"},
		},
		{
			// The bypass covers the mode it names and no other.
			name:       "a bypass for another mode does not cover this one",
			mcpEnabled: true,
			authMode:   "oidc",
			authBypass: []string{"cert"},
			wantErr:    "needs either oauth.enabled",
		},
		{
			name:         "the server cannot establish an identity in none mode",
			oauthEnabled: true,
			authMode:     "none",
			wantErr:      "needs an auth.mode other than",
		},
		{
			name:         "MCP with the server is the ordinary deployment",
			oauthEnabled: true,
			mcpEnabled:   true,
			authMode:     "oidc",
		},
		{
			name:       "MCP in none mode needs no server",
			mcpEnabled: true,
			authMode:   "none",
		},
		{
			// Allowed: the operator asked for it, and the REST API becomes a
			// second resource later.
			name:         "the server with nothing consuming it is allowed",
			oauthEnabled: true,
			authMode:     "oidc",
		},
		{
			name:     "neither enabled",
			authMode: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				MCP:   MCPConfig{Enabled: tt.mcpEnabled, AuthBypass: tt.authBypass},
				OAuth: OAuthConfig{Enabled: tt.oauthEnabled},
			}

			err := cfg.ValidateOAuth(tt.authMode)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected an error mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestSigningKeyBytes(t *testing.T) {
	raw := bytes.Repeat([]byte{0xAB}, 32)

	tests := []struct {
		name        string
		key         string
		wantRoot    []byte
		wantDecoded bool
	}{
		{"hex", hex.EncodeToString(raw), raw, true},
		{"standard base64", base64.StdEncoding.EncodeToString(raw), raw, true},
		{"raw url base64", base64.RawURLEncoding.EncodeToString(raw), raw, true},
		{
			"a passphrase is its own bytes",
			"correct-horse-battery-staple-abc",
			[]byte("correct-horse-battery-staple-abc"),
			false,
		},
		{
			// 32 hex characters decode to 16 bytes, not 32. A decoder that
			// checks only "is this hex" would take it.
			"hex-looking but half the length",
			"abcdefabcdefabcdefabcdefabcdefab",
			[]byte("abcdefabcdefabcdefabcdefabcdefab"),
			false,
		},
		{"unset", "", []byte(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, decoded := SigningKeyBytes(tt.key)

			if !bytes.Equal(root, tt.wantRoot) {
				t.Errorf("root = %x, want %x", root, tt.wantRoot)
			}

			if decoded != tt.wantDecoded {
				t.Errorf("decoded = %v, want %v", decoded, tt.wantDecoded)
			}
		})
	}
}
