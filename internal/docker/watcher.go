package docker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
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

	// Whether the watcher is still tracking the cluster. See Liveness.
	connected atomic.Bool
	lastSync  atomic.Int64 // UnixNano; zero until the first sync
}

// setConnected records reachability for both /-/health and /-/metrics.
func (w *Watcher) setConnected(connected bool) {
	w.connected.Store(connected)
	metrics.SetWatcherConnected(connected)
}

// Liveness reports whether the event stream is established, and when a full
// sync last reached the engine (zero before the first). Safe from any
// goroutine.
func (w *Watcher) Liveness() (connected bool, lastSync time.Time) {
	if nanos := w.lastSync.Load(); nanos != 0 {
		lastSync = time.Unix(0, nanos)
	}

	return w.connected.Load(), lastSync
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

// fullSync re-reads the cluster on the watcher's own schedule — at startup, on
// reconnect, and on the five-minutely tick inside watchEvents — so its outcome
// speaks for whether the engine is reachable at all.
func (w *Watcher) fullSync(ctx context.Context) error {
	return w.sync(ctx, true)
}

// sync re-fetches the cluster and replaces the cache with the result.
//
// tracksConnection says whether this sync speaks for the event stream's
// health. A watcher-driven sync does; a manual resync does not — it runs
// beside a healthy stream, and letting a transient failure there report the
// engine as unreachable left /-/health and the dashboard claiming "Cetacean
// cannot reach Docker" until the next periodic sync, five minutes later.
func (w *Watcher) sync(ctx context.Context, tracksConnection bool) error {
	start := time.Now()
	slog.Info("starting full sync")

	data, err := w.client.FullSync(ctx)
	if err != nil {
		slog.Error("full sync failed", "error", err)
		metrics.RecordSyncFailure()

		if tracksConnection {
			w.setConnected(false)
		}

		return err
	}

	done := time.Now()

	w.store.ReplaceAll(data)
	metrics.ObserveSyncDuration(done.Sub(start).Seconds())
	metrics.RecordSyncSuccess(done)
	w.lastSync.Store(done.UnixNano())

	if tracksConnection {
		w.setConnected(true)
	}

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
	if err := w.sync(ctx, false); err != nil {
		return err
	}
	w.writeSnapshot()
	return nil
}

const (
	debounceWindow = 50 * time.Millisecond
	workerCount    = 4

	// defaultSettleDelay is how long Swarm is given to reconcile a task record
	// after its container starts or exits.
	defaultSettleDelay = 750 * time.Millisecond

	// settleAttempts bounds how many times a task is re-read while its record
	// still lags, each wait twice the last. Three covers about five seconds.
	settleAttempts = 3
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

// actionSettle marks a task update triggered by its container ending,
// actionStarted one by its container starting. Both need a second read once
// Swarm has caught up, in opposite directions -- see taskCaughtUp.
const (
	actionSettle  = "settle"
	actionStarted = "started"
)

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

	// Only the disconnection: Events returns its channels before the request
	// behind them is made, so fullSync's success is what proves reachability.
	defer w.setConnected(false)

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

		// Treat container events as task updates. The two that bracket a
		// container's life are called out separately, because Swarm has not
		// reconciled the task record when either arrives — see scheduleSettle.
		if isContainerDeath(msg.Action) {
			return eventKey{resourceType: "task", id: taskID}, actionSettle
		}

		if msg.Action == "start" {
			return eventKey{resourceType: "task", id: taskID}, actionStarted
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
		if ev.action == actionSettle || ev.action == actionStarted {
			w.scheduleSettle(ctx, key, ev.action)
		}
	}
}

// scheduleSettle re-reads a task shortly after its container started or
// exited. Docker emits no task events, and Swarm reconciles the task record
// after the container it wraps -- so the inspect on the event reads a record
// that has not caught up, in either direction: a dead container still reads
// running, a started one still reads starting. Each read waits twice as long
// as the last, and the series stops as soon as the record catches up.
func (w *Watcher) scheduleSettle(ctx context.Context, key eventKey, action string) {
	if task, ok := w.store.GetTask(key.id); !ok || taskCaughtUp(task, action) {
		return
	}

	w.settles.Go(func() {
		delay := w.settleDelay

		for attempt := range settleAttempts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			w.inspectAndApply(ctx, key)

			task, ok := w.store.GetTask(key.id)
			if !ok || taskCaughtUp(task, action) {
				return
			}

			slog.Debug(
				"task still unsettled after re-read",
				"id", key.id,
				"attempt", attempt+1,
				"of", settleAttempts,
			)

			delay *= 2
		}
	})
}

// taskCaughtUp reports whether Swarm's record has caught up with the container
// event that prompted the re-read. DesiredState moves first, Status follows.
// The two directions rest at opposite states, which is why one predicate
// cannot serve both.
func taskCaughtUp(task swarm.Task, action string) bool {
	if !cache.TaskIsLive(task) || cache.IsTerminalState(task.Status.State) {
		return true
	}

	return action == actionStarted && task.Status.State == swarm.TaskStateRunning
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

	if action == actionSettle || action == actionStarted {
		w.scheduleSettle(ctx, key, action)
	}
}

func (w *Watcher) inspectAndApply(ctx context.Context, key eventKey) {
	resource, err := w.inspectWithRetry(ctx, key)
	if err != nil {
		// A not-found that outlived every retry is the daemon's answer, not a
		// race: Swarm garbage-collects a task's record without emitting a
		// removal event, so this is the only signal it went. Decided here
		// rather than in inspectWithRetry because the retries are what tell
		// that from "not registered yet" during a stack deploy.
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

	w.apply(resource)
}

// apply writes an inspected resource into the store under its own type.
func (w *Watcher) apply(resource any) {
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

// Refresh re-reads one resource from the engine into the cache, for a caller
// that cannot wait for the event stream. An inspect failure is returned rather
// than logged: the caller asked because it cannot tolerate a stale answer.
func (w *Watcher) Refresh(ctx context.Context, kind, id string) error {
	resource, err := w.client.Inspect(ctx, events.Type(kind), id)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			w.applyRemove(eventKey{resourceType: events.Type(kind), id: id})

			return nil
		}

		return err
	}

	w.apply(resource)

	return nil
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
