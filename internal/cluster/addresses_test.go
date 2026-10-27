package cluster

import (
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
	c.SetNetwork(network.Summary{ID: "ingress", Ingress: true})
	c.SetNetwork(network.Summary{ID: "backend"})

	attached := swarm.Task{NetworksAttachments: []swarm.NetworkAttachment{
		{Network: swarm.Network{ID: "backend"}},
		{Network: swarm.Network{ID: "ingress"}},
	}}

	tests := []struct {
		name    string
		event   cache.Event
		wantIDs []string
		wantAll bool
	}{
		{
			"an attached task",
			cache.Event{Type: cache.EventTask, Resource: attached},
			[]string{"backend", "ingress"},
			false,
		},
		{
			"a task on no network",
			cache.Event{Type: cache.EventTask, Resource: swarm.Task{}},
			nil,
			false,
		},
		{
			"a task without its record",
			cache.Event{Type: cache.EventTask, Action: "remove"},
			nil,
			false,
		},
		{"a service", cache.Event{Type: cache.EventService, Resource: swarm.Service{}}, nil, true},
		{"a node", cache.Event{Type: cache.EventNode, ID: "n1"}, []string{"ingress"}, false},
		{"a network", cache.Event{Type: cache.EventNetwork, ID: "backend"}, nil, false},
		{"a config", cache.Event{Type: cache.EventConfig, ID: "c1"}, nil, false},
	}

	for _, tt := range tests {
		ids, all := AddressNetworks(c, tt.event)
		if !slices.Equal(ids, tt.wantIDs) || all != tt.wantAll {
			t.Errorf("%s: got %v, %v; want %v, %v", tt.name, ids, all, tt.wantIDs, tt.wantAll)
		}
	}
}
