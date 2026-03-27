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

	"github.com/radiergummi/cetacean/internal/cache"
)

// DockerClient abstracts the Docker API methods used by the Watcher.
type DockerClient interface {
	FullSync(ctx context.Context) (cache.FullSyncData, error)
	Inspect(ctx context.Context, resourceType events.Type, id string) (any, error)
	Events(ctx context.Context) (<-chan events.Message, <-chan error)
	Logs(
		ctx context.Context,
		kind LogKind,
		id string,
		tail string,
		follow bool,
		since, until string,
	) (io.ReadCloser, error)
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
	if err := w.fullSync(ctx); err == nil {
		w.writeSnapshot()
		w.syncOnce.Do(func() { close(w.ready) })
	}

	// Event stream with reconnect and exponential backoff.
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		w.watchEvents(ctx)
		slog.Warn("event stream disconnected", "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		slog.Info("re-syncing after reconnect")
		if err := w.fullSync(ctx); err == nil {
			w.writeSnapshot()
			w.syncOnce.Do(func() { close(w.ready) })
			backoff = 1 * time.Second // Reset on success.
		} else {
			backoff = min(backoff*2, maxBackoff)
		}
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

func (w *Watcher) fullSync(ctx context.Context) error {
	slog.Info("starting full sync")

	data, err := w.client.FullSync(ctx)
	if err != nil {
		slog.Error("full sync failed", "error", err)
		return err
	}

	w.store.ReplaceAll(data)

	snap := w.store.Snapshot()
	slog.Info(
		"full sync complete",
		"nodes",
		snap.NodeCount,
		"services",
		snap.ServiceCount,
		"tasks",
		snap.TaskCount,
		"stacks",
		snap.StackCount,
	)

	return nil
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
	var timer *time.Timer
	var timerC <-chan time.Time // nil until first event arms it

	// Periodic re-sync runs inside the select loop so it is serialized
	// with event processing — this prevents a concurrent ReplaceAll from
	// re-inserting resources that were just deleted by an incremental event.
	syncTicker := time.NewTicker(5 * time.Minute)
	defer syncTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if len(pending) > 0 {
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				w.processBatch(flushCtx, pending)
				cancel()
			}
			return
		case err := <-errCh:
			if err != nil {
				slog.Warn("event stream error", "error", err)
			}
			if timer != nil {
				timer.Stop()
			}
			// Flush pending events with a fresh context — the parent ctx
			// may already be cancelled if shutdown raced with the stream error.
			if len(pending) > 0 {
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				w.processBatch(flushCtx, pending)
				cancel()
			}
			return
		case msg := <-msgCh:
			key, action := w.eventKeyFromMsg(msg)
			if key.id == "" {
				continue // unrecognized event, skip
			}
			pending[key] = coalesced{action: action}
			if timerC == nil {
				if timer == nil {
					timer = time.NewTimer(debounceWindow)
				} else {
					timer.Reset(debounceWindow)
				}
				timerC = timer.C
			}
		case <-timerC:
			timerC = nil
			if len(pending) > 0 {
				batch := pending
				pending = make(map[eventKey]coalesced)
				w.processBatch(ctx, batch)
			}
		case <-syncTicker.C:
			// Flush pending events before the full sync so we don't lose them.
			if timer != nil {
				timer.Stop()
				timerC = nil
			}
			if len(pending) > 0 {
				batch := pending
				pending = make(map[eventKey]coalesced)
				w.processBatch(ctx, batch)
			}
			slog.Info("periodic full re-sync")
			_ = w.fullSync(ctx)
			w.writeSnapshot()
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
	var removeKeys []eventKey
	for key, ev := range batch {
		if ev.action == "remove" {
			removeKeys = append(removeKeys, key)
		}
	}
	for _, key := range removeKeys {
		w.applyRemove(key)
		delete(batch, key)
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
	workers := min(len(batch), workerCount)
	for range workers {
		wg.Go(func() {
			for key := range work {
				w.inspectAndApply(ctx, key)
			}
		})
	}
	wg.Wait()
}

func (w *Watcher) applyRemove(key eventKey) {
	switch key.resourceType { //nolint:exhaustive // only swarm resource types are relevant
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
