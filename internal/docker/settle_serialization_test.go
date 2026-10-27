package docker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// startLoop runs the event loop until the test ends and waits for it to read.
func startLoop(t *testing.T, w *Watcher) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx, time.Time{}, true)
	}()
	t.Cleanup(func() {
		cancel()
		<-streamed
	})

	for !w.loopRunning.Load() {
		time.Sleep(time.Millisecond)
	}

	return ctx
}

// A settle's read is applied on the event loop, so a task the loop removed
// while the read was in flight is not put back by it.
func TestASettleCannotRestoreATaskRemovedDuringItsRead(t *testing.T) {
	mc := newMockClient()
	mc.services = []swarm.Service{{ID: "s1"}}
	mc.tasks = []swarm.Task{runningTask("t1", "s1", "n1", 1)}

	store := cache.New(nil)
	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour
	w.settleDelay = time.Millisecond

	if err := w.fullSync(context.Background()); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	entered := make(chan struct{})
	released := make(chan struct{})

	var once sync.Once
	mc.inspectFn = func(_ context.Context, kind events.Type, id string) (any, error) {
		if kind == events.ServiceEventType {
			return swarm.Service{ID: id}, nil
		}
		once.Do(func() { close(entered) })
		<-released

		task := runningTask("t1", "s1", "n1", 2)
		task.DesiredState = swarm.TaskStateShutdown
		task.Status.State = swarm.TaskStateShutdown

		return task, nil
	}

	ctx := startLoop(t, w)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(released) }) })

	w.scheduleSettle(ctx, eventKey{resourceType: "task", id: "t1"}, actionSettle)
	<-entered

	// The service's re-list no longer has the task, so the loop drops it.
	mc.setTasks(nil)
	mc.eventsCh <- events.Message{
		Type:   events.ServiceEventType,
		Action: "update",
		Actor:  events.Actor{ID: "s1"},
	}
	waitFor(t, "the re-list to drop the task", func() bool {
		_, ok := store.GetTask("t1")
		return !ok
	})

	release.Do(func() { close(released) })
	w.waitForSettles()

	// Give a queued apply time to reach the loop before looking.
	time.Sleep(20 * debounceWindow)

	if _, ok := store.GetTask("t1"); ok {
		t.Error("the settle's read put back a task removed while it was in flight")
	}
}

// Settles inspect off the loop, so a burst of container deaths neither holds
// a Refresh past its handoff nor loses settles to a loop that is merely busy.
func TestABurstOfSettlesStarvesNeitherRefreshNorItself(t *testing.T) {
	original := refreshHandoffTimeout
	refreshHandoffTimeout = 200 * time.Millisecond
	t.Cleanup(func() { refreshHandoffTimeout = original })

	const burst = 100

	mc := newMockClient()
	mc.services = []swarm.Service{{ID: "s1"}}
	for i := range burst {
		mc.tasks = append(mc.tasks, runningTask(fmt.Sprint("t", i), "s1", "n1", 1))
	}

	store := cache.New(nil)
	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour
	w.settleDelay = time.Millisecond

	if err := w.fullSync(context.Background()); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	mc.inspectFn = func(_ context.Context, kind events.Type, id string) (any, error) {
		time.Sleep(10 * time.Millisecond)
		if kind == events.ServiceEventType {
			return swarm.Service{ID: id}, nil
		}

		task := runningTask(id, "s1", "n1", 2)
		task.DesiredState = swarm.TaskStateShutdown
		task.Status.State = swarm.TaskStateShutdown

		return task, nil
	}

	ctx := startLoop(t, w)
	for i := range burst {
		w.scheduleSettle(ctx, eventKey{resourceType: "task", id: fmt.Sprint("t", i)}, actionSettle)
	}
	time.Sleep(5 * time.Millisecond)

	if err := w.Refresh(ctx, string(events.ServiceEventType), "s1"); err != nil {
		t.Errorf("Refresh during a burst of settles: %v", err)
	}

	w.waitForSettles()
	waitFor(t, "every settle to be applied", func() bool {
		for i := range burst {
			task, _ := store.GetTask(fmt.Sprint("t", i))
			if task.Status.State != swarm.TaskStateShutdown {
				return false
			}
		}

		return true
	})
}
