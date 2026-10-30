package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
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

	// Paged by cursor, as the feeds are: the ring moves under an offset.
	beforeID, limit, err := parseFeedPagination(r)
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return
	}

	// A download nobody paginated is the whole log the ring still holds.
	if wantCSV && q.Get("limit") == "" {
		limit = h.cache.History().Size()
	}

	window, err := cluster.ParseTimeWindow(q.Get("since"), q.Get("until"))
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return
	}

	// `resourceId` is a query parameter, so the canonical redirect never
	// reaches it and a caller holding a name would read an empty timeline.
	resourceType := q.Get("type")

	canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")

	resourceID, err := cluster.ResolveIdentifier(
		h.cache, resourceType, q.Get("resourceId"), canRead,
	)
	if h.reportAmbiguousName(w, r, resourceType, err) {
		return
	}

	query := cache.HistoryQuery{
		Type:         cache.EventType(resourceType),
		ResourceID:   resourceID,
		BeforeID:     beforeID,
		NameContains: q.Get("search"),
		Kind:         q.Get("kind"),
		Limit:        limit,
		After:        window.Since,
		Before:       window.Until,
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

	entries = cluster.NameHistoryTasks(h.cache, entries, canRead)

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
			wrapHistory(r.Context(), entries),
			len(entries),
			len(entries),
			0,
		),
	)
}

// wrapHistory types each item by its kind: a gap is not a change.
func wrapHistory(ctx context.Context, entries []cache.HistoryEntry) []Item[cache.HistoryEntry] {
	items := make([]Item[cache.HistoryEntry], len(entries))
	for i, e := range entries {
		typ := "HistoryEntry"
		if e.Kind == cache.KindGap {
			typ = "Gap"
		}

		items[i] = Item[cache.HistoryEntry]{
			id:  absPath(ctx, fmt.Sprintf("/history#%d", e.ID)),
			typ: typ,
			val: e,
		}
	}

	return items
}
