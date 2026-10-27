package cache

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/ipam"
)

type addressMemo struct {
	generation uint64
	counts     map[string]ipam.Addresses
}

// NetworkAddresses is ipam.Count over every network, keyed by network ID and
// counted at most once per generation. The map is shared between callers and
// must not be written to.
func (c *Cache) NetworkAddresses() map[string]ipam.Addresses {
	// Read before counting, so what is stored is never older than its key.
	generation := c.Generation()
	if memo := c.addresses.Load(); memo != nil && memo.generation == generation {
		return memo.counts
	}

	counts := ipam.Count(
		c.ListNetworks(),
		c.eachServiceUnordered,
		c.eachTaskUnordered,
		c.NodeCount(),
	)
	c.addresses.Store(&addressMemo{generation: generation, counts: counts})

	return counts
}

func (c *Cache) eachServiceUnordered(yield func(swarm.Service) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, s := range c.services {
		if !yield(s) {
			return
		}
	}
}
