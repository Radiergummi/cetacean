package config

import "testing"

func TestTLSConfig_NotEnabled(t *testing.T) {
	cfg := LoadTLS(nil, nil)
	if cfg.Enabled() {
		t.Error("expected TLS not enabled with empty config")
	}
}

func TestTLSConfig_RequiresBoth(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/path/to/cert.pem")
	cfg := LoadTLS(nil, nil)
	if err := ValidateTLS(cfg); err == nil {
		t.Error("expected error when only cert is set")
	}
}

func TestTLSConfig_RequiresBothKeyOnly(t *testing.T) {
	t.Setenv("CETACEAN_TLS_KEY", "/path/to/key.pem")
	cfg := LoadTLS(nil, nil)
	if err := ValidateTLS(cfg); err == nil {
		t.Error("expected error when only key is set")
	}
}

func TestTLSConfig_ValidConfig(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/path/to/cert.pem")
	t.Setenv("CETACEAN_TLS_KEY", "/path/to/key.pem")
	cfg := LoadTLS(nil, nil)
	if !cfg.Enabled() {
		t.Error("expected TLS enabled")
	}
	if err := ValidateTLS(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTLSConfig_FromFile(t *testing.T) {
	fc := &fileConfig{
		TLS: &fileTLS{
			Cert: new("/file/cert.pem"),
			Key:  new("/file/key.pem"),
		},
	}
	cfg := LoadTLS(nil, fc)
	if cfg.Cert != "/file/cert.pem" {
		t.Errorf("cert = %q, want /file/cert.pem", cfg.Cert)
	}
	if cfg.Key != "/file/key.pem" {
		t.Errorf("key = %q, want /file/key.pem", cfg.Key)
	}
}

func TestTLSConfig_EnvOverridesFile(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/env/cert.pem")
	t.Setenv("CETACEAN_TLS_KEY", "/env/key.pem")
	fc := &fileConfig{
		TLS: &fileTLS{
			Cert: new("/file/cert.pem"),
			Key:  new("/file/key.pem"),
		},
	}
	cfg := LoadTLS(nil, fc)
	if cfg.Cert != "/env/cert.pem" {
		t.Errorf("cert = %q, want /env/cert.pem (env should override file)", cfg.Cert)
	}
}

func TestTLSConfig_FromFlags(t *testing.T) {
	flags := &Flags{
		TLSCert: new("/flag/cert.pem"),
		TLSKey:  new("/flag/key.pem"),
	}
	cfg := LoadTLS(flags, nil)
	if cfg.Cert != "/flag/cert.pem" {
		t.Errorf("cert = %q, want /flag/cert.pem", cfg.Cert)
	}
	if cfg.Key != "/flag/key.pem" {
		t.Errorf("key = %q, want /flag/key.pem", cfg.Key)
	}
}

func TestTLSConfig_FlagOverridesEnvAndFile(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "/env/cert.pem")
	t.Setenv("CETACEAN_TLS_KEY", "/env/key.pem")
	flags := &Flags{
		TLSCert: new("/flag/cert.pem"),
		TLSKey:  new("/flag/key.pem"),
	}
	fc := &fileConfig{
		TLS: &fileTLS{
			Cert: new("/file/cert.pem"),
			Key:  new("/file/key.pem"),
		},
	}
	cfg := LoadTLS(flags, fc)
	if cfg.Cert != "/flag/cert.pem" {
		t.Errorf("cert = %q, want /flag/cert.pem (flag should win)", cfg.Cert)
	}
}

func TestTLSConfig_NeitherIsValid(t *testing.T) {
	cfg := TLSConfig{}
	if err := ValidateTLS(cfg); err != nil {
		t.Errorf("unexpected error for empty config: %v", err)
	}
}
