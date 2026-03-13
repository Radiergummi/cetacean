package config

import "testing"

func TestTLSConfig_NotEnabled(t *testing.T) {
	cfg := LoadTLS()
	if cfg.Enabled() {
		t.Error("expected TLS not enabled with empty config")
	}
}

func TestTLSConfig_RequiresBoth(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/path/to/cert.pem")
	cfg := LoadTLS()
	if err := ValidateTLS(cfg); err == nil {
		t.Error("expected error when only cert is set")
	}
}

func TestTLSConfig_RequiresBothKeyOnly(t *testing.T) {
	t.Setenv("CETACEAN_TLS_KEY", "/path/to/key.pem")
	cfg := LoadTLS()
	if err := ValidateTLS(cfg); err == nil {
		t.Error("expected error when only key is set")
	}
}

func TestTLSConfig_ValidConfig(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/path/to/cert.pem")
	t.Setenv("CETACEAN_TLS_KEY", "/path/to/key.pem")
	cfg := LoadTLS()
	if !cfg.Enabled() {
		t.Error("expected TLS enabled")
	}
	if err := ValidateTLS(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTLSConfig_NeitherIsValid(t *testing.T) {
	cfg := TLSConfig{}
	if err := ValidateTLS(cfg); err != nil {
		t.Errorf("unexpected error for empty config: %v", err)
	}
}
