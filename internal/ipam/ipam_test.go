package ipam

import (
	"slices"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

func TestUsable(t *testing.T) {
	tests := []struct {
		name string
		cfg  network.IPAMConfig
		want int
		ok   bool
	}{
		{"/24", network.IPAMConfig{Subnet: "10.0.1.0/24"}, 253, true},
		{"/16", network.IPAMConfig{Subnet: "10.1.0.0/16"}, 65533, true},
		{"/30", network.IPAMConfig{Subnet: "10.0.1.0/30"}, 1, true},
		{
			"/31 has no network or broadcast address",
			network.IPAMConfig{Subnet: "10.0.1.0/31"},
			1,
			true,
		},
		{"/32 is all gateway", network.IPAMConfig{Subnet: "10.0.1.1/32"}, 0, true},
		{"unmasked subnet", network.IPAMConfig{Subnet: "10.0.1.7/24"}, 253, true},
		{
			"explicit gateway elsewhere",
			network.IPAMConfig{Subnet: "10.0.1.0/24", Gateway: "10.0.1.254"},
			253,
			true,
		},
		{
			"range clear of the reserved addresses",
			network.IPAMConfig{Subnet: "10.0.0.0/16", IPRange: "10.0.4.0/24"},
			256,
			true,
		},
		{
			"range holding network and gateway",
			network.IPAMConfig{Subnet: "10.0.0.0/16", IPRange: "10.0.0.0/24"},
			254,
			true,
		},
		{
			"range holding broadcast",
			network.IPAMConfig{Subnet: "10.0.0.0/16", IPRange: "10.0.255.0/24"},
			255,
			true,
		},
		{
			"range equal to the subnet",
			network.IPAMConfig{Subnet: "10.0.1.0/24", IPRange: "10.0.1.0/24"},
			253,
			true,
		},
		{
			"auxiliary addresses are reserved",
			network.IPAMConfig{
				Subnet:     "10.0.1.0/24",
				AuxAddress: map[string]string{"a": "10.0.1.10", "b": "10.0.1.1", "c": "10.0.2.1"},
			},
			252,
			true,
		},
		{"IPv6", network.IPAMConfig{Subnet: "fd00::/64"}, 0, false},
		{"no subnet", network.IPAMConfig{}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := usable(tt.cfg)
			if got != tt.want || ok != tt.ok {
				t.Errorf("usable(%+v) = %d, %v; want %d, %v", tt.cfg, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func overlay(id string, subnets ...string) network.Summary {
	n := network.Summary{ID: id, Name: id, Driver: "overlay", Scope: "swarm"}
	for _, subnet := range subnets {
		n.IPAM.Config = append(n.IPAM.Config, network.IPAMConfig{Subnet: subnet})
	}

	return n
}

func vipService(vips ...swarm.EndpointVirtualIP) swarm.Service {
	return swarm.Service{Endpoint: swarm.Endpoint{VirtualIPs: vips}}
}

func task(node string, state swarm.TaskState, networkID string, addresses ...string) swarm.Task {
	return swarm.Task{
		NodeID: node,
		Status: swarm.TaskStatus{State: state},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{ID: networkID},
			Addresses: addresses,
		}},
	}
}

func TestCount(t *testing.T) {
	ingress := overlay("ingress", "10.0.0.0/24")
	ingress.Ingress = true

	attachable := overlay("attachable", "10.0.9.0/24")
	attachable.Attachable = true

	plugin := overlay("plugin", "10.0.8.0/24")
	plugin.IPAM.Driver = "infoblox"

	local := overlay("bridge", "172.17.0.0/16")
	local.Scope = "local"
	local.Driver = "bridge"

	networks := []network.Summary{
		overlay("app", "10.0.1.0/24", "10.0.2.0/24"),
		ingress,
		attachable,
		plugin,
		local,
		overlay("dual", "10.0.3.0/24", "fd00::/64"),
		overlay("v6only", "fd01::/64"),
	}

	services := []swarm.Service{
		vipService(
			swarm.EndpointVirtualIP{NetworkID: "app", Addr: "10.0.1.2/24"},
			swarm.EndpointVirtualIP{NetworkID: "ingress", Addr: "10.0.0.2/24"},
		),
		// The same VIP twice, as two service records never hold it, still counts once.
		vipService(swarm.EndpointVirtualIP{NetworkID: "app", Addr: "10.0.1.2/24"}),
		vipService(swarm.EndpointVirtualIP{NetworkID: "dual", Addr: "10.0.3.2/24"}),
		// dnsrr: no VIPs.
		{},
	}

	tasks := []swarm.Task{
		task("n1", swarm.TaskStateRunning, "app", "10.0.1.3/24"),
		task("n1", swarm.TaskStateStarting, "app", "10.0.1.4/24"),
		task("n2", swarm.TaskStateRunning, "app", "10.0.2.3/24"),
		// Not yet scheduled: holds an address, but no node holds a balancer for it.
		task("", swarm.TaskStatePending, "app", "10.0.1.5/24"),
		// Stopped: the allocator has already freed these.
		task("n3", swarm.TaskStateShutdown, "app", "10.0.1.6/24"),
		task("n3", swarm.TaskStateFailed, "app", "10.0.1.7/24"),
		task("n3", swarm.TaskStateComplete, "app", "10.0.1.8/24"),
		task("n1", swarm.TaskStateRunning, "ingress", "10.0.0.3/24"),
		task("n1", swarm.TaskStateRunning, "attachable", "10.0.9.3/24"),
		task("n1", swarm.TaskStateRunning, "plugin", "10.0.8.3/24"),
		task("n1", swarm.TaskStateRunning, "dual", "10.0.3.3/24", "fd00::3/64"),
	}

	got := Count(networks, slices.Values(services), slices.Values(tasks), 5)

	want := map[string]Addresses{
		"app":     {Usable: 506, Used: 7, VIPs: 1, Tasks: 4, LoadBalancers: 2},
		"ingress": {Usable: 253, Used: 7, VIPs: 1, Tasks: 1, LoadBalancers: 5},
		"attachable": {
			Usable:                     253,
			Used:                       2,
			Tasks:                      1,
			LoadBalancers:              1,
			ExcludesAttachedContainers: true,
		},
		"dual": {Usable: 253, Used: 3, VIPs: 1, Tasks: 1, LoadBalancers: 1},
	}

	if len(got) != len(want) {
		t.Errorf("Count returned %d networks, want %d: %+v", len(got), len(want), got)
	}

	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %+v, want %+v", id, got[id], w)
		}
	}
}

func TestLive(t *testing.T) {
	tests := []struct {
		state, desired swarm.TaskState
		want           bool
	}{
		{swarm.TaskStateNew, swarm.TaskStateRunning, true},
		{swarm.TaskStateAllocated, swarm.TaskStateRunning, true},
		{swarm.TaskStateReady, swarm.TaskStateRunning, true},
		{swarm.TaskStateRunning, swarm.TaskStateRunning, true},
		// Asked to stop but not yet stopped: the addresses are still held.
		{swarm.TaskStateRunning, swarm.TaskStateShutdown, true},
		// Failed while still wanted running: already freed.
		{swarm.TaskStateFailed, swarm.TaskStateRunning, false},
		{swarm.TaskStateComplete, swarm.TaskStateShutdown, false},
		{swarm.TaskStateShutdown, swarm.TaskStateShutdown, false},
		{swarm.TaskStateRejected, swarm.TaskStateRunning, false},
		{swarm.TaskStateRemove, swarm.TaskStateRemove, false},
		{swarm.TaskStateOrphaned, swarm.TaskStateRunning, false},
	}

	for _, tt := range tests {
		task := swarm.Task{DesiredState: tt.desired, Status: swarm.TaskStatus{State: tt.state}}
		if got := live(task); got != tt.want {
			t.Errorf("live(%s, desired %s) = %v, want %v", tt.state, tt.desired, got, tt.want)
		}
	}
}
