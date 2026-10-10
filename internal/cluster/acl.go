package cluster

import "github.com/docker/docker/api/types/swarm"

// NodeACLName returns the name a node is authorized as: its hostname, or its
// ID when it has none.
func NodeACLName(n swarm.Node) string {
	if n.Description.Hostname != "" {
		return n.Description.Hostname
	}

	return n.ID
}
