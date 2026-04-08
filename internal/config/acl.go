package config

// ACLConfig holds configuration for the grant-based RBAC system.
type ACLConfig struct {
	Policy              string // inline policy (JSON, YAML, or TOML)
	PolicyFile          string // path to policy file
	TailscaleCapability string // Tailscale capability key for per-user grants
	OIDCClaim           string // OIDC token claim containing grants
	HeadersACL          string // HTTP header containing grants (JSON)
	Labels              bool   // enable label-based ACL grants
}

// LoadACL loads ACL configuration from flags, env vars, and config file.
func LoadACL(flags *Flags, fc *fileConfig) ACLConfig {
	if flags == nil {
		flags = &Flags{}
	}

	var fPolicy, fPolicyFile *string
	var fTailscaleCap, fOIDCClaim, fHeadersACL *string
	var fLabels *bool
	if fc != nil && fc.ACL != nil {
		fPolicy = fc.ACL.Policy
		fPolicyFile = fc.ACL.PolicyFile
		fTailscaleCap = fc.ACL.TailscaleCapability
		fOIDCClaim = fc.ACL.OIDCClaim
		fHeadersACL = fc.ACL.HeadersACL
		fLabels = fc.ACL.Labels
	}

	return ACLConfig{
		Policy: resolve(flags.ACLPolicy, "CETACEAN_ACL_POLICY", fPolicy, ""),
		PolicyFile: resolve(
			flags.ACLPolicyFile,
			"CETACEAN_ACL_POLICY_FILE",
			fPolicyFile,
			"",
		),
		TailscaleCapability: resolve(
			nil,
			"CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY",
			fTailscaleCap,
			"",
		),
		OIDCClaim:  resolve(nil, "CETACEAN_AUTH_OIDC_ACL_CLAIM", fOIDCClaim, ""),
		HeadersACL: resolve(nil, "CETACEAN_AUTH_HEADERS_ACL", fHeadersACL, ""),
		Labels:     resolveBool(nil, "CETACEAN_ACL_LABELS", fLabels, false),
	}
}
