package config

// ACLConfig holds configuration for the grant-based RBAC system.
type ACLConfig struct {
	Policy              string // inline policy (JSON, YAML, or TOML)
	PolicyFile          string // path to policy file
	TailscaleCapability string // Tailscale capability key for per-user grants
	OIDCClaim           string // OIDC token claim containing grants
	HeadersACL          string // HTTP header containing grants (JSON)
	HeadersCapability   string // key of the grants array when that header is an object
}

// LoadACL loads ACL configuration from env vars and the config file; no ACL
// setting has a flag.
func LoadACL(fc *fileConfig) ACLConfig {
	var fPolicy, fPolicyFile *string
	var fTailscaleCap, fOIDCClaim, fHeadersACL, fHeadersCap *string
	if fc != nil && fc.ACL != nil {
		fPolicy = fc.ACL.Policy
		fPolicyFile = fc.ACL.PolicyFile
		fTailscaleCap = fc.ACL.TailscaleCapability
		fOIDCClaim = fc.ACL.OIDCClaim
		fHeadersACL = fc.ACL.HeadersACL
		fHeadersCap = fc.ACL.HeadersCapability
	}

	return ACLConfig{
		Policy:     resolve(nil, "CETACEAN_ACL_POLICY", fPolicy, ""),
		PolicyFile: resolve(nil, "CETACEAN_ACL_POLICY_FILE", fPolicyFile, ""),
		TailscaleCapability: resolve(
			nil,
			"CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY",
			fTailscaleCap,
			"",
		),
		OIDCClaim:  resolve(nil, "CETACEAN_AUTH_OIDC_ACL_CLAIM", fOIDCClaim, ""),
		HeadersACL: resolve(nil, "CETACEAN_AUTH_HEADERS_ACL", fHeadersACL, ""),
		HeadersCapability: resolve(
			nil,
			"CETACEAN_AUTH_HEADERS_ACL_CAPABILITY",
			fHeadersCap,
			"",
		),
	}
}
