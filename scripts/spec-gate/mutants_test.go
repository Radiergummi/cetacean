package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// An ambiguous pattern is the failure mode that matters: a mutant that quietly
// edits the wrong line proves nothing about the test that survives it.
func TestApplyMutantRequiresExactlyOneOccurrence(t *testing.T) {
	const src = "a\nb\na\n"

	if _, err := applyMutant(src, spec.Mutant{Replace: "a", With: "z"}); err == nil {
		t.Error("a pattern occurring twice was accepted")
	}

	if _, err := applyMutant(src, spec.Mutant{Replace: "q", With: "z"}); err == nil {
		t.Error("a pattern occurring zero times was accepted")
	}

	got, err := applyMutant(src, spec.Mutant{Replace: "b", With: "z"})
	if err != nil {
		t.Fatal(err)
	}

	if got != "a\nz\na\n" {
		t.Errorf("got %q", got)
	}
}

func TestOverlayNamesTheSourceFile(t *testing.T) {
	dir := t.TempDir()

	path, err := overlayFor(dir, "internal/oauth/resource.go", "package oauth\n")
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) // #nosec G304 -- a path this test just built
	if err != nil {
		t.Fatal(err)
	}

	var overlay struct {
		Replace map[string]string
	}

	if err := json.Unmarshal(body, &overlay); err != nil {
		t.Fatal(err)
	}

	if len(overlay.Replace) != 1 {
		t.Fatalf("overlay = %s, want one replacement", body)
	}

	for from, to := range overlay.Replace {
		if !strings.HasSuffix(from, "internal/oauth/resource.go") {
			t.Errorf("replaced %q, want the source file", from)
		}

		mutated, err := os.ReadFile(to) // #nosec G304 -- written by overlayFor above
		if err != nil {
			t.Fatal(err)
		}

		if string(mutated) != "package oauth\n" {
			t.Errorf("mutated file = %q", mutated)
		}
	}
}

// A mutant with no claimant that runs here proves nothing, so the gate has to
// say so rather than report it killed.
func TestAMutantWithOnlyATaggedClaimantIsAnError(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x",
		Mutants: []spec.Mutant{{File: "internal/oauth/resource.go", Replace: "x", With: "y"}},
	})

	claims := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e_test.go", Tagged: true}}

	if errs := checkMutable(reg, claims, ""); len(errs) != 1 {
		t.Fatalf("errors = %v, want one about the missing untagged claimant", errs)
	}
}
