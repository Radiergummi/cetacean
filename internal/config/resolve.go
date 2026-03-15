package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// resolve returns the first non-nil value in precedence order:
// flag > env > file > hardcoded default.
func resolve(flag *string, envKey string, file *string, def string) string {
	if flag != nil {
		return *flag
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if file != nil {
		return *file
	}
	return def
}

// resolveBool returns the first set value in precedence order:
// flag > env > file > hardcoded default.
func resolveBool(flag *bool, envKey string, file *bool, def bool) bool {
	if flag != nil {
		return *flag
	}
	if v := os.Getenv(envKey); v != "" {
		switch strings.ToLower(v) {
		case "true", "1":
			return true
		case "false", "0":
			return false
		}
	}
	if file != nil {
		return *file
	}
	return def
}

// resolveDuration returns the first set value in precedence order:
// flag > env > file > hardcoded default. Returns an error if any
// explicitly set value is unparseable or non-positive.
func resolveDuration(flag *string, envKey string, file *string, def time.Duration) (time.Duration, error) {
	// Check sources in precedence order; parse the first one found.
	var raw string
	var source string
	switch envVal := os.Getenv(envKey); {
	case flag != nil:
		raw, source = *flag, "flag"
	case envVal != "":
		raw, source = envVal, envKey
	case file != nil:
		raw, source = *file, "config file"
	default:
		return def, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid duration from %s %q: %w", source, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid duration from %s %q: must be positive", source, raw)
	}
	return d, nil
}
