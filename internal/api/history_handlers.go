package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
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
	// `resourceId` is a query parameter, so the canonical redirect never
	// reaches it and a caller holding a name would read an empty timeline.
	resourceType := q.Get("type")

	resourceID, err := cluster.ResolveIdentifier(h.cache, resourceType, q.Get("resourceId"))
	if h.reportAmbiguousName(w, r, resourceType, err) {
		return
	}

	// Paged by cursor, as the feeds are: the ring moves under an offset.
	beforeID, _ := strconv.ParseUint(q.Get("before"), 10, 64)

	query := cache.HistoryQuery{
		Type:         cache.EventType(resourceType),
		ResourceID:   resourceID,
		BeforeID:     beforeID,
		NameContains: q.Get("search"),
		Limit:        limit,
		Visible:      h.readableHistory(r),
	}

	// The type names what resourceId is, and a service's timeline holds its
	// tasks' entries too.
	if query.Type == cache.EventService && resourceID != "" {
		query.Type = ""
		query.Types = []cache.EventType{cache.EventService, cache.EventTask}
	}

	entries := h.cache.History().List(query)
	if entries == nil {
		entries = []cache.HistoryEntry{}
	}

	entries = cluster.NameHistoryTasks(h.cache, entries)

	if wantCSV {
		writeCSV(w, r, "history", csvTableForHistory(entries))
		return
	}

	if len(entries) == limit {
		next := r.URL.Query()
		next.Set("before", strconv.FormatUint(entries[len(entries)-1].ID, 10))
		next.Set("limit", strconv.Itoa(limit))
		w.Header().Add("Link", fmt.Sprintf(
			`<%s?%s>; rel="next"`, selfPath(r), next.Encode(),
		))
	}

	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(
				r.Context(),
				entries,
				"HistoryEntry",
				func(e cache.HistoryEntry) string { return fmt.Sprintf("/history#%d", e.ID) },
			),
			len(entries),
			len(entries),
			0,
		),
	)
}
