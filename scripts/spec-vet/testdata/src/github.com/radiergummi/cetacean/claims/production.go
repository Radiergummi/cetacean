package claims

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// Satisfies outside a test file would link testing into a shipped binary.
func notATest(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge") // want "outside a _test.go file"
}
