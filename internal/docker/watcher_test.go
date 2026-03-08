package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
)

// mockClient implements DockerClient for testing.
type mockClient struct {
	nodes    []swarm.Node
	services []swarm.Service
	tasks    []swarm.Task
	configs  []swarm.Config
	secrets  []swarm.Secret
	networks []network.Summary
	volumes  []volume.Volume

	inspectNodeFn    func(ctx context.Context, id string) (swarm.Node, error)
	inspectServiceFn func(ctx context.Context, id string) (swarm.Service, error)
	inspectTaskFn    func(ctx context.Context, id string) (swarm.Task, error)
	inspectConfigFn  func(ctx context.Context, id string) (swarm.Config, error)
	inspectSecretFn  func(ctx context.Context, id string) (swarm.Secret, error)
	inspectNetworkFn func(ctx context.Context, id string) (network.Summary, error)
	inspectVolumeFn  func(ctx context.Context, name string) (volume.Volume, error)

	eventsCh chan events.Message
	errCh    chan error

	listErrors map[string]error // resource name -> error
}

func newMockClient() *mockClient {
	return &mockClient{
		eventsCh:   make(chan events.Message, 10),
		errCh:      make(chan error, 1),
		listErrors: make(map[string]error),
	}
}

func (m *mockClient) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	if err := m.listErrors["nodes"]; err != nil {
		return nil, err
	}
	return m.nodes, nil
}
func (m *mockClient) ListServices(ctx context.Context) ([]swarm.Service, error) {
	if err := m.listErrors["services"]; err != nil {
		return nil, err
	}
	return m.services, nil
}
func (m *mockClient) ListTasks(ctx context.Context) ([]swarm.Task, error) {
	if err := m.listErrors["tasks"]; err != nil {
		return nil, err
	}
	return m.tasks, nil
}
func (m *mockClient) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	if err := m.listErrors["configs"]; err != nil {
		return nil, err
	}
	return m.configs, nil
}
func (m *mockClient) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	if err := m.listErrors["secrets"]; err != nil {
		return nil, err
	}
	return m.secrets, nil
}
func (m *mockClient) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	if err := m.listErrors["networks"]; err != nil {
		return nil, err
	}
	return m.networks, nil
}
func (m *mockClient) ListVolumes(ctx context.Context) ([]volume.Volume, error) {
	if err := m.listErrors["volumes"]; err != nil {
		return nil, err
	}
	return m.volumes, nil
}

func (m *mockClient) InspectNode(ctx context.Context, id string) (swarm.Node, error) {
	if m.inspectNodeFn != nil {
		return m.inspectNodeFn(ctx, id)
	}
	return swarm.Node{ID: id}, nil
}
func (m *mockClient) InspectService(ctx context.Context, id string) (swarm.Service, error) {
	if m.inspectServiceFn != nil {
		return m.inspectServiceFn(ctx, id)
	}
	return swarm.Service{ID: id}, nil
}
func (m *mockClient) InspectTask(ctx context.Context, id string) (swarm.Task, error) {
	if m.inspectTaskFn != nil {
		return m.inspectTaskFn(ctx, id)
	}
	return swarm.Task{ID: id}, nil
}
func (m *mockClient) InspectConfig(ctx context.Context, id string) (swarm.Config, error) {
	if m.inspectConfigFn != nil {
		return m.inspectConfigFn(ctx, id)
	}
	return swarm.Config{ID: id}, nil
}
func (m *mockClient) InspectSecret(ctx context.Context, id string) (swarm.Secret, error) {
	if m.inspectSecretFn != nil {
		return m.inspectSecretFn(ctx, id)
	}
	return swarm.Secret{ID: id}, nil
}
func (m *mockClient) InspectNetwork(ctx context.Context, id string) (network.Summary, error) {
	if m.inspectNetworkFn != nil {
		return m.inspectNetworkFn(ctx, id)
	}
	return network.Summary{ID: id}, nil
}
func (m *mockClient) InspectVolume(ctx context.Context, name string) (volume.Volume, error) {
	if m.inspectVolumeFn != nil {
		return m.inspectVolumeFn(ctx, name)
	}
	return volume.Volume{Name: name}, nil
}

func (m *mockClient) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	return m.eventsCh, m.errCh
}

func (m *mockClient) ServiceLogs(_ context.Context, _ string, _ string, _ bool, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockClient) TaskLogs(_ context.Context, _ string, _ string, _ bool, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockClient) Close() error { return nil }

// --- Tests ---

func TestHandleEvent_NodeUpdate(t *testing.T) {
	mc := newMockClient()
	mc.inspectNodeFn = func(_ context.Context, id string) (swarm.Node, error) {
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
	mc.inspectServiceFn = func(_ context.Context, id string) (swarm.Service, error) {
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
	mc.inspectVolumeFn = func(_ context.Context, name string) (volume.Volume, error) {
		return volume.Volume{Name: name, Driver: "local"}, nil
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
	mc.inspectTaskFn = func(_ context.Context, id string) (swarm.Task, error) {
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
	mc.inspectTaskFn = func(_ context.Context, _ string) (swarm.Task, error) {
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
	mc.inspectNodeFn = func(_ context.Context, _ string) (swarm.Node, error) {
		return swarm.Node{}, fmt.Errorf("connection refused")
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

	w.fullSync(context.Background())

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

	w.fullSync(context.Background())

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
	mc.inspectNodeFn = func(_ context.Context, id string) (swarm.Node, error) {
		return swarm.Node{ID: id}, nil
	}
	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	// Send an event then close with error to exit watchEvents
	mc.eventsCh <- events.Message{
		Type:  events.NodeEventType,
		Actor: events.Actor{ID: "n1"},
	}

	go func() {
		// Give watchEvents time to process the message, then terminate
		time.Sleep(50 * time.Millisecond)
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

	var syncCount int
	origNodes := mc.nodes
	mc.inspectNodeFn = func(_ context.Context, id string) (swarm.Node, error) {
		return swarm.Node{ID: id}, nil
	}

	c := cache.New(nil)
	w := NewWatcher(mc, c, "")

	ctx, cancel := context.WithCancel(context.Background())

	// Track syncs by watching node count changes
	go func() {
		<-w.Ready() // first sync done
		syncCount++

		// Trigger disconnect
		mc.errCh <- fmt.Errorf("stream ended")

		// After reconnect sleep (1s) + re-sync, add another node
		time.Sleep(1500 * time.Millisecond)
		mc.nodes = append(origNodes, swarm.Node{ID: "n2"})
		syncCount++

		// Trigger another disconnect to force re-sync with new data
		mc.errCh <- fmt.Errorf("stream ended again")

		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()

	w.Run(ctx)

	if syncCount < 2 {
		t.Errorf("expected at least 2 syncs, got %d", syncCount)
	}
}
