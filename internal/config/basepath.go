package config

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// NormalizeBasePath cleans input to canonical form: a leading slash and no
// trailing one, so "cetacean" and "/cetacean/" both become "/cetacean". Both ""
// and "/" normalize to "", meaning no prefix.
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

// ValidateBasePath rejects paths containing query strings, fragments, double
// slashes, or control characters. The input should already be normalized.
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
	// path.Clean preserves controls, and the base path reaches the
	// resource_metadata quoted-string, which cannot spell one — so an
	// unchecked byte here corrupts that header with no diagnostic.
	for _, r := range bp {
		if unicode.IsControl(r) {
			return fmt.Errorf(
				"base path must not contain control characters: found U+%04X in %q",
				r, bp,
			)
		}
	}
	return nil
}
