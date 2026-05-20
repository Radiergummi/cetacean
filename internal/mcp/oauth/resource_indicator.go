package oauth

import "errors"

// ValidateResourceIndicator checks the RFC 8707 resource indicator from a
// token request against the expected server resource URL.
//
// If raw is empty and required is false, the expected value is returned as the
// effective resource (lenient mode). If raw is empty and required is true, an
// error is returned. If raw is non-empty it must match expected exactly.
func ValidateResourceIndicator(raw string, expected string, required bool) (string, error) {
	if raw == "" {
		if required {
			return "", errors.New("invalid_target: resource parameter is required")
		}
		return expected, nil
	}
	if raw != expected {
		return "", errors.New("invalid_target: resource does not match this server")
	}
	return raw, nil
}
