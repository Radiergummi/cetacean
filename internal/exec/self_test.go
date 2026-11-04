package exec

import (
	"net/netip"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestSelfRecognisesItsOwnTaskByAddress(t *testing.T) {
	s := newSelf([]netip.Addr{netip.MustParseAddr("10.0.1.7")})

	own := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.7/24"}}},
	}
	other := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.8/24"}}},
	}
	garbled := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"not-an-ip"}}},
	}

	if !s.Is(own) || s.Is(other) || s.Is(garbled) || s.Is(swarm.Task{}) {
		t.Error("Is is wrong")
	}
}

func TestOwnContainerIDReadsTheHostnameMount(t *testing.T) {
	const id = "4309a3c3d29124bd2a3dd38c37c146ac8e9119b1f2a69541dbc306d11e648370"
	mountinfo := "1917 1 0:40 / / rw - overlay overlay rw\n" +
		"1926 1917 0:37 /docker/containers/" + id + "/hostname /etc/hostname rw - btrfs /dev/vdb1 rw\n"

	if got := ownContainerID([]byte(mountinfo)); got != id {
		t.Errorf("ownContainerID = %q, want %q", got, id)
	}
	if got := ownContainerID([]byte("1917 1 0:40 / / rw - overlay overlay rw\n")); got != "" {
		t.Errorf("ownContainerID outside a container = %q, want empty", got)
	}
}

func TestSelfRecognisesItsOwnContainerOnAnyNetwork(t *testing.T) {
	s := newSelf(nil)
	s.containerID = "own"

	own := hostNetworkTask("own")
	if !s.Is(own) || s.Is(hostNetworkTask("other")) {
		t.Error("Is is wrong")
	}
}

func TestSelfFailsClosedOnTheHostNetworkWithoutItsContainerID(t *testing.T) {
	s := newSelf(nil)

	if !s.Is(hostNetworkTask("any")) {
		t.Error("a host-network task is not refused when Cetacean cannot name its container")
	}
	overlay := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.8/24"}}},
	}
	if s.Is(overlay) {
		t.Error("an overlay task is refused")
	}
}

func hostNetworkTask(containerID string) swarm.Task {
	return swarm.Task{
		Status: swarm.TaskStatus{
			ContainerStatus: &swarm.ContainerStatus{ContainerID: containerID},
		},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network: swarm.Network{DriverState: swarm.Driver{Name: "host"}},
		}},
	}
}
