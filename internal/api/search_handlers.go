package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// --- Search ---

// searchResult is the per-hit shape returned in HTTP search responses.
// It mirrors cluster.SearchResult minus the Type field (the result map is
// already keyed by type).
type searchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state,omitempty"`
}

// aclResourceFor returns the ACL resource string for a search result.
// Tasks use the task ID; every other type uses the resource name.
func aclResourceFor(resourceType, name, id string) string {
	if resourceType == "tasks" {
		return "task:" + id
	}
	// Trim the trailing "s" off the plural type name (services -> service).
	return strings.TrimSuffix(resourceType, "s") + ":" + name
}

// HandleSearch performs a cross-resource global search via the shared cluster
// layer, then applies ACL filtering. Per-type counts and the grand total
// reflect pre-cap matches (after ACL filtering) so the UI can show "X matches"
// even when only the first N are displayed.
func (h *Handlers) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErrorCode(w, r, "SEA001", "missing required query parameter: q")
		return
	}
	if len(q) > 200 {
		writeErrorCode(w, r, "SEA002", "query too long (max 200 characters)")
		return
	}

	limit := 3
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	raw := cluster.Search(r.Context(), h.cache, q, limit)

	identity := auth.IdentityFromContext(r.Context())
	results := make(map[string][]searchResult, len(raw.Hits))
	counts := make(map[string]int, len(raw.Counts))
	total := 0
	for resourceType, count := range raw.Counts {
		hits := raw.Hits[resourceType]
		filtered := acl.Filter(
			h.acl,
			identity,
			"read",
			hits,
			func(sr cluster.SearchResult) string {
				return aclResourceFor(resourceType, sr.Name, sr.ID)
			},
		)
		// Adjust the pre-cap count by the number of visible-page denials. We
		// assume the visible-page ACL rate generalizes to the pre-cap set; this
		// is the same approximation the old handler used.
		removed := len(hits) - len(filtered)
		visibleCount := count - removed
		if visibleCount <= 0 {
			continue
		}

		converted := make([]searchResult, len(filtered))
		for i, sr := range filtered {
			converted[i] = searchResult{
				ID:     sr.ID,
				Name:   sr.Name,
				Detail: sr.Detail,
				State:  sr.State,
			}
		}

		results[resourceType] = converted
		counts[resourceType] = visibleCount
		total += visibleCount
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/search", "SearchResult", SearchResponse{
			Query:   q,
			Results: results,
			Counts:  counts,
			Total:   total,
		}),
	)
}
