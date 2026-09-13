package oauth

import (
	"errors"
	"slices"
)

// ValidateResourceIndicator checks the RFC 8707 resource indicator from a token
// request against the resource identifiers this server issues tokens for.
//
// If raw is empty and required is false, fallback is returned as the effective
// resource (lenient mode). If raw is empty and required is true, an error is
// returned. If raw is non-empty it must equal one of known exactly — a prefix
// match would make a token for one resource reach another mounted beneath it.
func ValidateResourceIndicator(
	raw string,
	known []string,
	fallback string,
	required bool,
) (string, error) {
	if raw == "" {
		if required {
			return "", errors.New("resource parameter is required")
		}

		return fallback, nil
	}

	if !slices.Contains(known, raw) {
		return "", errors.New("resource is not one this server issues tokens for")
	}

	return raw, nil
}
