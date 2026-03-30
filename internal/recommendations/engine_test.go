package recommendations

import (
	"context"
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
