package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAuth_DefaultsToNone(t *testing.T) {
	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != "none" {
		t.Errorf("expected mode \"none\", got %q", cfg.Mode)
	}
}

func TestLoadAuth_DefaultScopes(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://app.example.com/auth/callback")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"openid", "profile", "email"}
	if len(cfg.OIDC.Scopes) != len(want) {
		t.Fatalf("expected %d scopes, got %d", len(want), len(cfg.OIDC.Scopes))
	}
	for i, s := range want {
		if cfg.OIDC.Scopes[i] != s {
			t.Errorf("scope[%d]: expected %q, got %q", i, s, cfg.OIDC.Scopes[i])
		}
	}
}

func TestLoadAuth_InvalidMode(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "bogus")
	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestLoadAuth_OIDCRequiresFields(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for missing OIDC fields")
	}
}

func TestLoadAuth_OIDCHappyPath(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://app.example.com/auth/callback")
	t.Setenv("CETACEAN_AUTH_OIDC_SCOPES", "openid, custom")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://issuer.example.com" {
		t.Errorf("unexpected issuer: %q", cfg.OIDC.Issuer)
	}
	if len(cfg.OIDC.Scopes) != 2 || cfg.OIDC.Scopes[0] != "openid" ||
		cfg.OIDC.Scopes[1] != "custom" {
		t.Errorf("unexpected scopes: %v", cfg.OIDC.Scopes)
	}
}

func TestLoadAuth_OIDCRejectsHTTPRedirectURL(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "http://app.example.com/auth/callback")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for HTTP redirect URL")
	}
}

func TestLoadAuth_OIDCAllowsLocalhostHTTP(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "http://localhost/auth/callback")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.RedirectURL != "http://localhost/auth/callback" {
		t.Errorf("unexpected redirect URL: %q", cfg.OIDC.RedirectURL)
	}
}

func TestLoadAuth_OIDCAllows127001HTTP(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "http://127.0.0.1:8080/auth/callback")

	_, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error for loopback HTTP: %v", err)
	}
}

func TestLoadAuth_OIDCSessionKey(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://app.example.com/auth/callback")
	t.Setenv(
		"CETACEAN_AUTH_OIDC_SESSION_KEY",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.SessionKey == "" {
		t.Error("expected SessionKey to be set")
	}
}

func TestLoadAuth_TailscaleLocalDefault(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tailscale.Mode != "local" {
		t.Errorf("expected tailscale mode \"local\", got %q", cfg.Tailscale.Mode)
	}
	if cfg.Tailscale.Hostname != "cetacean" {
		t.Errorf("expected hostname \"cetacean\", got %q", cfg.Tailscale.Hostname)
	}
}

func TestLoadAuth_TailscaleInvalidMode(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_MODE", "invalid")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid tailscale mode")
	}
}

func TestLoadAuth_TailscaleTsnetRequiresAuthKey(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_MODE", "tsnet")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for tsnet without auth key")
	}
}

func TestLoadAuth_TailscaleTsnetHappyPath(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_MODE", "tsnet")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_AUTHKEY", "tskey-abc123")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tailscale.AuthKey != "tskey-abc123" {
		t.Errorf("unexpected auth key: %q", cfg.Tailscale.AuthKey)
	}
}

func TestLoadAuth_CertRequiresCA(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "cert")
	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for missing CA")
	}
}

func TestLoadAuth_CertHappyPath(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "cert")
	t.Setenv("CETACEAN_AUTH_CERT_CA", "/path/to/ca.pem")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cert.CA != "/path/to/ca.pem" {
		t.Errorf("unexpected CA: %q", cfg.Cert.CA)
	}
}

func TestLoadAuth_HeadersRequiresSubject(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for missing subject header")
	}
}

func TestLoadAuth_HeadersSecretRequiresValue(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Secret")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for secret header without value")
	}
}

func TestLoadAuth_HeadersHappyPath(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_NAME", "X-Name")
	t.Setenv("CETACEAN_AUTH_HEADERS_EMAIL", "X-Email")
	t.Setenv("CETACEAN_AUTH_HEADERS_GROUPS", "X-Groups")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Secret")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE", "s3cret")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "10.0.0.0/8")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Headers.Subject != "X-User" {
		t.Errorf("unexpected subject: %q", cfg.Headers.Subject)
	}
	if cfg.Headers.SecretValue != "s3cret" {
		t.Errorf("unexpected secret value: %q", cfg.Headers.SecretValue)
	}
}

func TestLoadAuth_HeadersRequiresTrustedProxies(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	// Secret set but no trusted proxies — should still fail.
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Proxy-Secret")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE", "s3cret")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error when trusted proxies are not set")
	}
	if !strings.Contains(err.Error(), "CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadAuth_HeadersTrustedProxiesOnly(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.1")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Headers.TrustedProxies) != 2 {
		t.Fatalf("expected 2 trusted proxies, got %d", len(cfg.Headers.TrustedProxies))
	}
	if cfg.Headers.TrustedProxies[0].String() != "10.0.0.0/8" {
		t.Errorf("proxy[0] = %s, want 10.0.0.0/8", cfg.Headers.TrustedProxies[0])
	}
	// Bare IP should be converted to /32.
	if cfg.Headers.TrustedProxies[1].String() != "192.168.1.1/32" {
		t.Errorf("proxy[1] = %s, want 192.168.1.1/32", cfg.Headers.TrustedProxies[1])
	}
}

func TestLoadAuth_HeadersTrustedProxiesIPv6(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "fd00::/8, ::1")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Headers.TrustedProxies) != 2 {
		t.Fatalf("expected 2 trusted proxies, got %d", len(cfg.Headers.TrustedProxies))
	}
}

func TestLoadAuth_HeadersTrustedProxiesInvalid(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "not-an-ip")

	_, err := LoadAuth(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid trusted proxy")
	}
	if !strings.Contains(err.Error(), "not a valid CIDR or IP address") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadAuth_HeadersBothSecretAndTrustedProxies(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Secret")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE", "s3cret")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "10.0.0.0/8")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Headers.SecretValue != "s3cret" {
		t.Errorf("unexpected secret: %q", cfg.Headers.SecretValue)
	}
	if len(cfg.Headers.TrustedProxies) != 1 {
		t.Errorf("expected 1 trusted proxy, got %d", len(cfg.Headers.TrustedProxies))
	}
}

func TestLoadAuth_FromConfigFile(t *testing.T) {
	fc := &fileConfig{
		Auth: &fileAuth{
			Mode: new("oidc"),
			OIDC: &fileAuthOIDC{
				Issuer:       new("https://file-issuer.example.com"),
				ClientID:     new("file-client"),
				ClientSecret: new("file-secret"),
				RedirectURL:  new("https://file-app.example.com/auth/callback"),
				Scopes:       new("openid,custom"),
			},
		},
	}

	cfg, err := LoadAuth(nil, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != "oidc" {
		t.Errorf("mode = %q, want oidc", cfg.Mode)
	}
	if cfg.OIDC.Issuer != "https://file-issuer.example.com" {
		t.Errorf("issuer = %q, want file-issuer", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "file-client" {
		t.Errorf("client_id = %q, want file-client", cfg.OIDC.ClientID)
	}
	if len(cfg.OIDC.Scopes) != 2 || cfg.OIDC.Scopes[1] != "custom" {
		t.Errorf("scopes = %v, want [openid custom]", cfg.OIDC.Scopes)
	}
}

func TestLoadAuth_EnvOverridesFile(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://env-issuer.example.com")

	fc := &fileConfig{
		Auth: &fileAuth{
			Mode: new("oidc"),
			OIDC: &fileAuthOIDC{
				Issuer:       new("https://file-issuer.example.com"),
				ClientID:     new("file-client"),
				ClientSecret: new("file-secret"),
				RedirectURL:  new("https://app.example.com/auth/callback"),
			},
		},
	}

	cfg, err := LoadAuth(nil, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://env-issuer.example.com" {
		t.Errorf("issuer = %q, want env-issuer (env should override file)", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "file-client" {
		t.Errorf(
			"client_id = %q, want file-client (should fall through to file)",
			cfg.OIDC.ClientID,
		)
	}
}

func TestLoadAuth_FlagOverridesEnvAndFile(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")

	fc := &fileConfig{
		Auth: &fileAuth{Mode: new("cert")},
	}
	flags := &Flags{AuthMode: new("none")}

	cfg, err := LoadAuth(flags, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != "none" {
		t.Errorf("mode = %q, want none (flag should win)", cfg.Mode)
	}
}

func TestLoadAuth_HeadersFromFile(t *testing.T) {
	fc := &fileConfig{
		Auth: &fileAuth{
			Mode: new("headers"),
			Headers: &fileAuthHeaders{
				Subject:        new("X-User"),
				SecretHeader:   new("X-Secret"),
				SecretValue:    new("s3cret"),
				TrustedProxies: new("10.0.0.0/8"),
			},
		},
	}

	cfg, err := LoadAuth(nil, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Headers.Subject != "X-User" {
		t.Errorf("subject = %q, want X-User", cfg.Headers.Subject)
	}
	if cfg.Headers.SecretValue != "s3cret" {
		t.Errorf("secret_value = %q, want s3cret", cfg.Headers.SecretValue)
	}
	if len(cfg.Headers.TrustedProxies) != 1 {
		t.Fatalf("expected 1 trusted proxy, got %d", len(cfg.Headers.TrustedProxies))
	}
}

func TestLoadAuth_TailscaleFromFile(t *testing.T) {
	fc := &fileConfig{
		Auth: &fileAuth{
			Mode: new("tailscale"),
			Tailscale: &fileAuthTS{
				Mode:    new("tsnet"),
				AuthKey: new("tskey-abc"),
			},
		},
	}

	cfg, err := LoadAuth(nil, fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tailscale.Mode != "tsnet" {
		t.Errorf("tailscale mode = %q, want tsnet", cfg.Tailscale.Mode)
	}
	if cfg.Tailscale.AuthKey != "tskey-abc" {
		t.Errorf("tailscale authkey = %q, want tskey-abc", cfg.Tailscale.AuthKey)
	}
}

func TestLoadAuth_OIDCSecretFromFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "client_secret")
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "client-id")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET_FILE", secretPath)
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://app.example.com/auth/callback")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.ClientSecret != "file-secret" {
		t.Errorf("client secret = %q, want file-secret", cfg.OIDC.ClientSecret)
	}
}

func TestLoadAuth_TailscaleAuthKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ts_authkey")
	if err := os.WriteFile(keyPath, []byte("tskey-from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_MODE", "tsnet")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_AUTHKEY_FILE", keyPath)

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tailscale.AuthKey != "tskey-from-file" {
		t.Errorf("auth key = %q, want tskey-from-file", cfg.Tailscale.AuthKey)
	}
}

func TestLoadAuth_HeadersSecretFromFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "proxy_secret")
	if err := os.WriteFile(secretPath, []byte("proxy-s3cret"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Secret")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE_FILE", secretPath)
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "10.0.0.0/8")

	cfg, err := LoadAuth(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Headers.SecretValue != "proxy-s3cret" {
		t.Errorf("secret value = %q, want proxy-s3cret", cfg.Headers.SecretValue)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"10.0.0.1", 1, false},
		{"10.0.0.0/8", 1, false},
		{"10.0.0.1, 172.16.0.0/12, 192.168.1.1", 3, false},
		{"::1", 1, false},
		{"fd00::/8", 1, false},
		{" 10.0.0.1 , 10.0.0.2 ", 2, false},
		{"10.0.0.1,,10.0.0.2", 2, false},
		{"", 0, false},
		{",,,", 0, false},
		{"not-valid", 0, true},
		{"10.0.0.1, bad, 10.0.0.2", 0, true},
	}
	for _, tt := range tests {
		prefixes, err := parseTrustedProxies(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseTrustedProxies(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if len(prefixes) != tt.want {
			t.Errorf(
				"parseTrustedProxies(%q) = %d prefixes, want %d",
				tt.input,
				len(prefixes),
				tt.want,
			)
		}
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"openid,profile,email", []string{"openid", "profile", "email"}},
		{" openid , profile , email ", []string{"openid", "profile", "email"}},
		{"openid,,email", []string{"openid", "email"}},
		{"", nil},
		{",,,", nil},
	}
	for _, tt := range tests {
		got := parseScopes(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseScopes(%q): got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseScopes(%q)[%d]: got %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
