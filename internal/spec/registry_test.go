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
