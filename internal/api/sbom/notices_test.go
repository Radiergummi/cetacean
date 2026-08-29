package sbom

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNoticesMatchesTheCommittedFile(t *testing.T) {
	// The repo file and the served document are the same artifact. If they
	// drift, one of the two audiences is reading stale attribution.
	committed, err := os.ReadFile("../../../THIRD_PARTY_LICENSES")
	if err != nil {
		t.Fatalf("read THIRD_PARTY_LICENSES: %v", err)
	}

	if !bytes.Equal(Notices(), committed) {
		t.Error("Notices() differs from THIRD_PARTY_LICENSES; run 'make sbom'")
	}
}

func TestNoticesCoversEveryComponent(t *testing.T) {
	doc, err := Project(Raw())
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	notices := string(Notices())

	var absent []string
	for _, component := range doc.Components {
		if !strings.Contains(notices, component.Name) {
			absent = append(absent, component.Name)
		}
	}

	if len(absent) > 0 {
		t.Errorf("%d components absent from the notices: %v", len(absent), absent)
	}
}

func TestNoticesCarriesTheCuratedPreamble(t *testing.T) {
	notices := string(Notices())

	for _, want := range []string{"elkjs", "Reserved Font Name", "Lucide"} {
		if !strings.Contains(notices, want) {
			t.Errorf("notices do not mention %q — the curated preamble was dropped", want)
		}
	}
}
