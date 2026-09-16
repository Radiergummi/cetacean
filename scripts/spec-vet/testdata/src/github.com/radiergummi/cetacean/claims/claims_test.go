package claims

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

const id = "fixture/not-a-document/not-a-requirement"

func helper(t *testing.T) {}

func TestClaimsFirst(t *testing.T) {
	spec.Satisfies(t, "fixture/not-a-document/not-a-requirement")

	helper(t)
}

func TestHelperCallIsAllowedBefore(t *testing.T) {
	t.Helper()
	spec.Satisfies(t, "fixture/not-a-document/not-a-requirement")
}

func TestClaimsAfterWork(t *testing.T) {
	helper(t)

	spec.Satisfies(t, "fixture/not-a-document/not-a-requirement") // want "must be the first statement"
}

func TestNonLiteralID(t *testing.T) {
	spec.Satisfies(t, id) // want "not a string literal"
}

func TestNoRequirement(t *testing.T) {
	spec.Satisfies(t) // want "names no requirement"
}

func TestSubtestClaimsFirst(t *testing.T) {
	helper(t)

	t.Run("sub", func(t *testing.T) {
		spec.Satisfies(t, "fixture/not-a-document/not-a-requirement")

		helper(t)
	})
}

func TestSubtestClaimsAfterWork(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		helper(t)

		spec.Satisfies(t, "fixture/not-a-document/not-a-requirement") // want "must be the first statement"
	})
}
