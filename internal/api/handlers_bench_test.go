package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
)

func populateCache(c *cache.Cache, n int) {
	for i := 0; i < n; i++ {
		stack := fmt.Sprintf("stack-%d", i%5)
		id := fmt.Sprintf("id-%d", i)

		c.SetNode(swarm.Node{
			ID:     id,
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			Description: swarm.NodeDescription{
				Hostname: fmt.Sprintf("node-%d.example.com", i),
			},
		})

		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("svc-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		})

		c.SetTask(swarm.Task{
			ID:        id,
			ServiceID: fmt.Sprintf("id-%d", i%10),
			NodeID:    fmt.Sprintf("id-%d", i%10),
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
		})

		c.SetConfig(swarm.Config{
			ID: id,
			Spec: swarm.ConfigSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("cfg-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		})

		c.SetSecret(swarm.Secret{
			ID: id,
			Spec: swarm.SecretSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("sec-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		})

		c.SetNetwork(network.Summary{
			ID:     id,
			Name:   fmt.Sprintf("net-%d", i),
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		})

		c.SetVolume(volume.Volume{
			Name:   fmt.Sprintf("vol-%d", i),
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		})
	}
}

var handlerSizes = []int{10, 100, 1000}

// --- HTTP handler benchmarks ---

func BenchmarkHandleListNodes(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/nodes", nil)
				w := httptest.NewRecorder()
				h.HandleListNodes(w, req)
			}
		})
	}
}

func BenchmarkHandleListNodes_Search(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/nodes?search=node-5", nil)
				w := httptest.NewRecorder()
				h.HandleListNodes(w, req)
			}
		})
	}
}

func BenchmarkHandleListServices(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/services", nil)
				w := httptest.NewRecorder()
				h.HandleListServices(w, req)
			}
		})
	}
}

func BenchmarkHandleListServices_Search(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/services?search=svc-5", nil)
				w := httptest.NewRecorder()
				h.HandleListServices(w, req)
			}
		})
	}
}

func BenchmarkHandleListTasks(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/tasks", nil)
				w := httptest.NewRecorder()
				h.HandleListTasks(w, req)
			}
		})
	}
}

func BenchmarkHandleCluster(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/cluster", nil)
				w := httptest.NewRecorder()
				h.HandleCluster(w, req)
			}
		})
	}
}

func BenchmarkHandleGetStack(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequest("GET", "/stacks/stack-0", nil)
				req.SetPathValue("name", "stack-0")
				w := httptest.NewRecorder()
				h.HandleGetStack(w, req)
			}
		})
	}
}

// --- SSE Broadcast benchmark ---

func BenchmarkBroadcast(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0)
			defer br.Close()

			// Register clients with buffered channels
			for i := 0; i < nClients; i++ {
				client := &sseClient{
					events: make(chan cache.Event, 64),
					done:   make(chan struct{}),
				}
				br.clients[client] = struct{}{}
				// Drain events in background
				go func(c *sseClient) {
					for {
						select {
						case <-c.done:
							return
						case <-c.events:
						}
					}
				}(client)
			}

			event := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

// --- Parallel HTTP benchmarks ---

func BenchmarkHandleListNodesParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, closedReady(), nil, nil)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequest("GET", "/nodes", nil)
					w := httptest.NewRecorder()
					h.HandleListNodes(w, req)
					if w.Code != http.StatusOK {
						b.Fatalf("expected 200, got %d", w.Code)
					}
				}
			})
		})
	}
}
