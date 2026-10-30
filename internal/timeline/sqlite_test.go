package timeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/timeline"
	"github.com/radiergummi/cetacean/internal/timeline/storetest"
)

func TestSQLite(t *testing.T) {
	storetest.Run(t, func(t *testing.T) func() timeline.Store {
		path := filepath.Join(t.TempDir(), "cetacean.db")

		return func() timeline.Store { return openSQLite(t, path) }
	})
}

// Opened once before, so the second open finds nothing to migrate and must
// take the lock all the same.
func TestSQLite_SecondProcessIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.db")

	if err := openSQLite(t, path).Close(); err != nil {
		t.Fatal(err)
	}

	first := openSQLite(t, path)
	defer first.Close() //nolint:errcheck // test cleanup

	second, err := timeline.OpenSQLite(context.Background(), path)
	if err == nil {
		_ = second.Close()
		t.Fatal("a second open of a held database succeeded")
	}

	// Startup waits on this error alone, for a process that is letting go.
	if !errors.Is(err, timeline.ErrHeld) {
		t.Fatalf("second open: %v, want ErrHeld", err)
	}

	if err := first.Commit(context.Background(), timeline.Batch{SeenAt: time.Now()}); err != nil {
		t.Fatalf("the first holder cannot commit after the refused open: %v", err)
	}
}

func TestSQLite_FilesAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.db")

	store := openSQLite(t, path)
	defer store.Close() //nolint:errcheck // test cleanup

	err := store.Commit(context.Background(), timeline.Batch{
		Entries: []cache.HistoryEntry{{ID: 1, Timestamp: time.Now(), Type: cache.EventService}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{path, path + "-wal"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}

		if mode := info.Mode().Perm(); mode != 0600 {
			t.Errorf("%s: mode %o, want 600", filepath.Base(name), mode)
		}
	}
}

// What a newer binary may have done: added a column and a migration this one
// does not know. Naming every column keeps this one reading and writing.
func TestSQLite_OpensANewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.db")
	ctx := context.Background()

	store := openSQLite(t, path)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	newer := timeline.OpenRaw(t, path)
	for _, stmt := range []string{
		`ALTER TABLE timeline ADD COLUMN later TEXT`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (9999, 0)`,
		`INSERT INTO timeline (id, kind, at, later) VALUES (1, 'later-kind', 0, 'x')`,
	} {
		if _, err := newer.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := newer.Close(); err != nil {
		t.Fatal(err)
	}

	store = openSQLite(t, path)

	err := store.Commit(ctx, timeline.Batch{
		Entries: []cache.HistoryEntry{
			{ID: 2, Timestamp: time.Now(), Type: cache.EventService},
			{ID: 3, Timestamp: time.Now(), Type: cache.EventService},
		},
		Keep: 1,
	})
	if err != nil {
		t.Fatalf("Commit against a newer schema: %v", err)
	}

	entries, err := store.Restore(ctx, 10)
	if err != nil || len(entries) != 1 || entries[0].ID != 3 {
		t.Fatalf("Restore = %+v, err=%v; want only the change 3", entries, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	raw := timeline.OpenRaw(t, path)
	defer raw.Close() //nolint:errcheck // test cleanup

	var kept int
	if err := raw.QueryRowContext(
		ctx, `SELECT count(*) FROM timeline WHERE kind = 'later-kind'`,
	).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("rows of the unknown kind = %d, err=%v; want it left alone", kept, err)
	}
}

func TestSQLite_MigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.db")

	for range 2 {
		if err := openSQLite(t, path).Close(); err != nil {
			t.Fatal(err)
		}
	}

	db := timeline.OpenRaw(t, path)
	defer db.Close() //nolint:errcheck // test cleanup

	var versions []int64

	rows, err := db.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}

		versions = append(versions, v)
	}

	if !slices.Equal(versions, []int64{1}) {
		t.Fatalf("schema_migrations = %v, want [1]", versions)
	}
}

func openSQLite(t *testing.T, path string) *timeline.SQLite {
	t.Helper()

	store, err := timeline.OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}

	return store
}
