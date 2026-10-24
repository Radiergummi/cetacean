package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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
		"CETACEAN_AUTH_HEADERS_ACL_CAPABILITY",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	cfg := mustLoadACL(t, nil, nil)
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
	if cfg.HeadersCapability != "" {
		t.Errorf("HeadersCapability=%q, want empty", cfg.HeadersCapability)
	}
}

func TestLoadACL_EnvVars(t *testing.T) {
	t.Setenv("CETACEAN_ACL_POLICY", `{"grants":[]}`)
	t.Setenv("CETACEAN_ACL_POLICY_FILE", "/etc/cetacean/policy.json")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY", "example.com/cap/cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_ACL_CLAIM", "cetacean_grants")
	t.Setenv("CETACEAN_AUTH_HEADERS_ACL", "X-ACL")
	t.Setenv("CETACEAN_AUTH_HEADERS_ACL_CAPABILITY", "example.com/cap/headers")

	cfg := mustLoadACL(t, nil, nil)
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
	if cfg.HeadersCapability != "example.com/cap/headers" {
		t.Errorf("HeadersCapability=%q", cfg.HeadersCapability)
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

	cfg := mustLoadACL(t, flags, nil)
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
		"CETACEAN_AUTH_HEADERS_ACL_CAPABILITY",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	policy := "file-policy"
	policyFile := "/path/to/policy.yaml"
	tsCap := "ts-cap"
	oidcClaim := "oidc-claim"
	headersACL := "headers-acl"
	headersCap := "headers-cap"

	fc := &fileConfig{
		ACL: &fileACL{
			Policy:              &policy,
			PolicyFile:          &policyFile,
			TailscaleCapability: &tsCap,
			OIDCClaim:           &oidcClaim,
			HeadersACL:          &headersACL,
			HeadersCapability:   &headersCap,
		},
	}

	cfg := mustLoadACL(t, nil, fc)
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
	if cfg.HeadersCapability != "headers-cap" {
		t.Errorf("HeadersCapability=%q", cfg.HeadersCapability)
	}
}

func TestLoadACL_OIDCUserClaims(t *testing.T) {
	unset := func(t *testing.T) {
		t.Setenv("CETACEAN_ACL_OIDC_USER_CLAIMS", "")
		os.Unsetenv("CETACEAN_ACL_OIDC_USER_CLAIMS")
	}

	t.Run("defaults to subject and email", func(t *testing.T) {
		unset(t)

		cfg := mustLoadACL(t, nil, nil)
		if !slices.Equal(cfg.OIDCUserClaims, []string{"sub", "email"}) {
			t.Errorf("OIDCUserClaims=%q", cfg.OIDCUserClaims)
		}
	})

	t.Run("env var is a comma-separated list over the file", func(t *testing.T) {
		t.Setenv("CETACEAN_ACL_OIDC_USER_CLAIMS", " sub ")

		cfg := mustLoadACL(t, nil, &fileConfig{ACL: &fileACL{OIDCUserClaims: []string{"email"}}})
		if !slices.Equal(cfg.OIDCUserClaims, []string{"sub"}) {
			t.Errorf("OIDCUserClaims=%q", cfg.OIDCUserClaims)
		}
	})

	t.Run("file", func(t *testing.T) {
		unset(t)

		cfg := mustLoadACL(t, nil, &fileConfig{ACL: &fileACL{OIDCUserClaims: []string{"sub"}}})
		if !slices.Equal(cfg.OIDCUserClaims, []string{"sub"}) {
			t.Errorf("OIDCUserClaims=%q", cfg.OIDCUserClaims)
		}
	})

	t.Run("refuses an unknown claim", func(t *testing.T) {
		t.Setenv("CETACEAN_ACL_OIDC_USER_CLAIMS", "sub,preferred_username")

		_, err := LoadACL(nil, nil)
		if err == nil || !strings.Contains(err.Error(), "preferred_username") {
			t.Errorf("err = %v, want one naming preferred_username", err)
		}
	})

	t.Run("refuses an empty list", func(t *testing.T) {
		unset(t)

		fc := &fileConfig{ACL: &fileACL{OIDCUserClaims: []string{}}}
		if _, err := LoadACL(nil, fc); err == nil {
			t.Error("an empty list was accepted")
		}
	})

	t.Run("an empty TOML array is set, not absent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cetacean.toml")
		if err := os.WriteFile(path, []byte("[acl]\noidc_user_claims = []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fc, err := LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fc.ACL.OIDCUserClaims == nil {
			t.Fatal("an empty TOML array decoded as unset")
		}
	})
}

func mustLoadACL(t *testing.T, flags *Flags, fc *fileConfig) ACLConfig {
	t.Helper()

	cfg, err := LoadACL(flags, fc)
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}
