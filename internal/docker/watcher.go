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
	FullSync(ctx context.Context) cache.FullSyncData
	Inspect(ctx context.Context, resourceType events.Type, id string) (any, error)
	Events(ctx context.Context) (<-chan events.Message, <-chan error)
	Logs(ctx context.Context, kind LogKind, id string, tail string, follow bool, since, until string) (io.ReadCloser, error)
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

	// Disk snapshot.
	WriteToDisk(path string) error
}

type Watcher struct {
	client       DockerClient
	store        Store
	syncOnce     sync.Once
	ready        chan struct{}
	snapshotPath string
}

func NewWatcher(client DockerClient, store Store, snapshotPath string) *Watcher {
	return &Watcher{
		client:       client,
		store:        store,
		ready:        make(chan struct{}),
		snapshotPath: snapshotPath,
	}
}

// Ready returns a channel that is closed after the first full sync completes.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run starts the watcher. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	w.fullSync(ctx)
	w.writeSnapshot()
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
				w.writeSnapshot()
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
		w.writeSnapshot()
	}
}

func (w *Watcher) writeSnapshot() {
	if w.snapshotPath == "" {
		return
	}
	if err := w.store.WriteToDisk(w.snapshotPath); err != nil {
		slog.Warn("snapshot write failed", "error", err)
	}
}

func (w *Watcher) fullSync(ctx context.Context) {
	slog.Info("starting full sync")

	data := w.client.FullSync(ctx)
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
	resource, err := w.client.Inspect(ctx, key.resourceType, key.id)
	if err != nil {
		slog.Warn("inspect failed", "type", string(key.resourceType), "id", key.id, "error", err)
		return
	}
	switch v := resource.(type) {
	case swarm.Node:
		w.store.SetNode(v)
	case swarm.Service:
		w.store.SetService(v)
	case swarm.Task:
		w.store.SetTask(v)
	case swarm.Config:
		w.store.SetConfig(v)
	case swarm.Secret:
		w.store.SetSecret(v)
	case network.Summary:
		w.store.SetNetwork(v)
	case volume.Volume:
		w.store.SetVolume(v)
	}
}
