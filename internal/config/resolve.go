package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// resolveSecret works like resolve but also checks for a _FILE variant
// of the env var. If envKey+"_FILE" is set, the file is read and its
// contents (trimmed) are used as the value. The _FILE variant has lower
// precedence than the direct env var but higher than the config file.
//
// Precedence: flag > env > env_FILE > config file > default.
func resolveSecret(flag *string, envKey string, file *string, def string) (string, error) {
	if flag != nil {
		return *flag, nil
	}
	if v := os.Getenv(envKey); v != "" {
		return v, nil
	}
	if path := os.Getenv(envKey + "_FILE"); path != "" {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return "", fmt.Errorf("reading %s_FILE (%s): %w", envKey, path, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if file != nil {
		return *file, nil
	}
	return def, nil
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
func resolveDuration(
	flag *string,
	envKey string,
	file *string,
	def time.Duration,
) (time.Duration, error) {
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

// resolveInt returns the first set value in precedence order:
// flag > env > file > hardcoded default. Returns an error if any
// explicitly set value is not a valid integer or is out of [min, max].
func resolveInt(flag *int, envKey string, file *int, def, min, max int) (int, error) {
	var raw string
	var source string
	switch envVal := os.Getenv(envKey); {
	case flag != nil:
		return checkIntRange(*flag, min, max, "flag")
	case envVal != "":
		raw, source = envVal, envKey
	case file != nil:
		return checkIntRange(*file, min, max, "config file")
	default:
		return def, nil
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer from %s %q: %w", source, raw, err)
	}
	return checkIntRange(v, min, max, source)
}

func checkIntRange(v, min, max int, source string) (int, error) {
	if v < min || v > max {
		return 0, fmt.Errorf("value %d from %s out of range [%d, %d]", v, source, min, max)
	}
	return v, nil
}
