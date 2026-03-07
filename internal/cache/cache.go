package cache

import (
	"sync"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

type Event struct {
	Type     string      `json:"type"`
	Action   string      `json:"action"`
	ID       string      `json:"id"`
	Resource interface{} `json:"resource,omitempty"`
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

type ClusterSnapshot struct {
	NodeCount    int            `json:"nodeCount"`
	ServiceCount int            `json:"serviceCount"`
	TaskCount    int            `json:"taskCount"`
	StackCount   int            `json:"stackCount"`
	TasksByState map[string]int `json:"tasksByState"`
	NodesReady   int            `json:"nodesReady"`
	NodesDown    int            `json:"nodesDown"`
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
	onChange       OnChangeFunc
	history        *History
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
		onChange:       onChange,
		history:        NewHistory(10000),
	}
}

func (c *Cache) History() *History { return c.history }

func (c *Cache) notify(e Event) {
	c.history.Append(HistoryEntry{
		Type:       e.Type,
		Action:     e.Action,
		ResourceID: e.ID,
		Name:       extractName(e),
	})
	if c.onChange != nil {
		c.onChange(e)
	}
}

func extractName(e Event) string {
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

// --- Nodes ---

func (c *Cache) SetNode(n swarm.Node) {
	c.mu.Lock()
	c.nodes[n.ID] = n
	c.mu.Unlock()
	c.notify(Event{Type: "node", Action: "update", ID: n.ID, Resource: n})
}

func (c *Cache) GetNode(id string) (swarm.Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.nodes[id]
	return n, ok
}

func (c *Cache) DeleteNode(id string) {
	c.mu.Lock()
	delete(c.nodes, id)
	c.mu.Unlock()
	c.notify(Event{Type: "node", Action: "remove", ID: id})
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
	c.mu.Lock()
	if old, ok := c.services[s.ID]; ok {
		c.removeFromStack("service", old.ID, old.Spec.Labels)
	}
	c.services[s.ID] = s
	c.addToStack("service", s.ID, s.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "service", Action: "update", ID: s.ID, Resource: s})
}

func (c *Cache) GetService(id string) (swarm.Service, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.services[id]
	return s, ok
}

func (c *Cache) DeleteService(id string) {
	c.mu.Lock()
	if old, ok := c.services[id]; ok {
		c.removeFromStack("service", id, old.Spec.Labels)
	}
	delete(c.services, id)
	c.mu.Unlock()
	c.notify(Event{Type: "service", Action: "remove", ID: id})
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
	if old, ok := c.tasks[t.ID]; ok {
		c.removeTaskIndex(old)
	}
	c.tasks[t.ID] = t
	c.addTaskIndex(t)
	c.mu.Unlock()
	c.notify(Event{Type: "task", Action: "update", ID: t.ID, Resource: t})
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
	c.notify(Event{Type: "task", Action: "remove", ID: id})
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
		c.removeFromStack("config", old.ID, old.Spec.Labels)
	}
	c.configs[cfg.ID] = cfg
	c.addToStack("config", cfg.ID, cfg.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "config", Action: "update", ID: cfg.ID, Resource: cfg})
}

func (c *Cache) GetConfig(id string) (swarm.Config, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cfg, ok := c.configs[id]
	return cfg, ok
}

func (c *Cache) DeleteConfig(id string) {
	c.mu.Lock()
	if old, ok := c.configs[id]; ok {
		c.removeFromStack("config", id, old.Spec.Labels)
	}
	delete(c.configs, id)
	c.mu.Unlock()
	c.notify(Event{Type: "config", Action: "remove", ID: id})
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
		c.removeFromStack("secret", old.ID, old.Spec.Labels)
	}
	c.secrets[s.ID] = s
	c.addToStack("secret", s.ID, s.Spec.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "secret", Action: "update", ID: s.ID, Resource: s})
}

func (c *Cache) GetSecret(id string) (swarm.Secret, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.secrets[id]
	return s, ok
}

func (c *Cache) DeleteSecret(id string) {
	c.mu.Lock()
	if old, ok := c.secrets[id]; ok {
		c.removeFromStack("secret", id, old.Spec.Labels)
	}
	delete(c.secrets, id)
	c.mu.Unlock()
	c.notify(Event{Type: "secret", Action: "remove", ID: id})
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
		c.removeFromStack("network", old.ID, old.Labels)
	}
	c.networks[n.ID] = n
	c.addToStack("network", n.ID, n.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "network", Action: "update", ID: n.ID, Resource: n})
}

func (c *Cache) GetNetwork(id string) (network.Summary, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.networks[id]
	return n, ok
}

func (c *Cache) DeleteNetwork(id string) {
	c.mu.Lock()
	if old, ok := c.networks[id]; ok {
		c.removeFromStack("network", id, old.Labels)
	}
	delete(c.networks, id)
	c.mu.Unlock()
	c.notify(Event{Type: "network", Action: "remove", ID: id})
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
		c.removeFromStack("volume", old.Name, old.Labels)
	}
	c.volumes[v.Name] = v
	c.addToStack("volume", v.Name, v.Labels)
	c.mu.Unlock()
	c.notify(Event{Type: "volume", Action: "update", ID: v.Name, Resource: v})
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
		c.removeFromStack("volume", name, old.Labels)
	}
	delete(c.volumes, name)
	c.mu.Unlock()
	c.notify(Event{Type: "volume", Action: "remove", ID: name})
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
	return s, ok
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
	return detail, true
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

// --- Snapshot ---

func (c *Cache) Snapshot() ClusterSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tasksByState := make(map[string]int)
	for _, t := range c.tasks {
		tasksByState[string(t.Status.State)]++
	}

	var nodesReady, nodesDown int
	for _, n := range c.nodes {
		switch n.Status.State {
		case swarm.NodeStateReady:
			nodesReady++
		case swarm.NodeStateDown:
			nodesDown++
		}
	}

	return ClusterSnapshot{
		NodeCount:    len(c.nodes),
		ServiceCount: len(c.services),
		TaskCount:    len(c.tasks),
		StackCount:   len(c.stacks),
		TasksByState: tasksByState,
		NodesReady:   nodesReady,
		NodesDown:    nodesDown,
	}
}

// --- Bulk replace (for full sync) ---

func (c *Cache) ReplaceNodes(nodes []swarm.Node) {
	m := make(map[string]swarm.Node, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n
	}
	c.mu.Lock()
	c.nodes = m
	c.mu.Unlock()
}

func (c *Cache) ReplaceServices(services []swarm.Service) {
	m := make(map[string]swarm.Service, len(services))
	for _, s := range services {
		m[s.ID] = s
	}
	c.mu.Lock()
	c.services = m
	c.mu.Unlock()
}

func (c *Cache) ReplaceTasks(tasks []swarm.Task) {
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

func (c *Cache) ReplaceConfigs(configs []swarm.Config) {
	m := make(map[string]swarm.Config, len(configs))
	for _, cfg := range configs {
		m[cfg.ID] = cfg
	}
	c.mu.Lock()
	c.configs = m
	c.mu.Unlock()
}

func (c *Cache) ReplaceSecrets(secrets []swarm.Secret) {
	m := make(map[string]swarm.Secret, len(secrets))
	for _, s := range secrets {
		m[s.ID] = s
	}
	c.mu.Lock()
	c.secrets = m
	c.mu.Unlock()
}

func (c *Cache) ReplaceNetworks(networks []network.Summary) {
	m := make(map[string]network.Summary, len(networks))
	for _, n := range networks {
		m[n.ID] = n
	}
	c.mu.Lock()
	c.networks = m
	c.mu.Unlock()
}

func (c *Cache) ReplaceVolumes(volumes []volume.Volume) {
	m := make(map[string]volume.Volume, len(volumes))
	for _, v := range volumes {
		m[v.Name] = v
	}
	c.mu.Lock()
	c.volumes = m
	c.mu.Unlock()
}

// RebuildStacks rebuilds all derived stack data from the current resource maps.
// Call this once after all Replace* calls complete during a full sync.
func (c *Cache) RebuildStacks() {
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
}

// ReplaceAll atomically replaces all resource maps and rebuilds derived state.
// This ensures API consumers never see a half-synced cache.
func (c *Cache) ReplaceAll(data FullSyncData) {
	// Build all maps outside the lock.
	nodes := make(map[string]swarm.Node, len(data.Nodes))
	for _, n := range data.Nodes {
		nodes[n.ID] = n
	}

	services := make(map[string]swarm.Service, len(data.Services))
	for _, s := range data.Services {
		services[s.ID] = s
	}

	tasks := make(map[string]swarm.Task, len(data.Tasks))
	byService := make(map[string]map[string]struct{})
	byNode := make(map[string]map[string]struct{})
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

	configs := make(map[string]swarm.Config, len(data.Configs))
	for _, cfg := range data.Configs {
		configs[cfg.ID] = cfg
	}

	secrets := make(map[string]swarm.Secret, len(data.Secrets))
	for _, s := range data.Secrets {
		secrets[s.ID] = s
	}

	networks := make(map[string]network.Summary, len(data.Networks))
	for _, n := range data.Networks {
		networks[n.ID] = n
	}

	volumes := make(map[string]volume.Volume, len(data.Volumes))
	for _, v := range data.Volumes {
		volumes[v.Name] = v
	}

	stacks := rebuildStacksFromMaps(services, configs, secrets, networks, volumes)

	// Single atomic swap under one lock.
	c.mu.Lock()
	c.nodes = nodes
	c.services = services
	c.tasks = tasks
	c.tasksByService = byService
	c.tasksByNode = byNode
	c.configs = configs
	c.secrets = secrets
	c.networks = networks
	c.volumes = volumes
	c.stacks = stacks
	c.mu.Unlock()
}
