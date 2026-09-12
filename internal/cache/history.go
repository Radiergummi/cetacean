package cache

import (
	"slices"
	"strings"
	"sync"
	"time"
)

type HistoryEntry struct {
	ID         uint64    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Type       EventType `json:"type"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary,omitempty"`
}

type HistoryQuery struct {
	Type         EventType
	ResourceID   string
	BeforeID     uint64
	NameContains string // case-insensitive substring match on Name
	Limit        int

	// Types narrows to entries of any of these types, where Type narrows to
	// exactly one; empty means every type, and both set means the intersection.
	// Pushed down rather than applied by the caller so the walk copies only
	// what is returned, under the read lock every Append contends with.
	Types []EventType

	// Before bounds the result at the newer end: only entries at or before it.
	// Unlike After it cannot end the walk — entries are visited newest-first,
	// so the ones this excludes come first — so it skips rather than breaks.
	// Zero means unbounded.
	Before time.Time

	// After bounds the walk rather than the result: entries are visited
	// newest-first, so the first at or before it ends the scan, instead of
	// copying the whole ring to discard it. Exclusive; zero means unbounded.
	// Named for the comparison, since "since" here means History.Since.
	After time.Time
}

type History struct {
	mu      sync.RWMutex
	entries []HistoryEntry
	size    int
	cursor  int
	count   uint64
	full    bool

	// byResource maps resource IDs to a ring of entry indices, enabling
	// fast filtered lookups without scanning the entire buffer.
	// Stale index entries (where the main ring has overwritten the slot)
	// are detected and skipped during iteration in listByResource.
	byResource map[string]*indexRing
}

// indexRing is a small ring buffer of int indices into History.entries.
type indexRing struct {
	indices []int
	cursor  int
	full    bool
}

const indexRingSize = 64

// maxListPrealloc bounds what List reserves up front. A caller filtering after
// the read passes the ring's whole size as the limit, but is usually bounded by
// `After` and stops within a few entries. A read that genuinely walks the ring
// end to end still grows to fit; it pays for the growth, not the reservation.
const maxListPrealloc = 512

func (r *indexRing) push(idx int) {
	r.indices[r.cursor] = idx
	r.cursor++
	if r.cursor >= len(r.indices) {
		r.cursor = 0
		r.full = true
	}
}

// iterNewest calls fn with each stored index, newest first.
// fn returns false to stop iteration.
func (r *indexRing) iterNewest(fn func(int) bool) {
	total := len(r.indices)
	if !r.full {
		total = r.cursor
	}

	for i := range total {
		idx := r.cursor - 1 - i
		if idx < 0 {
			idx += len(r.indices)
		}

		if !fn(r.indices[idx]) {
			return
		}
	}
}

func NewHistory(size int) *History {
	return &History{
		entries:    make([]HistoryEntry, size),
		size:       size,
		byResource: make(map[string]*indexRing),
	}
}

// Size is the ring's capacity — the most entries List can ever return. A
// caller applying its own filters after the read needs it, or it has to guess
// a window and silently under-report whatever falls outside it.
func (h *History) Size() int {
	return h.size
}

// Oldest is the timestamp of the oldest entry the ring still holds, and so the
// start of the only window a query over it can honestly answer for: the ring
// starts with the process and, once it wraps, begins at its oldest survivor.
// Neither is otherwise visible in a result. ok=false when nothing is recorded.
func (h *History) Oldest() (time.Time, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return time.Time{}, false
	}

	// Before the ring wraps the oldest entry is at index 0; after it wraps the
	// cursor points at the slot about to be overwritten, which is the oldest.
	oldest := 0
	if h.full {
		oldest = h.cursor
	}

	return h.entries[oldest].Timestamp, true
}

func (h *History) Count() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

// Since returns all entries with ID > afterID in chronological order.
// Returns ok=false if afterID has been overwritten or is a future ID,
// meaning the caller cannot trust the result is complete.
func (h *History) Since(afterID uint64) ([]HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Future ID or empty history
	if afterID > h.count {
		return nil, false
	}

	// Caught up — no new entries
	if afterID == h.count {
		return nil, true
	}

	// Determine the oldest ID still in the ring
	var oldestID uint64
	if h.full {
		oldestID = h.count - uint64(h.size) + 1
	} else {
		oldestID = 1
	}

	// afterID has been overwritten
	if afterID > 0 && afterID < oldestID {
		return nil, false
	}

	// Collect entries with ID > afterID in chronological order.
	// Walk the ring from oldest to newest.
	total := h.size
	if !h.full {
		total = h.cursor
	}

	var result []HistoryEntry

	for i := total - 1; i >= 0; i-- {
		idx := h.cursor - 1 - i
		if idx < 0 {
			idx += h.size
		}

		e := h.entries[idx]
		if e.ID > afterID {
			result = append(result, e)
		}
	}

	return result, true
}

func (h *History) Append(e HistoryEntry) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	e.ID = h.count
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	h.entries[h.cursor] = e

	// Update the per-resource index.
	ring := h.byResource[e.ResourceID]
	if ring == nil {
		ring = &indexRing{indices: make([]int, indexRingSize)}
		h.byResource[e.ResourceID] = ring
	}
	ring.push(h.cursor)

	h.cursor++
	if h.cursor >= h.size {
		h.cursor = 0
		h.full = true
	}

	return h.count
}

func (h *History) List(q HistoryQuery) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	// Fast path: a resource-ID filter reads the per-resource index instead of
	// scanning the ring — but only when the index can prove it answered the
	// whole question, since it holds the newest indexRingSize entries and runs
	// out both on a larger limit and on a cursor paging off its end. Anything
	// else falls through to the scan below.
	if q.ResourceID != "" {
		if found, complete := h.listByResource(q, limit); complete {
			return found
		}
	}

	result := make([]HistoryEntry, 0, min(limit, maxListPrealloc))

	// Iterate newest-first from cursor-1 backwards
	slots := h.size
	if !h.full {
		slots = h.cursor
	}

	pastCursor := q.BeforeID == 0

	for i := range slots {
		idx := h.cursor - 1 - i
		if idx < 0 {
			idx += h.size
		}

		e := h.entries[idx]
		if !pastCursor {
			if e.ID == q.BeforeID {
				pastCursor = true
			}
			continue
		}

		// Newest-first, so everything past this point is older still.
		if !q.After.IsZero() && !e.Timestamp.After(q.After) {
			break
		}

		if !q.matches(e) {
			continue
		}

		result = append(result, e)

		if len(result) == limit {
			break
		}
	}

	return result
}

// matches applies the filters that skip an entry rather than end the walk. The
// two list paths share it so a filter added here is honoured on both, which is
// the same reason listByResource takes the whole query.
func (q HistoryQuery) matches(e HistoryEntry) bool {
	if q.ResourceID != "" && e.ResourceID != q.ResourceID {
		return false
	}

	if q.Type != "" && e.Type != q.Type {
		return false
	}

	if len(q.Types) > 0 && !slices.Contains(q.Types, e.Type) {
		return false
	}

	if !q.Before.IsZero() && e.Timestamp.After(q.Before) {
		return false
	}

	if q.NameContains != "" && !strings.Contains(
		strings.ToLower(e.Name), strings.ToLower(q.NameContains),
	) {
		return false
	}

	return true
}

// listByResource answers a query already known to name a resource, taking the
// whole query rather than five fields so a new one is honoured on both paths or
// neither. complete says whether the answer is the whole one: the index is a
// fixed window, so only the caller's fallback to a scan separates the cases.
func (h *History) listByResource(
	q HistoryQuery,
	limit int,
) (entries []HistoryEntry, complete bool) {
	ring := h.byResource[q.ResourceID]
	if ring == nil {
		// Nothing was ever recorded under this ID: an empty answer is the
		// complete one. The index is never pruned, so its absence is proof.
		return nil, true
	}

	result := make([]HistoryEntry, 0, min(limit, indexRingSize))
	pastCursor := q.BeforeID == 0

	ring.iterNewest(func(idx int) bool {
		e := h.entries[idx]

		// Skip stale entries: the ring buffer slot may have been overwritten
		// by a different resource's entry since the index was recorded.
		if e.ResourceID != q.ResourceID {
			return true
		}

		if !pastCursor {
			if e.ID == q.BeforeID {
				pastCursor = true
			}
			return true
		}

		// Newest-first here too, so this ends the walk rather than skipping.
		if !q.After.IsZero() && !e.Timestamp.After(q.After) {
			return false
		}

		if !q.matches(e) {
			return true
		}

		result = append(result, e)

		return len(result) < limit
	})

	// A ring that has never wrapped holds every entry this resource ever had,
	// so a short answer off it is genuinely short. Otherwise only a filled
	// request proves the window did not cut the answer off.
	return result, !ring.full || len(result) == limit
}
