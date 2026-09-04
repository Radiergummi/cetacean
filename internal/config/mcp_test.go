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

	// A remembered approval must outlive the refresh token, or remembering
	// buys nothing: skipping the prompt once the token expires is the point.
	if cfg.ConsentTTL <= cfg.RefreshTokenTTL {
		t.Errorf(
			"consent TTL = %v, want longer than the refresh token TTL %v",
			cfg.ConsentTTL,
			cfg.RefreshTokenTTL,
		)
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
	t.Setenv("CETACEAN_MCP_CONSENT_TTL", "96h")
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "2")
	t.Setenv("CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR", "false")
	t.Setenv("CETACEAN_MCP_DCR_ENABLED", "false")
	t.Setenv("CETACEAN_MCP_DCR_RATE_LIMIT", "25")
	t.Setenv("CETACEAN_MCP_DCR_MAX_CLIENTS", "500")
	t.Setenv("CETACEAN_MCP_CIMD_ENABLED", "false")
	t.Setenv("CETACEAN_MCP_AUTH_BYPASS", "cert,headers")

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
	if cfg.MCP.ConsentTTL != 96*time.Hour {
		t.Errorf("consent TTL = %v, want 96h", cfg.MCP.ConsentTTL)
	}
	if cfg.MCP.OperationsLevel != OpsConfiguration {
		t.Errorf("ops level = %v, want OpsConfiguration", cfg.MCP.OperationsLevel)
	}
	if cfg.MCP.RequireResourceIndicator {
		t.Error("RequireResourceIndicator should be false")
	}
	if cfg.MCP.DCREnabled {
		t.Error("DCREnabled should be false")
	}
	if cfg.MCP.DCRRateLimit != 25 {
		t.Errorf("DCR rate limit = %d, want 25", cfg.MCP.DCRRateLimit)
	}
	if cfg.MCP.DCRMaxClients != 500 {
		t.Errorf("DCR max clients = %d, want 500", cfg.MCP.DCRMaxClients)
	}
	if cfg.MCP.CIMDEnabled {
		t.Error("CIMDEnabled should be false")
	}
	if got := cfg.MCP.AuthBypass; len(got) != 2 || got[0] != "cert" || got[1] != "headers" {
		t.Errorf("AuthBypass = %v, want [cert headers]", got)
	}
}

func TestMCPConfigIssuerOverride(t *testing.T) {
	t.Setenv("CETACEAN_MCP_ISSUER", "https://cetacean.example.com/")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.MCP.Issuer != "https://cetacean.example.com" {
		t.Errorf("Issuer = %q, want trailing slash trimmed", cfg.MCP.Issuer)
	}
}

func TestMCPConfigIssuerInvalid(t *testing.T) {
	cases := map[string]string{
		"bad scheme": "ftp://cetacean.example.com",
		"no host":    "https://",
		"has query":  "https://cetacean.example.com?foo=bar",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CETACEAN_MCP_ISSUER", raw)
			if _, err := Load(nil, nil); err == nil {
				t.Errorf("expected error for %q", raw)
			}
		})
	}
}

func TestMCPConfigIssuerFromFile(t *testing.T) {
	t.Setenv("CETACEAN_MCP_ISSUER", "")

	want := "https://cetacean.example.com"
	fc := &fileConfig{
		MCP: &fileMCP{Issuer: &want},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP.Issuer != want {
		t.Errorf("Issuer = %q, want %q", cfg.MCP.Issuer, want)
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

func TestLoadMCP_MaxConcurrentTasks_Default(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_CONCURRENT_TASKS", "")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxConcurrentTasks != 32 {
		t.Errorf("MaxConcurrentTasks = %d, want 32", cfg.MaxConcurrentTasks)
	}
}

func TestLoadMCP_MaxConcurrentTasks_Env(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_CONCURRENT_TASKS", "8")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxConcurrentTasks != 8 {
		t.Errorf("MaxConcurrentTasks = %d, want 8", cfg.MaxConcurrentTasks)
	}
}

func TestLoadMCP_MaxConcurrentTasks_RejectsZero(t *testing.T) {
	// Zero would wire mcp-go to refuse every task rather than "no limit",
	// which is a confusing way to spell "disabled".
	t.Setenv("CETACEAN_MCP_MAX_CONCURRENT_TASKS", "0")

	if _, err := loadMCP(nil); err == nil {
		t.Error("loadMCP accepted a zero task limit, want an error")
	}
}

func TestLoadMCP_MaxConcurrentTasks_File(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_CONCURRENT_TASKS", "")

	value := 64

	cfg, err := loadMCP(&fileMCP{MaxConcurrentTasks: &value})
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxConcurrentTasks != 64 {
		t.Errorf("MaxConcurrentTasks = %d, want 64", cfg.MaxConcurrentTasks)
	}
}

func TestLoadMCP_TaskTTL_Default(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.TaskTTL != 15*time.Minute {
		t.Errorf("TaskTTL = %v, want 15m", cfg.TaskTTL)
	}
}

func TestLoadMCP_TaskTTL_Env(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "45m")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.TaskTTL != 45*time.Minute {
		t.Errorf("TaskTTL = %v, want 45m", cfg.TaskTTL)
	}
}

// TestLoadMCP_TaskTTL_ZeroDisables — zero is a real setting here, not an
// error: it turns the fill-in off and leaves retention to whatever the client
// asks for.
func TestLoadMCP_TaskTTL_ZeroDisables(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "0")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP rejected a zero task TTL: %v", err)
	}

	if cfg.TaskTTL != 0 {
		t.Errorf("TaskTTL = %v, want 0", cfg.TaskTTL)
	}
}

func TestLoadMCP_TaskTTL_File(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "")

	value := "30m"

	cfg, err := loadMCP(&fileMCP{TaskTTL: &value})
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.TaskTTL != 30*time.Minute {
		t.Errorf("TaskTTL = %v, want 30m", cfg.TaskTTL)
	}
}

func TestLoadMCP_TaskTTL_RejectsNegative(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "-5m")

	if _, err := loadMCP(nil); err == nil {
		t.Error("loadMCP accepted a negative task TTL, want an error")
	}
}

func TestLoadMCP_MaxTaskTTL_Default(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_TASK_TTL", "")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxTaskTTL != time.Hour {
		t.Errorf("MaxTaskTTL = %v, want 1h", cfg.MaxTaskTTL)
	}
}

func TestLoadMCP_MaxTaskTTL_Env(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_TASK_TTL", "6h")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxTaskTTL != 6*time.Hour {
		t.Errorf("MaxTaskTTL = %v, want 6h", cfg.MaxTaskTTL)
	}
}

// TestLoadMCP_MaxTaskTTL_ZeroDisables — zero lifts the ceiling rather than
// pinning every task to nothing.
func TestLoadMCP_MaxTaskTTL_ZeroDisables(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_TASK_TTL", "0")

	cfg, err := loadMCP(nil)
	if err != nil {
		t.Fatalf("loadMCP rejected a zero maximum task TTL: %v", err)
	}

	if cfg.MaxTaskTTL != 0 {
		t.Errorf("MaxTaskTTL = %v, want 0", cfg.MaxTaskTTL)
	}
}

func TestLoadMCP_MaxTaskTTL_File(t *testing.T) {
	t.Setenv("CETACEAN_MCP_MAX_TASK_TTL", "")

	value := "2h"

	cfg, err := loadMCP(&fileMCP{MaxTaskTTL: &value})
	if err != nil {
		t.Fatalf("loadMCP: %v", err)
	}

	if cfg.MaxTaskTTL != 2*time.Hour {
		t.Errorf("MaxTaskTTL = %v, want 2h", cfg.MaxTaskTTL)
	}
}
