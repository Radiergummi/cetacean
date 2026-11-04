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
				ID:          8,
				Kind:        cache.KindChange,
				Timestamp:   at.Add(time.Second),
				Type:        cache.EventTask,
				Action:      "update",
				ResourceID:  "task1",
				ServiceID:   "svc1",
				Slot:        2,
				NodeID:      "node1",
				ServiceName: "web",
			},
			{
				ID:         9,
				Kind:       cache.KindChange,
				Timestamp:  at.Add(30 * time.Minute),
				Type:       cache.EventConfig,
				Action:     "create",
				ResourceID: "cfg1",
				Name:       "app_settings",
				Recovered:  true,
			},
			{
				ID:            10,
				Kind:          cache.KindGap,
				Timestamp:     at.Add(2 * time.Second),
				Until:         at.Add(time.Hour),
				RecoveredFrom: at.Add(20 * time.Minute),
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

			if !got[i].RecoveredFrom.Equal(want[i].RecoveredFrom) {
				t.Errorf(
					"entry %d: RecoveredFrom = %v, want %v",
					i,
					got[i].RecoveredFrom,
					want[i].RecoveredFrom,
				)
			}

			got[i].Timestamp, got[i].Until = want[i].Timestamp, want[i].Until
			got[i].RecoveredFrom = want[i].RecoveredFrom
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

	// A newer binary's kinds share the sequence; a rollback that restarted
	// it below them would reuse their IDs, and the store would drop the new.
	t.Run("restore reports the largest ID of any kind", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		batch := entries(1, 2, 3)
		batch[2].Kind = cache.KindAnnotation
		commit(t, store, timeline.Batch{Entries: batch})

		got, last, err := store.Restore(context.Background(), 10)
		if err != nil || !slices.Equal(ids(got), []uint64{1, 2}) || last != 3 {
			t.Fatalf("Restore = %v, last %d, err=%v; want [1 2] and 3", ids(got), last, err)
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

	t.Run("retention counts what was stored before a restart", func(t *testing.T) {
		open := newStore(t)

		store := open()
		commit(t, store, timeline.Batch{Entries: entries(1, 2, 3, 4)})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(5), Keep: 3})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{3, 4, 5}) {
			t.Fatalf("Restore = %v, want [3 4 5]", got)
		}
	})

	t.Run("retention counts an entry committed again once", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(1, 2, 3), Keep: 3})
		commit(t, store, timeline.Batch{Entries: entries(2, 3, 4), Keep: 3})

		if got := ids(restore(t, store, 10)); !slices.Equal(got, []uint64{2, 3, 4}) {
			t.Fatalf("Restore = %v, want [2 3 4]", got)
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

		if _, ok := startCoverage(t, store, start); ok {
			t.Fatal("first start of coverage found a previous one")
		}

		seen := start.Add(time.Hour)
		commit(t, store, timeline.Batch{SeenAt: seen})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		previous, ok := startCoverage(t, store, seen.Add(time.Hour))
		if !ok || !previous.Equal(seen) {
			t.Fatalf("previous coverage = %v, ok=%v; want %v", previous, ok, seen)
		}
	})

	// Every heartbeat commits the entries before it, a gap included, so a
	// stretch never heartbeated may have lost its gap; the next one covers it.
	t.Run("coverage that never heartbeated is skipped", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()
		startCoverage(t, store, start)

		commit(t, store, timeline.Batch{SeenAt: start.Add(time.Minute)})
		closeStore(t, store)

		store = open()
		startCoverage(t, store, start.Add(time.Hour))

		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		previous, ok := startCoverage(t, store, start.Add(2*time.Hour))
		if !ok || !previous.Equal(start.Add(time.Minute)) {
			t.Fatalf(
				"previous coverage = %v, ok=%v; want the last heartbeat, %v",
				previous, ok, start.Add(time.Minute),
			)
		}
	})

	t.Run("a heartbeat on a clock that stepped back still counts", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()
		startCoverage(t, store, start)
		commit(t, store, timeline.Batch{SeenAt: start.Add(-time.Minute)})
		closeStore(t, store)

		store = open()
		startCoverage(t, store, start.Add(time.Hour))
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		if previous, ok := startCoverage(t, store, start.Add(2*time.Hour)); !ok ||
			previous.Before(start) || previous.After(start.Add(time.Second)) {
			t.Fatalf("previous coverage = %v, ok=%v; want the heartbeated %v", previous, ok, start)
		}
	})

	// Retention may run before the next process opens coverage; the row its
	// gap starts from must survive, though a newer silent row exists.
	t.Run("retention keeps the newest heartbeated coverage", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()
		startCoverage(t, store, start)
		commit(t, store, timeline.Batch{SeenAt: start.Add(time.Minute)})
		closeStore(t, store)

		store = open()
		startCoverage(t, store, start.Add(time.Hour))
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		later := entries(1, 2, 3)
		for i := range later {
			later[i].Timestamp = start.Add(2 * time.Hour)
		}

		commit(t, store, timeline.Batch{Entries: later, Keep: 2})

		if previous, ok := startCoverage(t, store, start.Add(3*time.Hour)); !ok ||
			!previous.Equal(start.Add(time.Minute)) {
			t.Fatalf("previous coverage = %v, ok=%v; want the last heartbeat", previous, ok)
		}
	})

	t.Run("a heartbeat before coverage opens is ignored", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{SeenAt: time.Now()})

		if _, ok := startCoverage(t, store, time.Now()); ok {
			t.Fatal("a heartbeat before coverage opened left a previous one")
		}
	})

	// A process that has not opened coverage yet still retains; the previous
	// process's row is where its gap will start, however old.
	t.Run("retention keeps the newest coverage", func(t *testing.T) {
		open := newStore(t)
		start := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

		store := open()
		startCoverage(t, store, start)

		commit(t, store, timeline.Batch{SeenAt: start.Add(time.Minute)})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		later := entries(1, 2, 3)
		for i := range later {
			later[i].Timestamp = start.Add(time.Hour)
		}

		commit(t, store, timeline.Batch{Entries: later, Keep: 2})

		previous, ok := startCoverage(t, store, start.Add(2*time.Hour))
		if !ok || !previous.Equal(start.Add(time.Minute)) {
			t.Fatalf("previous coverage = %v, ok=%v; want the previous heartbeat", previous, ok)
		}
	})
	t.Run("annotations survive a restart with every field", func(t *testing.T) {
		open := newStore(t)
		want := []cache.Annotation{fullAnnotation(), {
			ID:        12,
			Version:   12,
			Timestamp: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
			Text:      "cluster-wide",
			Created:   time.Date(2026, 11, 1, 9, 1, 0, 0, time.UTC),
		}}

		store := open()
		for _, a := range want {
			putAnnotation(t, store, a)
		}
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		assertAnnotations(t, annotations(t, store), want)
	})

	t.Run("an annotation put again replaces it", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		a := fullAnnotation()
		putAnnotation(t, store, a)

		a.Version, a.Text, a.Until = 20, "edited", time.Time{}
		a.Tags = []string{"rollback"}
		a.Resources = a.Resources[1:]
		a.Updated = a.Created.Add(time.Hour)
		a.Editor = &cache.Actor{Subject: "bob", Provider: "oidc", Transport: cache.TransportREST}
		putAnnotation(t, store, a)

		assertAnnotations(t, annotations(t, store), []cache.Annotation{a})
	})

	t.Run("a deleted annotation is gone", func(t *testing.T) {
		open := newStore(t)

		store := open()
		a := fullAnnotation()
		putAnnotation(t, store, a)

		deleteAnnotation(t, store, a.ID)
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		assertAnnotations(t, annotations(t, store), nil)
	})

	// An annotation's writes take sequence IDs the timeline table never holds.
	t.Run("restore reports the largest annotation version", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		commit(t, store, timeline.Batch{Entries: entries(1, 2)})

		a := fullAnnotation()
		a.ID, a.Version = 3, 9
		putAnnotation(t, store, a)

		if _, last, err := store.Restore(context.Background(), 10); err != nil || last != 9 {
			t.Fatalf("Restore last = %d, err=%v; want 9", last, err)
		}
	})

	// A removal's ID is stored nowhere else, so only Last keeps it from coming back.
	t.Run("restore reports the last ID handed out, never less", func(t *testing.T) {
		open := newStore(t)
		store := open()

		a := fullAnnotation()
		a.ID, a.Version = 3, 3
		putAnnotation(t, store, a)

		deleteAnnotation(t, store, a.ID)

		commit(t, store, timeline.Batch{Last: 4})
		commit(t, store, timeline.Batch{Last: 2})
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		if _, last, err := store.Restore(context.Background(), 10); err != nil || last != 4 {
			t.Fatalf("Restore last = %d, err=%v; want 4", last, err)
		}
	})

	t.Run("the instance survives a restart and differs between stores", func(t *testing.T) {
		open := newStore(t)
		store := open()
		instance := store.Instance()
		closeStore(t, store)

		store = open()
		defer closeStore(t, store)

		if instance == "" || store.Instance() != instance {
			t.Fatalf("Instance = %q after a restart, want %q", store.Instance(), instance)
		}

		other := newStore(t)()
		defer closeStore(t, other)

		if other.Instance() == instance {
			t.Fatalf("two stores share the instance %q", instance)
		}
	})

	t.Run("retention leaves annotations alone", func(t *testing.T) {
		store := newStore(t)()
		defer closeStore(t, store)

		a := fullAnnotation()
		a.ID, a.Version = 1, 1
		putAnnotation(t, store, a)

		commit(t, store, timeline.Batch{Entries: entries(2, 3, 4), Keep: 1})

		assertAnnotations(t, annotations(t, store), []cache.Annotation{a})
	})
}

// startCoverage does what a writer does once ready: reads when the previous
// stretch was last seen, then opens its own.
func startCoverage(t *testing.T, store timeline.Store, at time.Time) (time.Time, bool) {
	t.Helper()

	previous, ok, err := store.LastSeen(context.Background())
	if err != nil {
		t.Fatalf("LastSeen: %v", err)
	}

	if err := store.OpenCoverage(context.Background(), at); err != nil {
		t.Fatalf("OpenCoverage: %v", err)
	}

	return previous, ok
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

	entries, _, err := store.Restore(context.Background(), limit)
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

func fullAnnotation() cache.Annotation {
	at := time.Date(2026, 11, 1, 14, 0, 0, 123456789, time.UTC)

	return cache.Annotation{
		ID:        10,
		Version:   11,
		Timestamp: at,
		Until:     at.Add(30 * time.Minute),
		Text:      "load test started",
		Tags:      []string{"loadtest", "db.failover"},
		Resources: []cache.AnnotationResource{
			{Type: cache.EventService, ID: "svc1", Name: "web", Stack: "shop"},
			{Type: cache.EventStack, ID: "shop", Name: "shop"},
			{Type: cache.EventNode, ID: "node1", Name: "manager-1"},
		},
		Actor: &cache.Actor{
			Subject:   "alice",
			Name:      "Alice",
			Provider:  "oidc",
			Client:    "claude",
			Transport: cache.TransportMCP,
		},
		Created: at.Add(time.Hour),
	}
}

func putAnnotation(t *testing.T, store timeline.Store, a cache.Annotation) {
	t.Helper()

	write := beginAnnotation(t, store)
	defer write.Release()

	if err := write.Put(context.Background(), a); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func deleteAnnotation(t *testing.T, store timeline.Store, id uint64) {
	t.Helper()

	write := beginAnnotation(t, store)
	defer write.Release()

	if err := write.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func beginAnnotation(t *testing.T, store timeline.Store) cache.AnnotationWrite {
	t.Helper()

	write, err := store.BeginAnnotation(context.Background())
	if err != nil {
		t.Fatalf("BeginAnnotation: %v", err)
	}

	return write
}

func annotations(t *testing.T, store timeline.Store) []cache.Annotation {
	t.Helper()

	result, err := store.Annotations(context.Background())
	if err != nil {
		t.Fatalf("Annotations: %v", err)
	}

	return result
}

// assertAnnotations compares times by instant, since a store need not keep
// their location.
func assertAnnotations(t *testing.T, got, want []cache.Annotation) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("Annotations = %d, want %d:\n%+v", len(got), len(want), got)
	}

	for i := range want {
		g, w := got[i], want[i]

		for _, pair := range [][2]time.Time{
			{g.Timestamp, w.Timestamp},
			{g.Until, w.Until},
			{g.Created, w.Created},
			{g.Updated, w.Updated},
		} {
			if !pair[0].Equal(pair[1]) {
				t.Errorf("annotation %d: time %v, want %v", w.ID, pair[0], pair[1])
			}
		}

		g.Timestamp, g.Until, g.Created, g.Updated = w.Timestamp, w.Until, w.Created, w.Updated
		if !reflect.DeepEqual(g, w) {
			t.Errorf("annotation %d:\n got %+v\nwant %+v", w.ID, g, w)
		}
	}
}
