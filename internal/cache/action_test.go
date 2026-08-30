package cache

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// Clients that hold a partially-paginated list need to tell a brand-new
// resource from a status change on one they simply haven't paged to yet: the
// first raises the collection's total, the second must not. The cache already
// knows which it is, so it says so.
func TestSetEmitsCreateThenUpdate(t *testing.T) {
	tests := []struct {
		name  string
		first func(*Cache)
		again func(*Cache)
	}{
		{
			"node",
			func(c *Cache) { c.SetNode(swarm.Node{ID: "n1"}) },
			func(c *Cache) {
				node := swarm.Node{ID: "n1"}
				node.Description.Hostname = "renamed"
				c.SetNode(node)
			},
		},
		{
			"service",
			func(c *Cache) { c.SetService(swarm.Service{ID: "s1"}) },
			func(c *Cache) {
				service := swarm.Service{ID: "s1"}
				service.Version.Index = 2
				c.SetService(service)
			},
		},
		{
			"task",
			func(c *Cache) { c.SetTask(swarm.Task{ID: "t1"}) },
			func(c *Cache) {
				task := swarm.Task{ID: "t1"}
				task.Status.State = swarm.TaskStateRunning
				c.SetTask(task)
			},
		},
		{
			"config",
			func(c *Cache) { c.SetConfig(swarm.Config{ID: "c1"}) },
			func(c *Cache) { c.SetConfig(swarm.Config{ID: "c1"}) },
		},
		{
			"secret",
			func(c *Cache) { c.SetSecret(swarm.Secret{ID: "sec1"}) },
			func(c *Cache) { c.SetSecret(swarm.Secret{ID: "sec1"}) },
		},
		{
			"network",
			func(c *Cache) { c.SetNetwork(network.Summary{ID: "net1"}) },
			func(c *Cache) { c.SetNetwork(network.Summary{ID: "net1"}) },
		},
		{
			"volume",
			func(c *Cache) { c.SetVolume(volume.Volume{Name: "v1"}) },
			func(c *Cache) { c.SetVolume(volume.Volume{Name: "v1"}) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var actions []string
			c := New(func(e Event) {
				if e.Type != EventSync && e.Action != "ref_changed" {
					actions = append(actions, e.Action)
				}
			})

			test.first(c)
			test.again(c)

			if len(actions) != 2 {
				t.Fatalf("got actions %v, want exactly two", actions)
			}

			if actions[0] != "create" {
				t.Errorf("first Set emitted %q, want \"create\"", actions[0])
			}

			if actions[1] != "update" {
				t.Errorf("second Set emitted %q, want \"update\"", actions[1])
			}
		})
	}
}
