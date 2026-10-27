package cache

import (
	"strconv"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/ipam"
)

type addressMemo struct {
	generation uint64
	counts     map[string]ipam.Addresses
}

// NetworkAddresses is ipam.Count over every network, keyed by network ID and
// counted at most once per generation. It counts every service and task, the
// caller's readable ones or not: only the totals leave, never which tasks hold
// an address. The map is shared between callers and must not be written to.
func (c *Cache) NetworkAddresses() map[string]ipam.Addresses {
	// Read before counting, so what is stored is never older than its key.
	generation := c.Generation()
	if memo := c.addresses.Load(); memo != nil && memo.generation == generation {
		return memo.counts
	}

	// Concurrent misses on one generation share a single count. A miss that
	// reaches Do after the first flight landed finds its memo instead.
	counted, _, _ := c.addressCounts.Do(strconv.FormatUint(generation, 10), func() (any, error) {
		if memo := c.addresses.Load(); memo != nil && memo.generation == generation {
			return memo.counts, nil
		}

		counts := ipam.Count(
			c.ListNetworks(),
			c.eachServiceUnordered,
			c.eachTaskUnordered,
			c.NodeCount(),
		)
		c.storeAddresses(&addressMemo{generation: generation, counts: counts})

		return counts, nil
	})

	return counted.(map[string]ipam.Addresses)
}

// NetworkAddressesOf is the figure of one network, nil when it has none.
func (c *Cache) NetworkAddressesOf(id string) *ipam.Addresses {
	if a, ok := c.NetworkAddresses()[id]; ok {
		return &a
	}

	return nil
}

// storeAddresses keeps memo unless a newer generation's is already stored: a
// count that started earlier can finish later.
func (c *Cache) storeAddresses(memo *addressMemo) {
	for {
		current := c.addresses.Load()
		if current != nil && current.generation >= memo.generation {
			return
		}

		if c.addresses.CompareAndSwap(current, memo) {
			return
		}
	}
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
