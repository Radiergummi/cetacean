package sse

import (
	"context"
	"fmt"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
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
			br := NewBroadcaster(0, noopErrorWriter, nil)
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
			br := NewBroadcaster(0, noopErrorWriter, nil)
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
			br := NewBroadcaster(0, noopErrorWriter, nil)
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
			br := NewBroadcaster(0, noopErrorWriter, nil)
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
	br := NewBroadcaster(0, noopErrorWriter, nil)
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

// benchRecorder counts flushes instead of retaining the stream. A benchmark
// needs to know when a batch reached the client without either polling the body
// — which copies it — or letting it grow for the length of the run.
type benchRecorder struct {
	*httptest.ResponseRecorder
	flushes atomic.Uint64
}

func (r *benchRecorder) Write(p []byte) (int, error) { return len(p), nil }
func (r *benchRecorder) Flush()                      { r.flushes.Add(1) }

// serveOneClient attaches a single SSE client and returns its recorder and a
// teardown. The batch interval is deliberately tiny: at the production default
// every iteration would measure the ticker rather than the work.
func serveOneClient(b *testing.B, target string) (*Broadcaster, *benchRecorder, func()) {
	b.Helper()

	br := NewBroadcaster(time.Microsecond, noopErrorWriter, nil)
	ctx, cancel := context.WithCancel(b.Context())
	req := httptest.NewRequestWithContext(ctx, "GET", target, nil)
	w := &benchRecorder{ResponseRecorder: httptest.NewRecorder()}

	var wg sync.WaitGroup
	wg.Go(func() { br.ServeHTTP(w, req) })

	for {
		br.mu.RLock()
		n := len(br.clients)
		br.mu.RUnlock()
		if n > 0 {
			break
		}
		runtime.Gosched()
	}

	return br, w, func() {
		cancel()
		wg.Wait()
		br.Close()
	}
}

// awaitFlush spins until every broadcast event has reached the client.
//
// Draining both queues first is what keeps the measurement honest: Broadcast
// is non-blocking by design, so a loop that only waited for the next flush
// would outrun the fan-out and measure a run that silently dropped events.
//
// That same non-blocking drop is why this gives up rather than spinning
// forever: an iteration whose events were all dropped, or all filtered out,
// drains to nothing without ever flushing.
func awaitFlush(b *testing.B, br *Broadcaster, w *benchRecorder, since uint64) {
	b.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		pending := len(br.inbox)
		br.mu.RLock()
		for c := range br.clients {
			pending += len(c.events)
		}
		br.mu.RUnlock()

		if pending == 0 && w.flushes.Load() != since {
			return
		}

		if time.Now().After(deadline) {
			b.Fatalf("no flush within 5s: %d events still queued", pending)
		}

		runtime.Gosched()
	}
}

func BenchmarkServeSSE(b *testing.B) {
	for _, nEvents := range []int{1, 10} {
		b.Run(fmt.Sprintf("events=%d", nEvents), func(b *testing.B) {
			br, w, stop := serveOneClient(b, "/events")
			defer stop()

			for b.Loop() {
				before := w.flushes.Load()
				for i := range nEvents {
					br.Broadcast(cache.Event{
						Type:   "service",
						Action: "update",
						ID:     fmt.Sprintf("svc-%d", i),
					})
				}
				awaitFlush(b, br, w, before)
			}
		})
	}
}

func BenchmarkServeSSEFiltered(b *testing.B) {
	br, w, stop := serveOneClient(b, "/events?types=service")
	defer stop()

	for b.Loop() {
		before := w.flushes.Load()
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
		awaitFlush(b, br, w, before)
	}
}
