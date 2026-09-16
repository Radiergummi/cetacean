package aliased

import (
	"testing"

	sp "github.com/radiergummi/cetacean/internal/spec" // want "imported as \"sp\""
)

func TestAliased(t *testing.T) {
	sp.Satisfies(t, "oauth/rfc7636/verifier-must-match-challenge")
}
