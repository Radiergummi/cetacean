package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	q := r.URL.Query()
	wantCSV := ContentTypeFromContext(r.Context()) == ContentTypeCSV

	limit := 50
	if wantCSV {
		// A download nobody paginated is the whole log the ring still holds.
		limit = h.cache.History().Size()
	}
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

	// Filter, not a Can per entry: it collects the caller's grants once, and a
	// CSV asks for the whole ring.
	entries = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		entries,
		func(e cache.HistoryEntry) string { return string(e.Type) + ":" + e.Name },
	)

	if wantCSV {
		writeCSV(w, r, "history", csvTableForHistory(entries))
		return
	}

	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(
				entries,
				"HistoryEntry",
				func(e cache.HistoryEntry) string { return fmt.Sprintf("/history/%d", e.ID) },
			),
			len(entries),
			len(entries),
			0,
		),
	)
}
