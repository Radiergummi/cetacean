package cache

import (
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

	// Fast path: when filtering by resource ID, use the per-resource index
	// instead of scanning the entire ring buffer.
	if q.ResourceID != "" {
		return h.listByResource(q.ResourceID, q.Type, q.BeforeID, q.NameContains, limit)
	}

	var result []HistoryEntry

	// Iterate newest-first from cursor-1 backwards
	total := h.size
	if !h.full {
		total = h.cursor
	}

	pastCursor := q.BeforeID == 0

	for i := 0; i < total && len(result) < limit; i++ {
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

		if q.Type != "" && e.Type != q.Type {
			continue
		}

		if q.NameContains != "" && !strings.Contains(
			strings.ToLower(e.Name), strings.ToLower(q.NameContains),
		) {
			continue
		}

		result = append(result, e)
	}

	return result
}

func (h *History) listByResource(
	resourceID string,
	typeFilter EventType,
	beforeID uint64,
	nameContains string,
	limit int,
) []HistoryEntry {
	ring := h.byResource[resourceID]
	if ring == nil {
		return nil
	}

	var result []HistoryEntry
	pastCursor := beforeID == 0

	ring.iterNewest(func(idx int) bool {
		e := h.entries[idx]

		// Skip stale entries: the ring buffer slot may have been overwritten
		// by a different resource's entry since the index was recorded.
		if e.ResourceID != resourceID {
			return true
		}

		if !pastCursor {
			if e.ID == beforeID {
				pastCursor = true
			}
			return true
		}

		if typeFilter != "" && e.Type != typeFilter {
			return true
		}

		if nameContains != "" && !strings.Contains(
			strings.ToLower(e.Name), strings.ToLower(nameContains),
		) {
			return true
		}

		result = append(result, e)
		return len(result) < limit
	})

	return result
}
