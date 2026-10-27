package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func (h *Handlers) HandleRecommendations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	results := cluster.ReadableRecommendations(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		h.recEngine.Results(),
	)
	if results == nil {
		results = []recommendations.Recommendation{}
	}

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		writeCSV(w, r, "recommendations", csvTableForRecommendations(results))
		return
	}

	summary := recommendations.ComputeSummary(results)
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/recommendations",
			"RecommendationCollection",
			RecommendationsResponse{
				Items:      results,
				Total:      len(results),
				Summary:    summary,
				ComputedAt: h.recEngine.LastTick(),
			},
		),
	)
}
