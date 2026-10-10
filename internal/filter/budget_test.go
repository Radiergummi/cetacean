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
		`len(reduce(1..20000, #acc + "` + strings.Repeat("a", 300) + `", "")) > 0`,
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
	prog, err := Compile(`all(["web", "api"], {# != name}) && any(1..3, {# == 2})`)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := EvaluateContext(t.Context(), prog, map[string]any{"name": "db"})
	if err != nil || !ok {
		t.Errorf("ok, err = %v, %v; want true, nil", ok, err)
	}
}

func TestBudgetKeepsPredicateTypeChecking(t *testing.T) {
	if _, err := Compile(`all(1..3, {# + 1})`); err == nil {
		t.Error("a predicate returning an int compiled")
	}
}

func TestEvaluateContextCannotBeDisarmed(t *testing.T) {
	for _, expression := range []string{
		`let $ctx = nil; all(1..3, {true})`,
		`let budgetContext = nil; all(1..3, {true})`,
	} {
		prog, err := Compile(expression)
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
