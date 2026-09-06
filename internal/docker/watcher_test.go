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
	m.mu.Lock()
	defer m.mu.Unlock()
	var data cache.FullSyncData
	var failed int
	if m.listErrors["nodes"] == nil {
		data.Nodes, data.HasNodes = m.nodes, true
	} else {
		failed++
	}
	if m.listErrors["services"] == nil {
		data.Services, data.HasServices = m.services, true
	} else {
		failed++
	}
	if m.listErrors["tasks"] == nil {
		data.Tasks, data.HasTasks = m.tasks, true
	} else {
		failed++
	}
	if m.listErrors["configs"] == nil {
		data.Configs, data.HasConfigs = m.configs, true
	} else {
		failed++
	}
	if m.listErrors["secrets"] == nil {
		data.Secrets, data.HasSecrets = m.secrets, true
	} else {
		failed++
	}
	if m.listErrors["networks"] == nil {
		data.Networks, data.HasNetworks = m.networks, true
	} else {
		failed++
	}
	if m.listErrors["volumes"] == nil {
		data.Volumes, data.HasVolumes = m.volumes, true
	} else {
		failed++
	}
	if failed == 7 {
		return data, fmt.Errorf("all resource syncs failed")
	}
	return data, nil
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

func (m *mockClient) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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

	w.handleEvent(context.Background(), events.Message{
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
	w.handleEvent(context.Background(), events.Message{
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

	w.watchEvents(ctx)
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
		w.watchEvents(ctx)
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

// Swarm garbage-collects a task's record once it falls out of the history
// window, and emits no removal event when it does — the only trace is that the
// next inspect 404s. Holding the record on that failure left the cache serving
// a task Docker had forgotten, still carrying whatever status it was last
// inspected with: a service restarting in a loop accumulated thirty of them,
// most reported "running", until the five-minutely full re-sync swept them.
//
// A not-found that survives every retry is the daemon's answer, not a race, so
// it deletes. The retries themselves still have to run: during a stack deploy
// a 404 means "not registered yet", which is why this is decided after they
// are exhausted rather than on the first one.
func TestHandleEventDeletesTaskThatNoLongerExists(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
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
	w.handleEvent(context.Background(), events.Message{
		Type:  "task",
		Actor: events.Actor{ID: "t1"},
	})

	if _, ok := c.GetTask("t1"); ok {
		t.Error("task Docker no longer has is still in the cache")
	}
}

// A transient failure is not evidence of absence. Dropping the record on one
// would turn a daemon hiccup into a resource vanishing from every listing,
// which is the opposite failure and a worse one: the periodic re-sync repairs
// a stale record, but nothing repairs a caller that already acted on an empty
// result.
func TestHandleEventKeepsTaskWhenInspectFailsTransiently(t *testing.T) {
	mc := newMockClient()
	mc.inspectFn = func(context.Context, events.Type, string) (any, error) {
		return nil, errors.New("connection reset by peer")
	}

	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "t1", ServiceID: "svc"})

	w := NewWatcher(mc, c, "")
	w.handleEvent(context.Background(), events.Message{
		Type:  "task",
		Actor: events.Actor{ID: "t1"},
	})

	if _, ok := c.GetTask("t1"); !ok {
		t.Error("task dropped on a transient inspect failure")
	}
}

// A container dying is the last event a failed task ever produces, and Swarm
// reconciles the task record a moment after the container it wraps. Inspecting
// on the event itself therefore reads the task as still running, desired
// running — and because nothing further arrives, the cache keeps that reading
// until the five-minutely full re-sync. Every replica figure derived from it
// overcounts in the meantime: a service crash-looping every eight seconds
// reported four running replicas against a desired one, and the convergence
// wait behind every deploy could not settle.
//
// Re-inspecting once, shortly after, is what closes the gap: by then Swarm has
// caught up and the terminal state is there to read.
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

	w.handleEvent(context.Background(), events.Message{
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

// An ordinary container event is not a death and must not pay for a second
// inspect: a busy cluster produces these constantly, and doubling every one
// would double the load the watcher puts on the daemon.
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

	w.handleEvent(context.Background(), events.Message{
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
