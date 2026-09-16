package config

import (
	"testing"
	"time"
)

// clearMCPEnv unsets every variable loadMCP reads, so a test asserting the file
// or default layer cannot be swayed by the ambient environment. The list tracks
// loadMCP: add a setting there and add it here.
func clearMCPEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"CETACEAN_MCP",
		"CETACEAN_MCP_OPERATIONS_LEVEL",
		"CETACEAN_MCP_MAX_CONCURRENT_TASKS",
		"CETACEAN_MCP_TASK_TTL",
		"CETACEAN_MCP_MAX_TASK_TTL",
		"CETACEAN_MCP_AUTH_BYPASS",
	} {
		t.Setenv(key, "")
	}
}

func TestMCPConfigDefaults(t *testing.T) {
	cfg := DefaultMCPConfig()

	if cfg.Enabled {
		t.Error("MCP should be disabled by default")
	}
	if cfg.OperationsLevel != OpsInherit {
		t.Errorf("operations level = %v, want OpsInherit", cfg.OperationsLevel)
	}
	if cfg.MaxConcurrentTasks != 32 {
		t.Errorf("max concurrent tasks = %d, want 32", cfg.MaxConcurrentTasks)
	}
}

func TestMCPConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_MCP", "true")
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "2")
	t.Setenv("CETACEAN_MCP_AUTH_BYPASS", "cert,headers")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.MCP.Enabled {
		t.Error("MCP should be enabled")
	}
	if cfg.MCP.OperationsLevel != OpsConfiguration {
		t.Errorf("ops level = %v, want OpsConfiguration", cfg.MCP.OperationsLevel)
	}
	if got := cfg.MCP.AuthBypass; len(got) != 2 || got[0] != "cert" || got[1] != "headers" {
		t.Errorf("AuthBypass = %v, want [cert headers]", got)
	}
}

// The bypass moved up out of [mcp.oauth] when that section dissolved; it is
// MCP's, not the authorization server's.
func TestMCPConfigFromFile(t *testing.T) {
	clearMCPEnv(t)

	enabled := true
	opsLevel := 2
	maxTasks := 8
	taskTTL := "20m"
	maxTaskTTL := "2h"

	fc := &fileConfig{
		MCP: &fileMCP{
			Enabled:            &enabled,
			OperationsLevel:    &opsLevel,
			MaxConcurrentTasks: &maxTasks,
			TaskTTL:            &taskTTL,
			MaxTaskTTL:         &maxTaskTTL,
			AuthBypass:         []string{"cert"},
		},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.MCP.Enabled {
		t.Error("MCP.Enabled should be true from file")
	}
	if cfg.MCP.OperationsLevel != OpsConfiguration {
		t.Errorf("OperationsLevel = %v, want OpsConfiguration", cfg.MCP.OperationsLevel)
	}
	if cfg.MCP.MaxConcurrentTasks != 8 {
		t.Errorf("MaxConcurrentTasks = %d, want 8", cfg.MCP.MaxConcurrentTasks)
	}
	if cfg.MCP.TaskTTL != 20*time.Minute {
		t.Errorf("TaskTTL = %v, want 20m", cfg.MCP.TaskTTL)
	}
	if cfg.MCP.MaxTaskTTL != 2*time.Hour {
		t.Errorf("MaxTaskTTL = %v, want 2h", cfg.MCP.MaxTaskTTL)
	}
	if got := cfg.MCP.AuthBypass; len(got) != 1 || got[0] != "cert" {
		t.Errorf("AuthBypass = %v, want [cert]", got)
	}
}

func TestMCPConfigEnvWinsOverFile(t *testing.T) {
	t.Setenv("CETACEAN_MCP_TASK_TTL", "30m")

	fileTTL := "20m"
	fc := &fileConfig{MCP: &fileMCP{TaskTTL: &fileTTL}}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.MCP.TaskTTL != 30*time.Minute {
		t.Errorf("TaskTTL = %v, want 30m (env should win over file)", cfg.MCP.TaskTTL)
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
	t.Setenv("CETACEAN_MCP_AUTH_BYPASS", "cert,headers,tailscale")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.MCP.AuthBypass) != 3 {
		t.Errorf("AuthBypass len = %d, want 3", len(cfg.MCP.AuthBypass))
	}
	if cfg.MCP.AuthBypass[0] != "cert" {
		t.Errorf("AuthBypass[0] = %q, want cert", cfg.MCP.AuthBypass[0])
	}
}

// The bypass hands /mcp to the upstream provider, and /mcp is exempt from
// cross-origin protection on the grounds that it carries no ambient credential.
// A mode that authenticates from a session cookie makes that false, so the rule
// the documentation states is enforced here rather than trusted.
func TestMCPConfigAuthBypassRefusesUnsafeModes(t *testing.T) {
	for _, mode := range []string{"oidc", "none", "not-a-mode"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CETACEAN_MCP_AUTH_BYPASS", mode)

			if _, err := Load(nil, nil); err == nil {
				t.Fatalf("auth_bypass accepted %q", mode)
			}
		})
	}
}

// Every bypassable mode must be one auth.mode actually accepts, or the list
// names a mode no deployment can be running.
func TestBypassableModesAreRealAuthModes(t *testing.T) {
	for _, mode := range bypassableAuthModes {
		if !validModes[mode] {
			t.Errorf("bypassableAuthModes names %q, which is not an auth mode", mode)
		}
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
