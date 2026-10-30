// Package timeline persists the change history, so a restart keeps the record.
package timeline

import (
	"context"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Store holds the timeline. Callers never see its SQL, so another database
// can implement it; storetest is the suite every implementation passes.
type Store interface {
	// Restore returns the newest limit entries, oldest first.
	Restore(ctx context.Context, limit int) ([]cache.HistoryEntry, error)

	// OpenCoverage starts this process's stretch of watching at at, and
	// returns when the previous one was last seen; ok=false when none exists.
	// A stretch never heartbeated never committed its gap, so it is skipped
	// for the one before it: the next gap then covers both.
	OpenCoverage(ctx context.Context, at time.Time) (previous time.Time, ok bool, err error)

	// Commit applies a batch in one transaction, or none of it.
	Commit(ctx context.Context, batch Batch) error

	Close() error
}

// Batch is one tick of the writer.
type Batch struct {
	// Entries are inserted by their own IDs; one already stored is skipped,
	// so a batch whose commit failed can be committed again.
	Entries []cache.HistoryEntry

	// SeenAt extends the open coverage; it is ignored when none is open.
	SeenAt time.Time

	// Keep, when positive, deletes all but the newest Keep entries, and the
	// coverage that ended before the oldest of them.
	Keep int
}
