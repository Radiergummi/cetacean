package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
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

// A manual resync runs on the event loop too. Fetching beside it let a remove
// applied mid-fetch be undone by the stale lists the fetch then replaced the
// cache with.
func TestAResyncCannotUndoARemoveAppliedDuringItsFetch(t *testing.T) {
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
	mc.fullSyncFn = func(int64) (cache.FullSyncData, error) {
		close(entered)
		<-released

		// What the engine listed before the remove below happened.
		return cache.FullSyncData{
			Services:    []swarm.Service{{ID: "s1"}, {ID: "s2"}},
			HasServices: true,
		}, nil
	}

	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx)
	}()

	for !w.loopRunning.Load() {
		time.Sleep(time.Millisecond)
	}

	resynced := make(chan error, 1)
	go func() { resynced <- w.Resync(ctx) }()

	<-entered

	mc.eventsCh <- events.Message{
		Type:   events.ServiceEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "s2"},
	}
	time.Sleep(20 * debounceWindow)

	close(released)

	if err := <-resynced; err != nil {
		t.Fatalf("Resync: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		if _, ok := store.GetService("s2"); !ok {
			break
		}

		select {
		case <-deadline:
			t.Fatal("the resync put back a service an event removed during its fetch")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	<-streamed
}

// The loop can stop between Refresh's check and its send, and during an outage
// it is gone for a whole reconnect interval. A request context has no deadline
// of its own, so an unbounded send would hold the write this refresh precedes
// until the client gave up.
func TestRefreshDoesNotWaitOutAnAbsentLoop(t *testing.T) {
	original := refreshHandoffTimeout
	refreshHandoffTimeout = 20 * time.Millisecond
	t.Cleanup(func() { refreshHandoffTimeout = original })

	w := &Watcher{refreshes: make(chan refreshRequest)}

	// What the window between the check and the send looks like: the flag is
	// set, and nothing is reading.
	w.loopRunning.Store(true)

	refreshed := make(chan error, 1)
	go func() {
		refreshed <- w.Refresh(context.Background(), string(events.ServiceEventType), "s1")
	}()

	select {
	case err := <-refreshed:
		if !cerrdefs.IsUnavailable(err) {
			t.Errorf("error = %v, want one the API answers with ENG001", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Refresh never returned, so the write it precedes hangs with it")
	}
}

// A hung engine fails a resync at the deadline instead of holding syncMu, and
// every sync queued behind it, until the process restarts.
func TestAResyncAgainstAHungEngineEnds(t *testing.T) {
	original := fullSyncTimeout
	fullSyncTimeout = 50 * time.Millisecond
	t.Cleanup(func() { fullSyncTimeout = original })

	mc := newMockClient()
	mc.syncGate = make(chan struct{}) // never opened: only the deadline ends it
	w := NewWatcher(mc, cache.New(nil), "")

	done := make(chan error, 1)
	go func() { done <- w.Resync(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a resync against a hung engine reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a resync against a hung engine never returned")
	}
}

// A loop too busy to take the resync held the caller as long as a hung sweep
// would, so it reads as the same timeout.
func TestAResyncTheLoopNeverTakesTimesOut(t *testing.T) {
	original := fullSyncTimeout
	fullSyncTimeout = 50 * time.Millisecond
	t.Cleanup(func() { fullSyncTimeout = original })

	w := NewWatcher(newMockClient(), cache.New(nil), "")
	w.loopRunning.Store(true) // and nothing reads resyncRequests

	if err := w.Resync(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
}
