package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/recommendations"
)

func (h *Handlers) HandleRecommendations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	results := h.recEngine.Results()
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
