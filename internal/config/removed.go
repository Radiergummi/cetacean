package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// removedEnv maps a variable Cetacean no longer reads to the one that replaced
// it. Nothing reads the old names, so a deployment still setting one gets the
// default instead of what it wrote, and several of these switch a capability
// off where the default is on.
//
// removedKeys is the config file's half of the same rule. Each entry in either
// can go once no deployment predates its rename.
var removedEnv = map[string]string{ //nolint:gosec // G101: names, not values
	"CETACEAN_MCP_ISSUER":                     "CETACEAN_OAUTH_ISSUER",
	"CETACEAN_MCP_SIGNING_KEY":                "CETACEAN_OAUTH_SIGNING_KEY",
	"CETACEAN_MCP_SIGNING_KEY_FILE":           "CETACEAN_OAUTH_SIGNING_KEY_FILE",
	"CETACEAN_MCP_ACCESS_TOKEN_TTL":           "CETACEAN_OAUTH_ACCESS_TOKEN_TTL",
	"CETACEAN_MCP_REFRESH_TOKEN_TTL":          "CETACEAN_OAUTH_REFRESH_TOKEN_TTL",
	"CETACEAN_MCP_CONSENT_TTL":                "CETACEAN_OAUTH_CONSENT_TTL",
	"CETACEAN_MCP_REQUIRE_RESOURCE_INDICATOR": "CETACEAN_OAUTH_REQUIRE_RESOURCE_INDICATOR",
	"CETACEAN_MCP_DCR_ENABLED":                "CETACEAN_OAUTH_DCR_ENABLED",
	"CETACEAN_MCP_DCR_RATE_LIMIT":             "CETACEAN_OAUTH_DCR_RATE_LIMIT",
	"CETACEAN_MCP_DCR_MAX_CLIENTS":            "CETACEAN_OAUTH_DCR_MAX_CLIENTS",
	"CETACEAN_MCP_CIMD_ENABLED":               "CETACEAN_OAUTH_CIMD_ENABLED",
	"CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES":   "CETACEAN_TRUSTED_PROXIES",
}

// checkRemovedEnv refuses startup while any removed variable is still set,
// naming every one at once so a deployment is fixed in one pass rather than one
// restart per variable.
func checkRemovedEnv() error {
	var removed []string
	for old, replacement := range removedEnv {
		// Empty is unset, as every resolver in this package treats it: an
		// orchestrator interpolating an undefined variable sets the name to
		// the empty string rather than omitting it, and that configured
		// nothing.
		if os.Getenv(old) != "" {
			removed = append(removed, old+" is now "+replacement)
		}
	}

	if len(removed) == 0 {
		return nil
	}

	slices.Sort(removed)

	return fmt.Errorf(
		"these variables are no longer read, and the settings they carried moved: %s. "+
			"Unset them once the deployment carries the new names",
		strings.Join(removed, "; "),
	)
}

// removedKeys maps a config-file key this release no longer decodes to the one
// that replaced it. LoadFile refuses an unknown key either way; this is what
// turns the refusal into a migration a reader can act on. mcp.oauth.auth_bypass
// moved in the file only, which is why it has no removedEnv counterpart.
var removedKeys = map[string]string{ //nolint:gosec // G101: names, not values
	"mcp.oauth":                            "oauth",
	"mcp.issuer":                           "oauth.issuer",
	"mcp.signing_key":                      "oauth.signing_key",
	"mcp.access_token_ttl":                 "oauth.access_token_ttl",
	"mcp.refresh_token_ttl":                "oauth.refresh_token_ttl",
	"mcp.consent_ttl":                      "oauth.consent_ttl",
	"mcp.oauth.require_resource_indicator": "oauth.require_resource_indicator",
	"mcp.oauth.dcr_enabled":                "oauth.dcr_enabled",
	"mcp.oauth.dcr_rate_limit":             "oauth.dcr_rate_limit",
	"mcp.oauth.dcr_max_clients":            "oauth.dcr_max_clients",
	"mcp.oauth.cimd_enabled":               "oauth.cimd_enabled",
	"mcp.oauth.auth_bypass":                "mcp.auth_bypass",
	"auth.headers.trusted_proxies":         "server.trusted_proxies",
}

// unknownKeyError reports the keys a file carries that nothing decodes, naming
// the replacement for each one that only moved. Keys arrive sorted.
func unknownKeyError(path string, keys []string) error {
	var moved, unknown []string

	for _, key := range keys {
		// Undecoded() reports a table as well as the keys inside it, and the
		// table says nothing the key it contains has not already said.
		if slices.ContainsFunc(keys, func(other string) bool {
			return strings.HasPrefix(other, key+".")
		}) {
			continue
		}

		if replacement, ok := removedKeys[key]; ok {
			moved = append(moved, key+" is now "+replacement)
			continue
		}

		unknown = append(unknown, key)
	}

	parts := make([]string, 0, 2)
	if len(moved) > 0 {
		parts = append(parts, "moved: "+strings.Join(moved, "; "))
	}

	if len(unknown) > 0 {
		parts = append(parts, "unknown: "+strings.Join(unknown, ", "))
	}

	return fmt.Errorf(
		"%s carries settings this release does not read — %s. Check them against "+
			"docs/configuration.mdx",
		path,
		strings.Join(parts, ", and "),
	)
}
