package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// AddressNetworks names the networks whose address figure an event may move:
// a task's own networks, ingress for a node, and every network for a service,
// whose update can drop a VIP the new record no longer names.
func AddressNetworks(c *cache.Cache, ev cache.Event) (ids []string, all bool) {
	switch ev.Type {
	case cache.EventService:
		return nil, true

	case cache.EventTask:
		task, ok := ev.Resource.(swarm.Task)
		if !ok {
			return nil, false
		}

		for _, attachment := range task.NetworksAttachments {
			ids = append(ids, attachment.Network.ID)
		}

		return ids, false

	case cache.EventNode:
		for net := range c.EachNetwork {
			if net.Ingress {
				ids = append(ids, net.ID)
			}
		}

		return ids, false

	default:
		return nil, false
	}
}
