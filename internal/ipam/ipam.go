// Package ipam counts how much of a swarm network's IPv4 address pool is in
// use. It is a leaf so the domain layer and the recommendations engine share
// one reading of it.
package ipam

import (
	"encoding/binary"
	"iter"
	"net/netip"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

// Addresses is one network's IPv4 pool: how many addresses it can hand out,
// and how many are taken, by what. Used is VIPs + TaskAddresses + LoadBalancers.
type Addresses struct {
	Usable        int `json:"usable"`
	Used          int `json:"used"`
	VIPs          int `json:"vips"`
	TaskAddresses int `json:"taskAddresses"`
	LoadBalancers int `json:"loadBalancers"`

	// ExcludesAttachedContainers is set on an attachable network, whose
	// containers started with `docker run --network` hold addresses no task
	// records.
	ExcludesAttachedContainers bool `json:"excludesAttachedContainers"`
}

// Count reads the address pool of every swarm-scoped network on the default
// IPAM driver with an IPv4 subnet, keyed by network ID; the others are absent.
// IPv6 is left out: each family is allocated separately and a /64 never runs out.
func Count(
	networks []network.Summary,
	services iter.Seq[swarm.Service],
	tasks iter.Seq[swarm.Task],
	nodes int,
) map[string]Addresses {
	type tally struct {
		Addresses
		ingress bool
		taken   map[netip.Addr]struct{}
		nodes   map[string]struct{}
	}

	tallies := make(map[string]*tally)

	for _, n := range networks {
		if n.Scope != "swarm" || (n.IPAM.Driver != "" && n.IPAM.Driver != "default") {
			continue
		}

		total, any4 := 0, false
		for _, cfg := range n.IPAM.Config {
			if count, ok := usable(cfg); ok {
				total += count
				any4 = true
			}
		}

		if !any4 {
			continue
		}

		tallies[n.ID] = &tally{
			Usable:                     total,
			ExcludesAttachedContainers: n.Attachable,
			ingress:                    n.Ingress,
			taken:                      make(map[netip.Addr]struct{}),
			nodes:                      make(map[string]struct{}),
		}
	}

	// take records addr against t, and reports whether no one held it yet.
	take := func(t *tally, addr string) bool {
		prefix, err := netip.ParsePrefix(addr)
		if err != nil || !prefix.Addr().Is4() {
			return false
		}

		if _, held := t.taken[prefix.Addr()]; held {
			return false
		}

		t.taken[prefix.Addr()] = struct{}{}

		return true
	}

	for svc := range services {
		for _, vip := range svc.Endpoint.VirtualIPs {
			if t, ok := tallies[vip.NetworkID]; ok && take(t, vip.Addr) {
				t.VIPs++
			}
		}
	}

	for task := range tasks {
		if !live(task) {
			continue
		}

		for _, attachment := range task.NetworksAttachments {
			t, ok := tallies[attachment.Network.ID]
			if !ok {
				continue
			}

			if task.NodeID != "" {
				t.nodes[task.NodeID] = struct{}{}
			}

			for _, addr := range attachment.Addresses {
				if take(t, addr) {
					t.TaskAddresses++
				}
			}
		}
	}

	result := make(map[string]Addresses, len(tallies))

	for id, t := range tallies {
		// Every node holds an ingress balancer; on any other overlay, a node
		// holds one once a task there joins the network.
		t.LoadBalancers = len(t.nodes)
		if t.ingress {
			t.LoadBalancers = nodes
		}

		t.Used = t.VIPs + t.TaskAddresses + t.LoadBalancers
		result[id] = t.Addresses
	}

	return result
}

// live reports whether a task still holds its addresses. The allocator frees
// them once the observed state passes running, whatever the desired state,
// although the task still lists them.
func live(task swarm.Task) bool {
	switch task.Status.State {
	case swarm.TaskStateNew,
		swarm.TaskStateAllocated,
		swarm.TaskStatePending,
		swarm.TaskStateAssigned,
		swarm.TaskStateAccepted,
		swarm.TaskStatePreparing,
		swarm.TaskStateReady,
		swarm.TaskStateStarting,
		swarm.TaskStateRunning:
		return true
	default:
		return false
	}
}

// usable counts the addresses an IPv4 pool can hand out: its IPRange, or the
// whole subnet, less the network and broadcast addresses (none on a /31 or
// /32) and the gateway. Without an explicit one, the gateway is the first free
// address of the range. Swarm reserves no auxiliary addresses.
func usable(cfg network.IPAMConfig) (int, bool) {
	subnet, err := netip.ParsePrefix(cfg.Subnet)
	if err != nil || !subnet.Addr().Is4() {
		return 0, false
	}

	subnet = subnet.Masked()
	first, last := bounds(subnet)
	low, high := first, last

	if pool, err := netip.ParsePrefix(cfg.IPRange); err == nil && pool.Addr().Is4() {
		poolFirst, poolLast := bounds(pool.Masked())
		low, high = max(low, poolFirst), min(high, poolLast)

		if low > high {
			return 0, true
		}
	}

	reserved := map[uint64]struct{}{}

	if subnet.Bits() <= 30 {
		reserved[first] = struct{}{}
		reserved[last] = struct{}{}
	}

	if addr, err := netip.ParseAddr(cfg.Gateway); err == nil && addr.Is4() {
		reserved[toUint(addr)] = struct{}{}
	} else {
		for addr := low; addr <= high; addr++ {
			if _, taken := reserved[addr]; !taken {
				reserved[addr] = struct{}{}

				break
			}
		}
	}

	count := high - low + 1
	for addr := range reserved {
		if addr >= low && addr <= high {
			count--
		}
	}

	return int(count), true
}

func bounds(prefix netip.Prefix) (uint64, uint64) {
	first := toUint(prefix.Addr())

	return first, first + (uint64(1) << (32 - prefix.Bits())) - 1
}

func toUint(addr netip.Addr) uint64 {
	b := addr.As4()

	return uint64(binary.BigEndian.Uint32(b[:]))
}
