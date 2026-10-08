package claims

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// Satisfies outside a test file would link testing into a shipped binary.
func notATest(t *testing.T) {
	spec.Satisfies(t, "fixture/not-a-document/not-a-requirement") // want "outside a _test.go file"
}
