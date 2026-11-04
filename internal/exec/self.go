package exec

import (
	"net"
	"net/netip"
	"os"
	"regexp"

	"github.com/docker/docker/api/types/swarm"
)

// Self recognises Cetacean's own tasks by its container ID, or by the overlay
// addresses they hold. A shell in Cetacean is a shell next to its Docker socket
// and signing keys.
type Self struct {
	addrs       map[netip.Addr]bool
	containerID string
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

	s := newSelf(addrs)
	if mountinfo, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		s.containerID = ownContainerID(mountinfo)
	}

	return s, nil
}

func newSelf(addrs []netip.Addr) *Self {
	s := &Self{addrs: make(map[netip.Addr]bool, len(addrs))}
	for _, a := range addrs {
		s.addrs[a.Unmap()] = true
	}

	return s
}

// containerMount is the source Docker bind-mounts /etc/hostname from.
var containerMount = regexp.MustCompile(`/containers/([0-9a-f]{64})/hostname `)

// ownContainerID reads the container ID from the hostname mount Docker gives
// every container, whatever its network; empty outside a container.
func ownContainerID(mountinfo []byte) string {
	if m := containerMount.FindSubmatch(mountinfo); m != nil {
		return string(m[1])
	}

	return ""
}

// Is fails closed on the host network when the container ID is unknown: a task
// there holds no overlay address to recognise Cetacean by.
func (s *Self) Is(task swarm.Task) bool {
	if s.containerID != "" && task.Status.ContainerStatus != nil &&
		task.Status.ContainerStatus.ContainerID == s.containerID {
		return true
	}

	for _, attachment := range task.NetworksAttachments {
		if s.containerID == "" && attachment.Network.DriverState.Name == "host" {
			return true
		}

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
