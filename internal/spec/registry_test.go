package spec

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadReadsTheEmbeddedRegistry(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(reg.Documents) == 0 {
		t.Fatal("no documents loaded")
	}

	const id = "oauth/rfc7636/verifier-must-match-challenge"

	got, ok := reg.Lookup(id)
	if !ok {
		t.Fatalf("Lookup(%q) found nothing", id)
	}

	if got.Level != MUST {
		t.Errorf("level = %q, want MUST", got.Level)
	}

	if got.Document.Source != "RFC 7636" {
		t.Errorf("document source = %q, want RFC 7636", got.Document.Source)
	}
}

// url accepts a scalar or a sequence, because a requirement often has two homes
// that drift apart and both are worth recording.
func TestURLAcceptsAScalarAndASequence(t *testing.T) {
	var scalar struct {
		URL URLs `yaml:"url"`
	}
	mustUnmarshal(t, "url: https://example.test/a", &scalar)

	if len(scalar.URL) != 1 || scalar.URL[0] != "https://example.test/a" {
		t.Errorf("scalar form = %v", scalar.URL)
	}

	var seq struct {
		URL URLs `yaml:"url"`
	}
	mustUnmarshal(t, "url:\n  - https://example.test/a\n  - https://example.test/b", &seq)

	if len(seq.URL) != 2 {
		t.Errorf("sequence form = %v", seq.URL)
	}
}

func mustUnmarshal(t *testing.T, in string, out any) {
	t.Helper()

	if err := yaml.Unmarshal([]byte(in), out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

// The denominator is the vacuity guard: without it the cheapest way to green
// the gate is to delete the requirement, and nothing would notice.
func TestInventoryCountMustAccountForEveryRequirement(t *testing.T) {
	doc := &Document{
		Family: "test", Name: "doc",
		Inventory:    &Inventory{Count: 3},
		Requirements: []Requirement{{ID: "a", Level: MUST, Text: "x"}},
		Dismissed:    map[string]string{"b": "not ours"},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	if err := reg.add(doc); err != nil {
		t.Fatal(err)
	}

	errs := reg.Validate()
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly one about the inventory", errs)
	}

	if !strings.Contains(errs[0].Error(), "3") {
		t.Errorf("error does not name the expected count: %v", errs[0])
	}
}

func TestDismissalNeedsAReason(t *testing.T) {
	doc := &Document{
		Family: "test", Name: "doc",
		Requirements: []Requirement{{ID: "a", Level: MUST, Text: "x"}},
		Dismissed:    map[string]string{"b": "   "},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	if err := reg.add(doc); err != nil {
		t.Fatal(err)
	}

	if errs := reg.Validate(); len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the empty dismissal reason", errs)
	}
}

func TestFamilyCannotContainASlash(t *testing.T) {
	doc := &Document{
		Family: "oauth/nested", Name: "doc",
		Requirements: []Requirement{{ID: "a", Level: MUST, Text: "x"}},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	err := reg.add(doc)
	if err == nil {
		t.Fatal("want an error about family containing /")
	}

	if !strings.Contains(err.Error(), "/") {
		t.Errorf("error does not mention the slash: %v", err)
	}
}
