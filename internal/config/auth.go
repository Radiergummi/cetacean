package config

import (
	"fmt"
	"net/netip"
	"net/url"
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
	Mode       string // "local" or "tsnet"
	AuthKey    string
	Hostname   string
	StateDir   string
	Capability string // app capability name for groups (e.g. "example.com/cap/cetacean")
}

type CertConfig struct {
	CA string // path to CA bundle
}

type HeadersConfig struct {
	Subject        string
	Name           string
	Email          string
	Groups         string
	SecretHeader   string
	SecretValue    string
	TrustedProxies []netip.Prefix // parsed from comma-separated CIDRs/IPs
}

var validModes = map[string]bool{
	"none":      true,
	"oidc":      true,
	"tailscale": true,
	"cert":      true,
	"headers":   true,
}

func LoadAuth(flags *Flags, fc *fileConfig) (*AuthConfig, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// Extract file-level pointers (safely handle nil sub-structs).
	var fa *fileAuth
	if fc != nil {
		fa = fc.Auth
	}

	mode := resolve(
		flags.AuthMode,
		"CETACEAN_AUTH_MODE",
		fileField(fa, func(a *fileAuth) *string { return a.Mode }),
		"none",
	)
	if !validModes[mode] {
		return nil, fmt.Errorf("unknown auth mode %q", mode)
	}

	cfg := &AuthConfig{Mode: mode}

	// Resolve only the active mode's settings. Secrets use resolveSecret
	// to support _FILE env var variants (e.g. Docker Swarm secrets).
	switch mode {
	case "oidc":
		fo := fileOIDC(fa)
		clientSecret, err := resolveSecret(
			flags.OIDCClientSecret,
			"CETACEAN_AUTH_OIDC_CLIENT_SECRET",
			fileField(fo, func(o *fileAuthOIDC) *string { return o.ClientSecret }),
			"",
		)
		if err != nil {
			return nil, err
		}
		sessionKey, err := resolveSecret(
			flags.OIDCSessionKey,
			"CETACEAN_AUTH_OIDC_SESSION_KEY",
			fileField(fo, func(o *fileAuthOIDC) *string { return o.SessionKey }),
			"",
		)
		if err != nil {
			return nil, err
		}
		cfg.OIDC = OIDCConfig{
			Issuer: resolve(
				flags.OIDCIssuer,
				"CETACEAN_AUTH_OIDC_ISSUER",
				fileField(fo, func(o *fileAuthOIDC) *string { return o.Issuer }),
				"",
			),
			ClientID: resolve(
				flags.OIDCClientID,
				"CETACEAN_AUTH_OIDC_CLIENT_ID",
				fileField(fo, func(o *fileAuthOIDC) *string { return o.ClientID }),
				"",
			),
			ClientSecret: clientSecret,
			RedirectURL: resolve(
				flags.OIDCRedirectURL,
				"CETACEAN_AUTH_OIDC_REDIRECT_URL",
				fileField(fo, func(o *fileAuthOIDC) *string { return o.RedirectURL }),
				"",
			),
			Scopes: parseScopes(
				resolve(
					flags.OIDCScopes,
					"CETACEAN_AUTH_OIDC_SCOPES",
					fileField(fo, func(o *fileAuthOIDC) *string { return o.Scopes }),
					"openid,profile,email",
				),
			),
			SessionKey: sessionKey,
		}
		if cfg.OIDC.Issuer == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "" ||
			cfg.OIDC.RedirectURL == "" {
			return nil, fmt.Errorf(
				"oidc mode requires CETACEAN_AUTH_OIDC_ISSUER, CETACEAN_AUTH_OIDC_CLIENT_ID, CETACEAN_AUTH_OIDC_CLIENT_SECRET, and CETACEAN_AUTH_OIDC_REDIRECT_URL",
			)
		}
		if err := validateRedirectURL(cfg.OIDC.RedirectURL); err != nil {
			return nil, err
		}

	case "tailscale":
		ft := fileTailscale(fa)
		authKey, err := resolveSecret(
			flags.TailscaleAuthKey,
			"CETACEAN_AUTH_TAILSCALE_AUTHKEY",
			fileField(ft, func(t *fileAuthTS) *string { return t.AuthKey }),
			"",
		)
		if err != nil {
			return nil, err
		}
		cfg.Tailscale = TailscaleConfig{
			Mode: resolve(
				flags.TailscaleMode,
				"CETACEAN_AUTH_TAILSCALE_MODE",
				fileField(ft, func(t *fileAuthTS) *string { return t.Mode }),
				"local",
			),
			AuthKey: authKey,
			Hostname: resolve(
				flags.TailscaleHostname,
				"CETACEAN_AUTH_TAILSCALE_HOSTNAME",
				fileField(ft, func(t *fileAuthTS) *string { return t.Hostname }),
				"cetacean",
			),
			StateDir: resolve(
				flags.TailscaleStateDir,
				"CETACEAN_AUTH_TAILSCALE_STATE_DIR",
				fileField(ft, func(t *fileAuthTS) *string { return t.StateDir }),
				"",
			),
			Capability: resolve(
				flags.TailscaleCapability,
				"CETACEAN_AUTH_TAILSCALE_CAPABILITY",
				fileField(ft, func(t *fileAuthTS) *string { return t.Capability }),
				"",
			),
		}
		if cfg.Tailscale.Mode != "local" && cfg.Tailscale.Mode != "tsnet" {
			return nil, fmt.Errorf(
				"tailscale mode must be \"local\" or \"tsnet\", got %q",
				cfg.Tailscale.Mode,
			)
		}
		if cfg.Tailscale.Mode == "tsnet" && cfg.Tailscale.AuthKey == "" {
			return nil, fmt.Errorf("tailscale tsnet mode requires CETACEAN_AUTH_TAILSCALE_AUTHKEY")
		}

	case "cert":
		fc := fileCert(fa)
		cfg.Cert = CertConfig{
			CA: resolve(
				flags.CertCA,
				"CETACEAN_AUTH_CERT_CA",
				fileField(fc, func(c *fileAuthCert) *string { return c.CA }),
				"",
			),
		}
		if cfg.Cert.CA == "" {
			return nil, fmt.Errorf("cert mode requires CETACEAN_AUTH_CERT_CA")
		}

	case "headers":
		fh := fileHeaders(fa)
		secretValue, err := resolveSecret(
			flags.HeadersSecretValue,
			"CETACEAN_AUTH_HEADERS_SECRET_VALUE",
			fileField(fh, func(h *fileAuthHeaders) *string { return h.SecretValue }),
			"",
		)
		if err != nil {
			return nil, err
		}
		trustedProxies, err := parseTrustedProxies(
			resolve(
				flags.HeadersTrustedProxies,
				"CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.TrustedProxies }),
				"",
			),
		)
		if err != nil {
			return nil, err
		}
		cfg.Headers = HeadersConfig{
			Subject: resolve(
				flags.HeadersSubject,
				"CETACEAN_AUTH_HEADERS_SUBJECT",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.Subject }),
				"",
			),
			Name: resolve(
				flags.HeadersName,
				"CETACEAN_AUTH_HEADERS_NAME",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.Name }),
				"",
			),
			Email: resolve(
				flags.HeadersEmail,
				"CETACEAN_AUTH_HEADERS_EMAIL",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.Email }),
				"",
			),
			Groups: resolve(
				flags.HeadersGroups,
				"CETACEAN_AUTH_HEADERS_GROUPS",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.Groups }),
				"",
			),
			SecretHeader: resolve(
				flags.HeadersSecretHeader,
				"CETACEAN_AUTH_HEADERS_SECRET_HEADER",
				fileField(fh, func(h *fileAuthHeaders) *string { return h.SecretHeader }),
				"",
			),
			SecretValue:    secretValue,
			TrustedProxies: trustedProxies,
		}
		if cfg.Headers.Subject == "" {
			return nil, fmt.Errorf("headers mode requires CETACEAN_AUTH_HEADERS_SUBJECT")
		}
		if cfg.Headers.SecretHeader != "" && cfg.Headers.SecretValue == "" {
			return nil, fmt.Errorf(
				"CETACEAN_AUTH_HEADERS_SECRET_HEADER requires CETACEAN_AUTH_HEADERS_SECRET_VALUE",
			)
		}
		if cfg.Headers.SecretHeader == "" && len(cfg.Headers.TrustedProxies) == 0 {
			return nil, fmt.Errorf(
				"headers mode requires CETACEAN_AUTH_HEADERS_SECRET_HEADER or CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES (or both); without either, any client can spoof identity headers",
			)
		}
	}

	return cfg, nil
}

// fileField safely extracts a pointer field from a nil-able file config sub-struct.
func fileField[T any](s *T, f func(*T) *string) *string {
	if s == nil {
		return nil
	}
	return f(s)
}

func fileOIDC(fa *fileAuth) *fileAuthOIDC {
	if fa == nil {
		return nil
	}
	return fa.OIDC
}

func fileTailscale(fa *fileAuth) *fileAuthTS {
	if fa == nil {
		return nil
	}
	return fa.Tailscale
}

func fileCert(fa *fileAuth) *fileAuthCert {
	if fa == nil {
		return nil
	}
	return fa.Cert
}

func fileHeaders(fa *fileAuth) *fileAuthHeaders {
	if fa == nil {
		return nil
	}
	return fa.Headers
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
	return fmt.Errorf(
		"CETACEAN_AUTH_OIDC_REDIRECT_URL must use HTTPS (got %q); loopback addresses are exempt per OAuth 2.1",
		u.Scheme+"://"+u.Host,
	)
}

// parseTrustedProxies parses a comma-separated list of CIDRs and/or IP
// addresses into netip.Prefix values. Bare IPs are converted to single-host
// prefixes (e.g. "10.0.0.1" → "10.0.0.1/32").
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for s := range strings.SplitSeq(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		// Try as CIDR first.
		if prefix, err := netip.ParsePrefix(s); err == nil {
			prefixes = append(prefixes, prefix)
			continue
		}

		// Try as bare IP → single-host prefix.
		if addr, err := netip.ParseAddr(s); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}

		return nil, fmt.Errorf(
			"CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES: %q is not a valid CIDR or IP address",
			s,
		)
	}
	return prefixes, nil
}

func parseScopes(s string) []string {
	var scopes []string
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			scopes = append(scopes, part)
		}
	}
	return scopes
}
