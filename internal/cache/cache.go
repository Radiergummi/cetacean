package cache

import (
	"cmp"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"golang.org/x/sync/singleflight"

	"github.com/radiergummi/cetacean/internal/metrics"
	"github.com/radiergummi/cetacean/internal/placement"
	"github.com/radiergummi/cetacean/internal/quorum"
)

// EventType identifies the kind of resource an event relates to.
type EventType string

const (
	EventNode    EventType = "node"
	EventService EventType = "service"
	EventTask    EventType = "task"
	EventConfig  EventType = "config"
	EventSecret  EventType = "secret"
	EventNetwork EventType = "network"
	EventVolume  EventType = "volume"
	EventStack   EventType = "stack"
	EventSync    EventType = "sync"
)

type Event struct {
	Type      EventType `json:"type"`
	Action    string    `json:"action"`
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Resource  any       `json:"resource,omitempty"`
	HistoryID uint64    `json:"-"`

	// Previous is the config or secret an update replaced, for a reader that
	// must know what changed: one relabelled out of its version group.
	Previous any `json:"-"`

	// Service and Stack name what a removed resource belonged to, which the
	// cache can no longer resolve once it is gone.
	Service string `json:"-"`
	Stack   string `json:"-"`
}

type Stack struct {
	Name     string   `json:"name"`
	Services []string `json:"services"`
	Configs  []string `json:"configs"`
	Secrets  []string `json:"secrets"`
	Networks []string `json:"networks"`
	Volumes  []string `json:"volumes"`
}

// clone copies the ID lists, which the cache mutates in place. An empty list
// stays non-nil, so it marshals as [] rather than null.
func (s Stack) clone() Stack {
	return Stack{
		Name:     s.Name,
		Services: append([]string{}, s.Services...),
		Configs:  append([]string{}, s.Configs...),
		Secrets:  append([]string{}, s.Secrets...),
		Networks: append([]string{}, s.Networks...),
		Volumes:  append([]string{}, s.Volumes...),
	}
}

type StackDetail struct {
	Name     string            `json:"name"`
	Services []swarm.Service   `json:"services"`
	Configs  []swarm.Config    `json:"configs"`
	Secrets  []swarm.Secret    `json:"secrets"`
	Networks []network.Summary `json:"networks"`
	Volumes  []volume.Volume   `json:"volumes"`
}

type StackSummary struct {
	Name             string         `json:"name"`
	ServiceCount     int            `json:"serviceCount"`
	ConfigCount      int            `json:"configCount"`
	SecretCount      int            `json:"secretCount"`
	NetworkCount     int            `json:"networkCount"`
	VolumeCount      int            `json:"volumeCount"`
	DesiredTasks     int            `json:"desiredTasks"`
	TasksByState     map[string]int `json:"tasksByState"`
	UpdatingServices int            `json:"updatingServices"`
	MemoryLimitBytes int64          `json:"memoryLimitBytes"`
	CPULimitCores    float64        `json:"cpuLimitCores"`
	MemoryUsageBytes int64          `json:"memoryUsageBytes"`
	CPUUsagePercent  float64        `json:"cpuUsagePercent"`
}

type ClusterSnapshot struct {
	NodeCount      int            `json:"nodeCount"`
	ServiceCount   int            `json:"serviceCount"`
	TaskCount      int            `json:"taskCount"`
	StackCount     int            `json:"stackCount"`
	TasksByState   map[string]int `json:"tasksByState"`
	NodesReady     int            `json:"nodesReady"`
	NodesDown      int            `json:"nodesDown"`
	NodesDraining  int            `json:"nodesDraining"`
	TotalCPU       int            `json:"totalCPU"`
	TotalMemory    int64          `json:"totalMemory"`
	ReservedCPU    int64          `json:"reservedCPU"`
	ReservedMemory int64          `json:"reservedMemory"`
	MaxNodeCPU     int            `json:"maxNodeCPU"`
	MaxNodeMemory  int64          `json:"maxNodeMemory"`
	LastSync       time.Time      `json:"lastSync"`
	Quorum         quorum.Quorum  `json:"quorum"`

	// RunningByService is the per-service running-task count, taken in the same
	// walk as TasksByState so a caller deriving service states reads the task
	// table once. Not serialized: the REST /cluster payload is a published contract.
	RunningByService map[string]int `json:"-"`

	// Flapping names the services in a restart loop, which are unhealthy
	// whatever their running count says at the moment it was taken.
	Flapping map[string]bool `json:"-"`

	// CompletedJobsByService is CompletedJobTaskCounts, from the same walk.
	CompletedJobsByService map[string]int `json:"-"`
}

// FlappingServices returns the services currently in a restart loop.
func (c *Cache) FlappingServices() map[string]bool {
	return c.restarts.Flapping()
}

// IsFlapping reports whether one service is in a restart loop.
func (c *Cache) IsFlapping(serviceID string) bool {
	return c.restarts.IsFlapping(serviceID)
}

type OnChangeFunc func(Event)

type listenerEntry struct {
	id uint64
	fn OnChangeFunc
}

type Cache struct {
	mu             sync.RWMutex
	nodes          ResourceMap[swarm.Node]
	services       map[string]swarm.Service
	tasks          map[string]swarm.Task
	tasksByService map[string]map[string]struct{} // serviceID -> set of taskIDs
	tasksByNode    map[string]map[string]struct{} // nodeID -> set of taskIDs
	goneServices   map[string]swarm.Service       // removed services whose tasks remain
	configs        ResourceMap[swarm.Config]
	secrets        ResourceMap[swarm.Secret]
	networks       ResourceMap[network.Summary]
	volumes        ResourceMap[volume.Volume]
	stacks         map[string]Stack
	lastSync       time.Time
	tasksSynced    bool // Docker's task list has been synced, so later ones can be diffed
	listenerMu     sync.RWMutex
	listeners      []listenerEntry

	// notifyMu makes recording an event and handing it to listeners one step,
	// so listeners see history order. Never held with c.mu; a listener must
	// not write to the cache.
	notifyMu       sync.Mutex
	nextListenerID uint64
	history        *History
	attributions   *Attributions
	serviceRef     serviceRefIndex
	restarts       *RestartTracker

	// diskMu serializes snapshot writers, which share one temp path, and makes
	// the later-taken snapshot the one that lands last.
	diskMu sync.Mutex

	// generation counts every change to the cache's contents, including a full
	// resync. It is not the history sequence, which does not advance on a resync
	// (see notify); a validator derived from that would miss a replaced cache.
	generation atomic.Uint64

	// versionGeneration advances only when a config or secret is set or removed,
	// or on a resync: all that config and secret version groups are made of.
	versionGeneration atomic.Uint64

	addresses     atomic.Pointer[addressMemo]
	addressCounts singleflight.Group
}

// Generation returns a counter that advances on every change to the cache's
// contents; equal values mean nothing changed. Read without the lock, so it may
// already be stale alongside a concurrent write.
func (c *Cache) Generation() uint64 { return c.generation.Load() }

// VersionGeneration is Generation counted over config and secret sets and
// removals alone, the inputs of config and secret version groups.
func (c *Cache) VersionGeneration() uint64 { return c.versionGeneration.Load() }

// Option configures a Cache at construction.
type Option func(*Cache)

// WithHistorySize sizes the change-history ring, 10,000 entries by default.
func WithHistorySize(size int) Option {
	return func(c *Cache) { c.history = NewHistory(size) }
}

func New(onChange OnChangeFunc, opts ...Option) *Cache {
	c := &Cache{
		services:       make(map[string]swarm.Service),
		tasks:          make(map[string]swarm.Task),
		tasksByService: make(map[string]map[string]struct{}),
		tasksByNode:    make(map[string]map[string]struct{}),
		goneServices:   make(map[string]swarm.Service),
		stacks:         make(map[string]Stack),
		serviceRef:     newServiceRefIndex(),
		history:        NewHistory(10000),
		attributions:   newAttributions(),
		restarts:       NewRestartTracker(7*24*time.Hour, time.Hour),
	}
	for _, opt := range opts {
		opt(c)
	}
	if onChange != nil {
		c.AddOnChangeListener(onChange)
	}

	c.nodes = ResourceMap[swarm.Node]{
		mu:       &c.mu,
		items:    make(map[string]swarm.Node),
		nameFunc: func(n swarm.Node) string { return n.Description.Hostname },
		version:  func(n swarm.Node) uint64 { return n.Version.Index },
	}

	c.configs = ResourceMap[swarm.Config]{
		mu:       &c.mu,
		items:    make(map[string]swarm.Config),
		nameFunc: func(cfg swarm.Config) string { return cfg.Spec.Name },
		version:  func(cfg swarm.Config) uint64 { return cfg.Version.Index },
		onSet: func(_ string, old *swarm.Config, new_ *swarm.Config) {
			if old != nil {
				c.removeFromStack(EventConfig, old.ID, old.Spec.Labels)
			}
			c.addToStack(EventConfig, new_.ID, new_.Spec.Labels)
		},
		onDelete: func(_ string, old swarm.Config) {
			c.removeFromStack(EventConfig, old.ID, old.Spec.Labels)
		},
	}

	c.secrets = ResourceMap[swarm.Secret]{
		mu:       &c.mu,
		items:    make(map[string]swarm.Secret),
		nameFunc: func(s swarm.Secret) string { return s.Spec.Name },
		version:  func(s swarm.Secret) uint64 { return s.Version.Index },
		onSet: func(_ string, old *swarm.Secret, new_ *swarm.Secret) {
			if old != nil {
				c.removeFromStack(EventSecret, old.ID, old.Spec.Labels)
			}
			new_.Spec.Data = nil
			c.addToStack(EventSecret, new_.ID, new_.Spec.Labels)
		},
		onDelete: func(_ string, old swarm.Secret) {
			c.removeFromStack(EventSecret, old.ID, old.Spec.Labels)
		},
	}

	c.networks = ResourceMap[network.Summary]{
		mu:       &c.mu,
		items:    make(map[string]network.Summary),
		nameFunc: func(n network.Summary) string { return n.Name },
		onSet: func(_ string, old *network.Summary, new_ *network.Summary) {
			if old != nil {
				c.removeFromStack(EventNetwork, old.ID, old.Labels)
			}
			c.addToStack(EventNetwork, new_.ID, new_.Labels)
		},
		onDelete: func(_ string, old network.Summary) {
			c.removeFromStack(EventNetwork, old.ID, old.Labels)
		},
	}

	c.volumes = ResourceMap[volume.Volume]{
		mu:       &c.mu,
		items:    make(map[string]volume.Volume),
		nameFunc: func(v volume.Volume) string { return v.Name },
		onSet: func(_ string, old *volume.Volume, new_ *volume.Volume) {
			if old != nil {
				c.removeFromStack(EventVolume, old.Name, old.Labels)
			}
			c.addToStack(EventVolume, new_.Name, new_.Labels)
		},
		onDelete: func(_ string, old volume.Volume) {
			c.removeFromStack(EventVolume, old.Name, old.Labels)
		},
	}

	return c
}

func (c *Cache) History() *History { return c.history }

// Attributions is where a write registers itself, so the history entry its
// event produces names who made it.
func (c *Cache) Attributions() *Attributions { return c.attributions }

// Populated reports whether the cache holds cluster state, from a sync or from
// a snapshot loaded off disk; a failed sync replaces nothing and leaves it unset.
func (c *Cache) Populated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return !c.lastSync.IsZero()
}

// RestartTrackingSince is the earliest moment the restart tracker can account
// for. Reported beside every restart count, because a count is otherwise
// labelled by a window it may not span — see RestartTracker.TrackingSince.
func (c *Cache) RestartTrackingSince() time.Time {
	return c.restarts.TrackingSince()
}

// RestartCount returns the number of involuntary task terminations recorded for
// serviceID within the lookback window (capped at the tracker horizon).
func (c *Cache) RestartCount(serviceID string, lookback time.Duration) uint64 {
	return c.restarts.Count(serviceID, lookback)
}

// Restarts returns the underlying RestartTracker for snapshot serialization.
func (c *Cache) Restarts() *RestartTracker { return c.restarts }

// AddOnChangeListener registers fn to be called on every cache change event,
// one at a time and in history order. fn must not write to the cache.
// The returned cancel function removes the listener; it is safe to call multiple times.
func (c *Cache) AddOnChangeListener(fn OnChangeFunc) func() {
	c.listenerMu.Lock()
	id := c.nextListenerID
	c.nextListenerID++
	c.listeners = append(c.listeners, listenerEntry{id: id, fn: fn})
	c.listenerMu.Unlock()

	return func() {
		c.listenerMu.Lock()
		for i, entry := range c.listeners {
			if entry.id == id {
				c.listeners = append(c.listeners[:i], c.listeners[i+1:]...)
				break
			}
		}
		c.listenerMu.Unlock()
	}
}

func (c *Cache) notify(e Event) {
	// Populate the Name field for SSE authorization checks.
	if e.Name == "" {
		e.Name = ExtractName(e)
	}

	// Sync events are internal bookkeeping; broadcast them to SSE clients
	// but don't record them in history where they drown out real changes.
	// Every event means the contents changed, a resync included.
	c.generation.Add(1)

	// A ref_changed moves who uses a config or secret, not its group.
	switch e.Type {
	case EventConfig, EventSecret:
		if e.Action != "ref_changed" {
			c.versionGeneration.Add(1)
		}
	case EventSync:
		c.versionGeneration.Add(1)
	default:
	}

	if e.Action == "remove" && e.Service == "" && e.Stack == "" {
		e.Service, e.Stack = c.removedFrom(e.Resource)
	}

	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()

	if e.Type != EventSync {
		entry := HistoryEntry{
			Type:       e.Type,
			Action:     e.Action,
			ResourceID: e.ID,
			Name:       e.Name,
			Service:    e.Service,
			Stack:      e.Stack,
		}
		if task, ok := e.Resource.(swarm.Task); ok {
			entry.ServiceID = task.ServiceID
		}
		if actor, ok := c.attributions.match(e); ok {
			entry.Actor = &actor
		}

		e.HistoryID = c.history.Append(entry)
		metrics.RecordCacheMutation(string(e.Type), e.Action)

		if e.Action == "create" || e.Action == "remove" {
			c.recordResourceCount(e.Type)
		}
	} else {
		e.HistoryID = c.history.Count()
	}

	// Snapshot listeners under read lock, then call outside it, so a listener
	// may read the cache.
	c.listenerMu.RLock()
	fns := make([]OnChangeFunc, len(c.listeners))
	for i, entry := range c.listeners {
		fns[i] = entry.fn
	}
	c.listenerMu.RUnlock()

	for _, fn := range fns {
		fn(e)
	}
}

func ExtractName(e Event) string {
	switch r := e.Resource.(type) {
	case swarm.Node:
		return r.Description.Hostname
	case swarm.Service:
		return r.Spec.Name
	case swarm.Task:
		return r.ID
	case swarm.Config:
		return r.Spec.Name
	case swarm.Secret:
		return r.Spec.Name
	case network.Summary:
		return r.Name
	case volume.Volume:
		return r.Name
	default:
		return ""
	}
}

// --- Cross-reference event support ---

// refSet is what a service references, each mapped to whether its current
// spec does. A config or secret its previous spec alone names maps to false.
type refSet struct {
	configs  map[string]bool
	secrets  map[string]bool
	networks map[string]bool
	volumes  map[string]bool
}

func serviceRefs(s swarm.Service) refSet {
	r := refSet{
		configs:  make(map[string]bool),
		secrets:  make(map[string]bool),
		networks: make(map[string]bool),
		volumes:  make(map[string]bool),
	}
	if prev := s.PreviousSpec; prev != nil && prev.TaskTemplate.ContainerSpec != nil {
		for _, c := range prev.TaskTemplate.ContainerSpec.Configs {
			r.configs[c.ConfigID] = false
		}
		for _, sec := range prev.TaskTemplate.ContainerSpec.Secrets {
			r.secrets[sec.SecretID] = false
		}
	}
	if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
		for _, c := range cs.Configs {
			r.configs[c.ConfigID] = true
		}
		for _, sec := range cs.Secrets {
			r.secrets[sec.SecretID] = true
		}
		for _, m := range cs.Mounts {
			if m.Type == "volume" && m.Source != "" {
				r.volumes[m.Source] = true
			}
		}
	}
	for _, n := range s.Spec.TaskTemplate.Networks {
		r.networks[n.Target] = true
	}
	return r
}

func (c *Cache) notifyRefChanges(old, new refSet) {
	// Named after the referenced resource, which is what every scoped stream
	// authorizes on; volumes are keyed by name already.
	// A reference moving between the current and the previous spec changes
	// it too: the service now holds it for a rollback only, or again for real.
	diffNotify := func(typ EventType, oldSet, newSet map[string]bool, name func(string) string) {
		for id, was := range oldSet {
			if now, ok := newSet[id]; !ok || now != was {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id, Name: name(id)})
			}
		}
		for id := range newSet {
			if _, ok := oldSet[id]; !ok {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id, Name: name(id)})
			}
		}
	}
	diffNotify(EventConfig, old.configs, new.configs, func(id string) string {
		cfg, _ := c.GetConfig(id)
		return cfg.Spec.Name
	})
	diffNotify(EventSecret, old.secrets, new.secrets, func(id string) string {
		sec, _ := c.GetSecret(id)
		return sec.Spec.Name
	})
	diffNotify(EventNetwork, old.networks, new.networks, func(id string) string {
		net, _ := c.GetNetwork(id)
		return net.Name
	})
	diffNotify(EventVolume, old.volumes, new.volumes, func(id string) string { return id })
}

// --- Nodes ---

func (c *Cache) SetNode(n swarm.Node) {
	if event, ok := c.nodes.Set(n.ID, n, EventNode); ok {
		c.notify(event)
	}
}

func (c *Cache) GetNode(id string) (swarm.Node, bool) { return c.nodes.Get(id) }

func (c *Cache) DeleteNode(id string) {
	c.notify(c.nodes.Delete(id, EventNode))
}

func (c *Cache) ListNodes() []swarm.Node { return c.nodes.List() }

// EachNode calls yield for every node in ListNodes order. See ResourceMap.Each
// for the rule about calling back into the cache from yield.
func (c *Cache) EachNode(yield func(swarm.Node) bool) { c.nodes.Each(yield) }

// EachConfig, EachSecret, EachNetwork and EachVolume are EachNode for the other
// ResourceMap-backed types.
func (c *Cache) EachConfig(yield func(swarm.Config) bool)     { c.configs.Each(yield) }
func (c *Cache) EachSecret(yield func(swarm.Secret) bool)     { c.secrets.Each(yield) }
func (c *Cache) EachNetwork(yield func(network.Summary) bool) { c.networks.Each(yield) }
func (c *Cache) EachVolume(yield func(volume.Volume) bool)    { c.volumes.Each(yield) }

// EachService calls yield for every service in ListServices order.
func (c *Cache) EachService(yield func(swarm.Service) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.services))
	for id := range c.services {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		if !yield(c.services[id]) {
			return
		}
	}
}

// EachTask calls yield for every task in ListTasks order, which is by slot and
// then ID rather than by key — so the sort key is gathered rather than the map
// keys, and the tasks themselves are still never copied into a slice.
func (c *Cache) EachTask(yield func(swarm.Task) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	type key struct {
		slot int
		id   string
	}

	keys := make([]key, 0, len(c.tasks))
	for id, t := range c.tasks {
		keys = append(keys, key{t.Slot, id})
	}
	slices.SortFunc(keys, func(a, b key) int {
		if bySlot := cmp.Compare(a.slot, b.slot); bySlot != 0 {
			return bySlot
		}

		return cmp.Compare(a.id, b.id)
	})

	for _, k := range keys {
		if !yield(c.tasks[k.id]) {
			return
		}
	}
}

// eachTaskUnordered is EachTask in no particular order, for a scan that only
// counts and so need not pay for the sort.
func (c *Cache) eachTaskUnordered(yield func(swarm.Task) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, t := range c.tasks {
		if !yield(t) {
			return
		}
	}
}

// --- Services ---

// SetService ignores a service older than the cached one, as SetTask does a
// task: two inspects of one resource can land in either order.
func (c *Cache) SetService(s swarm.Service) {
	newRefs := serviceRefs(s)

	c.mu.Lock()
	var oldRefs refSet
	old, existed := c.services[s.ID]
	if existed && s.Version.Index < old.Version.Index {
		c.mu.Unlock()
		return
	}
	if existed {
		oldRefs = serviceRefs(old)
		c.removeFromStack(EventService, old.ID, old.Spec.Labels)
		c.serviceRef.remove(old, oldRefs)
	}
	c.services[s.ID] = s
	c.addToStack(EventService, s.ID, s.Spec.Labels)
	c.serviceRef.add(s, newRefs)
	c.mu.Unlock()

	c.notify(Event{Type: EventService, Action: setAction(existed), ID: s.ID, Resource: s})
	c.notifyRefChanges(oldRefs, newRefs)
}

func (c *Cache) GetService(id string) (swarm.Service, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.services[id]
	return s, ok
}

func (c *Cache) DeleteService(id string) {
	c.mu.Lock()
	var oldRefs refSet
	var name string
	old, existed := c.services[id]
	if existed {
		name = old.Spec.Name
		oldRefs = serviceRefs(old)
		c.removeFromStack(EventService, id, old.Spec.Labels)
		c.serviceRef.remove(old, oldRefs)
		if len(c.tasksByService[id]) > 0 {
			c.goneServices[id] = old
		}
	}
	delete(c.services, id)
	c.mu.Unlock()

	c.restarts.Forget(id)
	event := Event{Type: EventService, Action: "remove", ID: id, Name: name}
	if existed {
		event.Resource = old
	}
	c.notify(event)
	c.notifyRefChanges(oldRefs, refSet{})
}

func (c *Cache) ListServices() []swarm.Service {
	c.mu.RLock()
	out := make([]swarm.Service, 0, len(c.services))
	for _, s := range c.services {
		out = append(out, s)
	}
	c.mu.RUnlock()

	// out is this caller's own copy, so the sort — the expensive half of the
	// call on a large cluster — does not belong under the lock the watcher's
	// writers are contending for.
	sortByIndex(out, func(a, b int) int { return cmp.Compare(out[a].ID, out[b].ID) })

	return out
}

// --- Tasks ---

func (c *Cache) SetTask(t swarm.Task) {
	c.mu.Lock()
	changed := true
	wasFailure := false
	old, existed := c.tasks[t.ID]
	if existed && t.Version.Index < old.Version.Index {
		c.mu.Unlock()
		return
	}
	if existed {
		// Status.Message captures the human-readable transition reason
		// ("started", "shutdown requested", "rejected: …"). Without it
		// the SSE stream silently coalesces task error-message changes.
		changed = old.Status.State != t.Status.State ||
			old.DesiredState != t.DesiredState ||
			old.Status.Err != t.Status.Err ||
			old.Status.Message != t.Status.Message ||
			old.NodeID != t.NodeID ||
			old.Version != t.Version
		wasFailure = IsFailureState(old.Status.State)
		c.removeTaskIndex(old)
	}
	c.tasks[t.ID] = t
	c.addTaskIndex(t)
	c.mu.Unlock()

	if IsFailureState(t.Status.State) && !wasFailure && t.ServiceID != "" {
		c.restarts.Record(t.ServiceID, t.Status.Timestamp)
	}

	if changed {
		c.notify(Event{Type: EventTask, Action: setAction(existed), ID: t.ID, Resource: t})
	}
}

func (c *Cache) GetTask(id string) (swarm.Task, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tasks[id]
	return t, ok
}

func (c *Cache) DeleteTask(id string) {
	c.mu.Lock()
	old, existed := c.tasks[id]
	var service, stack string
	if existed {
		c.removeTaskIndex(old)
		// Resolved here, under the same lock as removeTaskIndex: a concurrent
		// DeleteTask for the service's other last task must not be able to
		// forget the goneServices entry before this one reads it.
		service, stack = c.removedTaskFromLocked(old)
		if c.tasksByService[old.ServiceID] == nil {
			delete(c.goneServices, old.ServiceID)
		}
	}
	delete(c.tasks, id)
	c.mu.Unlock()

	// The removed task rides along: node, service and stack streams match a
	// task event on its parents, which the cache can no longer look up.
	event := Event{
		Type:    EventTask,
		Action:  "remove",
		ID:      id,
		Name:    id,
		Service: service,
		Stack:   stack,
	}
	if existed {
		event.Resource = old
	}
	c.notify(event)
}

// addTaskIndex adds a task to the secondary indexes. Must be called with c.mu held for writing.
func (c *Cache) addTaskIndex(t swarm.Task) {
	addIndexed(c.tasksByService, t.ServiceID, t.ID)
	addIndexed(c.tasksByNode, t.NodeID, t.ID)
}

// pruneGoneServices forgets removed services no task refers to any more. Must
// be called with c.mu held for writing.
func (c *Cache) pruneGoneServices() {
	for id := range c.goneServices {
		if c.tasksByService[id] == nil {
			delete(c.goneServices, id)
		}
	}
}

// removeTaskIndex removes a task from the secondary indexes. Must be called with c.mu held for writing.
func (c *Cache) removeTaskIndex(t swarm.Task) {
	if t.ServiceID != "" {
		if m := c.tasksByService[t.ServiceID]; m != nil {
			delete(m, t.ID)
			if len(m) == 0 {
				delete(c.tasksByService, t.ServiceID)
			}
		}
	}
	if t.NodeID != "" {
		if m := c.tasksByNode[t.NodeID]; m != nil {
			delete(m, t.ID)
			if len(m) == 0 {
				delete(c.tasksByNode, t.NodeID)
			}
		}
	}
}

func (c *Cache) ListTasks() []swarm.Task {
	c.mu.RLock()
	out := make([]swarm.Task, 0, len(c.tasks))
	for _, t := range c.tasks {
		out = append(out, t)
	}
	c.mu.RUnlock()

	sortByIndex(out, func(a, b int) int { return compareTasks(out[a], out[b]) })

	return out
}

// --- Configs ---

func (c *Cache) SetConfig(cfg swarm.Config) {
	if event, ok := c.configs.Set(cfg.ID, cfg, EventConfig); ok {
		c.notify(event)
	}
}

// AddConfig stores cfg unless the cache already holds a config with its ID.
func (c *Cache) AddConfig(cfg swarm.Config) {
	if event, ok := c.configs.Add(cfg.ID, cfg, EventConfig); ok {
		c.notify(event)
	}
}

func (c *Cache) GetConfig(id string) (swarm.Config, bool) { return c.configs.Get(id) }

func (c *Cache) DeleteConfig(id string) {
	c.notify(c.configs.Delete(id, EventConfig))
}

func (c *Cache) ListConfigs() []swarm.Config { return c.configs.List() }

// --- Secrets ---

func (c *Cache) SetSecret(s swarm.Secret) {
	if event, ok := c.secrets.Set(s.ID, s, EventSecret); ok {
		c.notify(event)
	}
}

// AddSecret stores s unless the cache already holds a secret with its ID.
func (c *Cache) AddSecret(s swarm.Secret) {
	if event, ok := c.secrets.Add(s.ID, s, EventSecret); ok {
		c.notify(event)
	}
}

func (c *Cache) GetSecret(id string) (swarm.Secret, bool) { return c.secrets.Get(id) }

func (c *Cache) DeleteSecret(id string) {
	c.notify(c.secrets.Delete(id, EventSecret))
}

func (c *Cache) ListSecrets() []swarm.Secret { return c.secrets.List() }

// --- Networks ---

func (c *Cache) SetNetwork(n network.Summary) {
	if event, ok := c.networks.Set(n.ID, n, EventNetwork); ok {
		c.notify(event)
	}
}

func (c *Cache) GetNetwork(id string) (network.Summary, bool) { return c.networks.Get(id) }

func (c *Cache) DeleteNetwork(id string) {
	c.notify(c.networks.Delete(id, EventNetwork))
}

func (c *Cache) ListNetworks() []network.Summary { return c.networks.List() }

// --- Volumes ---

func (c *Cache) SetVolume(v volume.Volume) {
	if event, ok := c.volumes.Set(v.Name, v, EventVolume); ok {
		c.notify(event)
	}
}

func (c *Cache) GetVolume(name string) (volume.Volume, bool) { return c.volumes.Get(name) }

func (c *Cache) DeleteVolume(name string) {
	c.notify(c.volumes.Delete(name, EventVolume))
}

func (c *Cache) ListVolumes() []volume.Volume { return c.volumes.List() }

// --- Stacks (derived) ---

func (c *Cache) GetStack(name string) (Stack, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stacks[name]
	if !ok {
		return Stack{}, false
	}
	return s.clone(), true
}

func (c *Cache) ListStacks() []Stack {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Stack, 0, len(c.stacks))
	for _, s := range c.stacks {
		out = append(out, s.clone())
	}
	sortByName(out, func(s Stack) string { return s.Name })
	return out
}

func (c *Cache) GetStackDetail(name string) (StackDetail, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stacks[name]
	if !ok {
		return StackDetail{}, false
	}
	detail := StackDetail{
		Name:     s.Name,
		Services: []swarm.Service{},
		Configs:  []swarm.Config{},
		Secrets:  []swarm.Secret{},
		Networks: []network.Summary{},
		Volumes:  []volume.Volume{},
	}
	for _, id := range s.Services {
		if svc, ok := c.services[id]; ok {
			detail.Services = append(detail.Services, svc)
		}
	}
	for _, id := range s.Configs {
		if cfg, ok := c.configs.items[id]; ok {
			detail.Configs = append(detail.Configs, cfg)
		}
	}
	for _, id := range s.Secrets {
		if sec, ok := c.secrets.items[id]; ok {
			detail.Secrets = append(detail.Secrets, sec)
		}
	}
	for _, id := range s.Networks {
		if net, ok := c.networks.items[id]; ok {
			detail.Networks = append(detail.Networks, net)
		}
	}
	for _, name := range s.Volumes {
		if vol, ok := c.volumes.items[name]; ok {
			detail.Volumes = append(detail.Volumes, vol)
		}
	}
	sortByName(detail.Services, func(s swarm.Service) string { return s.Spec.Name })
	sortByName(detail.Configs, func(c swarm.Config) string { return c.Spec.Name })
	sortByName(detail.Secrets, func(s swarm.Secret) string { return s.Spec.Name })
	sortByName(detail.Networks, func(n network.Summary) string { return n.Name })
	sortByName(detail.Volumes, func(v volume.Volume) string { return v.Name })

	// Secret data is nilled by the secrets map's onSet hook (see secret
	// ResourceMap above), so detail.Secrets[*].Spec.Data is already nil here.
	// Don't re-walk to be defensive — that duplicates the redaction contract
	// and invites it to drift apart.
	return detail, true
}

func (c *Cache) ListStackSummaries() []StackSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	schedulable := c.schedulableNodesLocked(true)

	out := make([]StackSummary, 0, len(c.stacks))
	for _, stack := range c.stacks {
		s := StackSummary{
			Name:         stack.Name,
			ServiceCount: len(stack.Services),
			ConfigCount:  len(stack.Configs),
			SecretCount:  len(stack.Secrets),
			NetworkCount: len(stack.Networks),
			VolumeCount:  len(stack.Volumes),
			TasksByState: make(map[string]int),
		}

		for _, svcID := range stack.Services {
			svc, ok := c.services[svcID]
			if !ok {
				continue
			}

			// Desired replicas. A job has none: its tasks run to completion, and
			// counting one would call the stack degraded once the job is done.
			replicas := 1
			if IsJobService(svc) {
				replicas = 0
			} else if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				replicas = int(*svc.Spec.Mode.Replicated.Replicas)
			} else if svc.Spec.Mode.Global != nil {
				replicas = globalTaskCount(svc, schedulable)
			}
			s.DesiredTasks += replicas

			// Update status
			if svc.UpdateStatus != nil && svc.UpdateStatus.State == swarm.UpdateStateUpdating {
				s.UpdatingServices++
			}

			// Resource limits (multiplied by replica count)
			if res := svc.Spec.TaskTemplate.Resources; res != nil && res.Limits != nil {
				s.MemoryLimitBytes += int64(replicas) * res.Limits.MemoryBytes
				s.CPULimitCores += float64(replicas) * float64(res.Limits.NanoCPUs) / 1e9
			}

			// Task states
			for taskID := range c.tasksByService[svcID] {
				if t, ok := c.tasks[taskID]; ok {
					s.TasksByState[string(t.Status.State)]++
				}
			}
		}

		out = append(out, s)
	}
	sortByName(out, func(s StackSummary) string { return s.Name })
	return out
}

// schedulableNodesLocked is where a global service runs: every ready node that
// is active or paused and meets its constraints, since pausing stops new
// placements but keeps tasks. Without paused nodes, it is where one can start.
func (c *Cache) schedulableNodesLocked(withPaused bool) []swarm.Node {
	schedulable := make([]swarm.Node, 0, len(c.nodes.items))
	for _, n := range c.nodes.items {
		availability := n.Spec.Availability
		if n.Status.State == swarm.NodeStateReady &&
			(availability == swarm.NodeAvailabilityActive ||
				withPaused && availability == swarm.NodeAvailabilityPause) {
			schedulable = append(schedulable, n)
		}
	}

	return schedulable
}

// EligibleNodeCounts is how many nodes each of the given global services could
// start a task on, keyed by service ID; services of any other mode are absent.
func (c *Cache) EligibleNodeCounts(services ...swarm.Service) map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	eligible := c.schedulableNodesLocked(false)
	out := make(map[string]int)
	for _, svc := range services {
		if svc.Spec.Mode.Global != nil {
			out[svc.ID] = globalTaskCount(svc, eligible)
		}
	}

	return out
}

// globalTaskCount is how many of the given nodes a global service places a
// task on. Platforms are not consulted: Swarm creates the task regardless and
// leaves it pending, which is a shortfall the summary should show.
func globalTaskCount(svc swarm.Service, nodes []swarm.Node) int {
	spec := svc.Spec.TaskTemplate.Placement
	if spec == nil || len(spec.Constraints) == 0 {
		return len(nodes)
	}

	count := 0
	for _, n := range nodes {
		if ok, _ := placement.Satisfies(n, spec.Constraints); ok {
			count++
		}
	}

	return count
}

// setAction names what a Set did, so subscribers can tell a resource appearing
// from one they already know about changing.
func setAction(existed bool) string {
	if existed {
		return "update"
	}

	return "create"
}

// sortByName orders a slice of resources by the string each one calls its name.
func sortByName[T any](items []T, name func(T) string) {
	slices.SortFunc(items, func(a, b T) int { return cmp.Compare(name(a), name(b)) })
}

// compareTasks orders tasks by slot, then ID. Slot groups the replicas of a
// service the way the UI lists them; the ID breaks ties between a slot's
// historical tasks (and orders global-mode tasks, which have no slot).
func compareTasks(a, b swarm.Task) int {
	if bySlot := cmp.Compare(a.Slot, b.Slot); bySlot != 0 {
		return bySlot
	}

	return cmp.Compare(a.ID, b.ID)
}

// --- Filtered task lists ---

func (c *Cache) ListTasksByService(serviceID string) []swarm.Task {
	c.mu.RLock()
	ids := c.tasksByService[serviceID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	c.mu.RUnlock()

	slices.SortFunc(out, compareTasks)
	return out
}

// TaskIsLive reports whether the orchestrator still intends this task to run.
// Swarm keeps a record for every replica it has replaced, and those are history
// rather than slots awaiting a start. DesiredState decides it, not the current
// state. It lives here because the replica counters below need it.
func TaskIsLive(task swarm.Task) bool {
	return task.DesiredState != swarm.TaskStateShutdown &&
		task.DesiredState != swarm.TaskStateRemove
}

// CountsAsRunningReplica reports whether a task counts towards a service's
// running replica total. Status.State alone is not enough twice over: a task
// marked for shutdown keeps it running while it drains, and one Docker has
// garbage-collected keeps its last-inspected status forever.
func CountsAsRunningReplica(task swarm.Task) bool {
	return task.Status.State == swarm.TaskStateRunning && TaskIsLive(task)
}

// RunningTaskCount returns the number of tasks in "running" state for a service.
func (c *Cache) RunningTaskCount(serviceID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for id := range c.tasksByService[serviceID] {
		if t, ok := c.tasks[id]; ok && CountsAsRunningReplica(t) {
			count++
		}
	}
	return count
}

// RunningTaskCounts returns the number of running tasks per service, in a
// single pass under one read lock. The alternative — handing a caller
// ListTasks() to count itself — clones and sorts the whole task table to answer
// a question about one integer per service, on a path completion walks per keystroke.
func (c *Cache) RunningTaskCounts() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	counts := make(map[string]int, len(c.services))
	for _, t := range c.tasks {
		if CountsAsRunningReplica(t) {
			counts[t.ServiceID]++
		}
	}

	return counts
}

// CountsAsJobCompletion reports whether a task completed its job service's
// current run; false for a service that is not a job. A re-run bumps JobIteration
// and leaves the earlier run's tasks behind, so those do not count.
func CountsAsJobCompletion(svc swarm.Service, task swarm.Task) bool {
	if !IsJobService(svc) || task.Status.State != swarm.TaskStateComplete {
		return false
	}

	return inCurrentJobRun(svc, task)
}

// countsAsJobFailure is CountsAsJobCompletion for a task that failed or was
// rejected.
func countsAsJobFailure(svc swarm.Service, task swarm.Task) bool {
	state := task.Status.State

	return IsJobService(svc) &&
		(state == swarm.TaskStateFailed || state == swarm.TaskStateRejected) &&
		inCurrentJobRun(svc, task)
}

// inCurrentJobRun reports whether a job's task belongs to its current run; one
// either side cannot place counts.
func inCurrentJobRun(svc swarm.Service, task swarm.Task) bool {
	if svc.JobStatus == nil || task.JobIteration == nil {
		return true
	}

	return task.JobIteration.Index == svc.JobStatus.JobIteration.Index
}

// TaskTally is how a service's tasks stand: Running as RunningTaskCounts counts
// it, and for a job how many of its current run's tasks completed or failed.
type TaskTally struct {
	Running, Completed, Failed int
}

// TaskTallies is a TaskTally for each of the given services, keyed by ID,
// under one read lock.
func (c *Cache) TaskTallies(services ...swarm.Service) map[string]TaskTally {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tallies := make(map[string]TaskTally, len(services))
	for _, given := range services {
		svc, ok := c.services[given.ID]
		if !ok {
			continue
		}

		var tally TaskTally
		for id := range c.tasksByService[svc.ID] {
			t, ok := c.tasks[id]
			switch {
			case !ok:
			case CountsAsRunningReplica(t):
				tally.Running++
			case CountsAsJobCompletion(svc, t):
				tally.Completed++
			case countsAsJobFailure(svc, t):
				tally.Failed++
			}
		}
		tallies[svc.ID] = tally
	}

	return tallies
}

// IsJobService reports whether a service runs to completion rather than forever.
func IsJobService(svc swarm.Service) bool {
	return svc.Spec.Mode.ReplicatedJob != nil || svc.Spec.Mode.GlobalJob != nil
}

// CompletedJobTaskCount returns how many tasks completed a job service's
// current run; 0 for a service that is not a job.
func (c *Cache) CompletedJobTaskCount(serviceID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	svc, ok := c.services[serviceID]
	if !ok {
		return 0
	}

	count := 0
	for id := range c.tasksByService[serviceID] {
		if t, ok := c.tasks[id]; ok && CountsAsJobCompletion(svc, t) {
			count++
		}
	}

	return count
}

// CompletedJobTaskCounts is CompletedJobTaskCount for every job service, in one
// pass under one read lock, as RunningTaskCounts is for running tasks.
func (c *Cache) CompletedJobTaskCounts() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	counts := make(map[string]int)
	for _, t := range c.tasks {
		if svc, ok := c.services[t.ServiceID]; ok && CountsAsJobCompletion(svc, t) {
			counts[t.ServiceID]++
		}
	}

	return counts
}

func (c *Cache) ListTasksByNode(nodeID string) []swarm.Task {
	c.mu.RLock()
	ids := c.tasksByNode[nodeID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	c.mu.RUnlock()

	slices.SortFunc(out, compareTasks)
	return out
}

// --- Cross-references ---

// ServicesUsingConfig returns services whose spec or previous spec references
// the given config ID; Rollback marks the latter.
func (c *Cache) ServicesUsingConfig(configID string) []ServiceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceRef.lookup(c.serviceRef.byConfig, configID, c.services)
}

// ServicesUsingSecret is ServicesUsingConfig for a secret.
func (c *Cache) ServicesUsingSecret(secretID string) []ServiceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceRef.lookup(c.serviceRef.bySecret, secretID, c.services)
}

// ServicesUsingNetwork returns services that reference the given network ID.
func (c *Cache) ServicesUsingNetwork(networkID string) []ServiceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceRef.lookup(c.serviceRef.byNetwork, networkID, c.services)
}

// ServicesUsingVolume returns services that mount the given volume name.
func (c *Cache) ServicesUsingVolume(volumeName string) []ServiceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceRef.lookup(c.serviceRef.byVolume, volumeName, c.services)
}

// --- Snapshot ---

func (c *Cache) Snapshot() ClusterSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := c.nodeTotalsLocked()

	// Counted once and returned in the snapshot: BuildClusterStatus needs the
	// same map to name the degraded services.
	tasksByState := make(map[string]int)
	runningByService := make(map[string]int, len(c.services))
	completedJobsByService := make(map[string]int)
	for _, t := range c.tasks {
		tasksByState[string(t.Status.State)]++

		if CountsAsRunningReplica(t) {
			runningByService[t.ServiceID]++
		}

		if svc, ok := c.services[t.ServiceID]; ok && CountsAsJobCompletion(svc, t) {
			completedJobsByService[t.ServiceID]++
		}
	}

	// A job reserves nothing here: it has no standing task count to reserve for.
	schedulable := c.schedulableNodesLocked(true)
	var reservedCPU, reservedMemory int64
	for _, svc := range c.services {
		var desired int
		switch {
		case svc.Spec.Mode.Global != nil:
			desired = globalTaskCount(svc, schedulable)
		case svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil:
			desired = int(*svc.Spec.Mode.Replicated.Replicas)
		}

		res := svc.Spec.TaskTemplate.Resources
		if res == nil || res.Reservations == nil {
			continue
		}

		reservedCPU += res.Reservations.NanoCPUs * int64(desired)
		reservedMemory += res.Reservations.MemoryBytes * int64(desired)
	}

	return ClusterSnapshot{
		NodeCount:        nodes.Count,
		ServiceCount:     len(c.services),
		TaskCount:        len(c.tasks),
		StackCount:       len(c.stacks),
		TasksByState:     tasksByState,
		NodesReady:       nodes.Ready,
		NodesDown:        nodes.Down,
		NodesDraining:    nodes.Draining,
		TotalCPU:         nodes.TotalCPU,
		TotalMemory:      nodes.TotalMemory,
		ReservedCPU:      reservedCPU,
		ReservedMemory:   reservedMemory,
		MaxNodeCPU:       nodes.MaxNodeCPU,
		MaxNodeMemory:    nodes.MaxNodeMemory,
		LastSync:         c.lastSync,
		Quorum:           quorum.Of(maps.Values(c.nodes.items)),
		RunningByService: runningByService,
		Flapping:         c.restarts.Flapping(),

		CompletedJobsByService: completedJobsByService,
	}
}

// NodeTotals is the node half of a ClusterSnapshot, for a caller needing
// nothing else; CPUs are whole cores.
type NodeTotals struct {
	Count         int
	Ready         int
	Down          int
	Draining      int
	TotalCPU      int
	TotalMemory   int64
	MaxNodeCPU    int
	MaxNodeMemory int64
}

func (c *Cache) NodeTotals() NodeTotals {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.nodeTotalsLocked()
}

// NodeCount is len(ListNodes()) without the copy.
func (c *Cache) NodeCount() int { return c.nodes.Len() }

// nodeTotalsLocked expects c.mu held for reading.
func (c *Cache) nodeTotalsLocked() NodeTotals {
	var totals NodeTotals
	var totalNanoCPUs, maxNanoCPUs int64
	for _, n := range c.nodes.items {
		switch n.Status.State {
		case swarm.NodeStateReady:
			totals.Ready++
		case swarm.NodeStateDown:
			totals.Down++
		default:
			// Unknown/disconnected nodes are not counted separately.
		}
		if n.Spec.Availability == swarm.NodeAvailabilityDrain {
			totals.Draining++
		}
		totalNanoCPUs += n.Description.Resources.NanoCPUs
		totals.TotalMemory += n.Description.Resources.MemoryBytes
		if n.Description.Resources.NanoCPUs > maxNanoCPUs {
			maxNanoCPUs = n.Description.Resources.NanoCPUs
		}
		if n.Description.Resources.MemoryBytes > totals.MaxNodeMemory {
			totals.MaxNodeMemory = n.Description.Resources.MemoryBytes
		}
	}

	totals.Count = len(c.nodes.items)
	totals.TotalCPU = int(totalNanoCPUs / 1e9)
	totals.MaxNodeCPU = int(maxNanoCPUs / 1e9)

	return totals
}

func addIndexed(by map[string]map[string]struct{}, parent, id string) {
	if parent == "" {
		return
	}
	if by[parent] == nil {
		by[parent] = make(map[string]struct{})
	}
	by[parent][id] = struct{}{}
}

func removeIndexed(by map[string]map[string]struct{}, parent, id string) {
	delete(by[parent], id)
	if len(by[parent]) == 0 {
		delete(by, parent)
	}
}

// FullSyncData holds all resource lists for an atomic full sync.
type FullSyncData struct {
	Nodes    []swarm.Node
	Services []swarm.Service
	Tasks    []swarm.Task
	Configs  []swarm.Config
	Secrets  []swarm.Secret
	Networks []network.Summary
	Volumes  []volume.Volume

	HasNodes, HasServices, HasTasks bool
	HasConfigs, HasSecrets          bool
	HasNetworks, HasVolumes         bool
}

// ReplaceAll atomically replaces resource maps for which Has* flags are true,
// then rebuilds derived state. Types that failed to sync are preserved, and a
// cached object newer than its listed copy is kept; networks and volumes carry
// no version, so their lists win outright.
func (c *Cache) ReplaceAll(data FullSyncData) {
	// Build new maps outside the lock for resource types that succeeded.
	var nodes map[string]swarm.Node
	if data.HasNodes {
		nodes = make(map[string]swarm.Node, len(data.Nodes))
		for _, n := range data.Nodes {
			nodes[n.ID] = n
		}
	}

	var services map[string]swarm.Service
	if data.HasServices {
		services = make(map[string]swarm.Service, len(data.Services))
		for _, s := range data.Services {
			services[s.ID] = s
		}
	}

	var tasks map[string]swarm.Task
	var byService map[string]map[string]struct{}
	var byNode map[string]map[string]struct{}
	if data.HasTasks {
		tasks = make(map[string]swarm.Task, len(data.Tasks))
		byService = make(map[string]map[string]struct{})
		byNode = make(map[string]map[string]struct{})
		for _, t := range data.Tasks {
			tasks[t.ID] = t
			addIndexed(byService, t.ServiceID, t.ID)
			addIndexed(byNode, t.NodeID, t.ID)
		}
	}

	var configs map[string]swarm.Config
	if data.HasConfigs {
		configs = make(map[string]swarm.Config, len(data.Configs))
		for _, cfg := range data.Configs {
			configs[cfg.ID] = cfg
		}
	}

	var secrets map[string]swarm.Secret
	if data.HasSecrets {
		secrets = make(map[string]swarm.Secret, len(data.Secrets))
		for _, s := range data.Secrets {
			s.Spec.Data = nil
			secrets[s.ID] = s
		}
	}

	var networks map[string]network.Summary
	if data.HasNetworks {
		networks = make(map[string]network.Summary, len(data.Networks))
		for _, n := range data.Networks {
			networks[n.ID] = n
		}
	}

	var volumes map[string]volume.Volume
	if data.HasVolumes {
		volumes = make(map[string]volume.Volume, len(data.Volumes))
		for _, v := range data.Volumes {
			volumes[v.Name] = v
		}
	}

	// Single atomic swap under one lock — only replace types that succeeded.
	c.mu.Lock()
	if data.HasNodes {
		c.nodes.Replace(nodes)
	}
	if data.HasServices {
		// Forget restart buckets for services no longer present, and keep a
		// dropped service whose tasks remain, mirroring DeleteService: a
		// reconnect resync can land between a `docker service rm`'s removal
		// of the service and of its tasks.
		for id, old := range c.services {
			if _, ok := services[id]; ok {
				continue
			}
			c.restarts.Forget(id)
			remaining := c.tasksByService[id]
			if data.HasTasks {
				remaining = byService[id]
			}
			if len(remaining) > 0 {
				c.goneServices[id] = old
			}
		}
		keepNewer(services, c.services, func(s swarm.Service) uint64 { return s.Version.Index })
		c.services = services
	}

	// Failures the live event stream missed: a task failed/rejected/orphaned
	// here but not in the previous snapshot. One the first sync finds that the
	// cache never held is retained history, not a failure the tracker saw, so
	// it only feeds the flapping window.
	var newFailures, backfilled []swarm.Task
	if data.HasTasks {
		for id, listed := range tasks {
			held, ok := c.tasks[id]
			if !ok || held.Version.Index <= listed.Version.Index {
				continue
			}
			removeIndexed(byService, listed.ServiceID, id)
			removeIndexed(byNode, listed.NodeID, id)
			addIndexed(byService, held.ServiceID, id)
			addIndexed(byNode, held.NodeID, id)
			tasks[id] = held
		}
		for id, t := range tasks {
			if !IsFailureState(t.Status.State) {
				continue
			}
			old, existed := c.tasks[id]
			switch {
			case existed && IsFailureState(old.Status.State):
				// Counted when it failed.
			case !existed && !c.tasksSynced:
				backfilled = append(backfilled, t)
			default:
				newFailures = append(newFailures, t)
			}
		}
		c.tasksSynced = true
		c.tasks = tasks
		c.tasksByService = byService
		c.tasksByNode = byNode
		c.pruneGoneServices()
	}
	if data.HasConfigs {
		c.configs.Replace(configs)
	}
	if data.HasSecrets {
		c.secrets.Replace(secrets)
	}
	if data.HasNetworks {
		c.networks.Replace(networks)
	}
	if data.HasVolumes {
		c.volumes.Replace(volumes)
	}

	// Rebuild derived indexes from the current (possibly partially updated) maps.
	c.rebuildStacks()
	c.serviceRef.rebuild(c.services)
	c.lastSync = time.Now()
	c.mu.Unlock()

	for _, t := range newFailures {
		c.restarts.Record(t.ServiceID, t.Status.Timestamp)
	}

	for _, t := range backfilled {
		c.restarts.Backfill(t.ServiceID, t.Status.Timestamp)
	}

	for _, typ := range [...]EventType{
		EventNode, EventService, EventTask, EventConfig, EventSecret, EventNetwork, EventVolume,
	} {
		c.recordResourceCount(typ)
	}

	c.notify(Event{Type: EventSync, Action: "full_sync", ID: ""})
}

// recordResourceCount publishes how many resources of one type the cache
// holds. Called without c.mu, which it takes for reading.
func (c *Cache) recordResourceCount(typ EventType) {
	c.mu.RLock()
	var count int
	switch typ { //nolint:exhaustive // stacks and syncs are not counted
	case EventNode:
		count = len(c.nodes.items)
	case EventService:
		count = len(c.services)
	case EventTask:
		count = len(c.tasks)
	case EventConfig:
		count = len(c.configs.items)
	case EventSecret:
		count = len(c.secrets.items)
	case EventNetwork:
		count = len(c.networks.items)
	case EventVolume:
		count = len(c.volumes.items)
	default:
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	metrics.SetCacheResources(string(typ)+"s", count)
}
