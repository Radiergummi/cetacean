package filter

import (
	"testing"
	"time"
)

// FuzzFilterCompile exercises Compile and Evaluate, the expr-lang parser and
// runtime behind the ?filter= query parameter that every list endpoint
// accepts. It is reachable by anyone who can issue a read request — there is
// no authentication in front of it — so a pathological expression is a
// denial-of-service vector, not merely a bad query.
//
// Property: compiling and evaluating an arbitrary string never panics, and
// never runs longer than a fixed time budget. A program that fails to
// compile is never evaluated, matching how the real handlers use these two
// functions.
//
// The budget only catches an expression that is slow. One that never
// terminates hangs the fuzz worker instead of tripping this check, and is
// caught by `go test`'s own timeout, not by anything here.
func FuzzFilterCompile(f *testing.F) {
	seeds := []string{
		`name == "web"`,
		`contains(image, "nginx") && mode == "replicated"`,
		`labels["env"] == "prod"`,
		`name ==`, // syntax error: never reaches Evaluate
		`((((((((((true))))))))))`,
		`repeat("a", 100000000) != ""`, // deliberately expensive
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// Env shaped like ServiceEnv's output, plus a labels map — filter
	// expressions commonly index into labels (e.g. labels["env"]).
	env := map[string]any{
		"id":       "svc1",
		"name":     "web",
		"image":    "nginx:latest",
		"mode":     "replicated",
		"stack":    "mystack",
		"replicas": 3,
		"labels": map[string]any{
			"env":  "prod",
			"team": "platform",
		},
	}

	const budget = 2 * time.Second

	f.Fuzz(func(t *testing.T, expression string) {
		start := time.Now()

		prog, err := Compile(expression)
		if err != nil {
			return
		}

		_, _ = Evaluate(prog, env)

		if elapsed := time.Since(start); elapsed > budget {
			t.Fatalf("expression took %s (budget %s): %q", elapsed, budget, expression)
		}
	})
}
