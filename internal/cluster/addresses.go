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
	return ipam.Count(networks, c.EachService, c.EachTaskUnordered, c.NodeCount())
}

// NetworkAddressesOf is NetworkAddresses for one network, nil when it has no
// figure.
func NetworkAddressesOf(c *cache.Cache, net network.Summary) *ipam.Addresses {
	if a, ok := NetworkAddresses(c, []network.Summary{net})[net.ID]; ok {
		return &a
	}

	return nil
}
