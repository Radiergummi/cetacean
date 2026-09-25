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

func TestAClaimForAnUnknownRequirementFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	claims := []Claim{
		{ID: "test/doc/a", Func: "TestA", File: "a_test.go"},
		{ID: "test/doc/ghost", Func: "TestB", File: "b_test.go", Line: 12},
	}

	errs := unknown(reg, claims)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "ghost") {
		t.Fatalf("errors = %v, want one naming the unknown id", errs)
	}
}

// The same rule over observations: a typo there names a requirement nothing
// will ever attach evidence to, and the report would drop it in silence.
func TestAnObservationForAnUnknownRequirementFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	errs := unknown(reg, []Claim{
		{ID: "test/doc/ghost", Func: "TestB", File: "b_test.go", Line: 12},
	})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "ghost") {
		t.Fatalf("errors = %v, want one naming the unknown id", errs)
	}
}

// checkObservations is the join between the two lists: an observation whose
// own test filed no claim is evidence the report will never publish.
func TestAnObservationWithoutAClaimInTheSameTestFailsTheGate(t *testing.T) {
	claims := []Claim{{ID: "test/doc/a", Func: "TestA", File: "a_test.go"}}

	errs := checkObservations(claims, []Claim{
		{ID: "test/doc/a", Func: "TestA", File: "a_test.go", Line: 9},
		{ID: "test/doc/a", Func: "TestB", File: "a_test.go", Line: 20},
	})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "TestB") {
		t.Fatalf("errors = %v, want one naming TestB", errs)
	}
}

// The lane declaration is what lets the report count a requirement as not run
// rather than uncovered, so an undeclared one would be silently excused.
func TestAClaimOnlyBehindTheE2ETagMustDeclareItsLane(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	claims := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e_test.go", Tagged: true}}

	errs := checkStatic(reg, claims)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "lane: e2e") {
		t.Fatalf("errors = %v, want one asking for the lane declaration", errs)
	}
}

// And the declaration has to stay true: a requirement the unit suite reaches
// would otherwise keep its excuse after gaining the test that retires it.
func TestALaneDeclarationAUnitTestContradictsFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Lane: spec.LaneE2E,
	})

	claims := []Claim{{ID: "test/doc/a", Func: "TestUnit", File: "u_test.go"}}

	errs := checkStatic(reg, claims)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "lane: e2e") {
		t.Fatalf("errors = %v, want one about the stale declaration", errs)
	}
}

func TestADeclaredE2ELaneWithATaggedClaimantPasses(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Lane: spec.LaneE2E,
	})

	claims := []Claim{{ID: "test/doc/a", Func: "TestE2E", File: "e_test.go", Tagged: true}}

	if errs := checkStatic(reg, claims); len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
}

// A gap says nobody has written the test yet, so the test arriving is what
// retires it. Nothing else does: the report would go on counting an exercised
// requirement as one still waiting to be written.
func TestAGapAClaimantContradictsFailsTheGate(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Gap: "no test yet",
	})

	claims := []Claim{{ID: "test/doc/a", Func: "TestUnit", File: "u_test.go"}}

	errs := checkStatic(reg, claims)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "the gap is what is stale") {
		t.Fatalf("errors = %v, want one about the stale gap", errs)
	}
}
