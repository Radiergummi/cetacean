package config

import (
	"os"
	"testing"
)

func TestLoadACL_Defaults(t *testing.T) {
	// Clear any env vars that might interfere.
	for _, key := range []string{
		"CETACEAN_ACL_POLICY",
		"CETACEAN_ACL_POLICY_FILE",
		"CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY",
		"CETACEAN_AUTH_OIDC_ACL_CLAIM",
		"CETACEAN_AUTH_HEADERS_ACL",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	cfg := LoadACL(nil, nil)
	if cfg.Policy != "" {
		t.Errorf("Policy=%q, want empty", cfg.Policy)
	}
	if cfg.PolicyFile != "" {
		t.Errorf("PolicyFile=%q, want empty", cfg.PolicyFile)
	}
	if cfg.TailscaleCapability != "" {
		t.Errorf("TailscaleCapability=%q, want empty", cfg.TailscaleCapability)
	}
	if cfg.OIDCClaim != "" {
		t.Errorf("OIDCClaim=%q, want empty", cfg.OIDCClaim)
	}
	if cfg.HeadersACL != "" {
		t.Errorf("HeadersACL=%q, want empty", cfg.HeadersACL)
	}
}

func TestLoadACL_EnvVars(t *testing.T) {
	t.Setenv("CETACEAN_ACL_POLICY", `{"grants":[]}`)
	t.Setenv("CETACEAN_ACL_POLICY_FILE", "/etc/cetacean/policy.json")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY", "example.com/cap/cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_ACL_CLAIM", "cetacean_grants")
	t.Setenv("CETACEAN_AUTH_HEADERS_ACL", "X-ACL")

	cfg := LoadACL(nil, nil)
	if cfg.Policy != `{"grants":[]}` {
		t.Errorf("Policy=%q", cfg.Policy)
	}
	if cfg.PolicyFile != "/etc/cetacean/policy.json" {
		t.Errorf("PolicyFile=%q", cfg.PolicyFile)
	}
	if cfg.TailscaleCapability != "example.com/cap/cetacean" {
		t.Errorf("TailscaleCapability=%q", cfg.TailscaleCapability)
	}
	if cfg.OIDCClaim != "cetacean_grants" {
		t.Errorf("OIDCClaim=%q", cfg.OIDCClaim)
	}
	if cfg.HeadersACL != "X-ACL" {
		t.Errorf("HeadersACL=%q", cfg.HeadersACL)
	}
}

func TestLoadACL_FlagOverridesEnv(t *testing.T) {
	t.Setenv("CETACEAN_ACL_POLICY", "env-policy")
	t.Setenv("CETACEAN_ACL_POLICY_FILE", "env-file")

	flagPolicy := "flag-policy"
	flagFile := "flag-file"
	flags := &Flags{
		ACLPolicy:     &flagPolicy,
		ACLPolicyFile: &flagFile,
	}

	cfg := LoadACL(flags, nil)
	if cfg.Policy != "flag-policy" {
		t.Errorf("Policy=%q, want flag-policy", cfg.Policy)
	}
	if cfg.PolicyFile != "flag-file" {
		t.Errorf("PolicyFile=%q, want flag-file", cfg.PolicyFile)
	}
}

func TestLoadACL_FileConfig(t *testing.T) {
	// Clear env vars.
	for _, key := range []string{
		"CETACEAN_ACL_POLICY",
		"CETACEAN_ACL_POLICY_FILE",
		"CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY",
		"CETACEAN_AUTH_OIDC_ACL_CLAIM",
		"CETACEAN_AUTH_HEADERS_ACL",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	policy := "file-policy"
	policyFile := "/path/to/policy.yaml"
	tsCap := "ts-cap"
	oidcClaim := "oidc-claim"
	headersACL := "headers-acl"

	fc := &fileConfig{
		ACL: &fileACL{
			Policy:              &policy,
			PolicyFile:          &policyFile,
			TailscaleCapability: &tsCap,
			OIDCClaim:           &oidcClaim,
			HeadersACL:          &headersACL,
		},
	}

	cfg := LoadACL(nil, fc)
	if cfg.Policy != "file-policy" {
		t.Errorf("Policy=%q", cfg.Policy)
	}
	if cfg.PolicyFile != "/path/to/policy.yaml" {
		t.Errorf("PolicyFile=%q", cfg.PolicyFile)
	}
	if cfg.TailscaleCapability != "ts-cap" {
		t.Errorf("TailscaleCapability=%q", cfg.TailscaleCapability)
	}
	if cfg.OIDCClaim != "oidc-claim" {
		t.Errorf("OIDCClaim=%q", cfg.OIDCClaim)
	}
	if cfg.HeadersACL != "headers-acl" {
		t.Errorf("HeadersACL=%q", cfg.HeadersACL)
	}
}
