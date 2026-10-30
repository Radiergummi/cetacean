// Package storetest is the conformance suite every timeline.Store passes.
package storetest

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/timeline"
)

// Factory prepares an empty backing store and returns a function opening it,
// which the suite calls again after closing to stand in for a restart.
type Factory func(t *testing.T) (open func() timeline.Store)

// Run runs the suite against the stores newStore opens.
func Run(t *testing.T, newStore Factory) {
	t.Run("an empty store restores nothing", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		if entries := restore(t, store, 10); len(entries) != 0 {
			t.Fatalf("Restore = %d entries, want none", len(entries))
		}
	})

	t.Run("entries survive a restart with every field", func(t *testing.T) {
		open := newStore(t)
		at := time.Date(2026, 10, 29, 14, 2, 3, 456789000, time.UTC)

		want := []cache.HistoryEntry{
			{
				ID:         7,
				Kind:       cache.KindChange,
				Timestamp:  at,
				Type:       cache.EventService,
				Action:     "update",
				ResourceID: "svc1",
				Name:       "web",
				Summary:    "image changed",
				Service:    "web",
				Stack:      "shop",
				Actor: &cache.Actor{
					Subject:   "alice",
					Name:      "Alice",
					Provider:  "oidc",
					Client:    "claude",
					Transport: cache.TransportMCP,
				},
			},
			{
				ID:         8,
				Kind:       cache.KindChange,
				Timestamp:  at.Add(time.Second),
				Type:       cache.EventTask,
				Action:     "update",
				ResourceID: "task1",
				ServiceID:  "svc1",
			},
			{
				ID:        9,
				Kind:      cache.KindGap,
				Timestamp: at.Add(2 * time.Second),
				Until:     at.Add(time.Hour),
			},
		}

		store := open()
		commit(t, store, timeline.Batch{Entries: want})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		got := restore(t, store, 10)
		if len(got) != len(want) {
			t.Fatalf("Restore = %d entries, want %d", len(got), len(want))
		}

		for i := range want {
			if !got[i].Timestamp.Equal(want[i].Timestamp) {
				t.Errorf(
					"entry %d: Timestamp = %v, want %v",
					i,
					got[i].Timestamp,
					want[i].Timestamp,
				)
			}

			if !got[i].Until.Equal(want[i].Until) {
				t.Errorf("entry %d: Until = %v, want %v", i, got[i].Until, want[i].Until)
			}

			got[i].Timestamp, got[i].Until = want[i].Timestamp, want[i].Until
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("restore returns the newest, oldest first", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(1, 2, 3, 4, 5)})

		if got := ids(restore(t, store, 3)); !slices.Equal(got, []uint64{3, 4, 5}) {
			t.Fatalf("Restore(3) = %v, want [3 4 5]", got)
		}
	})

	t.Run("IDs are the recorder's, gaps included", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(3, 40, 41)})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{3, 40, 41}) {
			t.Fatalf("Restore = %v, want [3 40 41]", got)
		}
	})

	t.Run("a batch committed again is stored once", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(1, 2)})
		commit(t, store, timeline.Batch{Entries: entries(1, 2, 3)})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{1, 2, 3}) {
			t.Fatalf("Restore = %v, want [1 2 3]", got)
		}
	})

	t.Run("retention keeps the newest", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(1, 2, 3, 4, 5), Keep: 3})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{3, 4, 5}) {
			t.Fatalf("Restore = %v, want [3 4 5]", got)
		}

		commit(t, store, timeline.Batch{Keep: 10})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{3, 4, 5}) {
			t.Fatalf("after a larger Keep, Restore = %v, want [3 4 5]", got)
		}
	})

	t.Run("retention counts gaps with changes", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		batch := entries(1, 2, 3, 4)
		batch[2] = cache.HistoryEntry{
			ID:        3,
			Kind:      cache.KindGap,
			Timestamp: batch[1].Timestamp,
			Until:     batch[3].Timestamp,
		}

		commit(t, store, timeline.Batch{Entries: batch, Keep: 2})

		got := restore(t, store, 10)
		if !slices.Equal(ids(got), []uint64{3, 4}) || got[0].Kind != cache.KindGap {
			t.Fatalf("Restore = %+v, want the gap 3 and the change 4", got)
		}
	})

	t.Run("coverage reports the previous process's last heartbeat", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()

		if _, ok, err := store.OpenCoverage(context.Background(), start); err != nil || ok {
			t.Fatalf("first OpenCoverage: ok=%v, err=%v; want no previous coverage", ok, err)
		}

		seen := start.Add(time.Hour)
		commit(t, store, timeline.Batch{SeenAt: seen})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		previous, ok, err := store.OpenCoverage(context.Background(), seen.Add(time.Hour))
		if err != nil || !ok || !previous.Equal(seen) {
			t.Fatalf("OpenCoverage = %v, ok=%v, err=%v; want %v", previous, ok, err, seen)
		}
	})

	t.Run("a heartbeat before coverage opens is ignored", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{SeenAt: time.Now()})

		if _, ok, err := store.OpenCoverage(context.Background(), time.Now()); err != nil || ok {
			t.Fatalf("OpenCoverage: ok=%v, err=%v; want no previous coverage", ok, err)
		}
	})

	// A process that has not opened coverage yet still retains; the previous
	// process's row is where its gap will start, however old.
	t.Run("retention keeps the newest coverage", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()
		if _, _, err := store.OpenCoverage(context.Background(), start); err != nil {
			t.Fatal(err)
		}

		commit(t, store, timeline.Batch{SeenAt: start.Add(time.Minute)})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		later := entries(1, 2, 3)
		for i := range later {
			later[i].Timestamp = start.Add(time.Hour)
		}

		commit(t, store, timeline.Batch{Entries: later, Keep: 2})

		previous, ok, err := store.OpenCoverage(context.Background(), start.Add(2*time.Hour))
		if err != nil || !ok || !previous.Equal(start.Add(time.Minute)) {
			t.Fatalf(
				"OpenCoverage = %v, ok=%v, err=%v; want the previous heartbeat",
				previous,
				ok,
				err,
			)
		}
	})
}

func entries(ids ...uint64) []cache.HistoryEntry {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	result := make([]cache.HistoryEntry, 0, len(ids))
	for _, id := range ids {
		result = append(result, cache.HistoryEntry{
			ID:         id,
			Kind:       cache.KindChange,
			Timestamp:  at.Add(time.Duration(id) * time.Second),
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: "svc1",
			Name:       "web",
		})
	}

	return result
}

func ids(entries []cache.HistoryEntry) []uint64 {
	result := make([]uint64, 0, len(entries))
	for _, e := range entries {
		result = append(result, e.ID)
	}

	return result
}

func commit(t *testing.T, store timeline.Store, batch timeline.Batch) {
	t.Helper()

	if err := store.Commit(context.Background(), batch); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func restore(t *testing.T, store timeline.Store, limit int) []cache.HistoryEntry {
	t.Helper()

	entries, err := store.Restore(context.Background(), limit)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	return entries
}

func closeStore(t *testing.T, store timeline.Store) {
	t.Helper()

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
