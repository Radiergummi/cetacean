package config

import (
	"strings"
	"testing"
)

func TestCheckRemovedEnvNamesEveryVariableAtOnce(t *testing.T) {
	t.Setenv("CETACEAN_MCP_ISSUER", "https://cetacean.example.com")
	t.Setenv("CETACEAN_MCP_CIMD_ENABLED", "true")

	err := checkRemovedEnv()
	if err == nil {
		t.Fatal("a removed variable that is set did not refuse startup")
	}
	for _, want := range []string{
		"CETACEAN_MCP_ISSUER is now CETACEAN_OAUTH_ISSUER",
		"CETACEAN_MCP_CIMD_ENABLED is now CETACEAN_OAUTH_CIMD_ENABLED",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// An orchestrator interpolating an undefined variable sets the name to the
// empty string rather than omitting it, so a deployment that configured
// nothing must still start.
func TestCheckRemovedEnvTreatsEmptyAsUnset(t *testing.T) {
	t.Setenv("CETACEAN_MCP_ISSUER", "")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "")

	if err := checkRemovedEnv(); err != nil {
		t.Errorf("an empty removed variable refused startup: %v", err)
	}
}
