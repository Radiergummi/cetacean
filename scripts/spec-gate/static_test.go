package main

import (
	"os"
	"path/filepath"
	"slices"
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

// At run time an observation joins a claim made on its own t or a parent's,
// so one from a sibling subtest is dropped; lexically, it lies outside the
// function the claim was made in.
func TestAnObservationOutsideTheClaimingFunctionFailsTheGate(t *testing.T) {
	src := `package p

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestSiblings(t *testing.T) {
	t.Run("claims", func(t *testing.T) {
		spec.Satisfies(t, "test/doc/a")
	})
	t.Run("observes", func(t *testing.T) {
		spec.Observed(t, "test/doc/a", "sibling")
	})
}

func TestNested(t *testing.T) {
	spec.Satisfies(t, "test/doc/a")
	t.Run("case", func(t *testing.T) {
		spec.Observed(t, "test/doc/a", "child")
	})
}
`

	claims, observations, errs := ScanFile(writeTemp(t, "s_test.go", src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}

	errs = checkObservations(claims, observations)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "s_test.go:14") {
		t.Fatalf("errors = %v, want one for the sibling's observation at line 14", errs)
	}
}

// A refactor that moves a mutant's target is caught here, in a second, rather
// than by the mutant run that takes minutes.
func TestAMutantWhosePatternIsGoneFailsTheGate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "a.go"),
		[]byte("x := 1\ny := 2\ny := 2\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x",
		Mutants: []spec.Mutant{
			{File: "a.go", Replace: "x := 1", With: "x := 0"},
			{File: "a.go", Replace: "z := 3", With: "z := 0"},
			{File: "a.go", Replace: "y := 2", With: "y := 0"},
			{File: "gone.go", Replace: "x", With: "y"},
		},
	})

	errs := checkMutantPatterns(root, reg)
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want three: a missing pattern, a repeated one, a missing file", errs)
	}

	for _, want := range []string{"pattern not found", "occurs 2 times", "gone.go"} {
		if !slices.ContainsFunc(
			errs,
			func(e error) bool { return strings.Contains(e.Error(), want) },
		) {
			t.Errorf("errors = %v, want one mentioning %q", errs, want)
		}
	}
}
