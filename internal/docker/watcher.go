package docker

import (
	"context"
	"errors"
	"fmt"
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
	"golang.org/x/sync/singleflight"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/metrics"
)

// DockerClient abstracts the Docker API methods used by the Watcher.
type DockerClient interface {
	FullSync(ctx context.Context) (cache.FullSyncData, error)
	Inspect(ctx context.Context, resourceType events.Type, id string) (any, error)

	// ListTasksIn lists one service's or one node's tasks, or every task for
	// a zero scope.
	ListTasksIn(ctx context.Context, scope TaskScope) ([]swarm.Task, error)
	// Events streams from since onwards, replaying what the daemon still holds
	// from before the call; a zero since streams only what follows it.
	Events(ctx context.Context, since time.Time) (<-chan events.Message, <-chan error)
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

// TaskScope narrows a task list to one service or one node; zero means all.
type TaskScope struct {
	Service string
	Node    string
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

	// The cached side of a task re-list, which drops what the list lacks.
	ListTasks() []swarm.Task
	ListTasksByService(string) []swarm.Task
	ListTasksByNode(string) []swarm.Task
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
	client   DockerClient
	store    Store
	syncOnce sync.Once

	// syncMu orders full syncs, so a slower fetch cannot replace the cache
	// after a newer one has; resyncs shares one manual resync among callers.
	syncMu  sync.Mutex
	resyncs singleflight.Group

	ready        chan struct{}
	snapshotPath string

	// settleDelay is how long to wait before re-reading a task whose container
	// has just exited. See scheduleSettle.
	settleDelay time.Duration

	// syncInterval is how often the event loop re-reads the whole cluster.
	syncInterval time.Duration

	// taskPollInterval is how often the event loop re-lists every task; zero
	// disables it. See SetTaskPollInterval.
	taskPollInterval time.Duration

	// settles tracks the re-reads still outstanding, so a test can wait for
	// them instead of sleeping and production can be sure they are not
	// silently dropped.
	settles sync.WaitGroup

	// refreshes carries a one-resource re-read to the event loop, which applies
	// every other write. See Refresh. resyncRequests does the same for Resync.
	refreshes      chan refreshRequest
	resyncRequests chan chan error

	// settled holds settle reads awaiting the loop, one per task; settledReady
	// wakes the loop to apply them. See settleRead.
	settledMu    sync.Mutex
	settled      map[string]settledRead
	settledReady chan struct{}

	// loopRunning reports whether the event loop is reading refreshes.
	loopRunning atomic.Bool

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
		syncInterval: defaultSyncInterval,
		refreshes:    make(chan refreshRequest),
		settled:      make(map[string]settledRead),
		settledReady: make(chan struct{}, 1),

		resyncRequests: make(chan chan error),
	}
}

// SetTaskPollInterval sets how often the event loop re-lists every task, zero
// for never. Container events reach the watcher only from the engine it is
// connected to, so a task elsewhere changes without one. Call before Run.
func (w *Watcher) SetTaskPollInterval(interval time.Duration) {
	w.taskPollInterval = interval
}

// waitForSettles blocks until every scheduled re-read has finished.
func (w *Watcher) waitForSettles() {
	w.settles.Wait()
}

// Ready returns a channel that is closed after the watcher's first successful
// full sync, whichever of startup, reconnect or periodic reaches it first; a
// manual resync does not count.
func (w *Watcher) Ready() <-chan struct{} {
	return w.ready
}

// Run starts the watcher. It blocks until the context is cancelled, and then
// writes a last snapshot, unless no sync ever succeeded: that cache holds
// nothing newer than the snapshot it was loaded from.
func (w *Watcher) Run(ctx context.Context) {
	defer func() {
		if w.lastSync.Load() != 0 {
			w.writeSnapshot()
		}
	}()

	since := eventsSince()
	synced := w.fullSync(ctx) == nil
	if synced {
		w.writeSnapshot()
	}

	// Event stream with reconnect and exponential backoff.
	backoff := 1 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		w.watchEvents(ctx, since, synced)
		slog.Warn("event stream disconnected", "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		slog.Info("re-syncing after reconnect")
		since = eventsSince()
		synced = w.fullSync(ctx) == nil
		if synced {
			w.writeSnapshot()
			backoff = 1 * time.Second // Reset on success.
		} else {
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// eventsSince is where a stream subscribed after a sync starts: before the sync
// listed anything, so a change made during the listing is replayed rather than
// lost. Replays are harmless, since every event re-reads its resource. The
// margin absorbs the daemon's clock running behind ours.
func eventsSince() time.Time {
	return time.Now().Add(-eventReplayMargin)
}

func (w *Watcher) writeSnapshot() {
	if w.snapshotPath == "" {
		return
	}
	if err := w.store.WriteToDisk(w.snapshotPath); err != nil {
		slog.Warn("snapshot write failed", "error", err)
	}
}

// fullSync re-reads the cluster where no stream is yet proven — at startup and
// on reconnect — so its outcome speaks for whether the engine is reachable at
// all. The periodic tick inside watchEvents speaks for it too on success, via
// markTracking; only a failed periodic sync is left out.
func (w *Watcher) fullSync(ctx context.Context) error {
	if err := w.sync(ctx); err != nil {
		w.setConnected(false)
		return err
	}

	w.markTracking()
	return nil
}

// markTracking records a successful watcher-driven sync. Connected is stored
// before ready closes, so a reader woken by Ready sees it.
func (w *Watcher) markTracking() {
	w.setConnected(true)
	w.syncOnce.Do(func() { close(w.ready) })
}

// sync re-fetches the cluster and replaces the cache with the result. It says
// nothing about the event stream: only the watcher's own syncs do, by caller.
func (w *Watcher) sync(ctx context.Context) error {
	w.syncMu.Lock()
	defer w.syncMu.Unlock()

	return w.syncLocked(ctx)
}

// syncLocked is sync's body, for a caller that already holds syncMu — the
// periodic tick, so it can try the lock instead of blocking behind it.
func (w *Watcher) syncLocked(ctx context.Context) error {
	start := time.Now()
	slog.Info("starting full sync")

	// Bounded: a hung engine would otherwise hold syncMu, and on the event
	// loop every event behind it, for good.
	ctx, cancel := context.WithTimeout(ctx, fullSyncTimeout)
	defer cancel()

	data, err := w.client.FullSync(ctx)
	if err != nil {
		slog.Error("full sync failed", "error", err)
		metrics.RecordSyncFailure()

		return err
	}

	done := time.Now()

	w.store.ReplaceAll(data)
	metrics.ObserveSyncDuration(done.Sub(start).Seconds())
	metrics.RecordSyncSuccess(done)
	w.lastSync.Store(done.UnixNano())

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
// Exposed for manual recovery from drift via the admin API. It runs on the
// event loop when there is one, for the reason Refresh does: a ReplaceAll from
// beside it can put back a resource an event has just removed.
func (w *Watcher) Resync(ctx context.Context) error {
	// Detached: callers who joined this flight must not fail with the first,
	// but each caller still stops waiting when its own context ends.
	flight := w.resyncs.DoChan("resync", func() (any, error) {
		return nil, w.resyncOnLoop(context.WithoutCancel(ctx))
	})

	select {
	case result := <-flight:
		return result.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// resyncOnLoop hands a manual resync to the event loop, or runs it here when no
// loop is running and nothing applies events beside it.
func (w *Watcher) resyncOnLoop(ctx context.Context) error {
	if !w.loopRunning.Load() {
		if err := w.sync(ctx); err != nil {
			return err
		}
		w.writeSnapshot()

		return nil
	}

	done := make(chan error, 1)

	// The loop may be inside a periodic sync, which is bounded the same way.
	handoff := time.NewTimer(fullSyncTimeout)
	defer handoff.Stop()

	select {
	case w.resyncRequests <- done:
	case <-handoff.C:
		return fmt.Errorf(
			"the event loop did not take the re-sync in time: %w",
			context.DeadlineExceeded,
		)
	}

	return <-done
}

const (
	debounceWindow = 50 * time.Millisecond
	workerCount    = 4

	// defaultSettleDelay is how long Swarm is given to reconcile a task record
	// after its container starts or exits.
	defaultSettleDelay = 750 * time.Millisecond

	// defaultSyncInterval is how often the event loop re-reads the whole
	// cluster, catching anything the stream never announced.
	defaultSyncInterval = 5 * time.Minute

	// settleAttempts bounds how many times a task is re-read while its record
	// still lags, each wait twice the last. Three covers about five seconds.
	settleAttempts = 3

	// eventReplayMargin is how far before a sync its event stream starts.
	eventReplayMargin = 5 * time.Second

	// maxBackoff caps the wait between reconnects, and between sync retries.
	maxBackoff = 30 * time.Second
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

func (w *Watcher) watchEvents(ctx context.Context, since time.Time, synced bool) {
	msgCh, errCh := w.client.Events(ctx, since)

	w.loopRunning.Store(true)
	defer w.loopRunning.Store(false)

	// Only the disconnection: Events returns its channels before the request
	// behind them is made, so a successful sync — fullSync's or the periodic
	// tick's — is what proves reachability, not this call returning.
	defer w.setConnected(false)

	pending := make(map[eventKey]coalesced)
	var timer *time.Timer
	var timerC <-chan time.Time // nil until first event arms it

	// Periodic re-sync runs inside the select loop so it is serialized
	// with event processing — this prevents a concurrent ReplaceAll from
	// re-inserting resources that were just deleted by an incremental event.
	syncTicker := time.NewTicker(w.syncInterval)
	defer syncTicker.Stop()

	// The task poll runs here for the same reason, and is nil when disabled.
	var pollC <-chan time.Time
	if w.taskPollInterval > 0 {
		pollTicker := time.NewTicker(w.taskPollInterval)
		defer pollTicker.Stop()
		pollC = pollTicker.C
	}

	// A full sync replaces the cache, so pending events are applied first.
	flushPending := func() {
		if timer != nil {
			timer.Stop()
			timerC = nil
		}
		if len(pending) > 0 {
			batch := pending
			pending = make(map[eventKey]coalesced)
			w.processBatch(ctx, batch)
		}
	}

	// loopSync re-reads the cluster from the loop and reports whether it did.
	loopSync := func(kind string) bool {
		flushPending()

		// TryLock, not sync: a Resync that started before this loop did
		// (it holds syncMu too) refreshes everything this one would, and
		// waiting for it here would stall event processing behind it.
		if !w.syncMu.TryLock() {
			slog.Info(kind + " full re-sync skipped: a sync is already running")
			return false
		}

		slog.Info(kind + " full re-sync")

		// Beside a live stream a failure is not a disconnection, but a
		// success is the stream's verdict restored. lastSync stopping is
		// what reports a cache gone stale.
		err := w.syncLocked(ctx)
		w.syncMu.Unlock()

		if err != nil {
			return false
		}

		w.markTracking()
		w.writeSnapshot()

		return true
	}

	// Until a sync succeeds beside this stream, one is retried on a backoff,
	// rather than leaving the cache stale, and the instance unready, until
	// the periodic tick.
	var retry *time.Timer
	var retryC <-chan time.Time
	retryDelay := time.Second
	if !synced {
		retry = time.NewTimer(retryDelay)
		defer retry.Stop()
		retryC = retry.C
	}

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
			// An update coalesced onto a create keeps the create's retry.
			if prev, ok := pending[key]; !ok || prev.action != "create" || action != "update" {
				pending[key] = coalesced{action: action}
			}
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
		case req := <-w.refreshes:
			req.done <- req.run(ctx)
		case <-w.settledReady:
			w.applySettled()
		case done := <-w.resyncRequests:
			flushPending()

			err := w.sync(ctx)
			if err == nil {
				w.writeSnapshot()
			}
			done <- err
		case <-pollC:
			// A list taken before a sync's must not be applied after it, or
			// it drops tasks the sync added; the sync re-reads them anyway.
			if !w.syncMu.TryLock() {
				continue
			}

			err := w.reconcileTasks(ctx, TaskScope{})
			w.syncMu.Unlock()

			if err != nil {
				slog.Debug("task poll failed", "error", err)
			}
		case <-syncTicker.C:
			if loopSync("periodic") && retryC != nil {
				retry.Stop()
				retryC = nil
			}
		case <-retryC:
			if loopSync("retried") {
				retryC = nil
				continue
			}

			retryDelay = min(retryDelay*2, maxBackoff)
			retry.Reset(retryDelay)
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
	case events.NetworkEventType, events.VolumeEventType:
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
	scopes := taskScopes(batch)

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
		w.reconcileScopes(ctx, scopes)
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
				w.inspectAndApply(ctx, key, batch[key].action == "create")
			}
		})
	}
	wg.Wait()

	w.reconcileScopes(ctx, scopes)

	// Scheduled after the batch rather than inside it: the inspect above may
	// already have read the settled record, and checking once here costs one
	// map lookup instead of a goroutine per event.
	for key, ev := range batch {
		if ev.action == actionSettle || ev.action == actionStarted {
			w.scheduleSettle(ctx, key, ev.action)
		}
	}
}

// taskScopes names the task lists a batch calls for: Swarm emits no task
// events, and a service or node changing is when its tasks most likely did.
// A removal is left to the poll: the engine refuses a filter on a removed one.
func taskScopes(batch map[eventKey]coalesced) []TaskScope {
	var scopes []TaskScope
	for key, ev := range batch {
		if ev.action == "remove" {
			continue
		}

		switch key.resourceType { //nolint:exhaustive // only services and nodes own tasks
		case events.ServiceEventType:
			scopes = append(scopes, TaskScope{Service: key.id})
		case events.NodeEventType:
			scopes = append(scopes, TaskScope{Node: key.id})
		}
	}

	return scopes
}

// reconcileScopes re-lists the tasks of each scope, one at a time: two lists
// taken at different moments can disagree on a task, and the later-applied
// one must not drop a task the other added. The same holds against a sync,
// so a running one wins. Past a handful, one list of every task is cheaper.
func (w *Watcher) reconcileScopes(ctx context.Context, scopes []TaskScope) {
	if len(scopes) == 0 || !w.syncMu.TryLock() {
		return
	}
	defer w.syncMu.Unlock()

	if len(scopes) > workerCount {
		scopes = []TaskScope{{}}
	}

	for _, scope := range scopes {
		if err := w.reconcileTasks(ctx, scope); err != nil {
			slog.Debug(
				"task re-list failed",
				"service", scope.Service,
				"node", scope.Node,
				"error", err,
			)
		}
	}
}

// reconcileTasks re-lists a scope's tasks and applies the difference: SetTask
// announces only a real change, and a cached task the list lacks is gone.
// Callers run it on the event loop, so nothing else applies meanwhile.
func (w *Watcher) reconcileTasks(ctx context.Context, scope TaskScope) error {
	ctx, cancel := context.WithTimeout(ctx, taskListTimeout)
	defer cancel()

	listed, err := w.client.ListTasksIn(ctx, scope)
	if err != nil {
		return err
	}

	var cached []swarm.Task
	switch {
	case scope.Service != "":
		cached = w.store.ListTasksByService(scope.Service)
	case scope.Node != "":
		cached = w.store.ListTasksByNode(scope.Node)
	default:
		cached = w.store.ListTasks()
	}

	present := make(map[string]struct{}, len(listed))
	for _, task := range listed {
		present[task.ID] = struct{}{}
		w.store.SetTask(task)
	}

	for _, task := range cached {
		if _, ok := present[task.ID]; !ok {
			w.store.DeleteTask(task.ID)
		}
	}

	return nil
}

// scheduleSettle re-reads a task shortly after its container started or exited.
// Docker emits no task events, and Swarm reconciles the record after the
// container it wraps, so the inspect on the event reads one that has not caught
// up in either direction. Each read waits twice as long as the last.
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

			if w.settleRead(ctx, key, action) {
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

// settledRead is a settle's inspect awaiting the loop: the task, or gone.
type settledRead struct {
	task swarm.Task
	gone bool
}

// settleRead inspects a task off the loop and queues the result for it, so a
// burst of exits holds neither the loop nor a Refresh behind it. It reports
// whether the task has caught up, which ends the re-reads.
func (w *Watcher) settleRead(ctx context.Context, key eventKey, action string) bool {
	resource, err := w.inspectWithRetry(ctx, key, false)

	var read settledRead
	switch {
	case cerrdefs.IsNotFound(err):
		read.gone = true
	case err != nil:
		slog.Warn("task re-read failed", "id", key.id, "error", err)
		return ctx.Err() != nil
	default:
		task, ok := resource.(swarm.Task)
		if !ok {
			return true
		}
		read.task = task
	}

	// With no loop there is nothing to order against, as for Refresh.
	if !w.loopRunning.Load() {
		w.applySettledRead(key.id, read)
	} else {
		w.queueSettled(key.id, read)
	}

	return read.gone || taskCaughtUp(read.task, action)
}

// queueSettled keeps the newest read per task until the loop takes it. A loop
// that stops first leaves it for the next one.
func (w *Watcher) queueSettled(id string, read settledRead) {
	w.settledMu.Lock()
	if queued, ok := w.settled[id]; !ok || queued.gone ||
		read.gone || read.task.Version.Index >= queued.task.Version.Index {
		w.settled[id] = read
	}
	w.settledMu.Unlock()

	select {
	case w.settledReady <- struct{}{}:
	default:
	}
}

// applySettled applies every queued settle read; it runs on the event loop.
func (w *Watcher) applySettled() {
	w.settledMu.Lock()
	reads := w.settled
	w.settled = make(map[string]settledRead, len(reads))
	w.settledMu.Unlock()

	for id, read := range reads {
		w.applySettledRead(id, read)
	}
}

// applySettledRead applies a read taken before the writes now in the cache:
// a task removed meanwhile stays removed, and SetTask refuses an older version.
func (w *Watcher) applySettledRead(id string, read settledRead) {
	if _, ok := w.store.GetTask(id); !ok {
		return
	}

	if read.gone {
		w.store.DeleteTask(id)
		return
	}

	w.store.SetTask(read.task)
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

func (w *Watcher) inspectAndApply(ctx context.Context, key eventKey, created bool) {
	resource, err := w.inspectWithRetry(ctx, key, created)
	if err != nil {
		// A not-found is the daemon's answer, not a race: Swarm garbage-collects
		// a task's record without emitting a removal event, so this is the only
		// signal it went. Only a create retries it, as "not registered yet".
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

// fullSyncTimeout bounds one full sync, however it was started.
var fullSyncTimeout = 2 * time.Minute

// taskListTimeout bounds one task re-list, which holds the event loop.
var taskListTimeout = 30 * time.Second

// inspectTimeout bounds one inspect attempt, which holds the event loop too.
var inspectTimeout = 30 * time.Second

// inspect reads one resource under inspectTimeout.
func (w *Watcher) inspect(ctx context.Context, kind events.Type, id string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	return w.client.Inspect(ctx, kind, id)
}

// refreshHandoffTimeout bounds how long Refresh waits for the event loop to
// take its request. Long enough that a loop busy with a burst of events still
// gets there, short enough that a caller learns the engine is unreachable
// rather than waiting out a reconnect.
var refreshHandoffTimeout = 2 * time.Second

// errRefreshUnavailable reports that no event loop took the refresh. It wraps
// the containerd sentinel so the API answers ENG001, as it does for every
// other way the engine turns out to be unreachable.
var errRefreshUnavailable = fmt.Errorf(
	"the Docker event loop is not accepting refreshes: %w", cerrdefs.ErrUnavailable,
)

// refreshRequest asks the event loop to run a re-read on its behalf.
type refreshRequest struct {
	run  func(context.Context) error
	done chan error
}

// Refresh re-reads one resource from the engine into the cache, for a caller
// that cannot wait for the event stream. The work runs on the event loop,
// which applies every other write: applying from the caller's goroutine lets a
// remove arriving mid-inspect be undone, resurrecting the resource.
func (w *Watcher) Refresh(ctx context.Context, kind, id string) error {
	refresh := func(ctx context.Context) error { return w.refreshNow(ctx, kind, id) }

	// Nothing to serialize against with no loop running, and waiting for one
	// would stall the write this refresh precedes.
	if !w.loopRunning.Load() {
		return refresh(ctx)
	}

	done := make(chan error, 1)

	// The loop can stop between that check and this send, and during an outage
	// it is gone for a whole reconnect interval. A request context carries no
	// deadline of its own, so an unbounded send would hold the write this
	// refresh precedes until the client gave up. Unavailable is what the
	// engine being out of reach already answers with.
	handoff := time.NewTimer(refreshHandoffTimeout)
	defer handoff.Stop()

	select {
	case w.refreshes <- refreshRequest{run: refresh, done: done}:
	case <-handoff.C:
		return errRefreshUnavailable
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// refreshNow is Refresh's work, on whichever goroutine may apply it. An
// inspect failure is returned rather than logged: the caller asked because it
// cannot tolerate a stale answer.
func (w *Watcher) refreshNow(ctx context.Context, kind, id string) error {
	resource, err := w.inspect(ctx, events.Type(kind), id)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			w.applyRemove(eventKey{resourceType: events.Type(kind), id: id})

			return nil
		}

		// Our own deadline, not the caller's: the engine is out of reach.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("%w: %w", cerrdefs.ErrUnavailable, err)
		}

		return err
	}

	w.apply(resource)

	return nil
}

// inspectWithRetry retries transient inspect failures with capped exponential
// backoff, holding the event loop meanwhile. A not-found is retried only after a
// create, which can outrun the object during a stack deploy; anywhere else the
// object is gone, and a task outlives every container event that names it.
func (w *Watcher) inspectWithRetry(
	ctx context.Context,
	key eventKey,
	created bool,
) (any, error) {
	const maxAttempts = 4
	backoff := 100 * time.Millisecond

	var lastErr error
	for range maxAttempts {
		resource, err := w.inspect(ctx, key.resourceType, key.id)
		if err == nil {
			return resource, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if cerrdefs.IsNotFound(err) && !created {
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
