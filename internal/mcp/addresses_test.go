package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The network resource and describe read through one digest, so pinning the
// resource pins both.
func TestNetworkResourceCarriesAddresses(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "backend",
		Driver: "overlay",
		Scope:  "swarm",
		IPAM:   network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.1.0/24"}}},
	})
	c.SetTask(swarm.Task{
		ID:     "t1",
		NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{ID: "net1"},
			Addresses: []string{"10.0.1.3/24"},
		}},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://networks/net1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var digest struct {
		Details struct {
			Addresses map[string]any `json:"addresses"`
		} `json:"details"`
	}
	if err := json.Unmarshal([]byte(body), &digest); err != nil {
		t.Fatal(err)
	}

	got := digest.Details.Addresses
	if got["usable"] != 253.0 || got["used"] != 2.0 || got["taskAddresses"] != 1.0 ||
		got["loadBalancers"] != 1.0 {
		t.Errorf("addresses = %v, want usable 253, used 2, taskAddresses 1, loadBalancers 1", got)
	}
}
