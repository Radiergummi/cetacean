package config

import (
	"fmt"
	"path"
	"strings"
)

// NormalizeBasePath cleans input to canonical form: leading slash, no trailing
// slash. Both "" and "/" normalize to "" (root/no prefix). Examples:
//   - "cetacean"   → "/cetacean"
//   - "/cetacean/" → "/cetacean"
//   - ""           → ""
//   - "/"          → ""
func NormalizeBasePath(s string) string {
	if s == "" {
		return ""
	}
	// path.Clean handles double slashes, trailing slashes, and ensures
	// the result starts with "/" when the input does after prepending one.
	cleaned := path.Clean("/" + strings.TrimLeft(s, "/"))
	if cleaned == "/" {
		return ""
	}
	return cleaned
}

// ValidateBasePath rejects paths containing query strings, fragments, or
// double slashes. The input should already be normalized.
func ValidateBasePath(bp string) error {
	if strings.Contains(bp, "?") {
		return fmt.Errorf("base path must not contain a query string: %q", bp)
	}
	if strings.Contains(bp, "#") {
		return fmt.Errorf("base path must not contain a fragment: %q", bp)
	}
	if strings.Contains(bp, "//") {
		return fmt.Errorf("base path must not contain double slashes: %q", bp)
	}
	return nil
}
