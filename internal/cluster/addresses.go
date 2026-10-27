package cluster

import (
	"github.com/docker/docker/api/types/network"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/ipam"
)

// NetworkAddresses reads the address pool of each of networks off the cache.
// It counts every service and task, the caller's readable ones or not: only
// the totals leave, never which tasks hold an address.
func NetworkAddresses(c *cache.Cache, networks []network.Summary) map[string]ipam.Addresses {
	nodes := 0
	for range c.EachNode {
		nodes++
	}

	return ipam.Count(networks, c.EachService, c.EachTask, nodes)
}
