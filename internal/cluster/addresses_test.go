package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/ipam"
)

func TestNetworkAddresses(t *testing.T) {
	c := cache.New(nil)
	ingress := network.Summary{
		ID:      "ingress",
		Scope:   "swarm",
		Ingress: true,
		IPAM:    network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.0.0/24"}}},
	}
	c.SetNetwork(ingress)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	c.SetService(swarm.Service{
		ID: "svc",
		Endpoint: swarm.Endpoint{
			VirtualIPs: []swarm.EndpointVirtualIP{{NetworkID: "ingress", Addr: "10.0.0.2/24"}},
		},
	})
	c.SetTask(swarm.Task{
		ID:     "task",
		NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{ID: "ingress"},
			Addresses: []string{"10.0.0.3/24"},
		}},
	})

	got := NetworkAddresses(c, []network.Summary{ingress})["ingress"]
	want := ipam.Addresses{Usable: 253, Used: 4, VIPs: 1, Tasks: 1, LoadBalancers: 2}

	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
