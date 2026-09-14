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
// The config file needs no such list: LoadFile refuses any key the schema does
// not know. This is the environment's version of that rule, and each entry can
// go once no deployment predates its rename.
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
