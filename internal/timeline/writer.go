package timeline

import (
	"context"
	"log/slog"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Writer copies the ring into a Store once a second, following the ring
// rather than fed from notify.
type Writer struct {
	store   Store
	history *cache.History
	keep    int

	// recordGap records the window between the previous process's last
	// heartbeat and this one's coverage; it reaches the store through the ring.
	recordGap func(from time.Time)

	// lastWritten is the newest ID committed; a failed commit leaves it, so
	// the next tick retries from the same place.
	lastWritten uint64

	retained bool

	// Each logs its first failure and its recovery once.
	writeFailing, coverageFailing bool

	// covering is set once coverage opened; until then a ready writer
	// retries at every tick.
	ready, covering bool
}

// NewWriter starts from the ring's count, which Restore set.
func NewWriter(
	store Store,
	history *cache.History,
	keep int,
	recordGap func(from time.Time),
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
			w.flush(ctx)
			return
		}
	}
}

func (w *Writer) openCoverage(ctx context.Context) {
	now := time.Now()

	previous, ok, err := w.store.OpenCoverage(ctx, now)
	if err != nil {
		if !w.coverageFailing {
			slog.WarnContext(ctx, "could not record the start of coverage; retrying", "error", err)
		}

		w.coverageFailing = true

		return
	}

	w.coverageFailing = false
	w.covering = true

	if ok && previous.Before(now) {
		w.recordGap(previous)
	}
}

// flush commits what the ring holds past lastWritten. After an overrun Since
// returns what survived: the entries in between left memory too, so skipping
// them is retention come early, not a hole in the record.
func (w *Writer) flush(ctx context.Context) {
	entries, _ := w.history.Since(w.lastWritten)

	batch := Batch{Entries: entries}
	if w.covering {
		batch.SeenAt = time.Now()
	}

	// A tick that inserted nothing cannot have pushed anything out; the first
	// retains regardless, since the configured size may have shrunk.
	if len(entries) > 0 || !w.retained {
		batch.Keep = w.keep
	}

	// Before coverage opens a quiet tick has nothing to write.
	if len(entries) == 0 && batch.SeenAt.IsZero() && batch.Keep == 0 {
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

	if len(entries) > 0 {
		w.lastWritten = entries[len(entries)-1].ID
	}
}
