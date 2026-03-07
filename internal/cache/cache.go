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

type ClusterSnapshot struct {
	NodeCount    int `json:"nodeCount"`
	ServiceCount int `json:"serviceCount"`
	TaskCount    int `json:"taskCount"`
	StackCount   int `json:"stackCount"`
}

type OnChangeFunc func(Event)

type Cache struct {
	mu       sync.RWMutex
	nodes    map[string]swarm.Node
	services map[string]swarm.Service
	tasks    map[string]swarm.Task
	configs  map[string]swarm.Config
	secrets  map[string]swarm.Secret
	networks map[string]network.Summary
	volumes  map[string]volume.Volume
	stacks   map[string]Stack
	onChange OnChangeFunc
}

func New(onChange OnChangeFunc) *Cache {
	return &Cache{
		nodes:    make(map[string]swarm.Node),
		services: make(map[string]swarm.Service),
		tasks:    make(map[string]swarm.Task),
		configs:  make(map[string]swarm.Config),
		secrets:  make(map[string]swarm.Secret),
		networks: make(map[string]network.Summary),
		volumes:  make(map[string]volume.Volume),
		stacks:   make(map[string]Stack),
		onChange: onChange,
	}
}

func (c *Cache) notify(e Event) {
	if c.onChange != nil {
		c.onChange(e)
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
	c.services[s.ID] = s
	c.rebuildStacks()
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
	delete(c.services, id)
	c.rebuildStacks()
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
	c.tasks[t.ID] = t
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
	delete(c.tasks, id)
	c.mu.Unlock()
	c.notify(Event{Type: "task", Action: "remove", ID: id})
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
	c.configs[cfg.ID] = cfg
	c.rebuildStacks()
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
	delete(c.configs, id)
	c.rebuildStacks()
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
	c.secrets[s.ID] = s
	c.rebuildStacks()
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
	delete(c.secrets, id)
	c.rebuildStacks()
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
	c.networks[n.ID] = n
	c.rebuildStacks()
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
	delete(c.networks, id)
	c.rebuildStacks()
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
	c.volumes[v.Name] = v
	c.rebuildStacks()
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
	delete(c.volumes, name)
	c.rebuildStacks()
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

// rebuildStacks must be called with c.mu held for writing.
func (c *Cache) rebuildStacks() {
	stacks := make(map[string]*Stack)

	ensure := func(name string) *Stack {
		if s, ok := stacks[name]; ok {
			return s
		}
		s := &Stack{Name: name}
		stacks[name] = s
		return s
	}

	for id, svc := range c.services {
		if ns, ok := svc.Spec.Labels["com.docker.stack.namespace"]; ok {
			s := ensure(ns)
			s.Services = append(s.Services, id)
		}
	}

	for id, cfg := range c.configs {
		if ns, ok := cfg.Spec.Labels["com.docker.stack.namespace"]; ok {
			s := ensure(ns)
			s.Configs = append(s.Configs, id)
		}
	}

	for id, sec := range c.secrets {
		if ns, ok := sec.Spec.Labels["com.docker.stack.namespace"]; ok {
			s := ensure(ns)
			s.Secrets = append(s.Secrets, id)
		}
	}

	for id, net := range c.networks {
		if ns, ok := net.Labels["com.docker.stack.namespace"]; ok {
			s := ensure(ns)
			s.Networks = append(s.Networks, id)
		}
	}

	for name, vol := range c.volumes {
		if ns, ok := vol.Labels["com.docker.stack.namespace"]; ok {
			s := ensure(ns)
			s.Volumes = append(s.Volumes, name)
		}
	}

	result := make(map[string]Stack, len(stacks))
	for name, s := range stacks {
		result[name] = *s
	}
	c.stacks = result
}

// --- Snapshot ---

func (c *Cache) Snapshot() ClusterSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ClusterSnapshot{
		NodeCount:    len(c.nodes),
		ServiceCount: len(c.services),
		TaskCount:    len(c.tasks),
		StackCount:   len(c.stacks),
	}
}
