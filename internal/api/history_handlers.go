package api

import (
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// --- History ---

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

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

	// Filter entries by per-resource ACL read permission.
	identity := auth.IdentityFromContext(r.Context())
	filtered := entries[:0]
	for _, e := range entries {
		resource := string(e.Type) + ":" + e.Name
		if h.acl.Can(identity, "read", resource) {
			filtered = append(filtered, e)
		}
	}
	entries = filtered

	writeCachedJSON(
		w, r,
		NewCollectionResponse(r.Context(), entries, len(entries), len(entries), 0),
	)
}
