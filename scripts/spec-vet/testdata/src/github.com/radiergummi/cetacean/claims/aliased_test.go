package claims

import (
	"testing"

	sp "github.com/radiergummi/cetacean/internal/spec" // want "imported as \"sp\""
)

func TestAliased(t *testing.T) {
	sp.Satisfies(t, "fixture/not-a-document/not-a-requirement")
}
