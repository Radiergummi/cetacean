package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// OIDCUserClaims are the claims acl.oidc_user_claims may name. Only claims the
// identity keeps in a field of its own qualify: a token minted from a session
// carries those and drops the rest of the claim set.
var OIDCUserClaims = []string{"sub", "email"}

// ACLConfig holds configuration for the grant-based RBAC system.
type ACLConfig struct {
	Policy              string   // inline policy (JSON, YAML, or TOML)
	PolicyFile          string   // path to policy file
	TailscaleCapability string   // Tailscale capability key for per-user grants
	OIDCClaim           string   // OIDC token claim containing grants
	HeadersACL          string   // HTTP header containing grants (JSON)
	HeadersCapability   string   // key of the grants array when that header is an object
	OIDCUserClaims      []string // identity claims a user: audience matches in oidc mode
}

// LoadACL loads ACL configuration from env vars and the config file; no ACL
// setting has a flag.
func LoadACL(fc *fileConfig) (ACLConfig, error) {
	var fPolicy, fPolicyFile *string
	var fTailscaleCap, fOIDCClaim, fHeadersACL, fHeadersCap *string
	var fUserClaims []string
	if fc != nil && fc.ACL != nil {
		fPolicy = fc.ACL.Policy
		fPolicyFile = fc.ACL.PolicyFile
		fTailscaleCap = fc.ACL.TailscaleCapability
		fOIDCClaim = fc.ACL.OIDCClaim
		fHeadersACL = fc.ACL.HeadersACL
		fHeadersCap = fc.ACL.HeadersCapability
		fUserClaims = fc.ACL.OIDCUserClaims
	}

	userClaims, err := resolveOIDCUserClaims(fUserClaims)
	if err != nil {
		return ACLConfig{}, err
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
		OIDCUserClaims: userClaims,
	}, nil
}

// resolveOIDCUserClaims reads acl.oidc_user_claims. An empty list in the file is
// refused rather than read as unset: it would silently disable every user: grant.
func resolveOIDCUserClaims(file []string) ([]string, error) {
	const envKey = "CETACEAN_ACL_OIDC_USER_CLAIMS"

	claims := file
	if v := os.Getenv(envKey); v != "" {
		claims = splitComma(v)
	} else if file == nil {
		return slices.Clone(OIDCUserClaims), nil
	}

	if len(claims) == 0 {
		return nil, errors.New(
			"acl.oidc_user_claims must name at least one claim; " +
				"leave it unset for the default of sub and email",
		)
	}
	for _, claim := range claims {
		if !slices.Contains(OIDCUserClaims, claim) {
			return nil, fmt.Errorf(
				"acl.oidc_user_claims accepts only %s, got %q",
				strings.Join(OIDCUserClaims, ", "),
				claim,
			)
		}
	}

	return claims, nil
}
