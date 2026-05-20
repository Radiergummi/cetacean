package config

import (
	"testing"
	"time"
)

func TestMCPConfigDefaults(t *testing.T) {
	cfg := DefaultMCPConfig()

	if cfg.Enabled {
		t.Error("MCP should be disabled by default")
	}
	if cfg.AccessTokenTTL != time.Hour {
		t.Errorf("access token TTL = %v, want 1h", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("refresh token TTL = %v, want 720h", cfg.RefreshTokenTTL)
	}
	if cfg.SessionIdleTTL != 30*time.Minute {
		t.Errorf("session idle TTL = %v, want 30m", cfg.SessionIdleTTL)
	}
	if cfg.MaxSessions != 256 {
		t.Errorf("max sessions = %d, want 256", cfg.MaxSessions)
	}
	if cfg.OperationsLevel != OpsInherit {
		t.Errorf("operations level = %v, want OpsInherit", cfg.OperationsLevel)
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

func TestMCPConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_MCP", "true")
	t.Setenv("CETACEAN_MCP_SIGNING_KEY", "test-secret")
	t.Setenv("CETACEAN_MCP_ACCESS_TOKEN_TTL", "2h")
	t.Setenv("CETACEAN_MCP_REFRESH_TOKEN_TTL", "48h")
	t.Setenv("CETACEAN_MCP_SESSION_IDLE_TTL", "15m")
	t.Setenv("CETACEAN_MCP_MAX_SESSIONS", "128")
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "2")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.MCP.Enabled {
		t.Error("MCP should be enabled")
	}
	if cfg.MCP.SigningKey != "test-secret" {
		t.Errorf("signing key = %q, want %q", cfg.MCP.SigningKey, "test-secret")
	}
	if cfg.MCP.AccessTokenTTL != 2*time.Hour {
		t.Errorf("access token TTL = %v, want 2h", cfg.MCP.AccessTokenTTL)
	}
	if cfg.MCP.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("refresh TTL = %v, want 48h", cfg.MCP.RefreshTokenTTL)
	}
	if cfg.MCP.SessionIdleTTL != 15*time.Minute {
		t.Errorf("idle TTL = %v, want 15m", cfg.MCP.SessionIdleTTL)
	}
	if cfg.MCP.MaxSessions != 128 {
		t.Errorf("max sessions = %d, want 128", cfg.MCP.MaxSessions)
	}
	if cfg.MCP.OperationsLevel != OpsConfiguration {
		t.Errorf("ops level = %v, want OpsConfiguration", cfg.MCP.OperationsLevel)
	}
}

func TestMCPEffectiveOperationsLevel(t *testing.T) {
	inherit := MCPConfig{OperationsLevel: OpsInherit}
	if got := inherit.EffectiveOperationsLevel(OpsImpactful); got != OpsImpactful {
		t.Errorf("OpsInherit should fall back to global, got %v", got)
	}

	explicit := MCPConfig{OperationsLevel: OpsConfiguration}
	if got := explicit.EffectiveOperationsLevel(OpsImpactful); got != OpsConfiguration {
		t.Errorf("explicit level should override global, got %v", got)
	}
}

func TestMCPConfigFromFile(t *testing.T) {
	// Clear all MCP env vars so file values are used.
	t.Setenv("CETACEAN_MCP", "")
	t.Setenv("CETACEAN_MCP_SIGNING_KEY", "")
	t.Setenv("CETACEAN_MCP_ACCESS_TOKEN_TTL", "")
	t.Setenv("CETACEAN_MCP_REFRESH_TOKEN_TTL", "")
	t.Setenv("CETACEAN_MCP_SESSION_IDLE_TTL", "")
	t.Setenv("CETACEAN_MCP_MAX_SESSIONS", "")
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "")
	t.Setenv("CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR", "")
	t.Setenv("CETACEAN_MCP_DCR_ENABLED", "")
	t.Setenv("CETACEAN_MCP_DCR_RATE_LIMIT", "")
	t.Setenv("CETACEAN_MCP_DCR_MAX_CLIENTS", "")
	t.Setenv("CETACEAN_MCP_CIMD_ENABLED", "")
	t.Setenv("CETACEAN_MCP_AUTH_BYPASS", "")

	enabled := true
	signingKey := "file-secret"
	accessTTL := "2h"
	refreshTTL := "48h"
	idleTTL := "15m"
	maxSessions := 64
	opsLevel := 2
	requireRI := false
	dcrEnabled := false
	dcrRate := 5
	dcrMax := 500
	cimdEnabled := false

	fc := &fileConfig{
		MCP: &fileMCP{
			Enabled:         &enabled,
			SigningKey:      &signingKey,
			AccessTokenTTL:  &accessTTL,
			RefreshTokenTTL: &refreshTTL,
			SessionIdleTTL:  &idleTTL,
			MaxSessions:     &maxSessions,
			OperationsLevel: &opsLevel,
			OAuth: &fileMCPOAuth{
				RequireResourceIndicator: &requireRI,
				DCREnabled:               &dcrEnabled,
				DCRRateLimit:             &dcrRate,
				DCRMaxClients:            &dcrMax,
				CIMDEnabled:              &cimdEnabled,
			},
		},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.MCP.Enabled {
		t.Error("MCP.Enabled should be true from file")
	}
	if cfg.MCP.SigningKey != "file-secret" {
		t.Errorf("SigningKey = %q, want file-secret", cfg.MCP.SigningKey)
	}
	if cfg.MCP.AccessTokenTTL != 2*time.Hour {
		t.Errorf("AccessTokenTTL = %v, want 2h", cfg.MCP.AccessTokenTTL)
	}
	if cfg.MCP.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want 48h", cfg.MCP.RefreshTokenTTL)
	}
	if cfg.MCP.SessionIdleTTL != 15*time.Minute {
		t.Errorf("SessionIdleTTL = %v, want 15m", cfg.MCP.SessionIdleTTL)
	}
	if cfg.MCP.MaxSessions != 64 {
		t.Errorf("MaxSessions = %d, want 64", cfg.MCP.MaxSessions)
	}
	if cfg.MCP.OperationsLevel != OpsConfiguration {
		t.Errorf("OperationsLevel = %v, want OpsConfiguration", cfg.MCP.OperationsLevel)
	}
	if cfg.MCP.RequireResourceIndicator {
		t.Error("RequireResourceIndicator should be false from file")
	}
	if cfg.MCP.DCREnabled {
		t.Error("DCREnabled should be false from file")
	}
	if cfg.MCP.DCRRateLimit != 5 {
		t.Errorf("DCRRateLimit = %d, want 5", cfg.MCP.DCRRateLimit)
	}
	if cfg.MCP.DCRMaxClients != 500 {
		t.Errorf("DCRMaxClients = %d, want 500", cfg.MCP.DCRMaxClients)
	}
	if cfg.MCP.CIMDEnabled {
		t.Error("CIMDEnabled should be false from file")
	}
}

func TestMCPConfigEnvWinsOverFile(t *testing.T) {
	// File says 2h, env says 3h — env must win.
	t.Setenv("CETACEAN_MCP_ACCESS_TOKEN_TTL", "3h")

	fileTTL := "2h"
	fc := &fileConfig{
		MCP: &fileMCP{
			AccessTokenTTL: &fileTTL,
		},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.MCP.AccessTokenTTL != 3*time.Hour {
		t.Errorf("AccessTokenTTL = %v, want 3h (env should win over file)", cfg.MCP.AccessTokenTTL)
	}
}

func TestMCPConfigMaxSessionsValidation(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_SESSIONS", "0")

	_, err := Load(nil, nil)
	if err == nil {
		t.Error("expected error for MaxSessions=0")
	}
}

func TestMCPConfigDCRRateLimitValidation(t *testing.T) {
	t.Setenv("CETACEAN_MCP_DCR_RATE_LIMIT", "0")

	_, err := Load(nil, nil)
	if err == nil {
		t.Error("expected error for DCRRateLimit=0")
	}
}

func TestMCPConfigDCRMaxClientsValidation(t *testing.T) {
	t.Setenv("CETACEAN_MCP_DCR_MAX_CLIENTS", "-1")

	_, err := Load(nil, nil)
	if err == nil {
		t.Error("expected error for DCRMaxClients=-1")
	}
}

func TestMCPConfigOpsLevelInheritDefault(t *testing.T) {
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.MCP.OperationsLevel != OpsInherit {
		t.Errorf("OperationsLevel = %v, want OpsInherit when env unset", cfg.MCP.OperationsLevel)
	}
}

func TestMCPConfigOpsLevelOutOfRange(t *testing.T) {
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "5")

	_, err := Load(nil, nil)
	if err == nil {
		t.Error("expected error for ops level 5")
	}
}

func TestMCPConfigAuthBypass(t *testing.T) {
	t.Setenv("CETACEAN_MCP_AUTH_BYPASS", "client1,client2,client3")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.MCP.AuthBypass) != 3 {
		t.Errorf("AuthBypass len = %d, want 3", len(cfg.MCP.AuthBypass))
	}
	if cfg.MCP.AuthBypass[0] != "client1" {
		t.Errorf("AuthBypass[0] = %q, want client1", cfg.MCP.AuthBypass[0])
	}
}
