package config

import (
	"testing"
)

func TestParseFlags_NoArgs(t *testing.T) {
	flags, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "" {
		t.Errorf("Config=%s, want empty", flags.Config)
	}
	if flags.Listen != nil {
		t.Error("Listen should be nil when not set")
	}
	if flags.Version {
		t.Error("Version should be false")
	}
}

func TestParseFlags_AllFlags(t *testing.T) {
	args := []string{
		"-config", "cetacean.toml",
		"-listen", ":8080",
		"-docker-host", "tcp://remote:2375",
		"-prometheus-url", "http://prom:9090",
		"-log-level", "debug",
		"-log-format", "text",
		"-auth-mode", "oidc",
		"-pprof",
		"-version",
		"-auth-oidc-issuer", "https://idp.example.com",
		"-auth-oidc-client-id", "my-client",
		"-auth-oidc-client-secret", "my-secret",
		"-auth-oidc-redirect-url", "https://app.example.com/auth/callback",
		"-auth-oidc-scopes", "openid,profile",
		"-auth-oidc-session-key", "abcdef",
		"-auth-tailscale-mode", "tsnet",
		"-auth-tailscale-authkey", "tskey-123",
		"-auth-tailscale-hostname", "myhost",
		"-auth-tailscale-state-dir", "/var/lib/ts",
		"-auth-tailscale-capability", "example.com/cap/cetacean",
		"-auth-cert-ca", "/etc/ca.pem",
		"-auth-headers-subject", "X-User",
		"-auth-headers-name", "X-Name",
		"-auth-headers-email", "X-Email",
		"-auth-headers-groups", "X-Groups",
		"-auth-headers-secret-header", "X-Secret",
		"-auth-headers-secret-value", "s3cret",
		"-auth-headers-trusted-proxies", "10.0.0.0/8",
		"-tls-cert", "/etc/cert.pem",
		"-tls-key", "/etc/key.pem",
	}

	flags, err := ParseFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "cetacean.toml" {
		t.Errorf("Config=%s", flags.Config)
	}
	if flags.Listen == nil || *flags.Listen != ":8080" {
		t.Error("Listen not set correctly")
	}
	if flags.DockerHost == nil || *flags.DockerHost != "tcp://remote:2375" {
		t.Error("DockerHost not set correctly")
	}
	if flags.PrometheusURL == nil || *flags.PrometheusURL != "http://prom:9090" {
		t.Error("PrometheusURL not set correctly")
	}
	if flags.LogLevel == nil || *flags.LogLevel != "debug" {
		t.Error("LogLevel not set correctly")
	}
	if flags.LogFormat == nil || *flags.LogFormat != "text" {
		t.Error("LogFormat not set correctly")
	}
	if flags.AuthMode == nil || *flags.AuthMode != "oidc" {
		t.Error("AuthMode not set correctly")
	}
	if flags.Pprof == nil || *flags.Pprof != true {
		t.Error("Pprof not set correctly")
	}
	if !flags.Version {
		t.Error("Version should be true")
	}
	// OIDC flags
	if flags.OIDCIssuer == nil || *flags.OIDCIssuer != "https://idp.example.com" {
		t.Error("OIDCIssuer not set correctly")
	}
	if flags.OIDCClientID == nil || *flags.OIDCClientID != "my-client" {
		t.Error("OIDCClientID not set correctly")
	}
	if flags.OIDCClientSecret == nil || *flags.OIDCClientSecret != "my-secret" {
		t.Error("OIDCClientSecret not set correctly")
	}
	if flags.OIDCRedirectURL == nil ||
		*flags.OIDCRedirectURL != "https://app.example.com/auth/callback" {
		t.Error("OIDCRedirectURL not set correctly")
	}
	if flags.OIDCScopes == nil || *flags.OIDCScopes != "openid,profile" {
		t.Error("OIDCScopes not set correctly")
	}
	if flags.OIDCSessionKey == nil || *flags.OIDCSessionKey != "abcdef" {
		t.Error("OIDCSessionKey not set correctly")
	}
	// Tailscale flags
	if flags.TailscaleMode == nil || *flags.TailscaleMode != "tsnet" {
		t.Error("TailscaleMode not set correctly")
	}
	if flags.TailscaleAuthKey == nil || *flags.TailscaleAuthKey != "tskey-123" {
		t.Error("TailscaleAuthKey not set correctly")
	}
	if flags.TailscaleHostname == nil || *flags.TailscaleHostname != "myhost" {
		t.Error("TailscaleHostname not set correctly")
	}
	// Cert flags
	if flags.CertCA == nil || *flags.CertCA != "/etc/ca.pem" {
		t.Error("CertCA not set correctly")
	}
	// Headers flags
	if flags.HeadersSubject == nil || *flags.HeadersSubject != "X-User" {
		t.Error("HeadersSubject not set correctly")
	}
	if flags.HeadersSecretValue == nil || *flags.HeadersSecretValue != "s3cret" {
		t.Error("HeadersSecretValue not set correctly")
	}
	if flags.HeadersTrustedProxies == nil || *flags.HeadersTrustedProxies != "10.0.0.0/8" {
		t.Error("HeadersTrustedProxies not set correctly")
	}
	// TLS flags
	if flags.TLSCert == nil || *flags.TLSCert != "/etc/cert.pem" {
		t.Error("TLSCert not set correctly")
	}
	if flags.TLSKey == nil || *flags.TLSKey != "/etc/key.pem" {
		t.Error("TLSKey not set correctly")
	}
}

func TestParseFlags_UnsetFlagsAreNil(t *testing.T) {
	flags, err := ParseFlags([]string{"-listen", ":8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Listen == nil {
		t.Error("Listen should be set")
	}
	if flags.DockerHost != nil {
		t.Error("DockerHost should be nil")
	}
	if flags.PrometheusURL != nil {
		t.Error("PrometheusURL should be nil")
	}
	if flags.Pprof != nil {
		t.Error("Pprof should be nil")
	}
}

func TestParseFlags_ConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_CONFIG", "/etc/cetacean.toml")
	flags, err := ParseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "/etc/cetacean.toml" {
		t.Errorf("Config=%s, want /etc/cetacean.toml", flags.Config)
	}
}

func TestParseFlags_ConfigFlagOverridesEnv(t *testing.T) {
	t.Setenv("CETACEAN_CONFIG", "/etc/cetacean.toml")
	flags, err := ParseFlags([]string{"-config", "/opt/cetacean.toml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Config != "/opt/cetacean.toml" {
		t.Errorf("Config=%s, want /opt/cetacean.toml", flags.Config)
	}
}

func TestParseFlags_InvalidFlag(t *testing.T) {
	_, err := ParseFlags([]string{"-nonexistent"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}
