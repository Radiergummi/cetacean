package config

import (
	"testing"
	"time"
)

func TestExecConfigDefaults(t *testing.T) {
	for _, key := range []string{
		"CETACEAN_EXEC", "CETACEAN_EXEC_IDLE_TIMEOUT", "CETACEAN_EXEC_MAX_DURATION",
		"CETACEAN_EXEC_MAX_SESSIONS", "CETACEAN_EXEC_MAX_SESSIONS_PER_IDENTITY",
	} {
		t.Setenv(key, "")
	}

	cfg, err := loadExec(nil)
	if err != nil {
		t.Fatalf("loadExec: %v", err)
	}

	want := ExecConfig{
		IdleTimeout: 30 * time.Minute, MaxDuration: 4 * time.Hour,
		MaxSessions: 4, MaxSessionsPerIdentity: 2,
	}
	if cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestExecConfigFromEnvAndFile(t *testing.T) {
	t.Setenv("CETACEAN_EXEC", "true")
	t.Setenv("CETACEAN_EXEC_MAX_SESSIONS", "9")

	cfg, err := loadExec(&fileExec{
		IdleTimeout: new("5m"),
		MaxSessions: new(2),
	})
	if err != nil {
		t.Fatalf("loadExec: %v", err)
	}
	if !cfg.Enabled || cfg.MaxSessions != 9 || cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("got %+v", cfg)
	}
}

func TestExecConfigRejectsZeroBounds(t *testing.T) {
	cases := map[string]string{
		"CETACEAN_EXEC_IDLE_TIMEOUT":              "0s",
		"CETACEAN_EXEC_MAX_DURATION":              "0s",
		"CETACEAN_EXEC_MAX_SESSIONS":              "0",
		"CETACEAN_EXEC_MAX_SESSIONS_PER_IDENTITY": "0",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := loadExec(nil); err == nil {
				t.Errorf("%s=%s was accepted", key, value)
			}
		})
	}
}

func TestAuditConfig(t *testing.T) {
	t.Setenv("CETACEAN_AUDIT", "")
	t.Setenv("CETACEAN_AUDIT_OUTPUT", "")

	cfg, err := loadAudit(nil)
	if err != nil {
		t.Fatalf("loadAudit: %v", err)
	}
	if cfg.Enabled || cfg.Output != "stdout" {
		t.Errorf("defaults = %+v", cfg)
	}

	t.Setenv("CETACEAN_AUDIT_OUTPUT", "/var/log/cetacean/audit.jsonl")
	cfg, err = loadAudit(nil)
	if err != nil || cfg.Output != "/var/log/cetacean/audit.jsonl" {
		t.Errorf("file output = %+v, %v", cfg, err)
	}

	t.Setenv("CETACEAN_AUDIT_OUTPUT", "relative/audit.log")
	if _, err := loadAudit(nil); err == nil {
		t.Error("a relative path was accepted")
	}
}

func TestValidateExecRefusesAnonymous(t *testing.T) {
	cfg := &Config{Exec: ExecConfig{Enabled: true}}
	if err := cfg.ValidateExec("none"); err == nil {
		t.Error("exec with auth.mode=none was accepted")
	}
	if err := cfg.ValidateExec("oidc"); err != nil {
		t.Errorf("exec with oidc was refused: %v", err)
	}

	cfg.Exec.Enabled = false
	if err := cfg.ValidateExec("none"); err != nil {
		t.Errorf("disabled exec was refused: %v", err)
	}
}
