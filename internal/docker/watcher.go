package docker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/metrics"
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

	// GetTask lets the watcher tell an already-settled task from one whose
	// terminal state Swarm has not published yet — see scheduleSettle.
	GetTask(string) (swarm.Task, bool)
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

	// settleDelay is how long to wait before re-reading a task whose container
	// has just exited. See scheduleSettle.
	settleDelay time.Duration

	// settles tracks the re-reads still outstanding, so a test can wait for
	// them instead of sleeping and production can be sure they are not
	// silently dropped.
	settles sync.WaitGroup
}

func NewWatcher(client DockerClient, store Store, snapshotPath string) *Watcher {
	return &Watcher{
		client:       client,
		store:        store,
		ready:        make(chan struct{}),
		snapshotPath: snapshotPath,
		settleDelay:  defaultSettleDelay,
	}
}

// waitForSettles blocks until every scheduled re-read has finished.
func (w *Watcher) waitForSettles() {
	w.settles.Wait()
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
	start := time.Now()
	slog.Info("starting full sync")

	data, err := w.client.FullSync(ctx)
	if err != nil {
		slog.Error("full sync failed", "error", err)
		return err
	}

	w.store.ReplaceAll(data)
	metrics.ObserveSyncDuration(time.Since(start).Seconds())

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

// Resync triggers a full re-fetch of cluster state and overwrites the cache.
// Exposed for manual recovery from drift via the admin API; the watcher's
// regular event-stream path remains independent of this call.
func (w *Watcher) Resync(ctx context.Context) error {
	if err := w.fullSync(ctx); err != nil {
		return err
	}
	w.writeSnapshot()
	return nil
}

const (
	debounceWindow = 50 * time.Millisecond
	workerCount    = 4

	// defaultSettleDelay is how long Swarm is given to reconcile a task record
	// after the container behind it exits. Long enough that the second read
	// sees the terminal state, short enough that the count is only briefly
	// wrong — the alternative was waiting out the five-minute re-sync.
	defaultSettleDelay = 750 * time.Millisecond
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

// actionSettle marks a task update triggered by its container ending, which
// needs a second read once Swarm has caught up. It is internal to the watcher
// and never reaches applyRemove, which only ever tests for "remove".
const actionSettle = "settle"

// isContainerDeath reports whether a container event means the container has
// stopped for good. Docker emits "die" for every exit; "kill" and "stop" are
// the signals that precede one and are treated the same way, since the task
// behind them is on its way out either way.
func isContainerDeath(action events.Action) bool {
	switch action {
	case "die", "kill", "stop", "destroy", "oom":
		return true

	default:
		return false
	}
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
			if err := w.fullSync(ctx); err == nil {
				w.writeSnapshot()
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

		// Treat container events as task updates. A container ending is called
		// out separately: it is the last event the task will ever produce, and
		// Swarm has not yet reconciled the task record when it arrives — see
		// scheduleSettle.
		if isContainerDeath(msg.Action) {
			return eventKey{resourceType: "task", id: taskID}, actionSettle
		}

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

	// Scheduled after the batch rather than inside it: the inspect above may
	// already have read the settled record, and checking once here costs one
	// map lookup instead of a goroutine per event.
	for key, ev := range batch {
		if ev.action == actionSettle {
			w.scheduleSettle(ctx, key)
		}
	}
}

// scheduleSettle re-reads a task shortly after the container behind it exited.
//
// A container dying is the last event a failed task ever produces, and Swarm
// reconciles the task record a moment after the container it wraps. Inspecting
// on the event itself therefore reads the task as still running, desired
// running, and since nothing further arrives the cache keeps that reading
// until the five-minutely full re-sync corrects it. Everything derived from it
// overcounts in the meantime: a service crash-looping every eight seconds
// reported four running replicas against a desired one, `find` and the
// placement view repeated the figure, and the convergence wait behind every
// deploy could not settle because the count it waited on never fell.
//
// One delayed read closes it. It is skipped when the first inspect already
// saw a terminal state — a task that lost its container long enough ago, or a
// daemon that reconciled before we asked — so the common path pays nothing.
func (w *Watcher) scheduleSettle(ctx context.Context, key eventKey) {
	if task, ok := w.store.GetTask(key.id); ok && !taskSettled(task) {
		w.settles.Go(func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.settleDelay):
			}

			w.inspectAndApply(ctx, key)
		})
	}
}

// taskSettled reports whether Swarm has finished with a task, so there is
// nothing a second read could learn. DesiredState is the orchestrator's own
// answer and moves first; Status follows.
func taskSettled(task swarm.Task) bool {
	return !cache.TaskIsLive(task) || cache.IsTerminalState(task.Status.State)
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

		return
	}

	w.inspectAndApply(ctx, key)

	if action == actionSettle {
		w.scheduleSettle(ctx, key)
	}
}

func (w *Watcher) inspectAndApply(ctx context.Context, key eventKey) {
	resource, err := w.inspectWithRetry(ctx, key)
	if err != nil {
		// A not-found that outlived every retry is the daemon's answer rather
		// than a race, so the resource is gone and the cached record with it.
		// Swarm garbage-collects a task's record once it falls out of the
		// history window and emits no removal event when it does, so this is
		// the only signal that it went: holding the record left a task Docker
		// had forgotten in every listing, still carrying the status it was
		// last inspected with, until the five-minutely re-sync swept it. On a
		// service restarting in a loop that is dozens of phantom replicas.
		//
		// This is decided here rather than inside inspectWithRetry because the
		// retries are what tell the two cases apart: during a stack deploy a
		// 404 means "not registered yet", and giving up on the first one would
		// drop resources that were about to exist.
		if cerrdefs.IsNotFound(err) {
			slog.Debug(
				"resource no longer exists; dropping cached record",
				"type", string(key.resourceType),
				"id", key.id,
			)
			w.applyRemove(key)

			return
		}

		// Anything else is transient as far as we can tell, and absence of
		// evidence is not evidence of absence: the cache stays as-is and the
		// periodic re-sync reconciles it. Log loudly so the operator can
		// correlate cache drift with the underlying Docker error.
		slog.Warn(
			"inspect failed; cache may drift until next periodic re-sync",
			"type", string(key.resourceType),
			"id", key.id,
			"error", err,
		)

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

// inspectWithRetry retries transient inspect failures with capped exponential
// backoff. Rapid stack deploys often race: an event arrives for a resource the
// daemon hasn't fully registered yet (404), or a network glitch surfaces. The
// previous one-shot inspect would silently drop the event and leave the cache
// stale until the periodic 5-minute re-sync.
func (w *Watcher) inspectWithRetry(ctx context.Context, key eventKey) (any, error) {
	const maxAttempts = 4
	backoff := 100 * time.Millisecond

	var lastErr error
	for range maxAttempts {
		resource, err := w.client.Inspect(ctx, key.resourceType, key.id)
		if err == nil {
			return resource, nil
		}
		lastErr = err
		// Don't retry on definitive 404s if the event was a "remove" raced
		// with us — but during stack deploy a 404 means "not visible yet",
		// so we still retry. Cancel/deadline shortcuts here only when the
		// outer context is dead.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Don't waste retries on errors that won't change (auth, not implemented).
		if cerrdefs.IsUnauthorized(err) ||
			cerrdefs.IsPermissionDenied(err) ||
			cerrdefs.IsNotImplemented(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return nil, lastErr
}
