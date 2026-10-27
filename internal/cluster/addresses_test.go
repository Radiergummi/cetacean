package cluster

import (
	"maps"
	"slices"
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

	got := c.NetworkAddresses()["ingress"]
	want := ipam.Addresses{Usable: 253, Used: 4, VIPs: 1, TaskAddresses: 1, LoadBalancers: 2}

	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAddressNetworks(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{
		ID:      "ingress",
		Scope:   "swarm",
		Ingress: true,
		IPAM:    network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.0.0/24"}}},
	})
	c.SetNetwork(network.Summary{
		ID:    "backend",
		Scope: "swarm",
		IPAM:  network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.1.0/24"}}},
	})
	c.SetNetwork(network.Summary{ID: "bridge", Scope: "local"})
	c.SetNetwork(network.Summary{
		ID:    "v6",
		Scope: "swarm",
		IPAM:  network.IPAM{Config: []network.IPAMConfig{{Subnet: "fd00::/64"}}},
	})
	c.SetNetwork(network.Summary{
		ID:    "plugin",
		Scope: "swarm",
		IPAM: network.IPAM{
			Driver: "infoblox",
			Config: []network.IPAMConfig{{Subnet: "10.0.2.0/24"}},
		},
	})

	attached := swarm.Task{NetworksAttachments: []swarm.NetworkAttachment{
		{Network: swarm.Network{ID: "backend"}},
		{Network: swarm.Network{ID: "ingress"}},
	}}

	tests := []struct {
		name  string
		event cache.Event
		want  []string
	}{
		{
			"an attached task",
			cache.Event{Type: cache.EventTask, Resource: attached},
			[]string{"backend", "ingress"},
		},
		{"a task on no network", cache.Event{Type: cache.EventTask, Resource: swarm.Task{}}, nil},
		{"a task without its record", cache.Event{Type: cache.EventTask, Action: "remove"}, nil},
		{
			"a service reaches every network with a figure",
			cache.Event{Type: cache.EventService, Resource: swarm.Service{}},
			[]string{"backend", "ingress"},
		},
		{"a node", cache.Event{Type: cache.EventNode, ID: "n1"}, []string{"ingress"}},
		{"a network", cache.Event{Type: cache.EventNetwork, ID: "backend"}, nil},
		{"a config", cache.Event{Type: cache.EventConfig, ID: "c1"}, nil},
	}

	for _, tt := range tests {
		if got := AddressNetworks(c, tt.event); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}

	counted := slices.Sorted(maps.Keys(c.NetworkAddresses()))
	if got := AddressNetworks(
		c,
		cache.Event{Type: cache.EventService},
	); !slices.Equal(
		got,
		counted,
	) {
		t.Errorf("a service names %v, but the networks with a figure are %v", got, counted)
	}
}
