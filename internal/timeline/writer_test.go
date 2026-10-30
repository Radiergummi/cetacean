package timeline

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// fakeStore records batches and fails a commit while err is set.
type fakeStore struct {
	mu       sync.Mutex
	err      error
	batches  []Batch
	coverage int

	// previous is the heartbeat OpenCoverage reports; zero reports none.
	previous time.Time

	// cancelled records a store call made with a cancelled context.
	cancelled bool
}

func (f *fakeStore) Restore(context.Context, int) ([]cache.HistoryEntry, uint64, error) {
	return nil, 0, nil
}

func (f *fakeStore) OpenCoverage(context.Context, time.Time) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return time.Time{}, false, f.err
	}

	f.coverage++

	return f.previous, !f.previous.IsZero(), nil
}

func (f *fakeStore) Commit(ctx context.Context, batch Batch) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cancelled = f.cancelled || ctx.Err() != nil

	if f.err != nil {
		return f.err
	}

	f.batches = append(f.batches, batch)

	return nil
}

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) committed() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	var ids []uint64
	for _, b := range f.batches {
		for _, e := range b.Entries {
			ids = append(ids, e.ID)
		}
	}

	return ids
}

func (f *fakeStore) coverages() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.coverage
}

func noGap(time.Time) {}

func appendN(h *cache.History, n int) {
	for range n {
		h.Append(cache.HistoryEntry{Type: cache.EventService, ResourceID: "svc1"})
	}
}

func TestWriter_WritesEachEntryOnce(t *testing.T) {
	store := &fakeStore{}
	history := cache.NewHistory(10)
	w := NewWriter(store, history, 10, noGap)

	appendN(history, 2)
	w.flush(context.Background())
	w.flush(context.Background())
	appendN(history, 1)
	w.flush(context.Background())

	if got := store.committed(); !slices.Equal(got, []uint64{1, 2, 3}) {
		t.Fatalf("committed %v, want [1 2 3]", got)
	}
}

func TestWriter_StartsAfterTheRestoredEntries(t *testing.T) {
	store := &fakeStore{}
	history := cache.NewHistory(10)
	history.Restore([]cache.HistoryEntry{{ID: 41, Type: cache.EventService}}, 41)

	w := NewWriter(store, history, 10, noGap)
	appendN(history, 1)
	w.flush(context.Background())

	if got := store.committed(); !slices.Equal(got, []uint64{42}) {
		t.Fatalf("committed %v, want only the new [42]", got)
	}
}

func TestWriter_RetriesAFailedCommit(t *testing.T) {
	store := &fakeStore{err: errors.New("disk full")}
	history := cache.NewHistory(10)
	w := NewWriter(store, history, 10, noGap)

	appendN(history, 2)
	w.flush(context.Background())

	store.err = nil
	appendN(history, 1)
	w.flush(context.Background())

	if got := store.committed(); !slices.Equal(got, []uint64{1, 2, 3}) {
		t.Fatalf("committed %v, want [1 2 3] after the retry", got)
	}
}

func TestWriter_OverrunWritesTheSurvivors(t *testing.T) {
	store := &fakeStore{}
	history := cache.NewHistory(3)
	w := NewWriter(store, history, 3, noGap)

	appendN(history, 5)
	w.flush(context.Background())

	if got := store.committed(); !slices.Equal(got, []uint64{3, 4, 5}) {
		t.Fatalf("committed %v, want the survivors [3 4 5]", got)
	}
}

func TestWriter_RetainsOnTheFirstTickAndAfterInserts(t *testing.T) {
	store := &fakeStore{}
	history := cache.NewHistory(10)
	w := NewWriter(store, history, 10, noGap)

	w.flush(context.Background())
	w.flush(context.Background())
	appendN(history, 1)
	w.flush(context.Background())

	var keeps []int
	for _, b := range store.batches {
		keeps = append(keeps, b.Keep)
	}

	if !slices.Equal(keeps, []int{10, 0, 10}) {
		t.Fatalf("Keep per batch = %v, want [10 0 10]", keeps)
	}
}

func TestWriter_HeartbeatsOnlyOnceCovering(t *testing.T) {
	store := &fakeStore{}
	history := cache.NewHistory(10)
	w := NewWriter(store, history, 10, noGap)

	w.flush(context.Background())
	w.openCoverage(context.Background())
	w.flush(context.Background())

	if !store.batches[0].SeenAt.IsZero() || store.batches[1].SeenAt.IsZero() {
		t.Fatalf("SeenAt = %v, %v; want zero before coverage and set after",
			store.batches[0].SeenAt, store.batches[1].SeenAt)
	}
}

func TestWriter_Run(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{}
		history := cache.NewHistory(10)
		w := NewWriter(store, history, 10, noGap)

		ready := make(chan struct{})
		ctx, cancel := context.WithCancel(t.Context())
		stopped := make(chan struct{})

		go func() {
			defer close(stopped)
			w.Run(ctx, ready)
		}()

		close(ready)
		synctest.Wait()

		if n := store.coverages(); n != 1 {
			t.Fatalf("coverage opened %d times once ready, want 1", n)
		}

		appendN(history, 1)
		time.Sleep(time.Second)
		synctest.Wait()

		if got := store.committed(); !slices.Equal(got, []uint64{1}) {
			t.Fatalf("committed %v after a tick, want [1]", got)
		}

		appendN(history, 1)
		cancel()
		<-stopped

		if got := store.committed(); !slices.Equal(got, []uint64{1, 2}) {
			t.Fatalf("committed %v, want [1 2] by the final flush", got)
		}

		if store.cancelled {
			t.Fatal("a commit ran under the cancelled context")
		}
	})
}

func TestWriter_RecordsTheGapSinceThePreviousHeartbeat(t *testing.T) {
	previous := time.Now().Add(-time.Hour)
	store := &fakeStore{err: errors.New("disk full"), previous: previous}

	var gaps []time.Time
	w := NewWriter(store, cache.NewHistory(10), 10, func(from time.Time) {
		gaps = append(gaps, from)
	})

	w.openCoverage(context.Background())

	if len(gaps) != 0 {
		t.Fatalf("recorded %v while coverage failed to open, want nothing", gaps)
	}

	store.err = nil
	w.openCoverage(context.Background())

	if len(gaps) != 1 || !gaps[0].Equal(previous) {
		t.Fatalf("recorded %v, want one gap from %v", gaps, previous)
	}
}

func TestWriter_RecordsNoGapWithoutAPreviousProcess(t *testing.T) {
	w := NewWriter(&fakeStore{}, cache.NewHistory(10), 10, func(from time.Time) {
		t.Fatalf("recorded a gap from %v with no previous coverage", from)
	})

	w.openCoverage(context.Background())
}
