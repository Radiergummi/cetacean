package exec

import (
	"net"
	"net/netip"

	"github.com/docker/docker/api/types/swarm"
)

// Self recognises Cetacean's own tasks by the overlay addresses they hold. A
// shell in Cetacean is a shell next to its Docker socket and signing keys.
type Self struct {
	addrs map[netip.Addr]bool
}

func NewSelf() (*Self, error) {
	ifaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var addrs []netip.Addr
	for _, a := range ifaceAddrs {
		if prefix, err := netip.ParsePrefix(a.String()); err == nil && !prefix.Addr().IsLoopback() {
			addrs = append(addrs, prefix.Addr())
		}
	}

	return newSelf(addrs), nil
}

func newSelf(addrs []netip.Addr) *Self {
	s := &Self{addrs: make(map[netip.Addr]bool, len(addrs))}
	for _, a := range addrs {
		s.addrs[a.Unmap()] = true
	}

	return s
}

func (s *Self) Is(task swarm.Task) bool {
	for _, attachment := range task.NetworksAttachments {
		for _, address := range attachment.Addresses {
			if prefix, err := netip.ParsePrefix(
				address,
			); err == nil &&
				s.addrs[prefix.Addr().Unmap()] {
				return true
			}
		}
	}

	return false
}
