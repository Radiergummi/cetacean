package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// The periodic re-sync runs beside a live stream, so a failure there is not a
// disconnection — the stream ending is. Reporting one left /-/health, the
// dashboard's stale marker and cetacean_watcher_connected claiming the engine
// was unreachable until the next tick, five minutes later.
func TestAFailedPeriodicResyncIsNotADisconnection(t *testing.T) {
	mc := newMockClient()
	mc.nodes = []swarm.Node{{ID: "n1"}}

	w := NewWatcher(mc, cache.New(nil), "")
	w.syncInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.fullSync(ctx); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	if connected, _ := w.Liveness(); !connected {
		t.Fatal("a watcher that just synced reports disconnected")
	}

	_, lastSync := w.Liveness()

	// Every periodic re-sync from here on fails, while the stream stays up.
	mc.mu.Lock()
	for _, name := range []string{
		"nodes", "services", "tasks", "configs", "secrets", "networks", "volumes",
	} {
		mc.listErrors[name] = errors.New("connection refused")
	}
	mc.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.watchEvents(ctx)
	}()

	before := mc.fullSyncs.Load()
	deadline := time.After(2 * time.Second)

	for mc.fullSyncs.Load() < before+2 {
		select {
		case <-deadline:
			t.Fatal("the periodic re-sync never ran")
		case <-time.After(time.Millisecond):
		}
	}

	connected, stale := w.Liveness()
	if !connected {
		t.Error("a failed periodic re-sync reported the stream as disconnected")
	}

	// The cache has gone stale, and that is the signal that must show it.
	if !stale.Equal(lastSync) {
		t.Errorf("a failed sync moved lastSync from %v to %v", lastSync, stale)
	}

	cancel()
	<-done
}

// A refresh runs on the event loop, so nothing else is applied while it is in
// flight. Inspecting on the loop and applying from the caller's goroutine let
// a remove delivered in between be undone, putting a deleted resource back
// into every listing until the next re-sync.
func TestARefreshIsSerializedWithEventProcessing(t *testing.T) {
	mc := newMockClient()
	mc.services = []swarm.Service{{ID: "s1"}, {ID: "s2"}}

	store := cache.New(nil)
	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.fullSync(ctx); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	entered := make(chan struct{})
	released := make(chan struct{})

	var once sync.Once
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
		once.Do(func() { close(entered) })
		<-released

		return swarm.Service{ID: "s1"}, nil
	}

	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx)
	}()

	for !w.loopRunning.Load() {
		time.Sleep(time.Millisecond)
	}

	refreshed := make(chan error, 1)
	go func() { refreshed <- w.Refresh(ctx, string(events.ServiceEventType), "s1") }()

	<-entered

	// A remove for an unrelated service, which needs no inspect of its own.
	mc.eventsCh <- events.Message{
		Type:   events.ServiceEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "s2"},
	}

	// Well past the debounce window: if the loop were free, it would have
	// applied the remove by now.
	time.Sleep(20 * debounceWindow)

	if _, ok := store.GetService("s2"); !ok {
		t.Error("an event was applied while a refresh was in flight, so the two are not serialized")
	}

	close(released)

	if err := <-refreshed; err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Once the refresh is done the loop drains the event as usual.
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := store.GetService("s2"); !ok {
			break
		}

		select {
		case <-deadline:
			t.Fatal("the remove was never applied after the refresh finished")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	<-streamed
}
