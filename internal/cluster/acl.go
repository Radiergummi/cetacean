package cluster

import (
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

// RecommendationACLResource maps a recommendation to the resource it is
// authorized as: the service, node or network it targets, or the swarm itself.
func RecommendationACLResource(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "service:" + rec.TargetName
	case recommendations.ScopeNode:
		return "node:" + rec.TargetName
	case recommendations.ScopeNetwork:
		return "network:" + rec.TargetName
	default:
		return "swarm:cluster"
	}
}
