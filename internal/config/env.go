package config

import (
	"fmt"
	"slices"
	"strings"
)

// nonSettingEnvVars are CETACEAN_* variables the repository's own tooling sets
// around the binary — test harnesses and the widget build — rather than settings.
var nonSettingEnvVars = map[string]bool{
	"CETACEAN_SPEC_CLAIMS": true,
	"CETACEAN_E2E_BINARY":  true,
	"CETACEAN_E2E_URL":     true,
	"CETACEAN_E2E_WRITE":   true,
	"CETACEAN_WIDGET":      true,
}

// CheckEnv refuses an environment carrying a CETACEAN_* variable no setting
// reads, for the reason LoadFile refuses an unknown key: a misspelt name would
// leave the setting at its default, which can be the permissive one.
func CheckEnv(environ []string) error {
	var unknown []string

	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "CETACEAN_") ||
			knownEnvVars[name] ||
			nonSettingEnvVars[name] {
			continue
		}

		unknown = append(unknown, name)
	}

	if len(unknown) == 0 {
		return nil
	}

	slices.Sort(unknown)

	return fmt.Errorf(
		"the environment carries variables this release does not read: %s. Check them "+
			"against docs/configuration.mdx",
		strings.Join(unknown, ", "),
	)
}
