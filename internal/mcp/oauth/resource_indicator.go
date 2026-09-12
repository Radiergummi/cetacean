package oauth

import "errors"

// ValidateResourceIndicator checks the RFC 8707 resource indicator from a token
// request against the expected server resource URL. A non-empty raw must match
// exactly; an empty one yields expected when required is false, and an error
// when it is true.
func ValidateResourceIndicator(raw string, expected string, required bool) (string, error) {
	if raw == "" {
		if required {
			return "", errors.New("resource parameter is required")
		}
		return expected, nil
	}
	if raw != expected {
		return "", errors.New("resource does not match this server")
	}
	return raw, nil
}
