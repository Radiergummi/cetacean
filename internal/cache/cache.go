package cache

import (
	"sync"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/metrics"
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
}

type Stack struct {
	Name     string   `json:"name"`
	Services []string `json:"services"`
	Configs  []string `json:"configs"`
	Secrets  []string `json:"secrets"`
	Networks []string `json:"networks"`
	Volumes  []string `json:"volumes"`
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
	NodeCount         int            `json:"nodeCount"`
	ServiceCount      int            `json:"serviceCount"`
	TaskCount         int            `json:"taskCount"`
	StackCount        int            `json:"stackCount"`
	TasksByState      map[string]int `json:"tasksByState"`
	NodesReady        int            `json:"nodesReady"`
	NodesDown         int            `json:"nodesDown"`
	NodesDraining     int            `json:"nodesDraining"`
	TotalCPU          int            `json:"totalCPU"`
	TotalMemory       int64          `json:"totalMemory"`
	ServicesConverged int            `json:"servicesConverged"`
	ServicesDegraded  int            `json:"servicesDegraded"`
	ReservedCPU       int64          `json:"reservedCPU"`
	ReservedMemory    int64          `json:"reservedMemory"`
	MaxNodeCPU        int            `json:"maxNodeCPU"`
	MaxNodeMemory     int64          `json:"maxNodeMemory"`
	LastSync          time.Time      `json:"lastSync"`
}

type OnChangeFunc func(Event)

type Cache struct {
	mu             sync.RWMutex
	nodes          map[string]swarm.Node
	services       map[string]swarm.Service
	tasks          map[string]swarm.Task
	tasksByService map[string]map[string]struct{} // serviceID -> set of taskIDs
	tasksByNode    map[string]map[string]struct{} // nodeID -> set of taskIDs
	configs        map[string]swarm.Config
	secrets        map[string]swarm.Secret
	networks       map[string]network.Summary
	volumes        map[string]volume.Volume
	stacks         map[string]Stack
	lastSync       time.Time
	onChange       OnChangeFunc
	history        *History
	serviceRef     serviceRefIndex
}

func New(onChange OnChangeFunc) *Cache {
	return &Cache{
		nodes:          make(map[string]swarm.Node),
		services:       make(map[string]swarm.Service),
		tasks:          make(map[string]swarm.Task),
		tasksByService: make(map[string]map[string]struct{}),
		tasksByNode:    make(map[string]map[string]struct{}),
		configs:        make(map[string]swarm.Config),
		secrets:        make(map[string]swarm.Secret),
		networks:       make(map[string]network.Summary),
		volumes:        make(map[string]volume.Volume),
		stacks:         make(map[string]Stack),
		serviceRef:     newServiceRefIndex(),
		onChange:       onChange,
		history:        NewHistory(10000),
	}
}

func (c *Cache) History() *History { return c.history }

func (c *Cache) notify(e Event) {
	// Populate the Name field for SSE authorization checks.
	if e.Name == "" {
		e.Name = ExtractName(e)
	}

	// Sync events are internal bookkeeping; broadcast them to SSE clients
	// but don't record them in history where they drown out real changes.
	if e.Type != EventSync {
		e.HistoryID = c.history.Append(HistoryEntry{
			Type:       e.Type,
			Action:     e.Action,
			ResourceID: e.ID,
			Name:       e.Name,
		})
		metrics.RecordCacheMutation(string(e.Type), e.Action)
	} else {
		e.HistoryID = c.history.Count()
	}
	if c.onChange != nil {
		c.onChange(e)
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
	if cs := s.Spec.TaskTemplate.ContainerSpec; cs != nil {
		for _, c := range cs.Configs {
			r.configs[c.ConfigID] = true
		}
		for _, s := range cs.Secrets {
			r.secrets[s.SecretID] = true
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
	diffNotify := func(typ EventType, oldSet, newSet map[string]bool) {
		for id := range oldSet {
			if !newSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
		for id := range newSet {
			if !oldSet[id] {
				c.notify(Event{Type: typ, Action: "ref_changed", ID: id})
			}
		}
	}
	diffNotify(EventConfig, old.configs, new.configs)
	diffNotify(EventSecret, old.secrets, new.secrets)
	diffNotify(EventNetwork, old.networks, new.networks)
	diffNotify(EventVolume, old.volumes, new.volumes)
}

// --- Nodes ---

func (c *Cache) SetNode(n swarm.Node) {
	c.mu.Lock()
	c.nodes[n.ID] = n
	c.mu.Unlock()
	c.notify(Event{Type: EventNode, Action: "update", ID: n.ID, Resource: n})
}

func (c *Cache) GetNode(id string) (swarm.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.nodes[id]
	return n, ok
}

func (c *Cache) DeleteNode(id string) {
	c.mu.Lock()
	var name string
	if old, ok := c.nodes[id]; ok {
		name = old.Description.Hostname
	}
	delete(c.nodes, id)
	c.mu.Unlock()
	c.notify(Event{Type: EventNode, Action: "remove", ID: id, Name: name})
}

func (c *Cache) ListNodes() []swarm.Node {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]swarm.Node, 0, len(c.nodes))
	for _, n := range c.nodes {
		out = append(out, n)
	}
	return out
}

// --- Services ---

func (c *Cache) SetService(s swarm.Service) {
	newRefs := serviceRefs(s)

	c.mu.Lock()
	var oldRefs refSet
	if old, ok := c.services[s.ID]; ok {
		oldRefs = serviceRefs(old)
		c.removeFromStack(EventService, old.ID, old.Spec.Labels)
		c.serviceRef.remove(old)
	}
	c.services[s.ID] = s
	c.addToStack(EventService, s.ID, s.Spec.Labels)
	c.serviceRef.add(s)
	c.mu.Unlock()

	c.notify(Event{Type: EventService, Action: "update", ID: s.ID, Resource: s})
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
	if old, ok := c.services[id]; ok {
		name = old.Spec.Name
		oldRefs = serviceRefs(old)
		c.removeFromStack(EventService, id, old.Spec.Labels)
		c.serviceRef.remove(old)
	}
	delete(c.services, id)
	c.mu.Unlock()

	c.notify(Event{Type: EventService, Action: "remove", ID: id, Name: name})
	c.notifyRefChanges(oldRefs, refSet{})
}

func (c *Cache) ListServices() []swarm.Service {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]swarm.Service, 0, len(c.services))
	for _, s := range c.services {
		out = append(out, s)
	}
	return out
}

// --- Tasks ---

func (c *Cache) SetTask(t swarm.Task) {
	c.mu.Lock()
	changed := true
	if old, ok := c.tasks[t.ID]; ok {
		changed = old.Status.State != t.Status.State ||
			old.DesiredState != t.DesiredState ||
			old.Status.Err != t.Status.Err ||
			old.NodeID != t.NodeID ||
			old.Version != t.Version
		c.removeTaskIndex(old)
	}
	c.tasks[t.ID] = t
	c.addTaskIndex(t)
	c.mu.Unlock()
	if changed {
		c.notify(Event{Type: EventTask, Action: "update", ID: t.ID, Resource: t})
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
	if old, ok := c.tasks[id]; ok {
		c.removeTaskIndex(old)
	}
	delete(c.tasks, id)
	c.mu.Unlock()
	c.notify(Event{Type: EventTask, Action: "remove", ID: id, Name: id})
}

// addTaskIndex adds a task to the secondary indexes. Must be called with c.mu held for writing.
func (c *Cache) addTaskIndex(t swarm.Task) {
	if t.ServiceID != "" {
		if c.tasksByService[t.ServiceID] == nil {
			c.tasksByService[t.ServiceID] = make(map[string]struct{})
		}
		c.tasksByService[t.ServiceID][t.ID] = struct{}{}
	}
	if t.NodeID != "" {
		if c.tasksByNode[t.NodeID] == nil {
			c.tasksByNode[t.NodeID] = make(map[string]struct{})
		}
		c.tasksByNode[t.NodeID][t.ID] = struct{}{}
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
	defer c.mu.RUnlock()
	out := make([]swarm.Task, 0, len(c.tasks))
	for _, t := range c.tasks {
		out = append(out, t)
	}
	return out
}

// --- Configs ---

func (c *Cache) SetConfig(cfg swarm.Config) {
	c.mu.Lock()
	if old, ok := c.configs[cfg.ID]; ok {
		c.removeFromStack(EventConfig, old.ID, old.Spec.Labels)
	}
	c.configs[cfg.ID] = cfg
	c.addToStack(EventConfig, cfg.ID, cfg.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: EventConfig, Action: "update", ID: cfg.ID, Resource: cfg})
}

func (c *Cache) GetConfig(id string) (swarm.Config, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cfg, ok := c.configs[id]
	return cfg, ok
}

func (c *Cache) DeleteConfig(id string) {
	c.mu.Lock()
	var name string
	if old, ok := c.configs[id]; ok {
		name = old.Spec.Name
		c.removeFromStack(EventConfig, id, old.Spec.Labels)
	}
	delete(c.configs, id)
	c.mu.Unlock()
	c.notify(Event{Type: EventConfig, Action: "remove", ID: id, Name: name})
}

func (c *Cache) ListConfigs() []swarm.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]swarm.Config, 0, len(c.configs))
	for _, cfg := range c.configs {
		out = append(out, cfg)
	}
	return out
}

// --- Secrets ---

func (c *Cache) SetSecret(s swarm.Secret) {
	c.mu.Lock()
	if old, ok := c.secrets[s.ID]; ok {
		c.removeFromStack(EventSecret, old.ID, old.Spec.Labels)
	}
	s.Spec.Data = nil
	c.secrets[s.ID] = s
	c.addToStack(EventSecret, s.ID, s.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: EventSecret, Action: "update", ID: s.ID, Resource: s})
}

func (c *Cache) GetSecret(id string) (swarm.Secret, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.secrets[id]
	return s, ok
}

func (c *Cache) DeleteSecret(id string) {
	c.mu.Lock()
	var name string
	if old, ok := c.secrets[id]; ok {
		name = old.Spec.Name
		c.removeFromStack(EventSecret, id, old.Spec.Labels)
	}
	delete(c.secrets, id)
	c.mu.Unlock()
	c.notify(Event{Type: EventSecret, Action: "remove", ID: id, Name: name})
}

func (c *Cache) ListSecrets() []swarm.Secret {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]swarm.Secret, 0, len(c.secrets))
	for _, s := range c.secrets {
		out = append(out, s)
	}
	return out
}

// --- Networks ---

func (c *Cache) SetNetwork(n network.Summary) {
	c.mu.Lock()
	if old, ok := c.networks[n.ID]; ok {
		c.removeFromStack(EventNetwork, old.ID, old.Labels)
	}
	c.networks[n.ID] = n
	c.addToStack(EventNetwork, n.ID, n.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: EventNetwork, Action: "update", ID: n.ID, Resource: n})
}

func (c *Cache) GetNetwork(id string) (network.Summary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.networks[id]
	return n, ok
}

func (c *Cache) DeleteNetwork(id string) {
	c.mu.Lock()
	var name string
	if old, ok := c.networks[id]; ok {
		name = old.Name
		c.removeFromStack(EventNetwork, id, old.Labels)
	}
	delete(c.networks, id)
	c.mu.Unlock()
	c.notify(Event{Type: EventNetwork, Action: "remove", ID: id, Name: name})
}

func (c *Cache) ListNetworks() []network.Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]network.Summary, 0, len(c.networks))
	for _, n := range c.networks {
		out = append(out, n)
	}
	return out
}

// --- Volumes ---

func (c *Cache) SetVolume(v volume.Volume) {
	c.mu.Lock()
	if old, ok := c.volumes[v.Name]; ok {
		c.removeFromStack(EventVolume, old.Name, old.Labels)
	}
	c.volumes[v.Name] = v
	c.addToStack(EventVolume, v.Name, v.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: EventVolume, Action: "update", ID: v.Name, Resource: v})
}

func (c *Cache) GetVolume(name string) (volume.Volume, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.volumes[name]
	return v, ok
}

func (c *Cache) DeleteVolume(name string) {
	c.mu.Lock()
	if old, ok := c.volumes[name]; ok {
		c.removeFromStack(EventVolume, name, old.Labels)
	}
	delete(c.volumes, name)
	c.mu.Unlock()
	c.notify(Event{Type: EventVolume, Action: "remove", ID: name, Name: name})
}

func (c *Cache) ListVolumes() []volume.Volume {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]volume.Volume, 0, len(c.volumes))
	for _, v := range c.volumes {
		out = append(out, v)
	}
	return out
}

// --- Stacks (derived) ---

func (c *Cache) GetStack(name string) (Stack, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stacks[name]
	if !ok {
		return Stack{}, false
	}
	return Stack{
		Name:     s.Name,
		Services: append([]string{}, s.Services...),
		Configs:  append([]string{}, s.Configs...),
		Secrets:  append([]string{}, s.Secrets...),
		Networks: append([]string{}, s.Networks...),
		Volumes:  append([]string{}, s.Volumes...),
	}, true
}

func (c *Cache) ListStacks() []Stack {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Stack, 0, len(c.stacks))
	for _, s := range c.stacks {
		out = append(out, s)
	}
	return out
}

func (c *Cache) GetStackDetail(name string) (StackDetail, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stacks[name]
	if !ok {
		return StackDetail{}, false
	}
	detail := StackDetail{Name: s.Name}
	for _, id := range s.Services {
		if svc, ok := c.services[id]; ok {
			detail.Services = append(detail.Services, svc)
		}
	}
	for _, id := range s.Configs {
		if cfg, ok := c.configs[id]; ok {
			detail.Configs = append(detail.Configs, cfg)
		}
	}
	for _, id := range s.Secrets {
		if sec, ok := c.secrets[id]; ok {
			detail.Secrets = append(detail.Secrets, sec)
		}
	}
	for _, id := range s.Networks {
		if net, ok := c.networks[id]; ok {
			detail.Networks = append(detail.Networks, net)
		}
	}
	for _, name := range s.Volumes {
		if vol, ok := c.volumes[name]; ok {
			detail.Volumes = append(detail.Volumes, vol)
		}
	}
	for i := range detail.Secrets {
		detail.Secrets[i].Spec.Data = nil
	}
	return detail, true
}

func (c *Cache) ListStackSummaries() []StackSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

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

			// Desired replicas
			replicas := 1
			if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				replicas = int(*svc.Spec.Mode.Replicated.Replicas)
			} else if svc.Spec.Mode.Global != nil {
				replicas = len(c.nodes)
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
	return out
}

// --- Filtered task lists ---

func (c *Cache) ListTasksByService(serviceID string) []swarm.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := c.tasksByService[serviceID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

// RunningTaskCount returns the number of tasks in "running" state for a service.
func (c *Cache) RunningTaskCount(serviceID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for id := range c.tasksByService[serviceID] {
		if t, ok := c.tasks[id]; ok && t.Status.State == swarm.TaskStateRunning {
			count++
		}
	}
	return count
}

func (c *Cache) ListTasksByNode(nodeID string) []swarm.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := c.tasksByNode[nodeID]
	out := make([]swarm.Task, 0, len(ids))
	for id := range ids {
		if t, ok := c.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

// --- Cross-references ---

// ServicesUsingConfig returns services that reference the given config ID.
func (c *Cache) ServicesUsingConfig(configID string) []ServiceRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceRef.lookup(c.serviceRef.byConfig, configID, c.services)
}

// ServicesUsingSecret returns services that reference the given secret ID.
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

	tasksByState := make(map[string]int)
	for _, t := range c.tasks {
		tasksByState[string(t.Status.State)]++
	}

	var nodesReady, nodesDown, nodesDraining int
	var totalNanoCPUs int64
	var totalMemory int64
	var maxNanoCPUs int64
	var maxMemory int64
	for _, n := range c.nodes {
		switch n.Status.State {
		case swarm.NodeStateReady:
			nodesReady++
		case swarm.NodeStateDown:
			nodesDown++
		default:
			// Unknown/disconnected nodes are not counted separately.
		}
		if n.Spec.Availability == swarm.NodeAvailabilityDrain {
			nodesDraining++
		}
		totalNanoCPUs += n.Description.Resources.NanoCPUs
		totalMemory += n.Description.Resources.MemoryBytes
		if n.Description.Resources.NanoCPUs > maxNanoCPUs {
			maxNanoCPUs = n.Description.Resources.NanoCPUs
		}
		if n.Description.Resources.MemoryBytes > maxMemory {
			maxMemory = n.Description.Resources.MemoryBytes
		}
	}

	// Count running tasks per service
	runningByService := make(map[string]int, len(c.services))
	for _, t := range c.tasks {
		if t.Status.State == swarm.TaskStateRunning {
			runningByService[t.ServiceID]++
		}
	}

	var servicesConverged, servicesDegraded int
	var reservedCPU, reservedMemory int64
	for _, svc := range c.services {
		if svc.Spec.Mode.Global != nil {
			servicesConverged++
		} else if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			desired := int(*svc.Spec.Mode.Replicated.Replicas)
			running := runningByService[svc.ID]
			if running >= desired {
				servicesConverged++
			} else {
				servicesDegraded++
			}
			if res := svc.Spec.TaskTemplate.Resources; res != nil && res.Reservations != nil {
				reservedCPU += res.Reservations.NanoCPUs * int64(desired)
				reservedMemory += res.Reservations.MemoryBytes * int64(desired)
			}
		}
	}

	return ClusterSnapshot{
		NodeCount:         len(c.nodes),
		ServiceCount:      len(c.services),
		TaskCount:         len(c.tasks),
		StackCount:        len(c.stacks),
		TasksByState:      tasksByState,
		NodesReady:        nodesReady,
		NodesDown:         nodesDown,
		NodesDraining:     nodesDraining,
		TotalCPU:          int(totalNanoCPUs / 1e9),
		TotalMemory:       totalMemory,
		ServicesConverged: servicesConverged,
		ServicesDegraded:  servicesDegraded,
		ReservedCPU:       reservedCPU,
		ReservedMemory:    reservedMemory,
		MaxNodeCPU:        int(maxNanoCPUs / 1e9),
		MaxNodeMemory:     maxMemory,
		LastSync:          c.lastSync,
	}
}

// --- Bulk replace (for full sync) ---

func (c *Cache) replaceNodes(nodes []swarm.Node) {
	m := make(map[string]swarm.Node, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	c.mu.Lock()
	c.nodes = m
	c.mu.Unlock()
}

func (c *Cache) replaceServices(services []swarm.Service) {
	m := make(map[string]swarm.Service, len(services))
	for _, s := range services {
		m[s.ID] = s
	}
	c.mu.Lock()
	c.services = m
	c.serviceRef.rebuild(c.services)
	c.mu.Unlock()
}

func (c *Cache) replaceTasks(tasks []swarm.Task) {
	m := make(map[string]swarm.Task, len(tasks))
	byService := make(map[string]map[string]struct{})
	byNode := make(map[string]map[string]struct{})
	for _, t := range tasks {
		m[t.ID] = t
		if t.ServiceID != "" {
			if byService[t.ServiceID] == nil {
				byService[t.ServiceID] = make(map[string]struct{})
			}
			byService[t.ServiceID][t.ID] = struct{}{}
		}
		if t.NodeID != "" {
			if byNode[t.NodeID] == nil {
				byNode[t.NodeID] = make(map[string]struct{})
			}
			byNode[t.NodeID][t.ID] = struct{}{}
		}
	}
	c.mu.Lock()
	c.tasks = m
	c.tasksByService = byService
	c.tasksByNode = byNode
	c.mu.Unlock()
}

func (c *Cache) replaceConfigs(configs []swarm.Config) {
	m := make(map[string]swarm.Config, len(configs))
	for _, cfg := range configs {
		m[cfg.ID] = cfg
	}
	c.mu.Lock()
	c.configs = m
	c.mu.Unlock()
}

func (c *Cache) replaceSecrets(secrets []swarm.Secret) {
	m := make(map[string]swarm.Secret, len(secrets))
	for _, s := range secrets {
		s.Spec.Data = nil
		m[s.ID] = s
	}
	c.mu.Lock()
	c.secrets = m
	c.mu.Unlock()
}

func (c *Cache) replaceNetworks(networks []network.Summary) {
	m := make(map[string]network.Summary, len(networks))
	for _, n := range networks {
		m[n.ID] = n
	}
	c.mu.Lock()
	c.networks = m
	c.mu.Unlock()
}

func (c *Cache) replaceVolumes(volumes []volume.Volume) {
	m := make(map[string]volume.Volume, len(volumes))
	for _, v := range volumes {
		m[v.Name] = v
	}
	c.mu.Lock()
	c.volumes = m
	c.mu.Unlock()
}

// rebuildStacksSynced acquires the lock and rebuilds derived stack data.
// Used by tests after calling individual replace* methods.
func (c *Cache) rebuildStacksSynced() {
	c.mu.Lock()
	c.rebuildStacks()
	c.mu.Unlock()
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
// then rebuilds derived state. Resource types that failed to sync (Has* = false)
// are preserved from the existing cache.
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
			if t.ServiceID != "" {
				if byService[t.ServiceID] == nil {
					byService[t.ServiceID] = make(map[string]struct{})
				}
				byService[t.ServiceID][t.ID] = struct{}{}
			}
			if t.NodeID != "" {
				if byNode[t.NodeID] == nil {
					byNode[t.NodeID] = make(map[string]struct{})
				}
				byNode[t.NodeID][t.ID] = struct{}{}
			}
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
		c.nodes = nodes
	}
	if data.HasServices {
		c.services = services
	}
	if data.HasTasks {
		c.tasks = tasks
		c.tasksByService = byService
		c.tasksByNode = byNode
	}
	if data.HasConfigs {
		c.configs = configs
	}
	if data.HasSecrets {
		c.secrets = secrets
	}
	if data.HasNetworks {
		c.networks = networks
	}
	if data.HasVolumes {
		c.volumes = volumes
	}

	// Rebuild derived indexes from the current (possibly partially updated) maps.
	c.rebuildStacks()
	c.serviceRef.rebuild(c.services)
	c.lastSync = time.Now()
	c.mu.Unlock()

	if data.HasNodes {
		metrics.SetCacheResources("nodes", len(data.Nodes))
	}
	if data.HasServices {
		metrics.SetCacheResources("services", len(data.Services))
	}
	if data.HasTasks {
		metrics.SetCacheResources("tasks", len(data.Tasks))
	}
	if data.HasConfigs {
		metrics.SetCacheResources("configs", len(data.Configs))
	}
	if data.HasSecrets {
		metrics.SetCacheResources("secrets", len(data.Secrets))
	}
	if data.HasNetworks {
		metrics.SetCacheResources("networks", len(data.Networks))
	}
	if data.HasVolumes {
		metrics.SetCacheResources("volumes", len(data.Volumes))
	}

	c.notify(Event{Type: EventSync, Action: "full_sync", ID: ""})
}
