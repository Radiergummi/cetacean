package timeline

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// quietHeartbeat is how often a tick that inserts nothing heartbeats. A crash
// widens the next gap's start by at most this much; nothing is lost in it, as
// every insert heartbeats.
const quietHeartbeat = 15 * time.Second

// Writer copies the ring into a Store once a second, following the ring
// rather than fed from notify.
type Writer struct {
	store   Store
	history *cache.History
	keep    int

	// recordGap records the window from the previous process's last heartbeat,
	// recovering what it can up to streamSince, or nothing when that is zero; it
	// reaches the store through the ring.
	recordGap func(ctx context.Context, from, streamSince time.Time)

	// lastWritten is the newest ID committed; a failed commit leaves it, so
	// the next tick retries from the same place.
	lastWritten uint64

	retained bool

	// heartbeat is when SeenAt last committed.
	heartbeat time.Time

	// Each logs its first failure and its recovery once.
	writeFailing, coverageFailing bool

	// gapChecked and covering are set once the gap is recorded and coverage
	// opened; until then a ready writer retries at every tick.
	ready, gapChecked, covering bool
}

// NewWriter starts from the ring's count, which Restore set.
func NewWriter(
	store Store,
	history *cache.History,
	keep int,
	recordGap func(ctx context.Context, from, streamSince time.Time),
) *Writer {
	return &Writer{
		store:       store,
		history:     history,
		keep:        keep,
		recordGap:   recordGap,
		lastWritten: history.Count(),
	}
}

// Run writes until ctx ends, then commits what remains. Coverage opens once
// ready closes; the caller ends ctx only after the watcher has stopped, so
// the last changes it recorded are written.
func (w *Writer) Run(ctx context.Context, ready <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// database/sql discards a connection whose transaction was cancelled, and
	// with it the store's lock, so ending ctx stops the loop, never a commit.
	stop := ctx.Done()
	ctx = context.WithoutCancel(ctx)

	for {
		select {
		case <-ready:
			ready = nil
			w.ready = true
			w.openCoverage(ctx)
		case <-ticker.C:
			if w.ready && !w.covering {
				w.openCoverage(ctx)
			}

			w.flush(ctx)
		case <-stop:
			// A clean stop's gap starts where it stopped.
			w.heartbeat = time.Time{}
			w.flush(ctx)

			return
		}
	}
}

// RecordGap records the gap since the previous stretch of watching, before
// the stream starting at streamSince records anything, so what it recovers sits
// below every change watched. Run's ready must close after it; when the store
// fails here, openCoverage records the gap then, recovering nothing.
func (w *Writer) RecordGap(ctx context.Context, streamSince time.Time) {
	previous, ok, err := w.store.LastSeen(ctx)
	if err != nil {
		w.coverageFailed(ctx, err)
		return
	}

	w.gapChecked = true

	if ok && previous.Before(time.Now()) {
		w.recordGap(ctx, previous, streamSince)
	}
}

// openCoverage records the gap since the previous stretch of watching, if
// RecordGap could not, then opens this one. The gap is recorded as soon as it
// is known, so a coverage row slow to write does not stretch it over watched time.
func (w *Writer) openCoverage(ctx context.Context) {
	if !w.gapChecked {
		w.RecordGap(ctx, time.Time{})

		if !w.gapChecked {
			return
		}
	}

	if err := w.store.OpenCoverage(ctx, time.Now()); err != nil {
		w.coverageFailed(ctx, err)
		return
	}

	w.coverageFailing = false
	w.covering = true
}

func (w *Writer) coverageFailed(ctx context.Context, err error) {
	if !w.coverageFailing {
		slog.WarnContext(ctx, "could not record the start of coverage; retrying", "error", err)
	}

	w.coverageFailing = true
}

// flush commits what the ring holds past lastWritten. After an overrun Since
// returns what survived: the entries in between left memory too, so skipping
// them is retention come early, not a hole in the record.
func (w *Writer) flush(ctx context.Context) {
	entries, _ := w.history.Since(w.lastWritten)

	last := w.lastWritten
	if len(entries) > 0 {
		last = entries[len(entries)-1].ID
	}

	// The annotation tables hold what these items changed, committed already.
	entries = slices.DeleteFunc(entries, func(e cache.HistoryEntry) bool {
		return e.Kind == cache.KindAnnotation
	})

	batch := Batch{Entries: entries}
	if last > w.lastWritten {
		batch.Last = last
	}

	now := time.Now()
	if w.covering && (len(entries) > 0 || now.Sub(w.heartbeat) >= quietHeartbeat) {
		batch.SeenAt = now
	}

	// A tick that inserted nothing cannot have pushed anything out; the first
	// retains regardless, since the configured size may have shrunk.
	if len(entries) > 0 || !w.retained {
		batch.Keep = w.keep
	}

	// A quiet tick between heartbeats, or before coverage opens, has nothing to write.
	if len(entries) == 0 && batch.SeenAt.IsZero() && batch.Keep == 0 && batch.Last == 0 {
		w.lastWritten = last
		return
	}

	if err := w.store.Commit(ctx, batch); err != nil {
		if !w.writeFailing {
			slog.WarnContext(ctx, "could not persist the change history; retrying", "error", err)
		}

		w.writeFailing = true

		return
	}

	if w.writeFailing {
		slog.InfoContext(ctx, "persisting the change history again")
	}

	w.writeFailing = false
	w.retained = true
	w.lastWritten = last

	if !batch.SeenAt.IsZero() {
		w.heartbeat = batch.SeenAt
	}
}
