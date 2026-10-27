package cluster

import (
	"fmt"
	"slices"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ValidatePorts rejects a port that publishes nothing. Docker accepts a
// PortConfig with TargetPort 0, assigns an ephemeral published port pointing
// at container port 0, and reports success — so the service holds a port that
// cannot serve. The target is the one field with no sensible default.
func ValidatePorts(ports []swarm.PortConfig) error {
	for i, p := range ports {
		if p.TargetPort == 0 {
			return fmt.Errorf(
				"port %d: TargetPort is required and must not be 0 "+
					"(the container port to publish)", i,
			)
		}
	}

	return nil
}

// PublishedPorts lists the published ports, naming only the nodes canRead
// admits, as a task names only a node its reader may see; a nil canRead
// admits all.
func PublishedPorts(c *cache.Cache, canRead func(resource string) bool) []cache.PublishedPort {
	ports := c.ListPublishedPorts()
	if canRead == nil {
		return ports
	}

	for i := range ports {
		ports[i].Nodes = slices.DeleteFunc(ports[i].Nodes, func(id string) bool {
			node, ok := c.GetNode(id)
			return !ok || !canRead("node:"+NodeACLName(node))
		})

		if len(ports[i].Nodes) == 0 {
			ports[i].Nodes = nil
		}
	}

	return ports
}
