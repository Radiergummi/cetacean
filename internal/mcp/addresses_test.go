package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// The network resource and describe read through one digest, so pinning the
// resource pins both.
func TestNetworkResourceCarriesAddresses(t *testing.T) {
	c := addressesCache()
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

func TestNetworkListingCarriesAddresses(t *testing.T) {
	c := addressesCache()
	c.SetNetwork(network.Summary{ID: "net2", Name: "bridge", Driver: "bridge", Scope: "local"})
	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://networks")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var listing []struct {
		ID        string         `json:"Id"`
		Addresses map[string]any `json:"Addresses"`
	}
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatal(err)
	}

	for _, entry := range listing {
		switch entry.ID {
		case "net1":
			if entry.Addresses["used"] != 2.0 {
				t.Errorf("net1: Addresses = %v, want used 2", entry.Addresses)
			}
		case "net2":
			if entry.Addresses != nil {
				t.Errorf("net2: Addresses = %v, want none", entry.Addresses)
			}
		}
	}

	if len(listing) != 2 {
		t.Errorf("listing = %d networks, want 2", len(listing))
	}
}

// find's raw records are the listing's, so they carry the figure too.
func TestFindRawNetworksCarryAddresses(t *testing.T) {
	srv := newToolTestServer(t, addressesCache(), &fakeWriteClient{}, config.OpsReadOnly)

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("find", map[string]any{
		"type": "networks",
		"raw":  true,
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var found struct {
		Raw []struct {
			Addresses map[string]any `json:"Addresses"`
		} `json:"raw"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if len(found.Raw) != 1 || found.Raw[0].Addresses["used"] != 2.0 {
		t.Errorf("raw = %+v, want one network with used 2", found.Raw)
	}
}

// addressesCache holds an overlay with one running task on it.
func addressesCache() *cache.Cache {
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

	return c
}
