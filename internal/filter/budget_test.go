package filter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// One run is quadratic in its accumulator yet allocates almost nothing expr
// counts, so only a check inside the expression's own loop can stop it.
func TestEvaluateContextStopsInsideOneRun(t *testing.T) {
	prog, err := Compile(
		`len(reduce(1..20000, #acc + "`+strings.Repeat("a", 300)+`", "")) > 0`,
		Services,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = EvaluateContext(ctx, prog, map[string]any{})
	if !errors.Is(err, ErrBudget) {
		t.Errorf("err = %v, want ErrBudget", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("took %s, want about 50ms", elapsed)
	}
}

func TestEvaluateContextKeepsPredicates(t *testing.T) {
	prog, err := Compile(`all(["web", "api"], {# != name}) && any(1..3, {# == 2})`, Services)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := EvaluateContext(t.Context(), prog, map[string]any{"name": "db"})
	if err != nil || !ok {
		t.Errorf("ok, err = %v, %v; want true, nil", ok, err)
	}
}

func TestBudgetKeepsPredicateTypeChecking(t *testing.T) {
	if _, err := Compile(`all(1..3, {# + 1})`, Services); err == nil {
		t.Error("a predicate returning an int compiled")
	}
}

func TestEvaluateContextCannotBeDisarmed(t *testing.T) {
	for _, expression := range []string{
		`let $ctx = nil; all(1..3, {true})`,
		`let budgetContext = nil; all(1..3, {true})`,
	} {
		prog, err := Compile(expression, Services)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := EvaluateContext(ctx, prog, map[string]any{}); err == nil {
			t.Errorf("%q evaluated under a done context", expression)
		}
	}
}

// A single regexp match or concatenation is one VM step, so neither reaches a
// predicate's check; each is bounded by its size instead.
func TestEvaluateContextBoundsOneStep(t *testing.T) {
	pattern := strings.Repeat(`[a-z]{1,100}`, 9) + "x"
	for _, tc := range []struct {
		name, expression string
	}{
		{"constant pattern", `repeat("a", 99999) matches "` + pattern + `"`},
		{"pattern from a field", `repeat("a", 99999) matches p`},
		{"doubling", `let a = repeat("a", 999999); let b = a + a; let c = b + b; len(c + c) > 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Compile(tc.expression, patterns)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			_, err = EvaluateContext(t.Context(), prog, map[string]any{"p": pattern})
			if !errors.Is(err, ErrBudget) {
				t.Errorf("err = %v, want ErrBudget", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Errorf("took %s", elapsed)
			}
		})
	}
}

func TestEvaluateContextKeepsOrdinaryMatchesAndConcatenation(t *testing.T) {
	prog, err := Compile(
		`name + "-1" == "web-1" && name matches "^w[a-z]+$" && 1 + 2 == 3`,
		Services,
	)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := EvaluateContext(t.Context(), prog, map[string]any{"name": "web"})
	if err != nil || !ok {
		t.Errorf("ok, err = %v, %v; want true, nil", ok, err)
	}
	if _, err := Compile(`name matches "["`, Services); err == nil {
		t.Error("an invalid constant pattern compiled")
	}
}

// patterns sets one field, p, for a match against a pattern that is not a constant.
var patterns = newEnv(func(p string, _ map[string]any) map[string]any {
	return map[string]any{"p": p}
})
