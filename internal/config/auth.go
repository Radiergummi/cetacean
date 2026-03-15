package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type AuthConfig struct {
	Mode      string // "none", "oidc", "tailscale", "cert", "headers"
	OIDC      OIDCConfig
	Tailscale TailscaleConfig
	Cert      CertConfig
	Headers   HeadersConfig
}

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	SessionKey   string // hex-encoded 32-byte HMAC key; random per-process if empty
}

type TailscaleConfig struct {
	Mode     string // "local" or "tsnet"
	AuthKey  string
	Hostname string
	StateDir string
}

type CertConfig struct {
	CA string // path to CA bundle
}

type HeadersConfig struct {
	Subject     string
	Name        string
	Email       string
	Groups      string
	SecretHeader string
	SecretValue  string
}

var validModes = map[string]bool{
	"none":      true,
	"oidc":      true,
	"tailscale": true,
	"cert":      true,
	"headers":   true,
}

func LoadAuth() (*AuthConfig, error) {
	cfg := &AuthConfig{
		Mode: envOr("CETACEAN_AUTH_MODE", "none"),
		OIDC: OIDCConfig{
			Issuer:       os.Getenv("CETACEAN_AUTH_OIDC_ISSUER"),
			ClientID:     os.Getenv("CETACEAN_AUTH_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("CETACEAN_AUTH_OIDC_REDIRECT_URL"),
			Scopes:       parseScopes(envOr("CETACEAN_AUTH_OIDC_SCOPES", "openid,profile,email")),
			SessionKey:   os.Getenv("CETACEAN_AUTH_OIDC_SESSION_KEY"),
		},
		Tailscale: TailscaleConfig{
			Mode:     envOr("CETACEAN_AUTH_TAILSCALE_MODE", "local"),
			AuthKey:  os.Getenv("CETACEAN_AUTH_TAILSCALE_AUTHKEY"),
			Hostname: envOr("CETACEAN_AUTH_TAILSCALE_HOSTNAME", "cetacean"),
			StateDir: os.Getenv("CETACEAN_AUTH_TAILSCALE_STATE_DIR"),
		},
		Cert: CertConfig{
			CA: os.Getenv("CETACEAN_AUTH_CERT_CA"),
		},
		Headers: HeadersConfig{
			Subject:      os.Getenv("CETACEAN_AUTH_HEADERS_SUBJECT"),
			Name:         os.Getenv("CETACEAN_AUTH_HEADERS_NAME"),
			Email:        os.Getenv("CETACEAN_AUTH_HEADERS_EMAIL"),
			Groups:       os.Getenv("CETACEAN_AUTH_HEADERS_GROUPS"),
			SecretHeader: os.Getenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER"),
			SecretValue:  os.Getenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE"),
		},
	}

	if !validModes[cfg.Mode] {
		return nil, fmt.Errorf("unknown auth mode %q", cfg.Mode)
	}

	switch cfg.Mode {
	case "oidc":
		if cfg.OIDC.Issuer == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "" || cfg.OIDC.RedirectURL == "" {
			return nil, fmt.Errorf("oidc mode requires CETACEAN_AUTH_OIDC_ISSUER, CETACEAN_AUTH_OIDC_CLIENT_ID, CETACEAN_AUTH_OIDC_CLIENT_SECRET, and CETACEAN_AUTH_OIDC_REDIRECT_URL")
		}
		if err := validateRedirectURL(cfg.OIDC.RedirectURL); err != nil {
			return nil, err
		}
	case "tailscale":
		if cfg.Tailscale.Mode != "local" && cfg.Tailscale.Mode != "tsnet" {
			return nil, fmt.Errorf("tailscale mode must be \"local\" or \"tsnet\", got %q", cfg.Tailscale.Mode)
		}
		if cfg.Tailscale.Mode == "tsnet" && cfg.Tailscale.AuthKey == "" {
			return nil, fmt.Errorf("tailscale tsnet mode requires CETACEAN_AUTH_TAILSCALE_AUTHKEY")
		}
	case "cert":
		if cfg.Cert.CA == "" {
			return nil, fmt.Errorf("cert mode requires CETACEAN_AUTH_CERT_CA")
		}
	case "headers":
		if cfg.Headers.Subject == "" {
			return nil, fmt.Errorf("headers mode requires CETACEAN_AUTH_HEADERS_SUBJECT")
		}
		if cfg.Headers.SecretHeader != "" && cfg.Headers.SecretValue == "" {
			return nil, fmt.Errorf("CETACEAN_AUTH_HEADERS_SECRET_HEADER requires CETACEAN_AUTH_HEADERS_SECRET_VALUE")
		}
	}

	return cfg, nil
}

// validateRedirectURL ensures the redirect URI uses HTTPS per OAuth 2.1 Section 2.3.1.
// Loopback addresses (127.0.0.1, [::1], localhost) are exempt and may use HTTP.
func validateRedirectURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid redirect URL: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return nil
	}
	return fmt.Errorf("CETACEAN_AUTH_OIDC_REDIRECT_URL must use HTTPS (got %q); loopback addresses are exempt per OAuth 2.1", u.Scheme+"://"+u.Host)
}

func parseScopes(s string) []string {
	var scopes []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			scopes = append(scopes, part)
		}
	}
	return scopes
}
