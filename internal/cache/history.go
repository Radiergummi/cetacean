package cache

import (
	"sync"
	"time"
)

type HistoryEntry struct {
	ID         uint64    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary,omitempty"`
}

type HistoryQuery struct {
	Type       string
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
}

func NewHistory(size int) *History {
	return &History{
		entries: make([]HistoryEntry, size),
		size:    size,
	}
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
		if q.ResourceID != "" && e.ResourceID != q.ResourceID {
			continue
		}
		result = append(result, e)
	}

	return result
}
