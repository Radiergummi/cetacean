package cluster

import (
	"slices"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// NodeACLName returns the name a node is authorized as: its hostname, or its
// ID when it has none.
func NodeACLName(n swarm.Node) string {
	if n.Description.Hostname != "" {
		return n.Description.Hostname
	}

	return n.ID
}

// PortACLName names a published port for the ACL: it is read as part of the
// service publishing it.
func PortACLName(p cache.PublishedPort) string { return p.ServiceName }

// PortACLResource is PortACLName as a resource string.
func PortACLResource(p cache.PublishedPort) string { return "service:" + PortACLName(p) }

// ReadableRecommendations keeps the recommendations whose target and every
// resource they reveal canRead allows.
func ReadableRecommendations(
	recs []recommendations.Recommendation,
	canRead func(resource string) bool,
) []recommendations.Recommendation {
	out := make([]recommendations.Recommendation, 0, len(recs))
	for _, rec := range recs {
		if canRead(RecommendationACLResource(rec)) &&
			!slices.ContainsFunc(rec.Reveals, func(r string) bool {
				return !canRead(r)
			}) {
			out = append(out, rec)
		}
	}

	return out
}

// RecommendationACLResource maps a recommendation to the resource it is
// authorized as: the service, node, secret or config it targets, or the swarm
// itself.
func RecommendationACLResource(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "service:" + rec.TargetName
	case recommendations.ScopeNode:
		return "node:" + rec.TargetName
	case recommendations.ScopeSecret:
		return "secret:" + rec.TargetName
	case recommendations.ScopeConfig:
		return "config:" + rec.TargetName
	default:
		return "swarm:cluster"
	}
}
