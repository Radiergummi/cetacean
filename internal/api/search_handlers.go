package api

import (
	"net/http"
	"strconv"
	"strings"

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

// HandleSearch performs a cross-resource global search via the shared cluster
// layer. Per-type counts and the grand total reflect the readable pre-cap
// matches so the UI can show "X matches" even when only the first N are displayed.
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

	raw := cluster.Search(
		r.Context(), h.cache, q, limit,
		h.acl.Checker(auth.IdentityFromContext(r.Context()), "read"),
	)

	results := make(map[string][]searchResult, len(raw.Hits))
	for resourceType, hits := range raw.Hits {
		converted := make([]searchResult, len(hits))
		for i, sr := range hits {
			converted[i] = searchResult{
				ID:     sr.ID,
				Name:   sr.Name,
				Detail: sr.Detail,
				State:  sr.State,
			}
		}

		results[resourceType] = converted
	}

	// This body echoes ?q= verbatim beside authenticated content, which is
	// the shape BREACH needs: compressed length would leak whether a guessed
	// query matched something the caller can see.
	writeCachedJSON(
		w,
		disableCompression(r),
		NewDetailResponse(r.Context(), "/search", "SearchResult", SearchResponse{
			Query:   q,
			Results: results,
			Counts:  raw.Counts,
			Total:   raw.Total,
		}),
	)
}
