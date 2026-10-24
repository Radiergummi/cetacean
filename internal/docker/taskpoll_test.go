package docker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func runningTask(id, service, node string, version uint64) swarm.Task {
	return swarm.Task{
		ID:           id,
		Meta:         swarm.Meta{Version: swarm.Version{Index: version}},
		ServiceID:    service,
		NodeID:       node,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Swarm emits no task events and a remote node's container events never reach
// this engine, so a service or node event is when its tasks get re-read.
func TestAServiceOrNodeEventReListsItsTasks(t *testing.T) {
	cases := []struct {
		name  string
		event events.Message
	}{
		{"service", events.Message{
			Type: events.ServiceEventType, Action: "update", Actor: events.Actor{ID: "s1"},
		}},
		{"node", events.Message{
			Type: events.NodeEventType, Action: "update", Actor: events.Actor{ID: "n2"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := newMockClient()
			mc.tasks = []swarm.Task{
				runningTask("changed", "s1", "n2", 1),
				runningTask("gone", "s1", "n2", 1),
				runningTask("other", "s9", "n9", 1),
			}

			store := cache.New(nil)
			w := NewWatcher(mc, store, "")
			w.syncInterval = time.Hour

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := w.fullSync(ctx); err != nil {
				t.Fatal(err)
			}

			failed := runningTask("changed", "s1", "n2", 2)
			failed.Status.State = swarm.TaskStateFailed
			mc.setTasks([]swarm.Task{failed, runningTask("other", "s9", "n9", 1)})

			streamed := make(chan struct{})
			go func() {
				defer close(streamed)
				w.watchEvents(ctx, time.Time{})
			}()

			mc.eventsCh <- tc.event

			waitFor(t, "the re-list to apply", func() bool {
				task, _ := store.GetTask("changed")
				_, stillThere := store.GetTask("gone")
				return task.Status.State == swarm.TaskStateFailed && !stillThere
			})

			if _, ok := store.GetTask("other"); !ok {
				t.Error("a task outside the scope was dropped")
			}

			cancel()
			<-streamed
		})
	}
}

// The poll catches what no event announces, and announces only what changed.
func TestTheTaskPollAppliesOnlyRealChanges(t *testing.T) {
	mc := newMockClient()
	mc.tasks = []swarm.Task{
		runningTask("steady", "s1", "n1", 1),
		runningTask("moving", "s1", "n2", 1),
		runningTask("gone", "s1", "n2", 1),
	}

	var (
		mu      sync.Mutex
		changes []cache.Event
	)
	store := cache.New(func(e cache.Event) {
		if e.Type == cache.EventTask {
			mu.Lock()
			changes = append(changes, e)
			mu.Unlock()
		}
	})

	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour
	w.SetTaskPollInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.fullSync(ctx); err != nil {
		t.Fatal(err)
	}

	moved := runningTask("moving", "s1", "n2", 2)
	moved.Status.State = swarm.TaskStateShutdown
	mc.setTasks([]swarm.Task{runningTask("steady", "s1", "n1", 1), moved})

	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx, time.Time{})
	}()

	waitFor(t, "the poll to apply", func() bool {
		task, _ := store.GetTask("moving")
		_, stillThere := store.GetTask("gone")
		return task.Status.State == swarm.TaskStateShutdown && !stillThere
	})

	// Several more polls of an unchanged cluster.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-streamed

	mu.Lock()
	defer mu.Unlock()

	seen := map[string]int{}
	for _, e := range changes {
		seen[e.ID+"/"+e.Action]++
	}
	want := map[string]int{"moving/update": 1, "gone/remove": 1}
	if len(seen) != len(want) || seen["moving/update"] != 1 || seen["gone/remove"] != 1 {
		t.Errorf("task events = %v, want exactly %v", seen, want)
	}
}

// A sync re-reads every task anyway, so a poll that finds one running stands
// aside rather than queueing behind it.
func TestTheTaskPollStandsAsideForASync(t *testing.T) {
	mc := newMockClient()

	var polls atomic.Int64
	mc.listTasksFn = func(context.Context, TaskScope) ([]swarm.Task, error) {
		polls.Add(1)
		return nil, nil
	}

	w := NewWatcher(mc, cache.New(nil), "")
	w.syncInterval = time.Hour
	w.SetTaskPollInterval(5 * time.Millisecond)

	w.syncMu.Lock()

	ctx, cancel := context.WithCancel(context.Background())
	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx, time.Time{})
	}()

	time.Sleep(50 * time.Millisecond)
	if n := polls.Load(); n != 0 {
		t.Errorf("the poll listed tasks %d times while a sync held the lock", n)
	}

	w.syncMu.Unlock()
	waitFor(t, "a poll once the sync is done", func() bool { return polls.Load() > 0 })

	cancel()
	<-streamed
}

// A hung engine must not hold the event loop, and every event behind it.
func TestATaskPollAgainstAHungEngineEnds(t *testing.T) {
	original := taskListTimeout
	taskListTimeout = 20 * time.Millisecond
	t.Cleanup(func() { taskListTimeout = original })

	mc := newMockClient()
	mc.listTasksFn = func(ctx context.Context, _ TaskScope) ([]swarm.Task, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	store := cache.New(nil)
	store.SetService(swarm.Service{ID: "s1"})

	w := NewWatcher(mc, store, "")
	w.syncInterval = time.Hour
	w.SetTaskPollInterval(time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	streamed := make(chan struct{})
	go func() {
		defer close(streamed)
		w.watchEvents(ctx, time.Time{})
	}()

	mc.eventsCh <- events.Message{
		Type: events.ServiceEventType, Action: "remove", Actor: events.Actor{ID: "s1"},
	}

	waitFor(t, "the event behind a hung poll", func() bool {
		_, ok := store.GetService("s1")
		return !ok
	})

	cancel()
	<-streamed
}
