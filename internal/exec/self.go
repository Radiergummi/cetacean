package exec

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"regexp"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
)

// Self recognises Cetacean's own tasks by its service, its container ID, or the
// overlay addresses they hold. A shell in any of them is a shell next to the
// Docker socket and signing keys.
type Self struct {
	addrs       map[netip.Addr]bool
	containerID string
	serviceID   string
}

// ContainerInspector reads Cetacean's own container from its local daemon.
type ContainerInspector interface {
	ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error)
}

func NewSelf(ctx context.Context, docker ContainerInspector) (*Self, error) {
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
	if s.containerID == "" {
		slog.WarnContext(
			ctx, "own container not identified; exec refuses tasks without an overlay address",
		)
		return s, nil
	}

	own, err := docker.ContainerInspect(ctx, s.containerID)
	if err != nil {
		return nil, fmt.Errorf("own container %s not inspected: %w", s.containerID, err)
	}
	if own.Config != nil {
		s.serviceID = own.Config.Labels["com.docker.swarm.service.id"]
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

// hostnameRoot is the source Docker bind-mounts /etc/hostname from.
var hostnameRoot = regexp.MustCompile(`/containers/([0-9a-f]{64})/hostname$`)

// ownContainerID reads the container ID from the hostname mount Docker gives
// every container, whatever its network; empty outside a container.
func ownContainerID(mountinfo []byte) string {
	for line := range bytes.Lines(mountinfo) {
		fields := bytes.Fields(line)
		if len(fields) < 5 || string(fields[4]) != "/etc/hostname" {
			continue
		}
		if m := hostnameRoot.FindSubmatch(fields[3]); m != nil {
			return string(m[1])
		}
	}

	return ""
}

// Is fails closed when the container ID is unknown: a task with no overlay
// address, on the host network or none, gives nothing to recognise Cetacean by.
func (s *Self) Is(task swarm.Task) bool {
	if s.serviceID != "" && task.ServiceID == s.serviceID {
		return true
	}
	if s.containerID != "" && task.Status.ContainerStatus != nil &&
		task.Status.ContainerStatus.ContainerID == s.containerID {
		return true
	}
	if s.containerID == "" && len(task.NetworksAttachments) == 0 {
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
