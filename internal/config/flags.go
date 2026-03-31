package config

import (
	"flag"
	"os"
)

// Flags holds parsed CLI flag values. Pointer fields distinguish
// "not set" (nil) from "set to zero value".
type Flags struct {
	Config          string // path to TOML config file
	Listen          *string
	DockerHost      *string
	PrometheusURL   *string
	LogLevel        *string
	LogFormat       *string
	Pprof           *bool
	SelfMetrics     *bool
	Recommendations *bool
	BasePath        *string
	Version         bool

	// Auth
	AuthMode *string

	// OIDC
	OIDCIssuer       *string
	OIDCClientID     *string
	OIDCClientSecret *string
	OIDCRedirectURL  *string
	OIDCScopes       *string
	OIDCSessionKey   *string

	// Tailscale
	TailscaleMode       *string
	TailscaleAuthKey    *string
	TailscaleHostname   *string
	TailscaleStateDir   *string
	TailscaleCapability *string

	// Cert
	CertCA *string

	// Headers
	HeadersSubject        *string
	HeadersName           *string
	HeadersEmail          *string
	HeadersGroups         *string
	HeadersSecretHeader   *string
	HeadersSecretValue    *string
	HeadersTrustedProxies *string

	// TLS
	TLSCert *string
	TLSKey  *string
}

// ParseFlags parses CLI flags from args (typically os.Args[1:]).
// Only flags that were explicitly set get non-nil pointer values.
func ParseFlags(args []string) (*Flags, error) {
	fs := flag.NewFlagSet("cetacean", flag.ContinueOnError)

	var f Flags

	// -config can also come from CETACEAN_CONFIG env var.
	configDefault := os.Getenv("CETACEAN_CONFIG")
	fs.StringVar(
		&f.Config,
		"config",
		configDefault,
		"Path to TOML config file (env: CETACEAN_CONFIG)",
	)

	listen := fs.String(
		"listen",
		"",
		"Listen address (env: CETACEAN_LISTEN_ADDR, default \":9000\")",
	)
	dockerHost := fs.String("docker-host", "", "Docker socket (env: CETACEAN_DOCKER_HOST)")
	prometheusURL := fs.String(
		"prometheus-url",
		"",
		"Prometheus URL (env: CETACEAN_PROMETHEUS_URL)",
	)
	logLevel := fs.String("log-level", "", "Log level (env: CETACEAN_LOG_LEVEL, default \"info\")")
	logFormat := fs.String(
		"log-format",
		"",
		"Log format (env: CETACEAN_LOG_FORMAT, default \"json\")",
	)
	pprof := fs.Bool("pprof", false, "Enable pprof (env: CETACEAN_PPROF)")
	selfMetrics := fs.Bool(
		"self-metrics",
		false,
		"Enable self-metrics endpoint (env: CETACEAN_SELF_METRICS, default true)",
	)
	recs := fs.Bool(
		"recommendations",
		false,
		"Enable recommendation engine (env: CETACEAN_RECOMMENDATIONS, default true)",
	)
	basePath := fs.String("base-path", "", "URL base path (env: CETACEAN_BASE_PATH)")
	fs.BoolVar(&f.Version, "version", false, "Print version and exit")

	// Auth
	authMode := fs.String("auth-mode", "", "Auth mode (env: CETACEAN_AUTH_MODE, default \"none\")")

	// OIDC
	oidcIssuer := fs.String("auth-oidc-issuer", "", "OIDC issuer URL")
	oidcClientID := fs.String("auth-oidc-client-id", "", "OIDC client ID")
	oidcClientSecret := fs.String("auth-oidc-client-secret", "", "OIDC client secret")
	oidcRedirectURL := fs.String("auth-oidc-redirect-url", "", "OIDC redirect URL")
	oidcScopes := fs.String("auth-oidc-scopes", "", "OIDC scopes (comma-separated)")
	oidcSessionKey := fs.String(
		"auth-oidc-session-key",
		"",
		"OIDC session HMAC key (hex-encoded 32 bytes)",
	)

	// Tailscale
	tsMode := fs.String("auth-tailscale-mode", "", "Tailscale mode: local or tsnet")
	tsAuthKey := fs.String("auth-tailscale-authkey", "", "Tailscale auth key (tsnet mode)")
	tsHostname := fs.String("auth-tailscale-hostname", "", "Tailscale hostname")
	tsStateDir := fs.String(
		"auth-tailscale-state-dir",
		"",
		"Tailscale state directory (tsnet mode)",
	)
	tsCapability := fs.String("auth-tailscale-capability", "", "Tailscale capability for groups")

	// Cert
	certCA := fs.String("auth-cert-ca", "", "CA bundle path for client cert auth")

	// Headers
	hSubject := fs.String("auth-headers-subject", "", "Header name for subject")
	hName := fs.String("auth-headers-name", "", "Header name for display name")
	hEmail := fs.String("auth-headers-email", "", "Header name for email")
	hGroups := fs.String("auth-headers-groups", "", "Header name for groups")
	hSecretHeader := fs.String("auth-headers-secret-header", "", "Header name for shared secret")
	hSecretValue := fs.String("auth-headers-secret-value", "", "Shared secret value")
	hTrustedProxies := fs.String(
		"auth-headers-trusted-proxies",
		"",
		"Trusted proxy CIDRs/IPs (comma-separated)",
	)

	// TLS
	tlsCert := fs.String("tls-cert", "", "TLS certificate path (PEM)")
	tlsKey := fs.String("tls-key", "", "TLS private key path (PEM)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Only set pointer fields for flags that were explicitly provided.
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "listen":
			f.Listen = listen
		case "docker-host":
			f.DockerHost = dockerHost
		case "prometheus-url":
			f.PrometheusURL = prometheusURL
		case "log-level":
			f.LogLevel = logLevel
		case "log-format":
			f.LogFormat = logFormat
		case "pprof":
			f.Pprof = pprof
		case "self-metrics":
			f.SelfMetrics = selfMetrics
		case "recommendations":
			f.Recommendations = recs
		case "base-path":
			f.BasePath = basePath
		case "auth-mode":
			f.AuthMode = authMode
		case "auth-oidc-issuer":
			f.OIDCIssuer = oidcIssuer
		case "auth-oidc-client-id":
			f.OIDCClientID = oidcClientID
		case "auth-oidc-client-secret":
			f.OIDCClientSecret = oidcClientSecret
		case "auth-oidc-redirect-url":
			f.OIDCRedirectURL = oidcRedirectURL
		case "auth-oidc-scopes":
			f.OIDCScopes = oidcScopes
		case "auth-oidc-session-key":
			f.OIDCSessionKey = oidcSessionKey
		case "auth-tailscale-mode":
			f.TailscaleMode = tsMode
		case "auth-tailscale-authkey":
			f.TailscaleAuthKey = tsAuthKey
		case "auth-tailscale-hostname":
			f.TailscaleHostname = tsHostname
		case "auth-tailscale-state-dir":
			f.TailscaleStateDir = tsStateDir
		case "auth-tailscale-capability":
			f.TailscaleCapability = tsCapability
		case "auth-cert-ca":
			f.CertCA = certCA
		case "auth-headers-subject":
			f.HeadersSubject = hSubject
		case "auth-headers-name":
			f.HeadersName = hName
		case "auth-headers-email":
			f.HeadersEmail = hEmail
		case "auth-headers-groups":
			f.HeadersGroups = hGroups
		case "auth-headers-secret-header":
			f.HeadersSecretHeader = hSecretHeader
		case "auth-headers-secret-value":
			f.HeadersSecretValue = hSecretValue
		case "auth-headers-trusted-proxies":
			f.HeadersTrustedProxies = hTrustedProxies
		case "tls-cert":
			f.TLSCert = tlsCert
		case "tls-key":
			f.TLSKey = tlsKey
		}
	})

	return &f, nil
}
