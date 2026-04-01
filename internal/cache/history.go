package cache

import (
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
	Type       EventType
	ResourceID string
	Limit      int
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

func (h *History) Append(e HistoryEntry) {
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
		return h.listByResource(q.ResourceID, q.Type, limit)
	}

	var result []HistoryEntry

	// Iterate newest-first from cursor-1 backwards
	total := h.size
	if !h.full {
		total = h.cursor
	}

	for i := 0; i < total && len(result) < limit; i++ {
		idx := h.cursor - 1 - i
		if idx < 0 {
			idx += h.size
		}

		e := h.entries[idx]
		if q.Type != "" && e.Type != q.Type {
			continue
		}

		result = append(result, e)
	}

	return result
}

func (h *History) listByResource(
	resourceID string,
	typeFilter EventType,
	limit int,
) []HistoryEntry {
	ring := h.byResource[resourceID]
	if ring == nil {
		return nil
	}

	var result []HistoryEntry

	ring.iterNewest(func(idx int) bool {
		e := h.entries[idx]

		// Skip stale entries: the ring buffer slot may have been overwritten
		// by a different resource's entry since the index was recorded.
		if e.ResourceID != resourceID {
			return true
		}

		if typeFilter != "" && e.Type != typeFilter {
			return true
		}

		result = append(result, e)
		return len(result) < limit
	})

	return result
}
