package cache

import (
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// The kinds of timeline item. A reader skips a kind it does not know.
const (
	KindChange = "change"
	KindGap    = "gap"

	// KindAnnotation is reserved for #498; nothing records it yet.
	KindAnnotation = "annotation"
)

type HistoryEntry struct {
	ID        uint64    `json:"id"`
	Kind      string    `json:"kind"`
	Timestamp time.Time `json:"timestamp"`

	// Until ends a gap, which runs from Timestamp; zero for a change.
	Until time.Time `json:"until,omitzero"`

	Type       EventType `json:"type"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary,omitempty"`

	// Actor is who made the change through Cetacean; nil for one made elsewhere.
	Actor *Actor `json:"actor,omitempty"`

	// Service and Stack are the remove Event's, for authorizing its replay.
	Service string `json:"-"`
	Stack   string `json:"-"`

	// ServiceID is the owning service of a task entry. A query for the service
	// returns it too, since a crash loop changes nothing on the service itself.
	ServiceID string `json:"-"`
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

	// Kind narrows to one kind; empty means every kind. It is the only filter
	// besides the window and Visible that a gap does not pass: a gap names no
	// resource, and nothing was recorded about any resource while it lasted.
	Kind string

	// Before bounds the result at the newer end: only entries starting at or
	// before it. Unlike After it cannot end the walk — entries are visited
	// newest-first, so the ones this excludes come first — so it skips.
	// Zero means unbounded.
	Before time.Time

	// After ends the walk at the first entry ending at or before it, instead
	// of copying the whole ring to discard it: ends never decrease from one ID
	// to the next, though a gap's start can. Exclusive; zero means unbounded.
	// Named for the comparison, since "since" here means History.Since.
	After time.Time

	// Visible, when set, keeps only the entries it admits, so Limit and
	// BeforeID page over what the caller may read. It runs under the history
	// lock and must not call back into History.
	Visible func(HistoryEntry) bool
}

type History struct {
	mu      sync.RWMutex
	entries []HistoryEntry
	size    int
	cursor  int
	count   uint64
	full    bool

	// byResource maps resource IDs to a ring of entry indices, enabling fast
	// filtered lookups without a scan. Stale indices are skipped; an ID leaves
	// the map once the ring overwrites its newest entry. Gaps name no resource
	// and sit under the empty ID.
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

func (r *indexRing) newest() int {
	if r.cursor == 0 {
		return r.indices[len(r.indices)-1]
	}

	return r.indices[r.cursor-1]
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

// Oldest is the timestamp of the oldest entry the ring still holds, restored
// ones included, and so the start of the only window a query over it can
// honestly answer for. Nothing in a result otherwise shows where the record
// begins. ok=false when nothing is recorded.
func (h *History) Oldest() (time.Time, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.cursor == 0 && !h.full {
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
// Returns ok=false if afterID is a future ID, or if entries after it have been
// overwritten, meaning the caller cannot trust the result is complete; in the
// latter case the result is what the ring still holds.
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

	// Read, not derived from count and size: restored IDs may jump.
	oldest := 0
	if h.full {
		oldest = h.cursor
	}

	// The entry right after afterID has been overwritten — zero included.
	complete := afterID+1 >= h.entries[oldest].ID

	slots := h.size
	if !h.full {
		slots = h.cursor
	}

	var result []HistoryEntry

	// Newest first, so a caller close behind visits only what it lacks.
	for i := range slots {
		index := h.cursor - 1 - i
		if index < 0 {
			index += h.size
		}

		e := h.entries[index]
		if e.ID <= afterID {
			break
		}

		result = append(result, e)
	}

	slices.Reverse(result)

	return result, complete
}

// Restore fills an empty ring with entries recorded by an earlier process,
// oldest first, keeping the newest that fit. It must precede any Append. Count
// skips a ring's worth past last, the largest ID stored of any kind: a process
// that crashed handed out up to a second of IDs it never wrote, which a feed
// reader has seen, and a kind the ring does not hold took the IDs it shows.
func (h *History) Restore(entries []HistoryEntry, last uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(entries) > h.size {
		entries = entries[len(entries)-h.size:]
	}

	for _, e := range entries {
		h.entries[h.cursor] = e
		h.index(e.ResourceID)
		if e.ServiceID != "" {
			h.index(e.ServiceID)
		}

		h.cursor++
		if h.cursor >= h.size {
			h.cursor = 0
			h.full = true
		}

		h.count = e.ID
	}

	h.count = max(h.count, last)
	if h.count > 0 {
		h.count += uint64(h.size)
	}
}

func (h *History) Append(e HistoryEntry) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	e.ID = h.count
	if e.Kind == "" {
		e.Kind = KindChange
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	// The ring overwrites oldest-first, so a resource whose newest entry sits
	// in the slot being reused has no entry left.
	if h.full {
		evicted := h.entries[h.cursor]
		for _, id := range [...]string{evicted.ResourceID, evicted.ServiceID} {
			if ring := h.byResource[id]; ring != nil && ring.newest() == h.cursor {
				delete(h.byResource, id)
			}
		}
	}

	h.entries[h.cursor] = e

	// Update the per-resource index, under the owning service as well.
	h.index(e.ResourceID)
	if e.ServiceID != "" {
		h.index(e.ServiceID)
	}

	h.cursor++
	if h.cursor >= h.size {
		h.cursor = 0
		h.full = true
	}

	return h.count
}

func (h *History) index(id string) {
	ring := h.byResource[id]
	if ring == nil {
		ring = &indexRing{indices: make([]int, indexRingSize)}
		h.byResource[id] = ring
	}
	ring.push(h.cursor)
}

// about reports whether an entry belongs on id's timeline.
func (e HistoryEntry) about(id string) bool {
	return e.ResourceID == id || e.ServiceID == id
}

// end is when an entry stops: a gap's Until, otherwise its Timestamp.
func (e HistoryEntry) end() time.Time {
	if e.Until.IsZero() {
		return e.Timestamp
	}

	return e.Until
}

func (h *History) List(q HistoryQuery) []HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

	// Fast path: a resource-ID filter reads the per-resource index rather than
	// scanning the ring, but only when the index can prove it answered the
	// whole question — it holds the newest indexRingSize entries, and runs out
	// both on a larger limit and on a cursor paging off its end.
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

		// Newest-first, so everything past this point ends earlier still.
		if !q.After.IsZero() && !e.end().After(q.After) {
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
	if q.Kind != "" && e.Kind != q.Kind {
		return false
	}

	if !q.Before.IsZero() && e.Timestamp.After(q.Before) {
		return false
	}

	if e.Kind != KindGap && !q.matchesResource(e) {
		return false
	}

	return q.Visible == nil || q.Visible(e)
}

// matchesResource applies the filters that select by resource.
func (q HistoryQuery) matchesResource(e HistoryEntry) bool {
	if q.ResourceID != "" && !e.about(q.ResourceID) {
		return false
	}

	if q.Type != "" && e.Type != q.Type {
		return false
	}

	if len(q.Types) > 0 && !slices.Contains(q.Types, e.Type) {
		return false
	}

	return q.NameContains == "" || strings.Contains(
		strings.ToLower(e.Name), strings.ToLower(q.NameContains),
	)
}

// listByResource answers a query already known to name a resource, taking the
// whole query rather than five fields so a new one is honoured on both paths or
// neither. complete says whether the answer is the whole one: the index is a
// fixed window, so only the caller's fallback to a scan separates the cases.
func (h *History) listByResource(
	q HistoryQuery,
	limit int,
) (entries []HistoryEntry, complete bool) {
	own := h.indexed(q.ResourceID, func(e HistoryEntry) bool { return e.about(q.ResourceID) })

	// Every gap belongs on every resource's timeline, so its index is merged in.
	gaps := indexedEntries{whole: true}
	if q.Kind == "" || q.Kind == KindGap {
		gaps = h.indexed("", func(e HistoryEntry) bool { return e.Kind == KindGap })
	}

	result := make([]HistoryEntry, 0, min(limit, indexRingSize))
	pastCursor := q.BeforeID == 0

	for len(result) < limit {
		// An index that has wrapped may have lost older entries that would
		// sort between the other's, so running out of one ends the answer.
		if len(own.entries) == 0 && !own.whole || len(gaps.entries) == 0 && !gaps.whole {
			return result, false
		}

		var e HistoryEntry

		switch {
		case len(own.entries) > 0 &&
			(len(gaps.entries) == 0 || own.entries[0].ID > gaps.entries[0].ID):
			e, own.entries = own.entries[0], own.entries[1:]
		case len(gaps.entries) > 0:
			e, gaps.entries = gaps.entries[0], gaps.entries[1:]
		default:
			return result, true
		}

		if !pastCursor {
			pastCursor = e.ID == q.BeforeID
			continue
		}

		// Newest-first here too, so this ends the walk rather than skipping.
		if !q.After.IsZero() && !e.end().After(q.After) {
			return result, true
		}

		if q.matches(e) {
			result = append(result, e)
		}
	}

	return result, true
}

// indexedEntries are the entries one index names, newest first. whole says
// they are all the ring holds under that ID: its index never wrapped.
type indexedEntries struct {
	entries []HistoryEntry
	whole   bool
}

// indexed reads id's index. A missing index is proof nothing under the ID
// survives: an index is pruned only once its newest entry is overwritten.
func (h *History) indexed(id string, belongs func(HistoryEntry) bool) indexedEntries {
	ring := h.byResource[id]
	if ring == nil {
		return indexedEntries{whole: true}
	}

	found := indexedEntries{whole: !ring.full}

	// Newest first, the entries an index still names have falling IDs. A slot
	// overwritten since it was indexed holds another entry, or a newer one
	// under this ID that an earlier reference already yielded.
	var last uint64 = math.MaxUint64

	ring.iterNewest(func(idx int) bool {
		e := h.entries[idx]
		if belongs(e) && e.ID < last {
			last = e.ID
			found.entries = append(found.entries, e)
		}

		return true
	})

	return found
}
