package recommendations

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type mockChecker struct {
	name     string
	interval time.Duration
	recs     []Recommendation
}

func (m *mockChecker) Name() string                             { return m.name }
func (m *mockChecker) Interval() time.Duration                  { return m.interval }
func (m *mockChecker) Check(_ context.Context) []Recommendation { return m.recs }

func TestEngine_NilSafe(t *testing.T) {
	var e *Engine
	if r := e.Results(); r != nil {
		t.Errorf("expected nil, got %v", r)
	}
	s := e.Summary()
	if s.Critical != 0 || s.Warning != 0 || s.Info != 0 {
		t.Errorf("expected zeros, got %+v", s)
	}
}

func TestEngine_MergesCheckers(t *testing.T) {
	e := NewEngine(
		&mockChecker{name: "a", interval: time.Minute, recs: []Recommendation{
			{Category: CategoryNoLimits, Severity: SeverityWarning, TargetName: "svc1"},
		}},
		&mockChecker{name: "b", interval: time.Minute, recs: []Recommendation{
			{Category: CategoryNodeDiskFull, Severity: SeverityCritical, TargetName: "node1"},
		}},
	)
	e.tick(context.Background(), true)
	results := e.Results()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Critical should sort first.
	if results[0].Severity != SeverityCritical {
		t.Errorf("expected critical first, got %s", results[0].Severity)
	}
}

func TestEngine_RespectsInterval(t *testing.T) {
	slow := &mockChecker{name: "slow", interval: 5 * time.Minute, recs: []Recommendation{
		{Severity: SeverityInfo},
	}}
	fast := &mockChecker{name: "fast", interval: time.Minute, recs: []Recommendation{
		{Severity: SeverityWarning},
	}}
	e := NewEngine(slow, fast)

	// Force run — both execute.
	e.tick(context.Background(), true)
	if len(e.Results()) != 2 {
		t.Fatalf("expected 2, got %d", len(e.Results()))
	}

	// Non-forced tick immediately after — neither should run (both just ran).
	fast.recs = nil
	e.tick(context.Background(), false)
	if len(e.Results()) != 2 {
		t.Fatalf("expected 2 (cached), got %d", len(e.Results()))
	}
}

func TestNewEngine_NilForNoCheckers(t *testing.T) {
	e := NewEngine()
	if e != nil {
		t.Error("expected nil engine with no checkers")
	}
}

// countingChecker records how many times the engine has run it.
type countingChecker struct {
	interval time.Duration
	runs     atomic.Int64
}

func (c *countingChecker) Name() string            { return "counting" }
func (c *countingChecker) Interval() time.Duration { return c.interval }

func (c *countingChecker) Check(_ context.Context) []Recommendation {
	c.runs.Add(1)

	return nil
}

// Pins the ordering the recommendations page depends on at boot. The startup
// tick is forced and a checker that has just run does not run again until its
// interval elapses, which for the sizing and operational checkers is five
// minutes — so a tick before the cache is filled reports an empty cluster.
func TestRunAfterHoldsTheStartupTick(t *testing.T) {
	checker := &countingChecker{interval: time.Minute}
	engine := NewEngine(checker)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ready := make(chan struct{})
	go engine.RunAfter(ctx, ready)

	time.Sleep(50 * time.Millisecond)

	if runs := checker.runs.Load(); runs != 0 {
		t.Fatalf("the checker ran %d times before the signal, want 0", runs)
	}

	close(ready)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if checker.runs.Load() > 0 {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("the checker never ran after the signal closed")
}

// TestRunAfterGivesUpOnACancelledContext covers the shutdown path: a signal
// that never arrives must not hold the goroutine open past the context.
func TestRunAfterGivesUpOnACancelledContext(t *testing.T) {
	checker := &countingChecker{interval: time.Minute}
	engine := NewEngine(checker)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)

		engine.RunAfter(ctx, make(chan struct{})) // never closed
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunAfter did not return when the context was cancelled")
	}

	if runs := checker.runs.Load(); runs != 0 {
		t.Errorf("the checker ran %d times, want 0 — the signal never arrived", runs)
	}
}
