package api

import (
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/cache"
)

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	entries := h.cache.History().List(cache.HistoryQuery{
		Type:       cache.EventType(q.Get("type")),
		ResourceID: q.Get("resourceId"),
		Limit:      limit,
	})
	if entries == nil {
		entries = []cache.HistoryEntry{}
	}
	writeJSONWithETag(
		w, r,
		NewCollectionResponse(r.Context(), entries, len(entries), len(entries), 0),
	)
}
