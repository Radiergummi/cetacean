package claims

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

const id = "oauth/rfc7636/verifier-must-match-challenge"

func helper(t *testing.T) {}

func TestClaimsFirst(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge")

	helper(t)
}

func TestHelperCallIsAllowedBefore(t *testing.T) {
	t.Helper()
	spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge")
}

func TestClaimsAfterWork(t *testing.T) {
	helper(t)

	spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge") // want "must be the first statement"
}

func TestNonLiteralID(t *testing.T) {
	spec.Satisfies(t, id) // want "not a string literal"
}

func TestNoRequirement(t *testing.T) {
	spec.Satisfies(t) // want "names no requirement"
}

// A subtest claims on its own t, which the parent's cleanups cannot reach.
func TestSubtestClaimsFirst(t *testing.T) {
	helper(t)

	t.Run("sub", func(t *testing.T) {
		spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge")

		helper(t)
	})
}

func TestSubtestClaimsAfterWork(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		helper(t)

		spec.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge") // want "must be the first statement"
	})
}
