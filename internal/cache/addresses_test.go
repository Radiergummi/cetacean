package cache

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/ipam"
)

func TestStoreAddressesKeepsTheNewerGeneration(t *testing.T) {
	c := New(nil)

	newer := &addressMemo{generation: 5}
	c.storeAddresses(newer)
	c.storeAddresses(&addressMemo{generation: 4})

	if c.addresses.Load() != newer {
		t.Error("an older count overwrote a newer one")
	}

	newest := &addressMemo{generation: 6}
	c.storeAddresses(newest)

	if c.addresses.Load() != newest {
		t.Error("a newer count was not stored")
	}
}

func TestNetworkAddressesSharesOneCountAmongConcurrentMisses(t *testing.T) {
	c := New(nil)
	for i := range 200 {
		c.SetTask(swarm.Task{ID: fmt.Sprintf("t%d", i)})
	}

	const callers = 16

	counts := make([]map[string]ipam.Addresses, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { counts[i] = c.NetworkAddresses() })
	}
	wg.Wait()

	first := reflect.ValueOf(counts[0]).Pointer()
	for i, got := range counts {
		if reflect.ValueOf(got).Pointer() != first {
			t.Fatalf("caller %d got a count of its own", i)
		}
	}
}

func TestNetworkAddressesCountsOncePerGeneration(t *testing.T) {
	c := New(nil)
	c.SetNetwork(network.Summary{
		ID:    "net1",
		Scope: "swarm",
		IPAM:  network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.1.0/24"}}},
	})

	c.NetworkAddresses()
	memo := c.addresses.Load()
	c.NetworkAddresses()

	if c.addresses.Load() != memo {
		t.Fatal("a second read in one generation counted again")
	}

	c.SetTask(swarm.Task{
		ID:     "t1",
		NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{ID: "net1"},
			Addresses: []string{"10.0.1.2/24"},
		}},
	})

	if got := c.NetworkAddresses()["net1"].Used; got != 2 {
		t.Errorf("after a task joined, used = %d, want 2", got)
	}
}
