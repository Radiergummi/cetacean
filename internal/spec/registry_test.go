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
		Inventory:    &Inventory{Count: 2},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	if err := reg.add(doc); err != nil {
		t.Fatal(err)
	}

	if errs := reg.Validate(); len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the empty dismissal reason", errs)
	}
}

func TestFamilyAndName(t *testing.T) {
	tests := []struct {
		path    string
		family  string
		name    string
		wantErr bool
		errVal  string
	}{
		{
			path:    "registry/oauth/rfc7636.yaml",
			family:  "oauth",
			name:    "rfc7636",
			wantErr: false,
		},
		{
			path:    "registry/oauth/nested/deep.yaml",
			family:  "",
			name:    "",
			wantErr: true,
			errVal:  "oauth/nested",
		},
		{
			path:    "registry/stray.yaml",
			family:  "",
			name:    "",
			wantErr: true,
			errVal:  "stray.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			fam, nam, err := familyAndName(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.errVal) {
					t.Errorf("error %q does not contain %q", err, tt.errVal)
				}
				return
			}

			if fam != tt.family {
				t.Errorf("family = %q, want %q", fam, tt.family)
			}
			if nam != tt.name {
				t.Errorf("name = %q, want %q", nam, tt.name)
			}
		})
	}
}

// A registered document and a citation of it have to canonicalise to the same
// string, or the sweep reads the document as uncited.
func TestTokenCanonicalisesEverySpellingTheTreeUses(t *testing.T) {
	for in, want := range map[string]string{
		"RFC 7636": "RFC7636",
		"RFC-7636": "RFC7636",
		"rfc7636":  "RFC7636",
		"SEP 2575": "SEP-2575",
		"SEP2575":  "SEP-2575",
		"sep-2575": "SEP-2575",
	} {
		if got := Token(in); got != want {
			t.Errorf("Token(%q) = %q, want %q", in, got, want)
		}
	}

	doc := &Document{Family: "oauth", Name: "rfc7636"}
	if doc.Token() != Token("RFC 7636") {
		t.Errorf("document token %q disagrees with the cited spelling", doc.Token())
	}
}

// A misspelled lane would be read as the unit one and quietly excuse nothing,
// so it is refused where every other unreadable field is.
func TestAnUnknownLaneIsRefused(t *testing.T) {
	_, err := NewForTest("test", "doc", []Requirement{
		{ID: "a", Level: MUST, Text: "x", Lane: "integration"},
	})
	if err == nil || !strings.Contains(err.Error(), "integration") {
		t.Fatalf("err = %v, want one naming the unknown lane", err)
	}
}

// Without an inventory a document has no denominator at all, and the guard
// above is opt-in rather than a rule.
func TestADocumentWithoutAnInventoryFailsValidation(t *testing.T) {
	doc := &Document{
		Family: "test", Name: "doc",
		Requirements: []Requirement{{ID: "a", Level: MUST, Text: "x"}},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	if err := reg.add(doc); err != nil {
		t.Fatal(err)
	}

	errs := reg.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "no inventory") {
		t.Fatalf("errors = %v, want one about the missing inventory", errs)
	}
}

// Most of what a document says about itself is the prose of a dismissal, and a
// pointer into another entry is the only part of it a gate can follow.
func TestAPointerAtAnotherEntryMustResolve(t *testing.T) {
	other := &Document{
		Family: "test", Name: "other",
		Inventory:    &Inventory{Count: 2},
		Requirements: []Requirement{{ID: "kept", Level: MUST, Text: "x"}},
		Dismissed:    map[string]string{"waved": "not ours"},
	}

	doc := &Document{
		Family: "test", Name: "doc",
		Inventory: &Inventory{Count: 4},
		Requirements: []Requirement{
			{ID: "a", Level: MUST, Text: "x", Deferred: "See test/other/kept."},
			{ID: "b", Level: MUST, Text: "x", Gap: "Same shape as test/other/waved."},
			{ID: "c", Level: MUST, Text: "x", Deferred: "Read https://example.test/a/b/c."},
		},
		Dismissed: map[string]string{"d": "Registered as test/other/renamed."},
	}

	reg := &Registry{byID: map[string]*Requirement{}}
	for _, d := range []*Document{other, doc} {
		if err := reg.add(d); err != nil {
			t.Fatal(err)
		}
	}

	errs := reg.Validate()
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want only the one about the renamed entry", errs)
	}

	if !strings.Contains(errs[0].Error(), "test/other/renamed") {
		t.Errorf("error does not name the dangling pointer: %v", errs[0])
	}
}
