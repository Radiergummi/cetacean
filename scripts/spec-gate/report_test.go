package main

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// A requirement that declares the e2e lane is "not run" when that suite was not
// part of this invocation — never "uncovered".
func TestAnE2ELaneRequirementIsNotRunRatherThanUncovered(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Lane: spec.LaneE2E,
	})

	got := summarise(reg, map[string][]spec.Evidence{}, false)
	if got.NotRun != 1 {
		t.Errorf("NotRun = %d, want 1", got.NotRun)
	}

	if len(got.Uncovered) != 0 {
		t.Errorf("Uncovered = %v, want none", got.Uncovered)
	}
}

// The same declaration, with the e2e lane in this invocation, excuses nothing.
func TestAnE2ELaneRequirementIsUncoveredWhenThatLaneRan(t *testing.T) {
	reg := registryWith(t, spec.Requirement{
		ID: "a", Level: spec.MUST, Text: "x", Lane: spec.LaneE2E,
	})

	got := summarise(reg, map[string][]spec.Evidence{}, true)
	if len(got.Uncovered) != 1 {
		t.Fatalf("Uncovered = %v, want one", got.Uncovered)
	}
}

// An undeclared requirement nothing ran is uncovered whatever lane its
// claimants live in: the static gate is what holds the declaration to them.
func TestAnUndeclaredRequirementThatDidNotRunIsUncovered(t *testing.T) {
	reg := registryWith(t, spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"})

	got := summarise(reg, map[string][]spec.Evidence{}, false)
	if len(got.Uncovered) != 1 {
		t.Fatalf("Uncovered = %v, want one", got.Uncovered)
	}
}

func TestAGapAndADeferralAreCountedRatherThanUncovered(t *testing.T) {
	reg := registryWith(t,
		spec.Requirement{ID: "a", Level: spec.MUST, Text: "x", Gap: "no test yet"},
		spec.Requirement{ID: "b", Level: spec.MUST, Text: "x", Deferred: "not implemented"},
	)

	got := summarise(reg, map[string][]spec.Evidence{}, false)

	if len(got.Gaps) != 1 || got.Deferred != 1 {
		t.Errorf("gaps = %v, deferred = %d, want one and 1", got.Gaps, got.Deferred)
	}

	if len(got.Uncovered) != 0 {
		t.Errorf("Uncovered = %v, want none", got.Uncovered)
	}
}

// Exercised and Observed answer different questions, so a claim with nothing
// recorded against it counts toward one and not the other.
func TestOnlyAClaimCarryingAnObservationCountsAsObserved(t *testing.T) {
	reg := registryWith(t,
		spec.Requirement{ID: "a", Level: spec.MUST, Text: "x"},
		spec.Requirement{ID: "b", Level: spec.MUST, Text: "x"},
	)

	got := summarise(reg, map[string][]spec.Evidence{
		"test/doc/a": {{Test: "TestA"}},
		"test/doc/b": {{Test: "TestB", Observations: []string{"status=400"}}},
	}, false)

	if got.Exercised != 2 {
		t.Errorf("Exercised = %d, want 2", got.Exercised)
	}

	if got.Observed != 1 {
		t.Errorf("Observed = %d, want 1", got.Observed)
	}
}
