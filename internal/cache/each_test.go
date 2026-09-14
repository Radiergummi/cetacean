package cache

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// Search depends on Each yielding exactly what List returns, in the same order:
// it truncates at a limit, so a divergence would change which results a query
// returns and the validator derived from them.
func TestEachMatchesListOrder(t *testing.T) {
	c := New(nil)
	for i := range 50 {
		id := fmt.Sprintf("id-%d", i)
		c.SetNode(swarm.Node{ID: id})
		c.SetService(swarm.Service{
			ID:   id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "svc-" + id}},
		})
		c.SetTask(swarm.Task{ID: id, Slot: i % 7, ServiceID: id})
		c.SetConfig(swarm.Config{ID: id})
		c.SetSecret(swarm.Secret{ID: id})
		c.SetNetwork(network.Summary{ID: id, Name: "net-" + id})
		c.SetVolume(volume.Volume{Name: "vol-" + id})
	}

	t.Run("nodes", func(t *testing.T) {
		want := c.ListNodes()
		var got []swarm.Node
		c.EachNode(func(n swarm.Node) bool { got = append(got, n); return true })
		assertIDs(t, len(want), len(got), func(i int) (string, string) {
			return want[i].ID, got[i].ID
		})
	})

	t.Run("services", func(t *testing.T) {
		want := c.ListServices()
		var got []swarm.Service
		c.EachService(func(s swarm.Service) bool { got = append(got, s); return true })
		assertIDs(t, len(want), len(got), func(i int) (string, string) {
			return want[i].ID, got[i].ID
		})
	})

	t.Run("tasks", func(t *testing.T) {
		want := c.ListTasks()
		var got []swarm.Task
		c.EachTask(func(tk swarm.Task) bool { got = append(got, tk); return true })
		assertIDs(t, len(want), len(got), func(i int) (string, string) {
			return fmt.Sprintf("%d/%s", want[i].Slot, want[i].ID),
				fmt.Sprintf("%d/%s", got[i].Slot, got[i].ID)
		})
	})

	t.Run("configs", func(t *testing.T) {
		want := c.ListConfigs()
		var got []swarm.Config
		c.EachConfig(func(v swarm.Config) bool { got = append(got, v); return true })
		assertIDs(t, len(want), len(got), func(i int) (string, string) {
			return want[i].ID, got[i].ID
		})
	})

	t.Run("volumes", func(t *testing.T) {
		want := c.ListVolumes()
		var got []volume.Volume
		c.EachVolume(func(v volume.Volume) bool { got = append(got, v); return true })
		assertIDs(t, len(want), len(got), func(i int) (string, string) {
			return want[i].Name, got[i].Name
		})
	})
}

func TestEachStopsOnFalse(t *testing.T) {
	c := New(nil)
	for i := range 10 {
		c.SetNode(swarm.Node{ID: fmt.Sprintf("id-%d", i)})
	}

	seen := 0
	c.EachNode(func(swarm.Node) bool {
		seen++
		return seen < 3
	})
	if seen != 3 {
		t.Fatalf("stopped after %d nodes, want 3", seen)
	}
}

func assertIDs(t *testing.T, wantLen, gotLen int, at func(int) (string, string)) {
	t.Helper()
	if wantLen != gotLen {
		t.Fatalf("Each yielded %d, List returned %d", gotLen, wantLen)
	}
	for i := range wantLen {
		w, g := at(i)
		if w != g {
			t.Fatalf("position %d: Each gave %q, List gave %q", i, g, w)
		}
	}
}
