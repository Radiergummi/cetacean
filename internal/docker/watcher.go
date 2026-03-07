package docker

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"cetacean/internal/cache"
)

// DockerClient abstracts the Docker API methods used by the Watcher.
type DockerClient interface {
	ListNodes(ctx context.Context) ([]swarm.Node, error)
	ListServices(ctx context.Context) ([]swarm.Service, error)
	ListTasks(ctx context.Context) ([]swarm.Task, error)
	ListConfigs(ctx context.Context) ([]swarm.Config, error)
	ListSecrets(ctx context.Context) ([]swarm.Secret, error)
	ListNetworks(ctx context.Context) ([]network.Summary, error)
	ListVolumes(ctx context.Context) ([]volume.Volume, error)
	InspectNode(ctx context.Context, id string) (swarm.Node, error)
	InspectService(ctx context.Context, id string) (swarm.Service, error)
	InspectTask(ctx context.Context, id string) (swarm.Task, error)
	InspectConfig(ctx context.Context, id string) (swarm.Config, error)
	InspectSecret(ctx context.Context, id string) (swarm.Secret, error)
	InspectNetwork(ctx context.Context, id string) (network.Summary, error)
	InspectVolume(ctx context.Context, name string) (volume.Volume, error)
	Events(ctx context.Context) (<-chan events.Message, <-chan error)
	ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
	TaskLogs(ctx context.Context, taskID string, tail string, follow bool, since, until string) (io.ReadCloser, error)
	Close() error
}

// Store is the interface the watcher uses to mutate cached state.
type Store interface {
	// Incremental updates (from event stream).
	SetNode(swarm.Node)
	DeleteNode(string)
	SetService(swarm.Service)
	DeleteService(string)
	SetTask(swarm.Task)
	DeleteTask(string)
	SetConfig(swarm.Config)
	DeleteConfig(string)
	SetSecret(swarm.Secret)
	DeleteSecret(string)
	SetNetwork(network.Summary)
	DeleteNetwork(string)
	SetVolume(volume.Volume)
	DeleteVolume(string)

	// Atomic bulk replacement (from full sync).
	ReplaceAll(cache.FullSyncData)

	// Read snapshot for logging.
	Snapshot() cache.ClusterSnapshot
}

type Watcher struct {
	client   DockerClient
	store    Store
	syncOnce sync.Once
	ready    chan struct{}
}

func NewWatcher(client DockerClient, store Store) *Watcher {
	return &Watcher{
		client: client,
		store:  store,
		ready:  make(chan struct{}),
	}
}

// Ready returns a channel that is closed after the first full sync completes.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run starts the watcher. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	w.fullSync(ctx)
	w.syncOnce.Do(func() { close(w.ready) })

	// Periodic re-sync safety net
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slog.Info("periodic full re-sync")
				w.fullSync(ctx)
			}
		}
	}()

	// Event stream with reconnect
	for {
		if ctx.Err() != nil {
			return
		}
		w.watchEvents(ctx)
		slog.Warn("event stream disconnected, reconnecting in 1s")
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
		slog.Info("re-syncing after reconnect")
		w.fullSync(ctx)
	}
}

func (w *Watcher) fullSync(ctx context.Context) {
	slog.Info("starting full sync")

	type result struct {
		name string
		err  error
	}

	var data cache.FullSyncData
	var mu sync.Mutex
	ch := make(chan result, 7)

	go func() {
		nodes, err := w.client.ListNodes(ctx)
		if err == nil {
			mu.Lock()
			data.Nodes = nodes
			mu.Unlock()
		}
		ch <- result{"nodes", err}
	}()

	go func() {
		services, err := w.client.ListServices(ctx)
		if err == nil {
			mu.Lock()
			data.Services = services
			mu.Unlock()
		}
		ch <- result{"services", err}
	}()

	go func() {
		tasks, err := w.client.ListTasks(ctx)
		if err == nil {
			mu.Lock()
			data.Tasks = tasks
			mu.Unlock()
		}
		ch <- result{"tasks", err}
	}()

	go func() {
		configs, err := w.client.ListConfigs(ctx)
		if err == nil {
			mu.Lock()
			data.Configs = configs
			mu.Unlock()
		}
		ch <- result{"configs", err}
	}()

	go func() {
		secrets, err := w.client.ListSecrets(ctx)
		if err == nil {
			mu.Lock()
			data.Secrets = secrets
			mu.Unlock()
		}
		ch <- result{"secrets", err}
	}()

	go func() {
		networks, err := w.client.ListNetworks(ctx)
		if err == nil {
			mu.Lock()
			data.Networks = networks
			mu.Unlock()
		}
		ch <- result{"networks", err}
	}()

	go func() {
		volumes, err := w.client.ListVolumes(ctx)
		if err == nil {
			mu.Lock()
			data.Volumes = volumes
			mu.Unlock()
		}
		ch <- result{"volumes", err}
	}()

	for i := 0; i < 7; i++ {
		r := <-ch
		if r.err != nil {
			slog.Warn("full sync resource failed", "resource", r.name, "error", r.err)
		}
	}

	w.store.ReplaceAll(data)

	snap := w.store.Snapshot()
	slog.Info("full sync complete", "nodes", snap.NodeCount, "services", snap.ServiceCount, "tasks", snap.TaskCount, "stacks", snap.StackCount)
}

const (
	debounceWindow = 50 * time.Millisecond
	workerCount    = 4
)

// eventKey identifies a unique resource for coalescing.
type eventKey struct {
	resourceType events.Type
	id           string
}

// coalesced holds the latest action for a given resource.
type coalesced struct {
	action string
}

func (w *Watcher) watchEvents(ctx context.Context) {
	msgCh, errCh := w.client.Events(ctx)

	pending := make(map[eventKey]coalesced)
	timer := time.NewTimer(debounceWindow)
	timer.Stop()
	timerRunning := false

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errCh:
			if err != nil {
				slog.Warn("event stream error", "error", err)
			}
			// Flush any pending events before returning.
			if len(pending) > 0 {
				w.processBatch(ctx, pending)
			}
			return
		case msg := <-msgCh:
			key, action := w.eventKeyFromMsg(msg)
			if key.id == "" {
				continue // unrecognized event, skip
			}
			pending[key] = coalesced{action: action}
			if !timerRunning {
				timer.Reset(debounceWindow)
				timerRunning = true
			}
		case <-timer.C:
			timerRunning = false
			if len(pending) > 0 {
				batch := pending
				pending = make(map[eventKey]coalesced)
				w.processBatch(ctx, batch)
			}
		}
	}
}

// eventKeyFromMsg normalizes a Docker event into a coalescing key.
// Container events are mapped to task events using the swarm task ID attribute.
func (w *Watcher) eventKeyFromMsg(msg events.Message) (eventKey, string) {
	switch msg.Type {
	case events.ContainerEventType:
		taskID := msg.Actor.Attributes["com.docker.swarm.task.id"]
		if taskID == "" {
			return eventKey{}, ""
		}
		// Treat container events as task updates.
		return eventKey{resourceType: "task", id: taskID}, "update"
	case events.NetworkEventType:
		action := string(msg.Action)
		if action == "destroy" {
			action = "remove"
		}
		return eventKey{resourceType: msg.Type, id: msg.Actor.ID}, action
	case events.VolumeEventType:
		action := string(msg.Action)
		if action == "destroy" {
			action = "remove"
		}
		return eventKey{resourceType: msg.Type, id: msg.Actor.ID}, action
	default:
		return eventKey{resourceType: msg.Type, id: msg.Actor.ID}, string(msg.Action)
	}
}

// processBatch handles a coalesced batch of events with a worker pool.
func (w *Watcher) processBatch(ctx context.Context, batch map[eventKey]coalesced) {
	// Process removes synchronously first — they're cheap (no Inspect).
	for key, ev := range batch {
		if ev.action == "remove" {
			w.applyRemove(key)
			delete(batch, key)
		}
	}

	if len(batch) == 0 {
		return
	}

	// Fan out inspects across workers.
	work := make(chan eventKey, len(batch))
	for key := range batch {
		work <- key
	}
	close(work)

	var wg sync.WaitGroup
	workers := workerCount
	if len(batch) < workers {
		workers = len(batch)
	}
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for key := range work {
				w.inspectAndApply(ctx, key)
			}
		}()
	}
	wg.Wait()
}

func (w *Watcher) applyRemove(key eventKey) {
	switch key.resourceType {
	case events.NodeEventType:
		w.store.DeleteNode(key.id)
	case events.ServiceEventType:
		w.store.DeleteService(key.id)
	case events.ConfigEventType:
		w.store.DeleteConfig(key.id)
	case events.SecretEventType:
		w.store.DeleteSecret(key.id)
	case events.NetworkEventType:
		w.store.DeleteNetwork(key.id)
	case events.VolumeEventType:
		w.store.DeleteVolume(key.id)
	case "task":
		w.store.DeleteTask(key.id)
	}
}

// handleEvent processes a single Docker event synchronously (inspect + apply).
// Used by tests; the production path uses watchEvents with debouncing.
func (w *Watcher) handleEvent(ctx context.Context, msg events.Message) {
	key, action := w.eventKeyFromMsg(msg)
	if key.id == "" {
		return
	}
	if action == "remove" {
		w.applyRemove(key)
	} else {
		w.inspectAndApply(ctx, key)
	}
}

func (w *Watcher) inspectAndApply(ctx context.Context, key eventKey) {
	var err error
	switch key.resourceType {
	case events.NodeEventType:
		var node swarm.Node
		node, err = w.client.InspectNode(ctx, key.id)
		if err == nil {
			w.store.SetNode(node)
		}
	case events.ServiceEventType:
		var svc swarm.Service
		svc, err = w.client.InspectService(ctx, key.id)
		if err == nil {
			w.store.SetService(svc)
		}
	case events.ConfigEventType:
		var cfg swarm.Config
		cfg, err = w.client.InspectConfig(ctx, key.id)
		if err == nil {
			w.store.SetConfig(cfg)
		}
	case events.SecretEventType:
		var sec swarm.Secret
		sec, err = w.client.InspectSecret(ctx, key.id)
		if err == nil {
			w.store.SetSecret(sec)
		}
	case events.NetworkEventType:
		var net network.Summary
		net, err = w.client.InspectNetwork(ctx, key.id)
		if err == nil {
			w.store.SetNetwork(net)
		}
	case events.VolumeEventType:
		var vol volume.Volume
		vol, err = w.client.InspectVolume(ctx, key.id)
		if err == nil {
			w.store.SetVolume(vol)
		}
	case "task":
		var task swarm.Task
		task, err = w.client.InspectTask(ctx, key.id)
		if err == nil {
			w.store.SetTask(task)
		}
	}
	if err != nil {
		slog.Warn("inspect failed", "type", string(key.resourceType), "id", key.id, "error", err)
	}
}
