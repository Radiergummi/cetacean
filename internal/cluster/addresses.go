package cluster

import (
	"slices"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/ipam"
)

// AddressNetworks names the networks whose address figure an event may move:
// a task's own networks, ingress for a node, and every network with a figure
// for a service, whose update can drop a VIP the new record no longer names.
// It runs on every event, so it names them without counting any.
func AddressNetworks(c *cache.Cache, ev cache.Event) []string {
	switch ev.Type {
	case cache.EventService:
		var ids []string
		for net := range c.EachNetwork {
			if ipam.Counted(net) {
				ids = append(ids, net.ID)
			}
		}

		slices.Sort(ids)

		return ids

	case cache.EventTask:
		task, ok := ev.Resource.(swarm.Task)
		if !ok {
			return nil
		}

		ids := make([]string, 0, len(task.NetworksAttachments))
		for _, attachment := range task.NetworksAttachments {
			ids = append(ids, attachment.Network.ID)
		}

		return ids

	case cache.EventNode:
		var ids []string
		for net := range c.EachNetwork {
			if net.Ingress {
				ids = append(ids, net.ID)
			}
		}

		return ids

	default:
		return nil
	}
}
