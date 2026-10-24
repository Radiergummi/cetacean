package docker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A settle re-read runs on the event loop too, for the reason a refresh does:
// an inspect applied beside the loop can undo a write the loop made meanwhile.
func TestASettleIsSerializedWithEventProcessing(t *testing.T) {
	mc := newMockClient()
	mc.services = []swarm.Service{{ID: "s2"}}
	mc.tasks = []swarm.Task{{
		ID:           "t1",
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	}}

	store := cache.New(nil)
	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour
	w.settleDelay = time.Millisecond

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

		return swarm.Task{
			ID:           "t1",
			DesiredState: swarm.TaskStateShutdown,
			Status:       swarm.TaskStatus{State: swarm.TaskStateShutdown},
		}, nil
	}

	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx, time.Time{})
	}()

	for !w.loopRunning.Load() {
		time.Sleep(time.Millisecond)
	}

	w.scheduleSettle(ctx, eventKey{resourceType: "task", id: "t1"}, actionSettle)

	<-entered

	mc.eventsCh <- events.Message{
		Type:   events.ServiceEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "s2"},
	}

	time.Sleep(20 * debounceWindow)

	if _, ok := store.GetService("s2"); !ok {
		t.Error("an event was applied while a settle was in flight, so the two are not serialized")
	}

	close(released)
	w.waitForSettles()

	cancel()
	<-streamed
}
