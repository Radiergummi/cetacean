package timeline_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/timeline"
)

// restoreCache does what startup does with the store.
func restoreCache(t testing.TB, store *timeline.SQLite) *cache.Cache {
	t.Helper()

	c := cache.New(nil)

	entries, last, err := store.Restore(context.Background(), c.History().Size())
	if err != nil {
		t.Fatal(err)
	}

	annotations, err := store.Annotations(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	c.History().Restore(entries, last)
	c.RestoreAnnotations(store, annotations)

	return c
}

func TestAnnotations_SurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.db")
	ctx := context.Background()

	store := openSQLite(t, path)
	c := restoreCache(t, store)

	a, err := c.CreateAnnotation(ctx, cache.Annotation{Timestamp: time.Now(), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}

	edited, err := c.UpdateAnnotation(ctx, a.ID, func(a *cache.Annotation) error {
		a.Text = "y"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openSQLite(t, path)
	defer store.Close() //nolint:errcheck // test cleanup

	c = restoreCache(t, store)

	if got, ok := c.Annotation(a.ID); !ok || got.Text != "y" || got.Version != edited.Version {
		t.Fatalf("restored %+v, ok=%v; want the edit", got, ok)
	}

	next, err := c.CreateAnnotation(ctx, cache.Annotation{Timestamp: time.Now(), Text: "z"})
	if err != nil || next.ID <= edited.Version {
		t.Fatalf("next ID %d, err=%v; want past version %d", next.ID, err, edited.Version)
	}
}

// BenchmarkCreateAnnotation measures how long a create holds notifyMu, which
// every event waits on: the whole call runs under it.
func BenchmarkCreateAnnotation(b *testing.B) {
	store, err := timeline.OpenSQLite(
		context.Background(),
		filepath.Join(b.TempDir(), "cetacean.db"),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close() //nolint:errcheck // benchmark cleanup

	c := restoreCache(b, store)
	a := cache.Annotation{
		Timestamp: time.Now(),
		Text:      "load test started",
		Tags:      []string{"loadtest", "incident"},
		Resources: []cache.AnnotationResource{
			{Type: cache.EventService, ID: "svc1", Name: "web", Stack: "shop"},
		},
		Actor: &cache.Actor{Subject: "alice", Provider: "oidc", Transport: cache.TransportREST},
	}

	durations := make([]time.Duration, 0, b.N)

	for b.Loop() {
		start := time.Now()

		created, err := c.CreateAnnotation(context.Background(), a)
		if err != nil {
			b.Fatal(err)
		}

		durations = append(durations, time.Since(start))

		// Under the cap, however long the benchmark runs.
		if err := c.DeleteAnnotation(
			context.Background(),
			created.ID,
			func(cache.Annotation) error { return nil },
		); err != nil {
			b.Fatal(err)
		}
	}

	slices.Sort(durations)
	b.ReportMetric(float64(durations[len(durations)/2].Microseconds()), "p50-µs")
	b.ReportMetric(float64(durations[len(durations)*99/100].Microseconds()), "p99-µs")
	b.ReportMetric(float64(durations[len(durations)-1].Microseconds()), "max-µs")
}
