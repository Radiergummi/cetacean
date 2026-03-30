package sse

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func drainClient(c *sseClient) {
	go func() {
		for {
			select {
			case <-c.done:
				return
			case <-c.events:
			}
		}
	}()
}

func setupClients(br *Broadcaster, n int, matchFn func(int) func(cache.Event) bool) {
	for i := range n {
		var match func(cache.Event) bool
		if matchFn != nil {
			match = matchFn(i)
		}
		client := &sseClient{
			events: make(chan cache.Event, 64),
			match:  match,
			done:   make(chan struct{}),
		}
		br.clients[client] = struct{}{}
		drainClient(client)
	}
}

func BenchmarkBroadcast(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0, noopErrorWriter)
			defer br.Close()
			setupClients(br, nClients, nil)

			event := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithPayload(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0, noopErrorWriter)
			defer br.Close()
			setupClients(br, nClients, nil)

			event := cache.Event{
				Type:   "service",
				Action: "update",
				ID:     "svc-1",
				Resource: swarm.Service{
					ID: "svc-1",
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "my-service"},
					},
				},
			}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithFiltering(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0, noopErrorWriter)
			defer br.Close()
			setupClients(br, nClients, func(i int) func(cache.Event) bool {
				if i%2 == 0 {
					return TypeMatcher("service")
				}
				return TypeMatcher("node")
			})

			event := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithResourceMatcher(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0, noopErrorWriter)
			defer br.Close()
			setupClients(br, nClients, func(i int) func(cache.Event) bool {
				return ResourceMatcher("service", fmt.Sprintf("svc-%d", i))
			})

			event := cache.Event{
				Type: "task", Action: "update", ID: "t1",
				Resource: swarm.Task{ServiceID: "svc-0"},
			}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkClientRegistration(b *testing.B) {
	br := NewBroadcaster(0, noopErrorWriter)
	defer br.Close()

	steady := &sseClient{
		events: make(chan cache.Event, 64),
		done:   make(chan struct{}),
	}
	br.mu.Lock()
	br.clients[steady] = struct{}{}
	br.mu.Unlock()
	drainClient(steady)

	ctx := b.Context()
	go func() {
		e := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				br.Broadcast(e)
			}
		}
	}()

	b.ResetTimer()
	for b.Loop() {
		client := &sseClient{
			events: make(chan cache.Event, 64),
			match:  TypeMatcher("service"),
			done:   make(chan struct{}),
		}
		br.mu.Lock()
		br.clients[client] = struct{}{}
		br.mu.Unlock()

		br.mu.Lock()
		delete(br.clients, client)
		br.mu.Unlock()
	}
}

func BenchmarkServeSSE(b *testing.B) {
	for _, nEvents := range []int{1, 10} {
		b.Run(fmt.Sprintf("events=%d", nEvents), func(b *testing.B) {
			for b.Loop() {
				br := NewBroadcaster(time.Millisecond, noopErrorWriter)

				ctx, cancel := context.WithCancel(context.Background())
				req := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
				w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

				var wg sync.WaitGroup
				wg.Go(func() {
					br.ServeHTTP(w, req)
				})

				for {
					br.mu.RLock()
					n := len(br.clients)
					br.mu.RUnlock()
					if n > 0 {
						break
					}
					time.Sleep(time.Microsecond)
				}

				for i := range nEvents {
					br.Broadcast(cache.Event{
						Type:   "service",
						Action: "update",
						ID:     fmt.Sprintf("svc-%d", i),
					})
				}

				time.Sleep(2 * time.Millisecond)
				cancel()
				wg.Wait()
				br.Close()
			}
		})
	}
}

func BenchmarkServeSSEFiltered(b *testing.B) {
	for b.Loop() {
		br := NewBroadcaster(time.Millisecond, noopErrorWriter)

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequestWithContext(ctx, "GET", "/events?types=service", nil)
		w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

		var wg sync.WaitGroup
		wg.Go(func() {
			br.ServeHTTP(w, req)
		})

		for {
			br.mu.RLock()
			n := len(br.clients)
			br.mu.RUnlock()
			if n > 0 {
				break
			}
			time.Sleep(time.Microsecond)
		}

		for i := range 5 {
			if i%2 == 0 {
				br.Broadcast(
					cache.Event{Type: "service", Action: "update", ID: fmt.Sprintf("svc-%d", i)},
				)
			} else {
				br.Broadcast(
					cache.Event{Type: "node", Action: "update", ID: fmt.Sprintf("n-%d", i)},
				)
			}
		}

		time.Sleep(2 * time.Millisecond)
		cancel()
		wg.Wait()
		br.Close()
	}
}
