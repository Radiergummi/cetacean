package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// A requirement whose only claimant is behind the e2e tag is "not run" when the
// e2e suite was not part of this invocation — never "uncovered".
func TestAnE2EOnlyRequirementIsNotRunRatherThanUncovered(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	static := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e_test.go", Tagged: true}}

	got := summarise(reg, static, map[string][]string{}, map[string]bool{"unit": true})
	if got.NotRun != 1 {
		t.Errorf("NotRun = %d, want 1", got.NotRun)
	}

	if len(got.Uncovered) != 0 {
		t.Errorf("Uncovered = %v, want none", got.Uncovered)
	}
}

// The same claimant, with the e2e lane in this invocation, has no such excuse.
func TestAnE2EOnlyRequirementIsUncoveredWhenTheLaneRan(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	static := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e_test.go", Tagged: true}}

	got := summarise(reg, static, map[string][]string{}, map[string]bool{"unit": true, "e2e": true})
	if len(got.Uncovered) != 1 {
		t.Fatalf("Uncovered = %v, want one", got.Uncovered)
	}
}

func TestAClaimantThatDidNotRunIsUncovered(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	static := []Claim{{ID: "test/doc/a", Func: "TestUnit", File: "u_test.go"}}

	got := summarise(reg, static, map[string][]string{}, map[string]bool{"unit": true})
	if len(got.Uncovered) != 1 {
		t.Fatalf("Uncovered = %v, want one", got.Uncovered)
	}
}

func TestAGapAndADeferralAreCountedRatherThanUncovered(t *testing.T) {
	reg := registryWith(t,
		spec.Requirement{ID: "a", Level: spec.MUST, Text: "x", Gap: "no test yet"},
		spec.Requirement{ID: "b", Level: spec.MUST, Text: "x", Deferred: "not implemented"},
	)

	got := summarise(reg, nil, map[string][]string{}, map[string]bool{"unit": true})

	if got.Gaps != 1 || got.Deferred != 1 {
		t.Errorf("gaps = %d, deferred = %d, want 1 and 1", got.Gaps, got.Deferred)
	}

	if len(got.Uncovered) != 0 {
		t.Errorf("Uncovered = %v, want none", got.Uncovered)
	}
}

func TestReadClaimsSplitsOnTheFirstTab(t *testing.T) {
	dir := t.TempDir()

	body := "test/doc/a\tTestOne\ntest/doc/a\tTestTwo\ntest/doc/b\tTestOne\n\n"
	if err := os.WriteFile(filepath.Join(dir, "1.claims"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadClaims(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(got["test/doc/a"]) != 2 || len(got["test/doc/b"]) != 1 {
		t.Fatalf("claims = %v", got)
	}
}
