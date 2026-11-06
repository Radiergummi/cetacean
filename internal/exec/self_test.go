package exec

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestSelfRecognisesItsOwnTaskByAddress(t *testing.T) {
	s := newSelf([]netip.Addr{netip.MustParseAddr("10.0.1.7")})
	s.containerID = "own"

	own := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.7/24"}}},
	}
	other := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.8/24"}}},
	}
	garbled := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"not-an-ip"}}},
	}

	if s.Check(own) != cluster.ExecSelf || s.Check(other) != cluster.ExecEligible ||
		s.Check(garbled) != cluster.ExecEligible || s.Check(swarm.Task{}) != cluster.ExecEligible {
		t.Error("Check is wrong")
	}
}

func TestOwnContainerIDReadsTheHostnameMount(t *testing.T) {
	const id = "4309a3c3d29124bd2a3dd38c37c146ac8e9119b1f2a69541dbc306d11e648370"
	mountinfo := "1917 1 0:40 / / rw - overlay overlay rw\n" +
		"1926 1917 0:37 /docker/containers/" + id + "/hostname /etc/hostname rw - btrfs /dev/vdb1 rw\n"

	if got := ownContainerID([]byte(mountinfo)); got != id {
		t.Errorf("ownContainerID = %q, want %q", got, id)
	}
	decoy := "1920 1917 0:37 /docker/containers/" + strings.Repeat("a", 64) +
		"/hostname /mnt/other rw - btrfs /dev/vdb1 rw\n"
	if got := ownContainerID([]byte(decoy + mountinfo)); got != id {
		t.Errorf("ownContainerID with a decoy mount = %q, want %q", got, id)
	}
	if got := ownContainerID([]byte("1917 1 0:40 / / rw - overlay overlay rw\n")); got != "" {
		t.Errorf("ownContainerID outside a container = %q, want empty", got)
	}
}

func TestSelfRecognisesItsOwnContainerOnAnyNetwork(t *testing.T) {
	s := newSelf(nil)
	s.containerID = "own"

	own := hostNetworkTask("own")
	if s.Check(own) != cluster.ExecSelf ||
		s.Check(hostNetworkTask("other")) != cluster.ExecEligible {
		t.Error("Check is wrong")
	}
}

func TestSelfFailsClosedOnTheHostNetworkWithoutItsContainerID(t *testing.T) {
	s := newSelf(nil)

	if got := s.Check(hostNetworkTask("any")); got != cluster.ExecSelfUnknown {
		t.Errorf("host-network task = %q, want self_unknown", got)
	}
	overlay := swarm.Task{
		NetworksAttachments: []swarm.NetworkAttachment{{Addresses: []string{"10.0.1.8/24"}}},
	}
	if got := s.Check(overlay); got != cluster.ExecEligible {
		t.Errorf("overlay task = %q, want it eligible", got)
	}
	if got := s.Check(swarm.Task{}); got != cluster.ExecSelfUnknown {
		t.Errorf("task with no network attachments = %q, want self_unknown", got)
	}
}

func TestSelfPrefersItsOwnAddressOverNotKnowingItsContainer(t *testing.T) {
	s := newSelf([]netip.Addr{netip.MustParseAddr("10.0.1.7")})

	task := hostNetworkTask("any")
	task.NetworksAttachments = append(task.NetworksAttachments, swarm.NetworkAttachment{
		Addresses: []string{"10.0.1.7/24"},
	})
	if got := s.Check(task); got != cluster.ExecSelf {
		t.Errorf("Check = %q, want the rule that identified Cetacean", got)
	}
}

func TestSelfRecognisesEveryTaskOfItsOwnService(t *testing.T) {
	s := newSelf(nil)
	s.containerID = "own"
	s.serviceID = "cetacean"

	replica := hostNetworkTask("replica")
	replica.ServiceID = "cetacean"
	other := hostNetworkTask("other")
	other.ServiceID = "web"

	if s.Check(replica) != cluster.ExecSelf || s.Check(other) != cluster.ExecEligible {
		t.Error("Check is wrong")
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
