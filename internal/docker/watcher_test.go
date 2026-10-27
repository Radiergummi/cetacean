package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// mockClient implements DockerClient for testing.
type mockClient struct {
	mu       sync.Mutex
	nodes    []swarm.Node
	services []swarm.Service
	tasks    []swarm.Task
	configs  []swarm.Config
	secrets  []swarm.Secret
	networks []network.Summary
	volumes  []volume.Volume

	inspectFn func(ctx context.Context, resourceType events.Type, id string) (any, error)

	eventsCh chan events.Message
	errCh    chan error

	listErrors map[string]error // resource name -> error

	fullSyncs atomic.Int64

	// syncGate, when set, holds every FullSync until it is closed.
	syncGate chan struct{}

	// listTasksFn, when set, replaces ListTasksIn's default behaviour.
	listTasksFn func(ctx context.Context, scope TaskScope) ([]swarm.Task, error)

	// eventsSince records the since of every Events call.
	eventsSince []time.Time

	// fullSyncFn, when set, replaces FullSync's default behaviour. It
	// receives the 1-based call number, so a test can tell calls apart.
	fullSyncFn func(call int64) (cache.FullSyncData, error)
}

func newMockClient() *mockClient {
	return &mockClient{
		eventsCh:   make(chan events.Message, 10),
		errCh:      make(chan error, 2),
		listErrors: make(map[string]error),
	}
}

func (m *mockClient) setNodes(nodes []swarm.Node) {
	m.mu.Lock()
	m.nodes = nodes
	m.mu.Unlock()
}

func (m *mockClient) FullSync(ctx context.Context) (cache.FullSyncData, error) {
	call := m.fullSyncs.Add(1)
	if m.syncGate != nil {
		select {
		case <-m.syncGate:
		case <-ctx.Done():
			return cache.FullSyncData{}, ctx.Err()
		}
	}
	if m.fullSyncFn != nil {
		return m.fullSyncFn(call)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var data cache.FullSyncData
	failed := make(map[string]error)
	for name, err := range m.listErrors {
		if err != nil {
			failed[name] = err
		}
	}
	if failed["nodes"] == nil {
		data.Nodes, data.HasNodes = m.nodes, true
	}
	if failed["services"] == nil {
		data.Services, data.HasServices = m.services, true
	}
	if failed["tasks"] == nil {
		data.Tasks, data.HasTasks = m.tasks, true
	}
	if failed["configs"] == nil {
		data.Configs, data.HasConfigs = m.configs, true
	}
	if failed["secrets"] == nil {
		data.Secrets, data.HasSecrets = m.secrets, true
	}
	if failed["networks"] == nil {
		data.Networks, data.HasNetworks = m.networks, true
	}
	if failed["volumes"] == nil {
		data.Volumes, data.HasVolumes = m.volumes, true
	}
	return data, fullSyncError(failed)
}

func (m *mockClient) ListTasksIn(ctx context.Context, scope TaskScope) ([]swarm.Task, error) {
	if m.listTasksFn != nil {
		return m.listTasksFn(ctx, scope)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []swarm.Task
	for _, task := range m.tasks {
		if scope.Service != "" && task.ServiceID != scope.Service {
			continue
		}
		if scope.Node != "" && task.NodeID != scope.Node {
			continue
		}
		out = append(out, task)
	}

	return out, nil
}

func (m *mockClient) setTasks(tasks []swarm.Task) {
	m.mu.Lock()
	m.tasks = tasks
	m.mu.Unlock()
}

func (m *mockClient) Inspect(
	ctx context.Context,
	resourceType events.Type,
	id string,
) (any, error) {
	if m.inspectFn != nil {
		return m.inspectFn(ctx, resourceType, id)
	}
	switch resourceType {
	case events.NodeEventType:
		return swarm.Node{ID: id}, nil
	case events.ServiceEventType:
		return swarm.Service{ID: id}, nil
	case events.ConfigEventType:
		return swarm.Config{ID: id}, nil
	case events.SecretEventType:
		return swarm.Secret{ID: id}, nil
	case events.NetworkEventType:
		return network.Summary{ID: id}, nil
	case events.VolumeEventType:
		return volume.Volume{Name: id}, nil
	case "task":
		return swarm.Task{ID: id}, nil
	default:
		return nil, fmt.Errorf("unknown type: %s", resourceType)
	}
}

func (m *mockClient) Events(
	_ context.Context,
	since time.Time,
) (<-chan events.Message, <-chan error) {
	m.mu.Lock()
	m.eventsSince = append(m.eventsSince, since)
	m.mu.Unlock()

	return m.eventsCh, m.errCh
}

func (m *mockClient) Logs(
	_ context.Context,
	_ LogKind,
	_ string,
	_ string,
	_ bool,
	_, _ string,
) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockClient) Close() error { return nil }

// deliver applies one event the way the event loop does: as a batch of one.
func deliver(ctx context.Context, w *Watcher, msg events.Message) {
	key, action := w.eventKeyFromMsg(msg)
	if key.id == "" {
		return
	}

	w.processBatch(ctx, map[eventKey]coalesced{key: {action: action}})
}

// --- Tests ---

func TestHandleEvent_NodeUpdate(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		n := swarm.Node{ID: id}
		n.Description.Hostname = "test-host"
		return n, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.NodeEventType,
		Actor: events.Actor{ID: "n1"},
	})

	node, ok := c.GetNode("n1")
	if !ok {
		t.Fatal("expected node n1 in cache")
	}
	if node.Description.Hostname != "test-host" {
		t.Errorf("expected hostname test-host, got %s", node.Description.Hostname)
	}
}

func TestHandleEvent_NodeRemove(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.NodeEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "n1"},
	})

	if _, ok := c.GetNode("n1"); ok {
		t.Fatal("expected node n1 to be removed from cache")
	}
}

func TestHandleEvent_ServiceUpdate(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		svc := swarm.Service{ID: id}
		svc.Spec.Name = "web"
		return svc, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.ServiceEventType,
		Actor: events.Actor{ID: "s1"},
	})

	svc, ok := c.GetService("s1")
	if !ok {
		t.Fatal("expected service s1 in cache")
	}
	if svc.Spec.Name != "web" {
		t.Errorf("expected name web, got %s", svc.Spec.Name)
	}
}

func TestHandleEvent_ServiceRemove(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.ServiceEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "s1"},
	})

	if _, ok := c.GetService("s1"); ok {
		t.Fatal("expected service s1 to be removed")
	}
}

func TestHandleEvent_ConfigUpdate(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.ConfigEventType,
		Actor: events.Actor{ID: "cfg1"},
	})

	if _, ok := c.GetConfig("cfg1"); !ok {
		t.Fatal("expected config cfg1 in cache")
	}
}

func TestHandleEvent_ConfigRemove(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "cfg1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.ConfigEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "cfg1"},
	})

	if _, ok := c.GetConfig("cfg1"); ok {
		t.Fatal("expected config cfg1 to be removed")
	}
}

func TestHandleEvent_SecretUpdate(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.SecretEventType,
		Actor: events.Actor{ID: "sec1"},
	})

	if _, ok := c.GetSecret("sec1"); !ok {
		t.Fatal("expected secret sec1 in cache")
	}
}

func TestHandleEvent_SecretRemove(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{ID: "sec1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.SecretEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "sec1"},
	})

	if _, ok := c.GetSecret("sec1"); ok {
		t.Fatal("expected secret sec1 to be removed")
	}
}

func TestHandleEvent_NetworkUpdate(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.NetworkEventType,
		Actor: events.Actor{ID: "net1"},
	})

	if _, ok := c.GetNetwork("net1"); !ok {
		t.Fatal("expected network net1 in cache")
	}
}

func TestHandleEvent_NetworkRemove(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.NetworkEventType,
		Action: "remove",
		Actor:  events.Actor{ID: "net1"},
	})

	if _, ok := c.GetNetwork("net1"); ok {
		t.Fatal("expected network net1 to be removed")
	}
}

func TestHandleEvent_NetworkDestroy(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.NetworkEventType,
		Action: "destroy",
		Actor:  events.Actor{ID: "net1"},
	})

	if _, ok := c.GetNetwork("net1"); ok {
		t.Fatal("expected network net1 to be removed on destroy")
	}
}

func TestHandleEvent_VolumeUpdate(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		return volume.Volume{Name: id, Driver: "local"}, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.VolumeEventType,
		Actor: events.Actor{ID: "vol1"},
	})

	vol, ok := c.GetVolume("vol1")
	if !ok {
		t.Fatal("expected volume vol1 in cache")
	}
	if vol.Driver != "local" {
		t.Errorf("expected driver local, got %s", vol.Driver)
	}
}

func TestHandleEvent_VolumeDestroy(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "vol1"})
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:   events.VolumeEventType,
		Action: "destroy",
		Actor:  events.Actor{ID: "vol1"},
	})

	if _, ok := c.GetVolume("vol1"); ok {
		t.Fatal("expected volume vol1 to be removed")
	}
}

func TestHandleEvent_ContainerToTask(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		return swarm.Task{ID: id, ServiceID: "svc1"}, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type: events.ContainerEventType,
		Actor: events.Actor{
			ID: "container1",
			Attributes: map[string]string{
				"com.docker.swarm.task.id":      "task1",
				"com.docker.swarm.service.name": "web",
			},
		},
	})

	task, ok := c.GetTask("task1")
	if !ok {
		t.Fatal("expected task task1 in cache")
	}
	if task.ServiceID != "svc1" {
		t.Errorf("expected serviceID svc1, got %s", task.ServiceID)
	}
}

func TestHandleEvent_ContainerWithoutTaskID(t *testing.T) {
	mc := newMockClient()
	called := false
	mc.inspectFn = func(_ context.Context, _ events.Type, _ string) (any, error) {
		called = true
		return swarm.Task{}, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	deliver(context.Background(), w, events.Message{
		Type:  events.ContainerEventType,
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{}},
	})

	if called {
		t.Fatal("InspectTask should not be called for non-swarm containers")
	}
}

func TestHandleEvent_InspectError(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, _ string) (any, error) {
		return nil, fmt.Errorf("connection refused")
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	// Should not panic, should not add to cache
	deliver(context.Background(), w, events.Message{
		Type:  events.NodeEventType,
		Actor: events.Actor{ID: "n1"},
	})

	if _, ok := c.GetNode("n1"); ok {
		t.Fatal("node should not be in cache after inspect error")
	}
}

func TestFullSync(t *testing.T) {
	mc := newMockClient()
	mc.nodes = []swarm.Node{{ID: "n1"}, {ID: "n2"}}
	mc.services = []swarm.Service{{ID: "s1"}}
	mc.tasks = []swarm.Task{{ID: "t1", ServiceID: "s1"}}
	mc.configs = []swarm.Config{{ID: "c1"}}
	mc.secrets = []swarm.Secret{{ID: "sec1"}}
	mc.networks = []network.Summary{{ID: "net1"}}
	mc.volumes = []volume.Volume{{Name: "vol1"}}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	_ = w.fullSync(context.Background())

	snap := c.Snapshot()
	if snap.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", snap.NodeCount)
	}
	if snap.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", snap.ServiceCount)
	}
	if snap.TaskCount != 1 {
		t.Errorf("expected 1 task, got %d", snap.TaskCount)
	}
}

func TestFullSync_PartialFailure(t *testing.T) {
	mc := newMockClient()
	mc.nodes = []swarm.Node{{ID: "n1"}}
	mc.services = []swarm.Service{{ID: "s1"}}
	mc.listErrors["tasks"] = fmt.Errorf("timeout")
	mc.listErrors["configs"] = fmt.Errorf("timeout")

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	_ = w.fullSync(context.Background())

	snap := c.Snapshot()
	if snap.NodeCount != 1 {
		t.Errorf("expected 1 node, got %d", snap.NodeCount)
	}
	if snap.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", snap.ServiceCount)
	}
	// Tasks and configs should remain empty (not updated on error)
	if snap.TaskCount != 0 {
		t.Errorf("expected 0 tasks, got %d", snap.TaskCount)
	}
}

func TestWatchEvents_ProcessesMessages(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	// Send an event then close with error to exit watchEvents
	mc.eventsCh <- events.Message{
		Type:  events.NodeEventType,
		Actor: events.Actor{ID: "n1"},
	}

	go func() {
		// Wait until the node appears in cache before terminating
		deadline := time.After(2 * time.Second)
		for found := false; !found; {
			if _, ok := c.GetNode("n1"); ok {
				found = true
			} else {
				select {
				case <-deadline:
					found = true
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
		mc.errCh <- fmt.Errorf("stream ended")
	}()

	w.watchEvents(ctx, time.Time{}, true)
	cancel()

	if _, ok := c.GetNode("n1"); !ok {
		t.Fatal("expected node n1 in cache after processing event")
	}
}

func TestWatchEvents_StopsOnContextCancel(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.watchEvents(ctx, time.Time{}, true)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("watchEvents did not return after context cancel")
	}
}

func TestRun_SignalsReady(t *testing.T) {
	mc := newMockClient()
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// Wait for ready, then cancel
		<-w.Ready()
		cancel()
	}()

	// Send an error immediately so watchEvents returns, then Run exits on cancelled ctx
	mc.errCh <- fmt.Errorf("done")

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Run did not exit after context cancel")
	}
}

// A manager started before its swarm elects a leader refuses every swarm list
// while its event stream stays up, so no reconnect follows. The sync is retried
// on a short backoff, so the instance is ready within seconds of a leader
// existing rather than at the next periodic sync.
func TestRun_RetriesAFailedStartupSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := newMockClient()
		for _, name := range swarmResources {
			mc.listErrors[name] = errors.New("the swarm does not have a leader")
		}

		w := NewWatcher(mc, cache.New(nil), "")

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			w.Run(ctx)
		}()

		// Long enough for several retries, each failing.
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if got := mc.fullSyncs.Load(); got < 3 {
			t.Errorf("FullSync ran %d times in five seconds after failing, want retries", got)
		}
		select {
		case <-w.Ready():
			t.Fatal("ready before any swarm resource could be listed")
		default:
		}

		mc.mu.Lock()
		clear(mc.listErrors)
		mc.mu.Unlock()

		time.Sleep(30 * time.Second)
		synctest.Wait()
		select {
		case <-w.Ready():
		default:
			t.Fatal("a failed startup sync was not retried within the backoff")
		}
		if connected, _ := w.Liveness(); !connected {
			t.Error("ready while the watcher reports the cluster as untracked")
		}

		// Retries stop once one succeeds.
		calls := mc.fullSyncs.Load()
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := mc.fullSyncs.Load(); got != calls {
			t.Errorf("FullSync ran %d more times after a success", got-calls)
		}

		cancel()
		<-done
	})
}

// A manual resync is not the watcher's own: it may run while the stream is
// down, so it neither reports the stream connected nor the instance ready.
func TestManualResyncLeavesReadinessToTheWatcher(t *testing.T) {
	mc := newMockClient()
	for _, name := range swarmResources {
		mc.listErrors[name] = errors.New("the swarm does not have a leader")
	}

	w := NewWatcher(mc, cache.New(nil), "")
	if err := w.fullSync(t.Context()); err == nil {
		t.Fatal("fullSync succeeded without any swarm resource")
	}

	clear(mc.listErrors)
	if err := w.Resync(t.Context()); err != nil {
		t.Fatalf("Resync: %v", err)
	}

	select {
	case <-w.Ready():
		connected, _ := w.Liveness()
		t.Fatalf("a manual resync signalled ready (connected=%v)", connected)
	default:
	}
}

func TestRun_ReconnectsAfterEventStreamError(t *testing.T) {
	mc := newMockClient()
	mc.nodes = []swarm.Node{{ID: "n1"}}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-w.Ready() // first sync done

		// Update nodes and trigger disconnect; the mutex in setNodes
		// synchronizes with FullSync's read during reconnect.
		mc.setNodes([]swarm.Node{{ID: "n1"}, {ID: "n2"}})
		mc.errCh <- fmt.Errorf("stream ended")

		// Poll until reconnect sync picks up the new node
		deadline := time.After(5 * time.Second)
		for len(c.ListNodes()) != 2 {
			select {
			case <-deadline:
				cancel()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}

		// Trigger another disconnect to verify repeated reconnect
		mc.setNodes([]swarm.Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}})
		mc.errCh <- fmt.Errorf("stream ended again")

		// Poll until second reconnect sync picks up the third node
		deadline = time.After(5 * time.Second)
		for len(c.ListNodes()) != 3 {
			select {
			case <-deadline:
				cancel()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
	}()

	w.Run(ctx)

	nodes := c.ListNodes()
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes after two reconnect syncs, got %d", len(nodes))
	}
}

// Swarm garbage-collects a task's record once it leaves the history window and
// emits no removal event, so the only trace is that the next inspect 404s. A
// task outlives its container, so a 404 for one is final, not worth a retry
// that would hold the event loop.
func TestHandleEventDeletesTaskThatNoLongerExists(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
		calls.Add(1)
		return nil, cerrdefs.ErrNotFound
	}

	c := cache.New(nil)
	c.SetTask(swarm.Task{
		ID:           "t1",
		ServiceID:    "svc",
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	w := NewWatcher(mc, c, "")
	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "destroy",
		Actor: events.Actor{
			ID:         "container1",
			Attributes: map[string]string{"com.docker.swarm.task.id": "t1"},
		},
	})
	w.waitForSettles()

	if _, ok := c.GetTask("t1"); ok {
		t.Error("task Docker no longer has is still in the cache")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("inspected a missing task %d times, want 1", got)
	}
}

// During a stack deploy a create event can outrun the object it announces, so
// a 404 on a create is retried rather than taken as the answer.
func TestCreateEventRetriesAnObjectNotVisibleYet(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		if calls.Add(1) == 1 {
			return nil, cerrdefs.ErrNotFound
		}
		return swarm.Service{ID: id}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	deliver(context.Background(), w, events.Message{
		Type:   events.ServiceEventType,
		Action: "create",
		Actor:  events.Actor{ID: "svc1"},
	})

	if _, ok := c.GetService("svc1"); !ok {
		t.Error("a service created a moment before its inspect was dropped")
	}
}

// Past its create, a 404 means the object is gone: its remove event is on the
// way, and retrying would only hold the event loop until it arrives.
func TestUpdateEventTakesNotFoundAsFinal(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
		calls.Add(1)
		return nil, cerrdefs.ErrNotFound
	}

	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1"})
	w := NewWatcher(mc, c, "")
	deliver(context.Background(), w, events.Message{
		Type:   events.ServiceEventType,
		Action: "update",
		Actor:  events.Actor{ID: "svc1"},
	})

	if _, ok := c.GetService("svc1"); ok {
		t.Error("a service Docker no longer has is still in the cache")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("inspected a missing service %d times, want 1", got)
	}
}

// An update coalesced onto a create in the same window must keep the create's
// retry, or the object the deploy just made is dropped.
func TestCoalescedCreateKeepsItsRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32

		mc := newMockClient()
		mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
			if calls.Add(1) == 1 {
				return nil, cerrdefs.ErrNotFound
			}
			return swarm.Service{ID: id}, nil
		}

		c := cache.New(nil)
		w := NewWatcher(mc, c, "")
		for _, action := range []events.Action{"create", "update"} {
			mc.eventsCh <- events.Message{
				Type:   events.ServiceEventType,
				Action: action,
				Actor:  events.Actor{ID: "svc1"},
			}
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			w.watchEvents(ctx, time.Time{}, true)
		}()

		time.Sleep(time.Second)
		synctest.Wait()
		cancel()
		<-done

		if _, ok := c.GetService("svc1"); !ok {
			t.Error("a create coalesced with an update lost its retry")
		}
	})
}

// A transient failure is not evidence of absence. Dropping the record on one
// would make a daemon hiccup vanish a resource from every listing; the
// periodic re-sync repairs a stale record, but nothing repairs a caller that
// already acted on an empty result.
func TestHandleEventKeepsTaskWhenInspectFailsTransiently(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
		return nil, errors.New("connection reset by peer")
	}

	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc"})

	w := NewWatcher(mc, c, "")
	deliver(context.Background(), w, events.Message{
		Type:  "task",
		Actor: events.Actor{ID: "t1"},
	})

	if _, ok := c.GetTask("t1"); !ok {
		t.Error("task dropped on a transient inspect failure")
	}
}

// A container dying is the last event a failed task ever produces, and Swarm
// reconciles the task record after the container it wraps -- so the inspect on
// the event reads the task as still running, and nothing further arrives to
// correct it. Re-inspecting shortly after is what closes the gap.
func TestContainerDeathReinspectsUntilSwarmCatchesUp(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		// First read sees Swarm's pre-reconciliation view, as production does.
		if calls.Add(1) == 1 {
			return swarm.Task{
				ID: id, ServiceID: "svc",
				DesiredState: swarm.TaskStateRunning,
				Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
			}, nil
		}

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateShutdown,
			Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = 10 * time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "die",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	task, ok := c.GetTask("t1")
	if !ok {
		t.Fatal("task missing from cache")
	}

	if task.DesiredState != swarm.TaskStateShutdown {
		t.Errorf("DesiredState = %q, want shutdown — the re-inspect did not land",
			task.DesiredState)
	}

	if got := c.RunningTaskCount("svc"); got != 0 {
		t.Errorf("RunningTaskCount = %d, want 0 for a task Swarm has shut down", got)
	}
}

// A container event whose task record has already caught up must not pay for a
// second inspect: a busy cluster produces these constantly. The re-read is
// bought only where it is needed.
func TestNonTerminalContainerEventInspectsOnce(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		calls.Add(1)

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateRunning,
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = 10 * time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	if got := calls.Load(); got != 1 {
		t.Errorf("inspected %d times for a container start; want 1", got)
	}
}

// The same race at the other end of a container's life: Swarm commits a task's
// running status after the start event that announced it, so the inspect reads
// it as still starting and nothing further arrives. This direction settles at
// running, the state the dying direction treats as not settled at all.
func TestContainerStartReinspectsUntilSwarmCatchesUp(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		// The first read sees Swarm's pre-reconciliation view, as production
		// does: the container is up, the task record has not caught up.
		if calls.Add(1) == 1 {
			return swarm.Task{
				ID: id, ServiceID: "svc",
				DesiredState: swarm.TaskStateRunning,
				Status:       swarm.TaskStatus{State: swarm.TaskStateStarting},
			}, nil
		}

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateRunning,
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = 10 * time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	task, ok := c.GetTask("t1")
	if !ok {
		t.Fatal("task missing from cache")
	}

	if task.Status.State != swarm.TaskStateRunning {
		t.Errorf(
			"Status.State = %q, want running — the re-inspect did not land",
			task.Status.State,
		)
	}

	// The count is the thing that actually hung: a service cannot converge
	// while the cache reports fewer replicas running than the engine holds.
	if got := c.RunningTaskCount("svc"); got != 1 {
		t.Errorf("RunningTaskCount = %d, want 1 for a task Swarm has started", got)
	}
}

// The re-reads stop as soon as the record reads running, rather than running
// out the bound: this direction is scheduled by every container start on the
// cluster, so a series that always ran to completion would be four inspects
// per started container instead of one.
func TestContainerStartStopsReinspectingOnceRunning(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		state := swarm.TaskStateRunning
		if calls.Add(1) == 1 {
			state = swarm.TaskStateStarting
		}

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateRunning,
			Status:       swarm.TaskStatus{State: state},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	// The event's own inspect, plus exactly one re-read.
	if got := calls.Load(); got != 2 {
		t.Errorf("inspects = %d, want 2 — the series did not stop at running", got)
	}
}

// One re-read is not enough when the daemon is slow to reconcile, and nothing
// else re-arms it. A following container event usually schedules another
// attempt in practice, but the fix must not rest on "usually".
func TestContainerDeathKeepsReinspectingWhileSwarmLags(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		// Two reads still see the pre-reconciliation view; only the third
		// catches up.
		if calls.Add(1) < 3 {
			return swarm.Task{
				ID: id, ServiceID: "svc",
				DesiredState: swarm.TaskStateRunning,
				Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
			}, nil
		}

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateShutdown,
			Status:       swarm.TaskStatus{State: swarm.TaskStateFailed},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = 5 * time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "die",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	if got := c.RunningTaskCount("svc"); got != 0 {
		t.Errorf(
			"RunningTaskCount = %d, want 0 — the retry after the first "+
				"unsettled re-read did not happen",
			got,
		)
	}
}

// The retries are bounded. A task that never settles must not keep a goroutine
// re-reading it for the life of the process.
func TestSettleRetriesAreBounded(t *testing.T) {
	var calls atomic.Int32

	mc := newMockClient()
	mc.inspectFn = func(_ context.Context, _ events.Type, id string) (any, error) {
		calls.Add(1)

		return swarm.Task{
			ID: id, ServiceID: "svc",
			DesiredState: swarm.TaskStateRunning,
			Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")
	w.settleDelay = time.Millisecond

	deliver(context.Background(), w, events.Message{
		Type:   events.ContainerEventType,
		Action: "die",
		Actor: events.Actor{ID: "container1", Attributes: map[string]string{
			"com.docker.swarm.task.id": "t1",
		}},
	})

	w.waitForSettles()

	// The event's own inspect, plus the bounded series of re-reads.
	if want := int32(1 + settleAttempts); calls.Load() != want {
		t.Errorf("inspects = %d, want %d", calls.Load(), want)
	}
}

// ─── liveness ───────────────────────────────────────────────────────────

// TestLivenessStartsDisconnected: a watcher that has never synced must not
// look healthy.
func TestLivenessStartsDisconnected(t *testing.T) {
	w := NewWatcher(newMockClient(), cache.New(nil), "")

	connected, lastSync := w.Liveness()
	if connected {
		t.Error("a watcher that has never synced reports connected")
	}

	if !lastSync.IsZero() {
		t.Errorf("lastSync = %v before any sync, want zero", lastSync)
	}
}

// TestLivenessFollowsTheEngine is the point of the signal: with the engine
// gone, every endpoint keeps answering and readiness keeps passing.
func TestLivenessFollowsTheEngine(t *testing.T) {
	mc := newMockClient()
	mc.nodes = []swarm.Node{{ID: "n1"}}

	w := NewWatcher(mc, cache.New(nil), "")

	before := time.Now()

	if err := w.fullSync(context.Background()); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	connected, lastSync := w.Liveness()
	if !connected {
		t.Error("a watcher that just synced reports disconnected")
	}

	if lastSync.Before(before) {
		t.Errorf("lastSync = %v, want at or after %v", lastSync, before)
	}

	// The engine goes away.
	mc.listErrors["nodes"] = errors.New("connection refused")
	mc.listErrors["services"] = errors.New("connection refused")
	mc.listErrors["tasks"] = errors.New("connection refused")
	mc.listErrors["configs"] = errors.New("connection refused")
	mc.listErrors["secrets"] = errors.New("connection refused")
	mc.listErrors["networks"] = errors.New("connection refused")
	mc.listErrors["volumes"] = errors.New("connection refused")

	if err := w.fullSync(context.Background()); err == nil {
		t.Fatal("fullSync succeeded with every list failing")
	}

	connected, stale := w.Liveness()
	if connected {
		t.Error("a watcher whose sync just failed still reports connected")
	}

	// The timestamp is when the cache was last correct, so a failure must not
	// advance it.
	if !stale.Equal(lastSync) {
		t.Errorf("a failed sync moved lastSync from %v to %v", lastSync, stale)
	}
}

// TestLivenessDropsWhenTheStreamEnds covers the other disconnection: the sync
// succeeded, then the stream went away.
func TestLivenessDropsWhenTheStreamEnds(t *testing.T) {
	mc := newMockClient()

	w := NewWatcher(mc, cache.New(nil), "")

	if err := w.fullSync(context.Background()); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	if connected, _ := w.Liveness(); !connected {
		t.Fatal("not connected after a successful sync")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.watchEvents(ctx, time.Time{}, true)

	if connected, _ := w.Liveness(); connected {
		t.Error("still connected after the event stream ended")
	}
}

// POST /-/resync shares the sync path with the watcher, but runs beside a
// healthy event stream. Letting its failure clear the connection verdict has
// /-/health and the dashboard report "Cetacean cannot reach Docker" until the
// next periodic sync, over a cluster nothing is wrong with.
func TestManualResyncFailureLeavesTheStreamVerdictAlone(t *testing.T) {
	mc := newMockClient()

	w := NewWatcher(mc, cache.New(nil), "")

	if err := w.fullSync(context.Background()); err != nil {
		t.Fatalf("fullSync: %v", err)
	}

	connected, lastSync := w.Liveness()
	if !connected {
		t.Fatal("not connected after a successful sync")
	}

	for _, kind := range []string{
		"nodes", "services", "tasks", "configs", "secrets", "networks", "volumes",
	} {
		mc.listErrors[kind] = errors.New("connection refused")
	}

	if err := w.Resync(context.Background()); err == nil {
		t.Fatal("Resync succeeded with a list failing")
	}

	connected, stale := w.Liveness()
	if !connected {
		t.Error("a failed manual resync reported the event stream as disconnected")
	}

	if !stale.Equal(lastSync) {
		t.Errorf("a failed resync moved lastSync from %v to %v", lastSync, stale)
	}
}

// Each manual resync sweeps the whole Docker API, and two racing ones let the
// older fetch overwrite the newer, so concurrent calls share one sync.
func TestResyncCoalescesConcurrentCalls(t *testing.T) {
	mc := newMockClient()
	mc.syncGate = make(chan struct{})
	w := NewWatcher(mc, cache.New(nil), "")

	var callers sync.WaitGroup
	for range 5 {
		callers.Go(func() {
			if err := w.Resync(context.Background()); err != nil {
				t.Errorf("Resync: %v", err)
			}
		})
	}

	deadline := time.Now().Add(2 * time.Second)
	for mc.fullSyncs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let the other callers join the flight
	close(mc.syncGate)
	callers.Wait()

	if got := mc.fullSyncs.Load(); got != 1 {
		t.Errorf("FullSync ran %d times, want 1", got)
	}
}

// A caller that gives up must not stay parked on the shared flight, which runs
// detached from every caller and may outlast them all.
func TestResyncReturnsWhenCallerCancels(t *testing.T) {
	mc := newMockClient()
	mc.syncGate = make(chan struct{})
	defer close(mc.syncGate)
	w := NewWatcher(mc, cache.New(nil), "")

	ctx, cancel := context.WithCancel(context.Background())
	resynced := make(chan error, 1)
	go func() { resynced <- w.Resync(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for mc.fullSyncs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-resynced:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Resync = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Resync still blocked after its caller's context was cancelled")
	}
}

// syncMu must serialize the periodic sync and a manual Resync that overlaps
// it, so a slower, older fetch cannot land after — and overwrite — a faster
// one triggered later.
func TestOlderSyncCannotOverwriteANewerOne(t *testing.T) {
	mc := newMockClient()
	store := cache.New(nil)
	w := NewWatcher(mc, store, "")

	entered := make(chan struct{})
	release := make(chan struct{})

	mc.fullSyncFn = func(call int64) (cache.FullSyncData, error) {
		if call == 1 {
			close(entered)
			<-release

			return cache.FullSyncData{Nodes: []swarm.Node{{ID: "stale"}}, HasNodes: true}, nil
		}

		return cache.FullSyncData{Nodes: []swarm.Node{{ID: "fresh"}}, HasNodes: true}, nil
	}

	periodic := make(chan error, 1)
	go func() { periodic <- w.fullSync(context.Background()) }()
	<-entered

	manual := make(chan error, 1)
	go func() { manual <- w.Resync(context.Background()) }()

	// Give the manual resync a chance to run unsynchronized, were syncMu not
	// there: its fetch isn't gated, so it would finish well within this.
	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-periodic; err != nil {
		t.Fatalf("periodic sync: %v", err)
	}
	if err := <-manual; err != nil {
		t.Fatalf("manual resync: %v", err)
	}

	if _, ok := store.GetNode("fresh"); !ok {
		t.Error("the later, faster sync's data was overwritten by the earlier, slower one")
	}
}

// The periodic tick runs on the event loop's own goroutine; waiting behind a
// manual Resync's syncMu would stall event processing for as long as that
// resync takes. It must skip itself instead.
func TestPeriodicSyncSkipsWhenAResyncIsRunning(t *testing.T) {
	mc := newMockClient()
	mc.syncGate = make(chan struct{})
	w := NewWatcher(mc, cache.New(nil), "")
	w.syncInterval = 5 * time.Millisecond

	resynced := make(chan error, 1)
	go func() { resynced <- w.Resync(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for mc.fullSyncs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.watchEvents(ctx, time.Time{}, true)
	}()

	// Several ticks' worth of time while the resync still holds syncMu.
	time.Sleep(50 * time.Millisecond)

	if got := mc.fullSyncs.Load(); got != 1 {
		t.Errorf("FullSync called %d times while a resync was in flight, want 1", got)
	}

	close(mc.syncGate)

	if err := <-resynced; err != nil {
		t.Fatalf("Resync: %v", err)
	}

	cancel()
	<-done
}

// hangingInspect answers only once its context ends, as a half-open TCP
// connection to the engine or a hung volume plugin does.
func hangingInspect(ctx context.Context, _ events.Type, _ string) (any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// An inspect on the event loop holds every event behind it, so one that never
// answers must give up on its own rather than wedge the loop for good.
func TestEventInspectGivesUpOnAHungEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := newMockClient()
		mc.inspectFn = hangingInspect

		c := cache.New(nil)
		c.SetNode(swarm.Node{ID: "n1"})
		w := NewWatcher(mc, c, "")

		deliver(t.Context(), w, events.Message{
			Type:  events.NodeEventType,
			Actor: events.Actor{ID: "n1"},
		})

		if _, ok := c.GetNode("n1"); !ok {
			t.Error("a timed-out inspect dropped the cached node")
		}
	})
}

// Refresh carries a request context with no deadline of its own, and runs on
// the event loop, so its inspect is bounded the same way, and answers as the
// engine being unreachable does.
func TestRefreshGivesUpOnAHungEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := newMockClient()
		mc.inspectFn = hangingInspect
		w := NewWatcher(mc, cache.New(nil), "")

		err := w.Refresh(t.Context(), string(events.ServiceEventType), "svc1")
		if !cerrdefs.IsUnavailable(err) {
			t.Errorf("Refresh = %v, want unavailable", err)
		}
	})
}
