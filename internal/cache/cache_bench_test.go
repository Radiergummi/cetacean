package cache

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func populateCache(c *Cache, n int) {
	for i := 0; i < n; i++ {
		stack := fmt.Sprintf("stack-%d", i%5)
		id := fmt.Sprintf("id-%d", i)

		c.nodes[id] = swarm.Node{
			ID:     id,
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			Description: swarm.NodeDescription{
				Hostname: fmt.Sprintf("node-%d.example.com", i),
			},
		}

		c.services[id] = swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("svc-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		}

		c.tasks[id] = swarm.Task{
			ID:        id,
			ServiceID: fmt.Sprintf("id-%d", i%10),
			NodeID:    fmt.Sprintf("id-%d", i%10),
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		}

		c.configs[id] = swarm.Config{
			ID: id,
			Spec: swarm.ConfigSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("cfg-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		}

		c.secrets[id] = swarm.Secret{
			ID: id,
			Spec: swarm.SecretSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("sec-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		}

		c.networks[id] = network.Summary{
			ID:     id,
			Name:   fmt.Sprintf("net-%d", i),
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		}

		c.volumes[fmt.Sprintf("vol-%d", i)] = volume.Volume{
			Name:   fmt.Sprintf("vol-%d", i),
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		}
	}
	c.rebuildStacks()
	c.tasksByService = make(map[string]map[string]struct{})
	c.tasksByNode = make(map[string]map[string]struct{})
	for _, t := range c.tasks {
		c.addTaskIndex(t)
	}
}

func newPopulatedCache(n int) *Cache {
	c := New(nil)
	populateCache(c, n)
	return c
}

var sizes = []int{10, 100, 1000}

// --- rebuildStacks ---

func BenchmarkRebuildStacks(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.rebuildStacks()
			}
		})
	}
}

// --- Set operations ---

func BenchmarkSetNode(b *testing.B) {
	node := swarm.Node{ID: "bench-node", Status: swarm.NodeStatus{State: swarm.NodeStateReady}}
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.SetNode(node)
			}
		})
	}
}

func BenchmarkSetService(b *testing.B) {
	svc := swarm.Service{
		ID: "bench-svc",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "bench-svc",
				Labels: map[string]string{"com.docker.stack.namespace": "bench-stack"},
			},
		},
	}
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.SetService(svc)
			}
		})
	}
}

func BenchmarkSetTask(b *testing.B) {
	task := swarm.Task{ID: "bench-task", ServiceID: "id-0", NodeID: "id-0"}
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.SetTask(task)
			}
		})
	}
}

// --- List operations ---

func BenchmarkListNodes(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.ListNodes()
			}
		})
	}
}

func BenchmarkListServices(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.ListServices()
			}
		})
	}
}

func BenchmarkListTasks(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.ListTasks()
			}
		})
	}
}

// --- Filtered task lists (linear scan) ---

func BenchmarkListTasksByService(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.ListTasksByService("id-0")
			}
		})
	}
}

func BenchmarkListTasksByNode(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.ListTasksByNode("id-0")
			}
		})
	}
}

// --- Snapshot ---

func BenchmarkSnapshot(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.Snapshot()
			}
		})
	}
}

// --- GetStackDetail ---

func BenchmarkGetStackDetail(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				c.GetStackDetail("stack-0")
			}
		})
	}
}

// --- Concurrent reads ---

func BenchmarkListNodesParallel(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					c.ListNodes()
				}
			})
		})
	}
}

func BenchmarkSnapshotParallel(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					c.Snapshot()
				}
			})
		})
	}
}

// --- Concurrent read + write ---

func BenchmarkConcurrentReadWrite(b *testing.B) {
	for _, n := range sizes {
		c := newPopulatedCache(n)
		node := swarm.Node{ID: "bench-rw", Status: swarm.NodeStatus{State: swarm.NodeStateReady}}
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					if i%10 == 0 {
						c.SetNode(node)
					} else {
						c.ListNodes()
					}
					i++
				}
			})
		})
	}
}
