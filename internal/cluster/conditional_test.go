package cluster

import (
	"testing"
)

func TestVersionedNamesTheKindsWhoseUpdatesTakeAVersion(t *testing.T) {
	for kind, want := range map[string]bool{
		"service": true,
		"node":    true,
		"config":  true,
		"secret":  true,
		"task":    false,
		"stack":   false,
		"network": false,
		"volume":  false,
		"plugin":  false,
		"":        false,
	} {
		if got := Versioned(kind); got != want {
			t.Errorf("Versioned(%q) = %v, want %v", kind, got, want)
		}
	}
}
