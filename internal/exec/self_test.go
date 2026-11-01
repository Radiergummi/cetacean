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
