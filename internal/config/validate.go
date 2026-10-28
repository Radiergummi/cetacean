package config

import "fmt"

// Validate refuses the deployments no single loader can see are broken, since
// each rule weighs one section against another: the auth mode against OAuth,
// TLS, CORS and the trusted proxies.
func Validate(cfg *Config, authCfg AuthConfig, tlsCfg TLSConfig) error {
	if err := cfg.ValidateOAuth(authCfg.Mode); err != nil {
		return err
	}

	if err := ValidateTLS(tlsCfg); err != nil {
		return fmt.Errorf("TLS configuration error: %w", err)
	}

	if authCfg.Mode == "cert" {
		err := ValidateCertMode(tlsCfg.Enabled(), authCfg.Cert.CA, cfg.TrustedProxies)
		if err != nil {
			return err
		}
	}

	if err := RefuseWildcardCORS(cfg.CORSOrigins, authCfg.Mode); err != nil {
		return err
	}

	if authCfg.Mode == "tailscale" && authCfg.Tailscale.Mode == "tsnet" {
		if err := cfg.ValidateTsnetMode(tlsCfg.Enabled()); err != nil {
			return err
		}
	}

	if issuer, reachable := cfg.OAuthIssuer(tlsCfg.Enabled()); cfg.OAuth.Enabled && !reachable {
		return fmt.Errorf(
			"the OAuth server needs an issuer clients can reach, and none could be derived "+
				"from server.listen_addr %q (derived %q). Set server.public_url to the URL "+
				"clients reach from outside, or oauth.issuer to override it",
			cfg.ListenAddr, issuer,
		)
	}

	if authCfg.Mode == "headers" {
		return RequireTrustedProxies(cfg.TrustedProxies)
	}

	return nil
}
