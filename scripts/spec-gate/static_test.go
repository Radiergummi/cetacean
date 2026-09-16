package main

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func registryWith(t *testing.T, reqs ...spec.Requirement) *spec.Registry {
	t.Helper()

	reg, err := spec.NewForTest("test", "doc", reqs)
	if err != nil {
		t.Fatal(err)
	}

	return reg
}

func TestAnUnclaimedRequirementFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	errs := checkStatic(reg, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "test/doc/a") {
		t.Fatalf("errors = %v, want one naming test/doc/a", errs)
	}
}

func TestAGapNeedsNoClaimant(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Gap: "gap: no test yet",
	})

	if errs := checkStatic(reg, nil); len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
}

// A deferred requirement's claiming test pins the non-conforming answer, so
// fixing the behaviour fails that test rather than passing quietly.
func TestADeferredRequirementStillNeedsAClaimant(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Deferred: "not implemented",
	})

	errs := checkStatic(reg, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "deferred") {
		t.Fatalf("errors = %v, want one about the missing pin", errs)
	}

	claims := []Claim{{ID: "test/doc/a", Func: "TestPin", File: "x_test.go"}}
	if errs := checkStatic(reg, claims); len(errs) != 0 {
		t.Fatalf("errors = %v, want none once pinned", errs)
	}
}

func TestAnEmptyReasonFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Gap: "   ",
	})

	if errs := checkStatic(reg, nil); len(errs) == 0 {
		t.Fatal("an empty gap reason was accepted")
	}
}

func TestAClaimForAnUnknownRequirementFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	claims := []Claim{
		{ID: "test/doc/a", Func: "TestA", File: "a_test.go"},
		{ID: "test/doc/ghost", Func: "TestB", File: "b_test.go", Line: 12},
	}

	errs := checkStatic(reg, claims)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "ghost") {
		t.Fatalf("errors = %v, want one naming the unknown id", errs)
	}
}
