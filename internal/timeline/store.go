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
	// Restore returns the newest limit entries of the kinds the ring holds,
	// oldest first, and the largest ID ever committed, annotation versions
	// and removals included.
	Restore(ctx context.Context, limit int) (entries []cache.HistoryEntry, last uint64, err error)

	// Annotations returns every annotation, by ID. Retention never removes one.
	Annotations(ctx context.Context) ([]cache.Annotation, error)

	// PutAnnotation stores an annotation, replacing the one under its ID, and
	// DeleteAnnotation removes one; each commits before it returns.
	cache.AnnotationStore

	// LastSeen is when an earlier stretch of watching was last seen; ok=false
	// when none exists. A stretch never heartbeated never committed its gap,
	// so it is skipped for the one before it: the next gap then covers both.
	LastSeen(ctx context.Context) (at time.Time, ok bool, err error)

	// OpenCoverage starts this process's stretch of watching at at.
	OpenCoverage(ctx context.Context, at time.Time) error

	// Commit applies a batch in one transaction, or none of it.
	Commit(ctx context.Context, batch Batch) error

	// Instance names this database for good: a new or recreated one never
	// shares it. A copy keeps it, so restoring an older copy can repeat the
	// IDs qualified by it that the newer one handed out.
	Instance() string

	Close() error
}

// Batch is one tick of the writer.
type Batch struct {
	// Entries are inserted by their own IDs; one already stored is skipped,
	// so a batch whose commit failed can be committed again.
	Entries []cache.HistoryEntry

	// SeenAt extends the open coverage; it is ignored when none is open.
	SeenAt time.Time

	// Last, when positive, is the newest ID the ring handed out; Restore
	// never reports less, even once nothing stored carries it.
	Last uint64

	// Keep, when positive, deletes all but the newest Keep entries, and the
	// coverage that ended before the oldest of them.
	Keep int
}
