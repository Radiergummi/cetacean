package config

import (
	"errors"
	"path/filepath"
	"time"
)

// ExecConfig holds the [exec] section: interactive shells in containers.
type ExecConfig struct {
	Enabled                bool
	IdleTimeout            time.Duration
	MaxDuration            time.Duration
	MaxSessions            int
	MaxSessionsPerIdentity int
}

// AuditConfig holds the [audit] section. Output is "stdout" or an absolute path.
type AuditConfig struct {
	Enabled bool
	Output  string
}

func loadExec(fe *fileExec) (ExecConfig, error) {
	var (
		fEnabled     *bool
		fIdle, fMax  *string
		fSessions    *int
		fPerIdentity *int
	)
	if fe != nil {
		fEnabled, fIdle, fMax = fe.Enabled, fe.IdleTimeout, fe.MaxDuration
		fSessions, fPerIdentity = fe.MaxSessions, fe.MaxSessionsPerIdentity
	}

	idle, err := resolveDuration(nil, "CETACEAN_EXEC_IDLE_TIMEOUT", fIdle, 30*time.Minute, false)
	if err != nil {
		return ExecConfig{}, err
	}

	maxDuration, err := resolveDuration(nil, "CETACEAN_EXEC_MAX_DURATION", fMax, 4*time.Hour, false)
	if err != nil {
		return ExecConfig{}, err
	}

	sessions, err := resolveInt(nil, "CETACEAN_EXEC_MAX_SESSIONS", fSessions, 4, 1, 1024)
	if err != nil {
		return ExecConfig{}, err
	}

	perIdentity, err := resolveInt(
		nil, "CETACEAN_EXEC_MAX_SESSIONS_PER_IDENTITY", fPerIdentity, 2, 1, 1024,
	)
	if err != nil {
		return ExecConfig{}, err
	}

	enabled, err := resolveBool(nil, "CETACEAN_EXEC", fEnabled, false)
	if err != nil {
		return ExecConfig{}, err
	}

	return ExecConfig{
		Enabled:                enabled,
		IdleTimeout:            idle,
		MaxDuration:            maxDuration,
		MaxSessions:            sessions,
		MaxSessionsPerIdentity: perIdentity,
	}, nil
}

func loadAudit(fa *fileAudit) (AuditConfig, error) {
	var (
		fEnabled *bool
		fOutput  *string
	)
	if fa != nil {
		fEnabled, fOutput = fa.Enabled, fa.Output
	}

	output := resolve(nil, "CETACEAN_AUDIT_OUTPUT", fOutput, "stdout")
	if output != "stdout" && !filepath.IsAbs(output) {
		return AuditConfig{}, errors.New(
			`audit.output must be "stdout" or an absolute path`,
		)
	}

	enabled, err := resolveBool(nil, "CETACEAN_AUDIT", fEnabled, false)
	if err != nil {
		return AuditConfig{}, err
	}

	return AuditConfig{
		Enabled: enabled,
		Output:  output,
	}, nil
}

// ValidateExec refuses shells nobody would be accountable for: without an
// auth mode every caller is the same anonymous identity.
func (c *Config) ValidateExec(authMode string) error {
	if c.Exec.Enabled && authMode == "none" {
		return errors.New(
			"exec.enabled requires an auth mode other than none: a shell must belong to someone",
		)
	}

	return nil
}
