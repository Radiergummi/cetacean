package config

import (
	"os"
	"path/filepath"
	"reflect"
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

// fileConfigKeys walks fileConfig's toml tags and returns every dotted key a
// config file may carry, which is exactly what LoadFile decodes.
func fileConfigKeys(t *testing.T, typ reflect.Type, prefix string, into map[string]bool) {
	t.Helper()

	for field := range typ.Fields() {
		tag := field.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}

		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}

		inner := field.Type
		for inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}

		into[key] = true

		if inner.Kind() == reflect.Struct {
			fileConfigKeys(t, inner, key, into)
		}
	}
}

// A replacement nothing decodes would send an operator from a refusal to a
// setting that silently does nothing — worse than the refusal it replaced.
func TestRemovedKeysNameLiveSettings(t *testing.T) {
	keys := map[string]bool{}
	fileConfigKeys(t, reflect.TypeFor[fileConfig](), "", keys)

	for removed, replacement := range removedKeys {
		if !keys[replacement] {
			t.Errorf("%s points at %s, which no config file key decodes", removed, replacement)
		}

		if keys[removed] {
			t.Errorf("%s is still decoded, so it was not removed", removed)
		}
	}
}

func TestUnknownKeyErrorSeparatesMovedFromUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.toml")
	body := "[mcp.oauth]\ncimd_enabled = true\n\n[server]\nlisten_addrs = \":9000\"\n"

	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("a file carrying a moved and an unknown key was accepted")
	}

	for _, want := range []string{
		"mcp.oauth.cimd_enabled is now oauth.cimd_enabled",
		"unknown: server.listen_addrs",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}
