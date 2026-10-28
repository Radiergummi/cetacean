package config

import (
	"net/netip"
	"strings"
	"testing"
)

// Each case breaks one rule Validate gathers; the base deployment breaks none.
func TestValidate(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	tests := []struct {
		name    string
		cfg     Config
		auth    AuthConfig
		tls     TLSConfig
		wantErr string
	}{
		{name: "base", cfg: Config{ListenAddr: "127.0.0.1:9000"}, auth: AuthConfig{Mode: "none"}},
		{
			name:    "oauth without an identity",
			cfg:     Config{ListenAddr: "127.0.0.1:9000", OAuth: OAuthConfig{Enabled: true}},
			auth:    AuthConfig{Mode: "none"},
			wantErr: "oauth.enabled needs an auth.mode",
		},
		{
			name:    "half a TLS pair",
			cfg:     Config{ListenAddr: "127.0.0.1:9000"},
			auth:    AuthConfig{Mode: "none"},
			tls:     TLSConfig{Cert: "cert.pem"},
			wantErr: "TLS configuration error: both tls.cert and tls.key",
		},
		{
			name:    "cert mode with nothing to verify a certificate",
			cfg:     Config{ListenAddr: "127.0.0.1:9000"},
			auth:    AuthConfig{Mode: "cert"},
			wantErr: "cert",
		},
		{
			name:    "wildcard CORS where the network is the identity",
			cfg:     Config{ListenAddr: "127.0.0.1:9000", CORSOrigins: []string{"*"}},
			auth:    AuthConfig{Mode: "none"},
			wantErr: "server.cors.origins cannot be",
		},
		{
			name: "oauth with no issuer a client can reach",
			cfg: Config{
				ListenAddr:     ":9000",
				OAuth:          OAuthConfig{Enabled: true},
				TrustedProxies: proxies,
			},
			auth:    AuthConfig{Mode: "headers"},
			wantErr: "issuer clients can reach",
		},
		{
			name:    "headers mode without proxies",
			cfg:     Config{ListenAddr: "127.0.0.1:9000"},
			auth:    AuthConfig{Mode: "headers"},
			wantErr: "server.trusted_proxies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg, tt.auth, tt.tls)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
