package cluster

import (
	"slices"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
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
// resource they reveal id may read, by acl.Filter's rules. It may reuse recs,
// which every caller has copied from the engine.
func ReadableRecommendations(
	e *acl.Evaluator,
	id *auth.Identity,
	recs []recommendations.Recommendation,
) []recommendations.Recommendation {
	readable := acl.Filter(e, id, "read", recs, RecommendationACLResource)

	canRead := e.Checker(id, "read")

	return slices.DeleteFunc(readable, func(rec recommendations.Recommendation) bool {
		return slices.ContainsFunc(rec.Reveals, func(r string) bool { return !canRead(r) })
	})
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
