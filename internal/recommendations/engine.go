package recommendations

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Checker produces recommendations from a specific domain.
type Checker interface {
	Name() string
	Interval() time.Duration
	Check(ctx context.Context) []Recommendation
}

type checkerState struct {
	checker Checker
	last    []Recommendation
	lastRun time.Time
}

// Engine runs all registered checkers and merges results.
type Engine struct {
	checkers []checkerState
	mu       sync.RWMutex
	results  []Recommendation
	lastTick time.Time
}

// NewEngine creates an engine with the given checkers.
// Returns nil if no checkers are provided.
func NewEngine(checkers ...Checker) *Engine {
	if len(checkers) == 0 {
		return nil
	}
	states := make([]checkerState, len(checkers))
	for i, c := range checkers {
		states[i] = checkerState{checker: c}
	}
	return &Engine{checkers: states}
}

// Run starts the engine tick loop. Ticks every 60 seconds,
// running only checkers whose interval has elapsed. Blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	if e == nil {
		return
	}
	e.tick(ctx, true) // force all on startup
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx, false)
		}
	}
}

// Results returns the latest merged recommendations. Nil-safe.
func (e *Engine) Results() []Recommendation {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Recommendation, len(e.results))
	copy(out, e.results)
	return out
}

// Summary returns severity counts. Nil-safe.
func (e *Engine) Summary() Summary {
	return ComputeSummary(e.Results())
}

// tick runs eligible checkers and merges results. Only called from the
// single goroutine in Run, so checker state writes are safe without locks.
func (e *Engine) tick(ctx context.Context, force bool) {
	now := time.Now()

	// Determine which checkers need to run.
	var toRun []int
	for i := range e.checkers {
		cs := &e.checkers[i]
		if force || now.Sub(cs.lastRun) >= cs.checker.Interval() {
			toRun = append(toRun, i)
		}
	}
	if len(toRun) == 0 {
		return
	}

	// Run eligible checkers in parallel.
	type result struct {
		index int
		recs  []Recommendation
	}
	ch := make(chan result, len(toRun))
	for _, i := range toRun {
		go func(idx int) {
			tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			recs := e.checkers[idx].checker.Check(tickCtx)
			ch <- result{idx, recs}
		}(i)
	}
	for range toRun {
		r := <-ch
		e.checkers[r.index].last = r.recs
		e.checkers[r.index].lastRun = now
	}

	// Merge all cached results and sort by severity.
	var merged []Recommendation
	for _, cs := range e.checkers {
		merged = append(merged, cs.last...)
	}
	sortBySeverity(merged)

	e.mu.Lock()
	e.results = merged
	e.lastTick = now
	e.mu.Unlock()

	slog.Debug("recommendations: tick complete", "checkers_run", len(toRun), "total", len(merged))
}

// LastTick returns the time of the most recent completed tick. Nil-safe.
func (e *Engine) LastTick() time.Time {
	if e == nil {
		return time.Time{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastTick
}

func sortBySeverity(recs []Recommendation) {
	rank := map[Severity]int{SeverityCritical: 0, SeverityWarning: 1, SeverityInfo: 2}
	slices.SortFunc(recs, func(a, b Recommendation) int {
		return rank[a.Severity] - rank[b.Severity]
	})
}
