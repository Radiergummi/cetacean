package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func (h *Handlers) HandleRecommendations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	results := acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		h.recEngine.Results(),
		recommendationResource,
	)
	if results == nil {
		results = []recommendations.Recommendation{}
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

// recommendationResource maps a recommendation to its ACL resource string.
// Cluster-scoped recommendations use "swarm:cluster" which requires a swarm
// grant; service/node-scoped recommendations use "service:name" or "node:name".
func recommendationResource(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "service:" + rec.TargetName
	case recommendations.ScopeNode:
		return "node:" + rec.TargetName
	default:
		return "swarm:cluster"
	}
}
