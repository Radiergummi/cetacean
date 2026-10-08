package cache

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func TestGenerationAdvancesOnEveryMutation(t *testing.T) {
	c := New(nil)

	mutations := []struct {
		name string
		do   func()
	}{
		{"SetNode", func() { c.SetNode(swarm.Node{ID: "n1"}) }},
		{"SetService", func() { c.SetService(swarm.Service{ID: "s1"}) }},
		{"SetTask", func() { c.SetTask(swarm.Task{ID: "t1", ServiceID: "s1"}) }},
		{"SetConfig", func() { c.SetConfig(swarm.Config{ID: "c1"}) }},
		{"SetSecret", func() { c.SetSecret(swarm.Secret{ID: "x1"}) }},
		{"SetNetwork", func() { c.SetNetwork(network.Summary{ID: "net1"}) }},
		{"SetVolume", func() { c.SetVolume(volume.Volume{Name: "v1"}) }},
		{"DeleteNode", func() { c.DeleteNode("n1") }},
		{"DeleteService", func() { c.DeleteService("s1") }},
		{"DeleteTask", func() { c.DeleteTask("t1") }},
		{"DeleteConfig", func() { c.DeleteConfig("c1") }},
		{"DeleteSecret", func() { c.DeleteSecret("x1") }},
		{"DeleteNetwork", func() { c.DeleteNetwork("net1") }},
		{"DeleteVolume", func() { c.DeleteVolume("v1") }},
	}

	for _, m := range mutations {
		before := c.Generation()
		m.do()
		if got := c.Generation(); got <= before {
			t.Errorf("%s left the generation at %d, want > %d", m.name, got, before)
		}
	}
}

// The reason the generation is not the history sequence. ReplaceAll swaps every
// resource map behind a single sync event, and notify does not advance history
// for a sync — so a validator built on history would call a wholly replaced
// cache unchanged.
func TestGenerationAdvancesOnFullResync(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{ID: "old"})

	genBefore := c.Generation()
	historyBefore := c.history.Count()

	c.ReplaceAll(FullSyncData{
		HasServices: true,
		Services:    []swarm.Service{{ID: "new"}},
	})

	if c.history.Count() != historyBefore {
		t.Fatalf("history advanced on a resync (%d -> %d); this test is no longer "+
			"describing the reason the generation is separate",
			historyBefore, c.history.Count())
	}
	if c.Generation() <= genBefore {
		t.Errorf("generation stayed at %d across a full resync that replaced every service",
			c.Generation())
	}
	if _, ok := c.GetService("new"); !ok {
		t.Error("resync did not apply")
	}
}

func TestGenerationStableWithoutMutation(t *testing.T) {
	c := New(nil)
	for i := range 20 {
		c.SetNode(swarm.Node{ID: fmt.Sprintf("n-%d", i)})
	}

	got := c.Generation()
	for range 5 {
		c.ListNodes()
		c.Snapshot()
		c.EachNode(func(swarm.Node) bool { return true })
		_, _ = c.GetNode("n-1")
	}
	if c.Generation() != got {
		t.Errorf("reads advanced the generation: %d -> %d", got, c.Generation())
	}
}
