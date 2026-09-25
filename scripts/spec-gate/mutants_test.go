package main

import (
	"encoding/json"
	"os"
	"slices"
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

// A mutant whose only claimant is behind a build tag cannot be run here, so the
// gate has to say so rather than report it killed.
func TestAMutantWithOnlyATaggedClaimantHasNoRunnableClaimants(t *testing.T) {
	claims := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e2e/e_test.go", Tagged: true}}

	if names, _ := claimants(claims, "test/doc/a"); len(names) != 0 {
		t.Fatalf("names = %v, want none", names)
	}
}

// The packages to run come from the claimants: a claimant outside the mutated
// file's package would otherwise be filtered out and read as "survived".
func TestClaimantsReportThePackagesTheirTestsLiveIn(t *testing.T) {
	claims := []Claim{
		{ID: "test/doc/a", Func: "TestB", File: "internal/oauth/b_test.go"},
		{ID: "test/doc/a", Func: "TestA", File: "internal/oauth/a_test.go"},
		{ID: "test/doc/a", Func: "TestC", File: "internal/mcp/c_test.go"},
	}

	names, pkgs := claimants(claims, "test/doc/a")

	if !slices.Equal(names, []string{"TestA", "TestB", "TestC"}) {
		t.Errorf("names = %v", names)
	}

	if !slices.Equal(pkgs, []string{"./internal/mcp/", "./internal/oauth/"}) {
		t.Errorf("pkgs = %v", pkgs)
	}
}

// go test exits non-zero for a timeout or a killed process as well, and
// neither says the claimants noticed the mutant.
func TestOnlyAClaimantFailingKillsAMutant(t *testing.T) {
	names := []string{"TestRefusesCORS", "TestOther"}

	for out, want := range map[string]bool{
		"--- FAIL: TestRefusesCORS (0.00s)\nFAIL\n":                        true,
		"--- FAIL: TestOther/suffix (0.00s)\nFAIL\n":                       true,
		"--- FAIL: TestRefusesCORSAgain (0.00s)\nFAIL\n":                   false,
		"panic: test timed out after 10m0s\nrunning tests:\n\tTestOther\n": false,
		"signal: killed\n": false,
	} {
		if got := claimantFailed([]byte(out), names); got != want {
			t.Errorf("claimantFailed(%q) = %v, want %v", out, got, want)
		}
	}
}
