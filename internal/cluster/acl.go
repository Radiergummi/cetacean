package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// NodeACLName returns the name a node is authorized as: its hostname, or its
// ID when it has none.
func NodeACLName(n swarm.Node) string {
	if n.Description.Hostname != "" {
		return n.Description.Hostname
	}

	return n.ID
}

// ServiceRefACLResource is the resource a service reference is read as.
func ServiceRefACLResource(ref cache.ServiceRef) string { return "service:" + ref.Name }

// PortACLName names a published port for the ACL: it is read as part of the
// service publishing it.
func PortACLName(p cache.PublishedPort) string { return p.ServiceName }

// PortACLResource is PortACLName as a resource string.
func PortACLResource(p cache.PublishedPort) string { return "service:" + PortACLName(p) }
