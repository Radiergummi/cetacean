# Cetacean Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a read-only Docker Swarm observability platform — single Go binary with embedded React SPA, event-driven state cache, Prometheus metrics, SSE real-time updates.

**Architecture:** Monolithic Go server. Docker watcher populates an in-memory cache via event stream. REST API serves cache data. SSE broadcasts changes to the frontend. Prometheus proxy forwards PromQL queries. React SPA embedded via `embed.FS`.

**Tech Stack:** Go 1.25 (stdlib `net/http`), Docker Engine API (`github.com/docker/docker`), React 19 + TypeScript + Vite, Tailwind CSS + shadcn/ui, uPlot for charts.

**Design doc:** `docs/plans/2026-03-07-cetacean-design.md`

---

### Task 1: Project Scaffolding — Go Backend

**Files:**
- Create: `cmd/cetacean/main.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `go.mod`
- Delete: `main.go` (GoLand template)

**Step 1: Delete the template main.go**

```bash
rm main.go
```

**Step 2: Write config test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("CETACEAN_DOCKER_HOST")
	os.Unsetenv("CETACEAN_PROMETHEUS_URL")
	os.Unsetenv("CETACEAN_LISTEN_ADDR")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when CETACEAN_PROMETHEUS_URL is not set")
	}
}

func TestLoad_WithRequiredEnv(t *testing.T) {
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prometheus:9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("expected default docker host, got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prometheus:9090" {
		t.Errorf("expected prometheus URL, got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":9000" {
		t.Errorf("expected default listen addr, got %s", cfg.ListenAddr)
	}
}

func TestLoad_AllEnvVars(t *testing.T) {
	t.Setenv("CETACEAN_DOCKER_HOST", "tcp://remote:2375")
	t.Setenv("CETACEAN_PROMETHEUS_URL", "http://prom:9090")
	t.Setenv("CETACEAN_LISTEN_ADDR", ":8080")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DockerHost != "tcp://remote:2375" {
		t.Errorf("got %s", cfg.DockerHost)
	}
	if cfg.PrometheusURL != "http://prom:9090" {
		t.Errorf("got %s", cfg.PrometheusURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("got %s", cfg.ListenAddr)
	}
}
```

**Step 3: Run test to verify it fails**

```bash
cd /Users/moritz/GolandProjects/cetacean && go test ./internal/config/...
```

Expected: FAIL — package doesn't exist yet.

**Step 4: Write config implementation**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DockerHost    string
	PrometheusURL string
	ListenAddr    string
}

func Load() (*Config, error) {
	cfg := &Config{
		DockerHost: envOr("CETACEAN_DOCKER_HOST", "unix:///var/run/docker.sock"),
		PrometheusURL: os.Getenv("CETACEAN_PROMETHEUS_URL"),
		ListenAddr: envOr("CETACEAN_LISTEN_ADDR", ":9000"),
	}

	if cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("CETACEAN_PROMETHEUS_URL is required")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: PASS (3 tests).

**Step 6: Write entrypoint**

Create `cmd/cetacean/main.go`:

```go
package main

import (
	"log"
	"os"

	"cetacean/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("cetacean starting on %s", cfg.ListenAddr)
	log.Printf("docker: %s", cfg.DockerHost)
	log.Printf("prometheus: %s", cfg.PrometheusURL)

	// Components will be wired here in subsequent tasks.
	_ = cfg
	os.Exit(0)
}
```

**Step 7: Verify it compiles and runs**

```bash
CETACEAN_PROMETHEUS_URL=http://localhost:9090 go run ./cmd/cetacean/
```

Expected: prints config values and exits.

**Step 8: Commit**

```bash
git add -A && git commit -m "feat: project scaffolding with config loading"
```

---

### Task 2: State Cache

**Files:**
- Create: `internal/cache/cache.go`
- Create: `internal/cache/cache_test.go`

**Context:** The cache stores all Swarm resource types in thread-safe maps. Stacks are derived from `com.docker.stack.namespace` labels. The cache notifies listeners on mutation via a callback.

**Step 1: Add Docker dependency**

```bash
go get github.com/docker/docker@latest
```

Note: The Docker client library types (`swarm.Node`, `swarm.Service`, etc.) are used as cache value types. We store them as-is from the Docker API.

**Step 2: Write cache tests**

Create `internal/cache/cache_test.go`:

```go
package cache

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestCache_SetGetNode(t *testing.T) {
	c := New(nil)
	node := swarm.Node{ID: "node1"}
	node.Description.Hostname = "host1"

	c.SetNode(node)

	got, ok := c.GetNode("node1")
	if !ok {
		t.Fatal("expected node to exist")
	}
	if got.Description.Hostname != "host1" {
		t.Errorf("expected host1, got %s", got.Description.Hostname)
	}
}

func TestCache_DeleteNode(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	c.DeleteNode("node1")

	_, ok := c.GetNode("node1")
	if ok {
		t.Fatal("expected node to be deleted")
	}
}

func TestCache_ListNodes(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})

	nodes := c.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestCache_SetService_DerivedStack(t *testing.T) {
	c := New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "mystack_web"
	svc.Spec.Labels = map[string]string{
		"com.docker.stack.namespace": "mystack",
	}

	c.SetService(svc)

	stack, ok := c.GetStack("mystack")
	if !ok {
		t.Fatal("expected stack to be derived")
	}
	if len(stack.Services) != 1 || stack.Services[0] != "svc1" {
		t.Errorf("expected stack to contain svc1, got %v", stack.Services)
	}
}

func TestCache_OnChange_Called(t *testing.T) {
	var called bool
	var gotEvent Event
	c := New(func(e Event) {
		called = true
		gotEvent = e
	})

	c.SetNode(swarm.Node{ID: "node1"})

	if !called {
		t.Fatal("expected onChange to be called")
	}
	if gotEvent.Type != "node" || gotEvent.Action != "update" || gotEvent.ID != "node1" {
		t.Errorf("unexpected event: %+v", gotEvent)
	}
}

func TestCache_Snapshot(t *testing.T) {
	c := New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})

	snap := c.Snapshot()
	if snap.NodeCount != 1 {
		t.Errorf("expected 1 node, got %d", snap.NodeCount)
	}
	if snap.ServiceCount != 1 {
		t.Errorf("expected 1 service, got %d", snap.ServiceCount)
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/cache/... -v
```

Expected: FAIL — package doesn't exist.

**Step 4: Write cache implementation**

Create `internal/cache/cache.go`:

```go
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
```

**Step 5: Run tests**

```bash
go test ./internal/cache/... -v
```

Expected: PASS (all 6 tests).

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: thread-safe in-memory state cache with derived stacks"
```

---

### Task 3: Docker Client Wrapper

**Files:**
- Create: `internal/docker/client.go`

**Context:** Thin wrapper around the Docker client that exposes the specific API calls we need. This is not heavily unit-tested — it's a thin integration layer. We test it indirectly via the watcher.

**Step 1: Write the client wrapper**

Create `internal/docker/client.go`:

```go
package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type Client struct {
	docker *client.Client
}

func NewClient(host string) (*Client, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{docker: c}, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	return c.docker.NodeList(ctx, types.NodeListOptions{})
}

func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return c.docker.ServiceList(ctx, types.ServiceListOptions{})
}

func (c *Client) ListTasks(ctx context.Context) ([]swarm.Task, error) {
	return c.docker.TaskList(ctx, types.TaskListOptions{})
}

func (c *Client) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	return c.docker.ConfigList(ctx, types.ConfigListOptions{})
}

func (c *Client) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	return c.docker.SecretList(ctx, types.SecretListOptions{})
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	return c.docker.NetworkList(ctx, network.ListOptions{})
}

func (c *Client) ListVolumes(ctx context.Context) ([]volume.Volume, error) {
	resp, err := c.docker.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]volume.Volume, len(resp.Volumes))
	for i, v := range resp.Volumes {
		out[i] = *v
	}
	return out, nil
}

func (c *Client) InspectNode(ctx context.Context, id string) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	return node, err
}

func (c *Client) InspectService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, types.ServiceInspectOptions{})
	return svc, err
}

func (c *Client) InspectTask(ctx context.Context, id string) (swarm.Task, error) {
	// Docker has no TaskInspect — filter task list by ID.
	tasks, err := c.docker.TaskList(ctx, types.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("id", id)),
	})
	if err != nil {
		return swarm.Task{}, err
	}
	if len(tasks) == 0 {
		return swarm.Task{}, fmt.Errorf("task %s not found", id)
	}
	return tasks[0], nil
}

func (c *Client) InspectConfig(ctx context.Context, id string) (swarm.Config, error) {
	cfg, _, err := c.docker.ConfigInspectWithRaw(ctx, id)
	return cfg, err
}

func (c *Client) InspectSecret(ctx context.Context, id string) (swarm.Secret, error) {
	sec, _, err := c.docker.SecretInspectWithRaw(ctx, id)
	return sec, err
}

func (c *Client) InspectNetwork(ctx context.Context, id string) (network.Summary, error) {
	resp, err := c.docker.NetworkInspect(ctx, id, network.InspectOptions{})
	if err != nil {
		return network.Summary{}, err
	}
	return network.Summary{
		ID:     resp.ID,
		Name:   resp.Name,
		Driver: resp.Driver,
		Scope:  resp.Scope,
		Labels: resp.Labels,
	}, nil
}

func (c *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	return c.docker.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", string(events.ServiceEventType)),
			filters.Arg("type", string(events.NodeEventType)),
			filters.Arg("type", string(events.SecretEventType)),
			filters.Arg("type", string(events.ConfigEventType)),
			filters.Arg("type", string(events.NetworkEventType)),
			filters.Arg("type", string(events.ContainerEventType)),
		),
	})
}

func (c *Client) ContainerInspect(ctx context.Context, id string) (containertypes.InspectResponse, error) {
	return c.docker.ContainerInspect(ctx, id)
}
```

Note: The exact Docker client API may need minor adjustments based on the version of `github.com/docker/docker` resolved. Adjust import paths and type names as needed during implementation — the Docker library has evolved its package layout. Use `go doc` or IDE assistance to verify correct types.

**Step 2: Verify it compiles**

```bash
go build ./internal/docker/...
```

Expected: compiles without error.

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: Docker client wrapper for Swarm API"
```

---

### Task 4: Docker Watcher

**Files:**
- Create: `internal/docker/watcher.go`

**Context:** The watcher performs a full sync on startup, then subscribes to Docker events to keep the cache updated. On disconnect, it reconnects and re-syncs. A periodic full re-sync every 5 minutes acts as a safety net.

**Step 1: Write the watcher**

Create `internal/docker/watcher.go`:

```go
package docker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"

	"cetacean/internal/cache"
)

type Watcher struct {
	client   *Client
	cache    *cache.Cache
	syncOnce sync.Once
	ready    chan struct{}
}

func NewWatcher(client *Client, cache *cache.Cache) *Watcher {
	return &Watcher{
		client: client,
		cache:  cache,
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
				log.Println("periodic full re-sync")
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
		log.Println("event stream disconnected, reconnecting in 1s...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
		log.Println("re-syncing after reconnect")
		w.fullSync(ctx)
	}
}

func (w *Watcher) fullSync(ctx context.Context) {
	log.Println("starting full sync")

	type result struct {
		name string
		err  error
	}
	ch := make(chan result, 7)

	go func() {
		nodes, err := w.client.ListNodes(ctx)
		if err == nil {
			for _, n := range nodes {
				w.cache.SetNode(n)
			}
		}
		ch <- result{"nodes", err}
	}()

	go func() {
		services, err := w.client.ListServices(ctx)
		if err == nil {
			for _, s := range services {
				w.cache.SetService(s)
			}
		}
		ch <- result{"services", err}
	}()

	go func() {
		tasks, err := w.client.ListTasks(ctx)
		if err == nil {
			for _, t := range tasks {
				w.cache.SetTask(t)
			}
		}
		ch <- result{"tasks", err}
	}()

	go func() {
		configs, err := w.client.ListConfigs(ctx)
		if err == nil {
			for _, c := range configs {
				w.cache.SetConfig(c)
			}
		}
		ch <- result{"configs", err}
	}()

	go func() {
		secrets, err := w.client.ListSecrets(ctx)
		if err == nil {
			for _, s := range secrets {
				w.cache.SetSecret(s)
			}
		}
		ch <- result{"secrets", err}
	}()

	go func() {
		networks, err := w.client.ListNetworks(ctx)
		if err == nil {
			for _, n := range networks {
				w.cache.SetNetwork(n)
			}
		}
		ch <- result{"networks", err}
	}()

	go func() {
		volumes, err := w.client.ListVolumes(ctx)
		if err == nil {
			for _, v := range volumes {
				w.cache.SetVolume(v)
			}
		}
		ch <- result{"volumes", err}
	}()

	for i := 0; i < 7; i++ {
		r := <-ch
		if r.err != nil {
			log.Printf("full sync %s failed: %v", r.name, r.err)
		}
	}

	snap := w.cache.Snapshot()
	log.Printf("full sync complete: %d nodes, %d services, %d tasks, %d stacks",
		snap.NodeCount, snap.ServiceCount, snap.TaskCount, snap.StackCount)
}

func (w *Watcher) watchEvents(ctx context.Context) {
	msgCh, errCh := w.client.Events(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errCh:
			if err != nil {
				log.Printf("event stream error: %v", err)
			}
			return
		case msg := <-msgCh:
			w.handleEvent(ctx, msg)
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, msg events.Message) {
	switch msg.Type {
	case events.NodeEventType:
		if msg.Action == "remove" {
			w.cache.DeleteNode(msg.Actor.ID)
		} else {
			node, err := w.client.InspectNode(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect node %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetNode(node)
		}

	case events.ServiceEventType:
		if msg.Action == "remove" {
			w.cache.DeleteService(msg.Actor.ID)
		} else {
			svc, err := w.client.InspectService(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect service %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetService(svc)
		}

	case events.ConfigEventType:
		if msg.Action == "remove" {
			w.cache.DeleteConfig(msg.Actor.ID)
		} else {
			cfg, err := w.client.InspectConfig(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect config %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetConfig(cfg)
		}

	case events.SecretEventType:
		if msg.Action == "remove" {
			w.cache.DeleteSecret(msg.Actor.ID)
		} else {
			sec, err := w.client.InspectSecret(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect secret %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetSecret(sec)
		}

	case events.NetworkEventType:
		if msg.Action == "remove" || msg.Action == "destroy" {
			w.cache.DeleteNetwork(msg.Actor.ID)
		} else {
			net, err := w.client.InspectNetwork(ctx, msg.Actor.ID)
			if err != nil {
				log.Printf("inspect network %s failed: %v", msg.Actor.ID, err)
				return
			}
			w.cache.SetNetwork(net)
		}

	case events.ContainerEventType:
		// Container events indicate task state changes.
		// Re-fetch tasks for the service if available.
		svcName := msg.Actor.Attributes["com.docker.swarm.service.name"]
		taskID := msg.Actor.Attributes["com.docker.swarm.task.id"]
		if taskID != "" {
			task, err := w.client.InspectTask(ctx, taskID)
			if err != nil {
				log.Printf("inspect task %s (svc: %s) failed: %v", taskID, svcName, err)
				return
			}
			w.cache.SetTask(task)
		}
	}
}
```

**Step 2: Verify it compiles**

```bash
go build ./internal/docker/...
```

Expected: compiles without error. Adjust any import paths or type mismatches based on the resolved Docker library version.

**Step 3: Commit**

```bash
git add -A && git commit -m "feat: Docker watcher with full sync and event-driven updates"
```

---

### Task 5: SSE Broadcaster

**Files:**
- Create: `internal/api/sse.go`
- Create: `internal/api/sse_test.go`

**Context:** The SSE broadcaster fans out cache change events to all connected HTTP clients. Clients can filter by resource type via query parameter.

**Step 1: Write SSE test**

Create `internal/api/sse_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cetacean/internal/cache"
)

func TestSSE_BroadcastsEvents(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	// Start a test SSE request
	req := httptest.NewRequest("GET", "/api/events", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for client to register
	time.Sleep(50 * time.Millisecond)

	// Send an event
	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})

	// Wait for it to arrive
	time.Sleep(50 * time.Millisecond)
	b.Close()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: node") {
		t.Errorf("expected event: node in body, got: %s", body)
	}
	if !strings.Contains(body, `"action":"update"`) {
		t.Errorf("expected action:update in body, got: %s", body)
	}
}

func TestSSE_FiltersByType(t *testing.T) {
	b := NewBroadcaster()
	defer b.Close()

	req := httptest.NewRequest("GET", "/api/events?types=service", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	b.Broadcast(cache.Event{Type: "node", Action: "update", ID: "n1"})
	b.Broadcast(cache.Event{Type: "service", Action: "update", ID: "s1"})

	time.Sleep(50 * time.Millisecond)
	b.Close()
	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: node") {
		t.Error("node event should have been filtered out")
	}
	if !strings.Contains(body, "event: service") {
		t.Error("service event should have been included")
	}
}

// flushRecorder implements http.Flusher for testing SSE.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/api/... -v
```

Expected: FAIL — package doesn't exist.

**Step 3: Write SSE implementation**

Create `internal/api/sse.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"cetacean/internal/cache"
)

type sseClient struct {
	events chan cache.Event
	types  map[string]bool // nil means all types
	done   chan struct{}
}

type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*sseClient]struct{}
	closed  bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[*sseClient]struct{}),
	}
}

func (b *Broadcaster) Broadcast(e cache.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for c := range b.clients {
		if c.types != nil && !c.types[e.Type] {
			continue
		}
		select {
		case c.events <- e:
		default:
			// Slow client, drop event
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for c := range b.clients {
		close(c.done)
	}
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	var types map[string]bool
	if t := r.URL.Query().Get("types"); t != "" {
		types = make(map[string]bool)
		for _, typ := range strings.Split(t, ",") {
			types[strings.TrimSpace(typ)] = true
		}
	}

	client := &sseClient{
		events: make(chan cache.Event, 64),
		types:  types,
		done:   make(chan struct{}),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.clients[client] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		b.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-client.done:
			return
		case e := <-client.events:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			flusher.Flush()
		}
	}
}
```

**Step 4: Run tests**

```bash
go test ./internal/api/... -v
```

Expected: PASS (2 tests).

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: SSE broadcaster with type filtering"
```

---

### Task 6: REST API Handlers

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/handlers.go`
- Create: `internal/api/handlers_test.go`

**Context:** All handlers read from the cache and return JSON. The router uses Go 1.25 stdlib `net/http` with its method+pattern routing.

**Step 1: Write handler tests**

Create `internal/api/handlers_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func TestHandleCluster(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/cluster", nil)
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var snap cache.ClusterSnapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if snap.NodeCount != 1 || snap.ServiceCount != 1 {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
}

func TestHandleListNodes(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var nodes []swarm.Node
	json.NewDecoder(w.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestHandleGetNode_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes/n1", nil)
	req.SetPathValue("id", "n1")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/api/... -v
```

Expected: FAIL — `NewHandlers` doesn't exist.

**Step 3: Write handlers**

Create `internal/api/handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"cetacean/internal/cache"
)

type Handlers struct {
	cache *cache.Cache
}

func NewHandlers(c *cache.Cache) *Handlers {
	return &Handlers{cache: c}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handlers) HandleCluster(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.Snapshot())
}

// --- Nodes ---

func (h *Handlers) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListNodes())
}

func (h *Handlers) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, node)
}

// --- Services ---

func (h *Handlers) HandleListServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListServices())
}

func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, svc)
}

// --- Tasks ---

func (h *Handlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListTasks())
}

func (h *Handlers) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, ok := h.cache.GetTask(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, task)
}

// --- Stacks ---

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListStacks())
}

func (h *Handlers) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	stack, ok := h.cache.GetStack(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, stack)
}

// --- Configs ---

func (h *Handlers) HandleListConfigs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListConfigs())
}

// --- Secrets ---

func (h *Handlers) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListSecrets())
}

// --- Networks ---

func (h *Handlers) HandleListNetworks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListNetworks())
}

// --- Volumes ---

func (h *Handlers) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.cache.ListVolumes())
}
```

**Step 4: Write the router**

Create `internal/api/router.go`:

```go
package api

import (
	"net/http"
)

func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler) http.Handler {
	mux := http.NewServeMux()

	// SSE
	mux.Handle("GET /api/events", b)

	// Cluster
	mux.HandleFunc("GET /api/cluster", h.HandleCluster)

	// Nodes
	mux.HandleFunc("GET /api/nodes", h.HandleListNodes)
	mux.HandleFunc("GET /api/nodes/{id}", h.HandleGetNode)

	// Services
	mux.HandleFunc("GET /api/services", h.HandleListServices)
	mux.HandleFunc("GET /api/services/{id}", h.HandleGetService)

	// Tasks
	mux.HandleFunc("GET /api/tasks", h.HandleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", h.HandleGetTask)

	// Stacks
	mux.HandleFunc("GET /api/stacks", h.HandleListStacks)
	mux.HandleFunc("GET /api/stacks/{name}", h.HandleGetStack)

	// Configs
	mux.HandleFunc("GET /api/configs", h.HandleListConfigs)

	// Secrets
	mux.HandleFunc("GET /api/secrets", h.HandleListSecrets)

	// Networks
	mux.HandleFunc("GET /api/networks", h.HandleListNetworks)

	// Volumes
	mux.HandleFunc("GET /api/volumes", h.HandleListVolumes)

	// Prometheus proxy
	mux.Handle("GET /api/metrics/", promProxy)

	// SPA fallback (must be last)
	mux.Handle("/", spa)

	return mux
}
```

**Step 5: Run tests**

```bash
go test ./internal/api/... -v
```

Expected: PASS (all 4 handler tests + 2 SSE tests).

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: REST API handlers and router"
```

---

### Task 7: Prometheus Proxy

**Files:**
- Create: `internal/api/prometheus.go`
- Create: `internal/api/prometheus_test.go`

**Context:** Reverse proxy that forwards `/api/metrics/query` and `/api/metrics/query_range` to the configured Prometheus URL. Passes through query parameters unchanged.

**Step 1: Write test**

Create `internal/api/prometheus_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrometheusProxy_ForwardsQuery(t *testing.T) {
	// Fake Prometheus server
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("expected query=up, got %s", r.URL.Query().Get("query"))
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/api/metrics/query?query=up", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"success"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestPrometheusProxy_ForwardsQueryRange(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer prom.Close()

	proxy := NewPrometheusProxy(prom.URL)

	req := httptest.NewRequest("GET", "/api/metrics/query_range?query=up&start=0&end=1&step=15", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/api/... -v -run TestPrometheus
```

Expected: FAIL — `NewPrometheusProxy` doesn't exist.

**Step 3: Write implementation**

Create `internal/api/prometheus.go`:

```go
package api

import (
	"io"
	"net/http"
	"strings"
)

type PrometheusProxy struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusProxy(baseURL string) *PrometheusProxy {
	return &PrometheusProxy{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (p *PrometheusProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Map /api/metrics/query → /api/v1/query
	// Map /api/metrics/query_range → /api/v1/query_range
	path := strings.TrimPrefix(r.URL.Path, "/api/metrics")
	targetURL := p.baseURL + "/api/v1" + path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "prometheus request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
```

**Step 4: Run tests**

```bash
go test ./internal/api/... -v -run TestPrometheus
```

Expected: PASS (2 tests).

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: Prometheus query proxy"
```

---

### Task 8: SPA Embedding & Server Wiring

**Files:**
- Create: `embed.go`
- Modify: `cmd/cetacean/main.go`

**Context:** Embed the frontend build output and wire all components together in main. For now, create a placeholder `frontend/dist/index.html` so embedding works before the frontend is built.

**Step 1: Create placeholder frontend**

```bash
mkdir -p frontend/dist
echo '<!DOCTYPE html><html><body><h1>Cetacean</h1><div id="root"></div></body></html>' > frontend/dist/index.html
```

**Step 2: Write embed.go**

Create `embed.go`:

```go
package main

import "embed"

//go:embed frontend/dist/*
var frontendFS embed.FS
```

Note: This file must be in package `main` alongside `cmd/cetacean/main.go`. Actually, since Go's embed requires the directive to be in the same directory as the embedded files, and `frontend/dist` is relative to the project root, this file goes in the project root but we need to reference it from main. A cleaner approach: put the embed in a dedicated package.

Revised approach — create `internal/frontend/embed.go`:

```go
package frontend

import "embed"

//go:embed dist/*
var DistFS embed.FS
```

And move `frontend/dist` so the embed path works. Actually, Go embed paths are relative to the source file, so `internal/frontend/embed.go` would look for `internal/frontend/dist/`. Let's keep it simple — put `embed.go` in `cmd/cetacean/` since that's the main package, or better yet, handle it at the project root.

Simplest approach: `embed.go` at project root won't work because `cmd/cetacean/main.go` is a different package. Let's put the SPA server in `internal/api/spa.go` and handle embedding at build time.

Final approach: The embed directive goes in `cmd/cetacean/main.go` itself, referencing `../../frontend/dist/*`. But Go embed doesn't support `..`.

Correct solution: Put `embed.go` in the project root as `package cetacean` won't work either since main is in `cmd/cetacean`.

**The standard pattern:** Embed from `cmd/cetacean/` by symlinking or by building from the project root. The cleanest Go pattern is:

Put in `cmd/cetacean/main.go`:
```go
//go:embed ../../frontend/dist
```
This doesn't work — Go embed forbids `..`.

**Actual solution:** Build from the project root with the embed file at root level. Change the project structure so main.go is at the root.

Revised structure: Move entrypoint to root `main.go` (delete the `cmd/cetacean/` directory, keeping it simple).

**Step 2 (revised): Create embed and SPA handler**

Create `internal/api/spa.go`:

```go
package api

import (
	"io/fs"
	"net/http"
	"strings"
)

func NewSPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if file exists
		f, err := fsys.Open(path)
		if err != nil {
			// Fall back to index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
```

**Step 3: Write main.go with full wiring**

Rewrite `cmd/cetacean/main.go`:

```go
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"cetacean/internal/api"
	"cetacean/internal/cache"
	"cetacean/internal/config"
	"cetacean/internal/docker"
)

// Frontend is embedded at build time.
// When developing, this will contain just the placeholder.
// In production builds, run `npm run build` in frontend/ first.
//
//go:embed frontend/dist/*
var frontendDist embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// SSE broadcaster
	broadcaster := api.NewBroadcaster()
	defer broadcaster.Close()

	// State cache — broadcasts changes via SSE
	stateCache := cache.New(func(e cache.Event) {
		broadcaster.Broadcast(e)
	})

	// Docker client + watcher
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		log.Fatalf("docker client error: %v", err)
	}
	defer dockerClient.Close()

	watcher := docker.NewWatcher(dockerClient, stateCache)

	// Start watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watcher.Run(ctx)

	// Wait for initial sync
	<-watcher.Ready()
	log.Println("initial sync complete, starting HTTP server")

	// API
	handlers := api.NewHandlers(stateCache)
	promProxy := api.NewPrometheusProxy(cfg.PrometheusURL)

	// SPA
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Fatalf("failed to create sub FS: %v", err)
	}
	spa := api.NewSPAHandler(distFS)

	router := api.NewRouter(handlers, broadcaster, promProxy, spa)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		cancel()
		server.Close()
	}()

	log.Printf("cetacean listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
```

Note: Since `cmd/cetacean/main.go` uses `//go:embed frontend/dist/*`, and Go embed paths cannot contain `..`, the `main.go` must live at the project root OR we adjust the structure. **Move main.go to the project root:**

- Delete `cmd/cetacean/main.go`
- The above `main.go` goes to the project root as `main.go`
- Build with `go build .` from the project root
- The `//go:embed frontend/dist/*` works because `frontend/dist/` is relative to root

Update project structure accordingly:
```
cetacean/
├── main.go               # Entrypoint (was cmd/cetacean/main.go)
├── internal/...
├── frontend/
│   └── dist/             # Build output (embedded)
├── embed will be inline in main.go
```

**Step 4: Verify it compiles**

```bash
go build .
```

Expected: compiles (may fail to start without Docker, but compiles).

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: SPA handler and full server wiring"
```

---

### Task 9: Frontend Scaffolding

**Files:**
- Create: `frontend/` — full Vite + React + TypeScript project
- Create: `frontend/vite.config.ts`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/api/client.ts`

**Context:** Initialize the React frontend with Vite, Tailwind, and shadcn/ui. Set up the API proxy for development.

**Step 1: Scaffold Vite project**

```bash
cd /Users/moritz/GolandProjects/cetacean
rm -rf frontend/dist
npm create vite@latest frontend -- --template react-ts
cd frontend
npm install
```

**Step 2: Install Tailwind and shadcn/ui**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend
npm install -D tailwindcss @tailwindcss/vite
npm install react-router-dom uplot uplot-react
npx shadcn@latest init
```

Follow shadcn init prompts — select default style, CSS variables, etc.

**Step 3: Configure Vite dev proxy**

Update `frontend/vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:9000',
    },
  },
})
```

**Step 4: Create API client**

Create `frontend/src/api/client.ts`:

```ts
const BASE = '/api'

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export interface ClusterSnapshot {
  nodeCount: number
  serviceCount: number
  taskCount: number
  stackCount: number
}

export const api = {
  cluster: () => fetchJSON<ClusterSnapshot>('/cluster'),
  nodes: () => fetchJSON<any[]>('/nodes'),
  node: (id: string) => fetchJSON<any>(`/nodes/${id}`),
  services: () => fetchJSON<any[]>('/services'),
  service: (id: string) => fetchJSON<any>(`/services/${id}`),
  tasks: () => fetchJSON<any[]>('/tasks'),
  stacks: () => fetchJSON<any[]>('/stacks'),
  stack: (name: string) => fetchJSON<any>(`/stacks/${name}`),
  configs: () => fetchJSON<any[]>('/configs'),
  secrets: () => fetchJSON<any[]>('/secrets'),
  networks: () => fetchJSON<any[]>('/networks'),
  volumes: () => fetchJSON<any[]>('/volumes'),
  metricsQuery: (query: string, time?: string) => {
    const params = new URLSearchParams({ query })
    if (time) params.set('time', time)
    return fetchJSON<any>(`/metrics/query?${params}`)
  },
  metricsQueryRange: (query: string, start: string, end: string, step: string) => {
    const params = new URLSearchParams({ query, start, end, step })
    return fetchJSON<any>(`/metrics/query_range?${params}`)
  },
}
```

**Step 5: Create SSE hook**

Create `frontend/src/hooks/useSSE.ts`:

```ts
import { useEffect, useRef, useCallback } from 'react'

interface SSEEvent {
  type: string
  action: string
  id: string
  resource?: any
}

export function useSSE(
  types: string[],
  onEvent: (event: SSEEvent) => void,
) {
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  useEffect(() => {
    const params = types.length > 0 ? `?types=${types.join(',')}` : ''
    const es = new EventSource(`/api/events${params}`)

    const handler = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as SSEEvent
        onEventRef.current(data)
      } catch {
        // ignore malformed events
      }
    }

    for (const type of types) {
      es.addEventListener(type, handler)
    }

    // If no types specified, listen to generic messages
    if (types.length === 0) {
      es.onmessage = handler
    }

    return () => es.close()
  }, [types.join(',')])
}
```

**Step 6: Create useSwarmResource hook**

Create `frontend/src/hooks/useSwarmResource.ts`:

```ts
import { useState, useEffect, useCallback } from 'react'
import { useSSE } from './useSSE'

export function useSwarmResource<T>(
  fetchFn: () => Promise<T[]>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [data, setData] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    fetchFn()
      .then(setData)
      .catch(setError)
      .finally(() => setLoading(false))
  }, [])

  useSSE([sseType], useCallback((event) => {
    if (event.action === 'remove') {
      setData(prev => prev.filter(item => getId(item) !== event.id))
    } else if (event.resource) {
      setData(prev => {
        const idx = prev.findIndex(item => getId(item) === event.id)
        if (idx >= 0) {
          const next = [...prev]
          next[idx] = event.resource
          return next
        }
        return [...prev, event.resource]
      })
    }
  }, []))

  return { data, loading, error }
}
```

**Step 7: Set up basic App with router**

Replace `frontend/src/App.tsx`:

```tsx
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom'
import ClusterOverview from './pages/ClusterOverview'

function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-background">
      <nav className="border-b px-6 py-3 flex items-center gap-6">
        <Link to="/" className="font-bold text-lg">Cetacean</Link>
        <Link to="/nodes" className="text-sm text-muted-foreground hover:text-foreground">Nodes</Link>
        <Link to="/stacks" className="text-sm text-muted-foreground hover:text-foreground">Stacks</Link>
        <Link to="/services" className="text-sm text-muted-foreground hover:text-foreground">Services</Link>
        <Link to="/configs" className="text-sm text-muted-foreground hover:text-foreground">Configs</Link>
        <Link to="/secrets" className="text-sm text-muted-foreground hover:text-foreground">Secrets</Link>
        <Link to="/networks" className="text-sm text-muted-foreground hover:text-foreground">Networks</Link>
        <Link to="/volumes" className="text-sm text-muted-foreground hover:text-foreground">Volumes</Link>
      </nav>
      <main className="p-6">{children}</main>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<ClusterOverview />} />
          {/* Additional routes added in subsequent tasks */}
        </Routes>
      </Layout>
    </BrowserRouter>
  )
}
```

**Step 8: Create initial ClusterOverview page**

Create `frontend/src/pages/ClusterOverview.tsx`:

```tsx
import { useState, useEffect } from 'react'
import { api, ClusterSnapshot } from '../api/client'

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null)

  useEffect(() => {
    api.cluster().then(setSnapshot)
  }, [])

  if (!snapshot) return <div>Loading...</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Cluster Overview</h1>
      <div className="grid grid-cols-4 gap-4">
        <StatCard label="Nodes" value={snapshot.nodeCount} />
        <StatCard label="Services" value={snapshot.serviceCount} />
        <StatCard label="Tasks" value={snapshot.taskCount} />
        <StatCard label="Stacks" value={snapshot.stackCount} />
      </div>
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg border bg-card p-6">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="text-3xl font-bold">{value}</div>
    </div>
  )
}
```

**Step 9: Verify frontend builds**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build
```

Expected: builds to `frontend/dist/`.

**Step 10: Verify Go binary builds with embedded frontend**

```bash
cd /Users/moritz/GolandProjects/cetacean && go build .
```

Expected: compiles successfully.

**Step 11: Commit**

```bash
git add -A && git commit -m "feat: React frontend scaffolding with Vite, Tailwind, shadcn/ui"
```

---

### Task 10: Frontend Pages — Node List & Detail

**Files:**
- Create: `frontend/src/pages/NodeList.tsx`
- Create: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/App.tsx` (add routes)

**Step 1: Create NodeList page**

Create `frontend/src/pages/NodeList.tsx`:

```tsx
import { Link } from 'react-router-dom'
import { useSwarmResource } from '../hooks/useSwarmResource'
import { api } from '../api/client'

export default function NodeList() {
  const { data: nodes, loading } = useSwarmResource(
    api.nodes,
    'node',
    (n: any) => n.ID,
  )

  if (loading) return <div>Loading...</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Nodes</h1>
      <div className="rounded-lg border">
        <table className="w-full">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="text-left p-3 text-sm font-medium">Hostname</th>
              <th className="text-left p-3 text-sm font-medium">Role</th>
              <th className="text-left p-3 text-sm font-medium">Status</th>
              <th className="text-left p-3 text-sm font-medium">Availability</th>
              <th className="text-left p-3 text-sm font-medium">Engine</th>
            </tr>
          </thead>
          <tbody>
            {nodes.map((node: any) => (
              <tr key={node.ID} className="border-b">
                <td className="p-3">
                  <Link to={`/nodes/${node.ID}`} className="text-blue-600 hover:underline">
                    {node.Description?.Hostname || node.ID}
                  </Link>
                </td>
                <td className="p-3 text-sm">{node.Spec?.Role}</td>
                <td className="p-3 text-sm">{node.Status?.State}</td>
                <td className="p-3 text-sm">{node.Spec?.Availability}</td>
                <td className="p-3 text-sm">{node.Description?.Engine?.EngineVersion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

**Step 2: Create NodeDetail page**

Create `frontend/src/pages/NodeDetail.tsx`:

```tsx
import { useParams } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { api } from '../api/client'

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>()
  const [node, setNode] = useState<any>(null)

  useEffect(() => {
    if (id) api.node(id).then(setNode)
  }, [id])

  if (!node) return <div>Loading...</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">
        {node.Description?.Hostname || node.ID}
      </h1>
      <div className="grid grid-cols-2 gap-4 mb-6">
        <InfoCard label="Role" value={node.Spec?.Role} />
        <InfoCard label="Status" value={node.Status?.State} />
        <InfoCard label="Availability" value={node.Spec?.Availability} />
        <InfoCard label="Engine" value={node.Description?.Engine?.EngineVersion} />
        <InfoCard label="OS" value={`${node.Description?.Platform?.OS} ${node.Description?.Platform?.Architecture}`} />
        <InfoCard label="Address" value={node.Status?.Addr} />
      </div>
      {/* Prometheus charts added in Task 13 */}
    </div>
  )
}

function InfoCard({ label, value }: { label: string; value?: string }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-sm text-muted-foreground">{label}</div>
      <div className="text-lg font-medium">{value || '—'}</div>
    </div>
  )
}
```

**Step 3: Add routes to App.tsx**

Add imports and route entries for NodeList and NodeDetail in `App.tsx`.

**Step 4: Build and verify**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build
```

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: node list and detail pages"
```

---

### Task 11: Frontend Pages — Stack, Service, Config, Secret, Network, Volume

**Files:**
- Create: `frontend/src/pages/StackList.tsx`
- Create: `frontend/src/pages/StackDetail.tsx`
- Create: `frontend/src/pages/ServiceList.tsx`
- Create: `frontend/src/pages/ServiceDetail.tsx`
- Create: `frontend/src/pages/ConfigList.tsx`
- Create: `frontend/src/pages/SecretList.tsx`
- Create: `frontend/src/pages/NetworkList.tsx`
- Create: `frontend/src/pages/VolumeList.tsx`
- Modify: `frontend/src/App.tsx` (add all routes)

**Context:** These follow the same pattern as NodeList/NodeDetail. Each list page uses `useSwarmResource` for live updates. Detail pages fetch a single resource. Implement all of them, register all routes.

**Step 1-8: Create each page file**

Follow the same pattern as Task 10. Key points:
- StackList groups by stack name, shows service count, aggregate health
- StackDetail shows sub-tables for its services, configs, secrets, networks, volumes
- ServiceList shows image, replicas (running/desired), update status
- ServiceDetail shows spec info, port mappings, and a task list
- ConfigList, SecretList, NetworkList, VolumeList are simple tables

**Step 9: Register all routes in App.tsx**

```tsx
<Routes>
  <Route path="/" element={<ClusterOverview />} />
  <Route path="/nodes" element={<NodeList />} />
  <Route path="/nodes/:id" element={<NodeDetail />} />
  <Route path="/stacks" element={<StackList />} />
  <Route path="/stacks/:name" element={<StackDetail />} />
  <Route path="/services" element={<ServiceList />} />
  <Route path="/services/:id" element={<ServiceDetail />} />
  <Route path="/configs" element={<ConfigList />} />
  <Route path="/secrets" element={<SecretList />} />
  <Route path="/networks" element={<NetworkList />} />
  <Route path="/volumes" element={<VolumeList />} />
</Routes>
```

**Step 10: Build and verify**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build
```

**Step 11: Commit**

```bash
git add -A && git commit -m "feat: stack, service, config, secret, network, volume pages"
```

---

### Task 12: Frontend — Prometheus Charts Component

**Files:**
- Create: `frontend/src/components/TimeSeriesChart.tsx`
- Create: `frontend/src/components/MetricsPanel.tsx`
- Install: `uplot` and `uplot-react` (already done in Task 9)

**Context:** A reusable chart component that takes a PromQL query, time range, and renders via uPlot. Used on node detail and service detail pages.

**Step 1: Create TimeSeriesChart component**

Create `frontend/src/components/TimeSeriesChart.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { api } from '../api/client'

interface Props {
  title: string
  query: string
  range: string // '1h', '6h', '24h', '7d'
  unit?: string // '%', 'bytes', 'bytes/s', etc.
}

const RANGE_SECONDS: Record<string, number> = {
  '1h': 3600,
  '6h': 21600,
  '24h': 86400,
  '7d': 604800,
}

export default function TimeSeriesChart({ title, query, range, unit }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<uPlot | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const rangeSec = RANGE_SECONDS[range] || 3600
    const now = Math.floor(Date.now() / 1000)
    const start = now - rangeSec
    const step = Math.max(Math.floor(rangeSec / 300), 15)

    api.metricsQueryRange(query, String(start), String(now), String(step))
      .then((resp: any) => {
        if (!resp.data?.result?.length) {
          setError('No data')
          return
        }
        setError(null)

        const series = resp.data.result
        const timestamps = series[0].values.map((v: any) => Number(v[0]))
        const data: uPlot.AlignedData = [
          timestamps,
          ...series.map((s: any) => s.values.map((v: any) => Number(v[1]))),
        ]

        if (chartRef.current) chartRef.current.destroy()

        const opts: uPlot.Options = {
          width: containerRef.current?.clientWidth || 600,
          height: 200,
          series: [
            {},
            ...series.map((s: any, i: number) => ({
              label: s.metric?.__name__ || `series-${i}`,
              stroke: `hsl(${i * 60}, 70%, 50%)`,
            })),
          ],
          axes: [
            {},
            { label: unit || '' },
          ],
        }

        chartRef.current = new uPlot(opts, data, containerRef.current!)
      })
      .catch(() => setError('Failed to load metrics'))

    return () => {
      if (chartRef.current) chartRef.current.destroy()
    }
  }, [query, range])

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-sm font-medium mb-2">{title}</div>
      {error ? (
        <div className="text-sm text-muted-foreground h-[200px] flex items-center justify-center">{error}</div>
      ) : (
        <div ref={containerRef} />
      )}
    </div>
  )
}
```

**Step 2: Create MetricsPanel component**

Create `frontend/src/components/MetricsPanel.tsx`:

```tsx
import { useState } from 'react'
import TimeSeriesChart from './TimeSeriesChart'

interface ChartDef {
  title: string
  query: string
  unit?: string
}

interface Props {
  charts: ChartDef[]
}

const RANGES = ['1h', '6h', '24h', '7d'] as const

export default function MetricsPanel({ charts }: Props) {
  const [range, setRange] = useState<string>('1h')

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <span className="text-sm text-muted-foreground">Time range:</span>
        {RANGES.map(r => (
          <button
            key={r}
            onClick={() => setRange(r)}
            className={`px-3 py-1 text-sm rounded ${
              range === r ? 'bg-primary text-primary-foreground' : 'bg-muted'
            }`}
          >
            {r}
          </button>
        ))}
      </div>
      <div className="grid grid-cols-2 gap-4">
        {charts.map(chart => (
          <TimeSeriesChart key={chart.query} {...chart} range={range} />
        ))}
      </div>
    </div>
  )
}
```

**Step 3: Add charts to NodeDetail**

Update `frontend/src/pages/NodeDetail.tsx` to include:

```tsx
import MetricsPanel from '../components/MetricsPanel'

// Inside the component, after the info cards:
<MetricsPanel charts={[
  { title: 'CPU Usage', query: `100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle",instance=~"${node.Status?.Addr}.*"}[5m])) * 100)`, unit: '%' },
  { title: 'Memory Usage', query: `(1 - node_memory_MemAvailable_bytes{instance=~"${node.Status?.Addr}.*"} / node_memory_MemTotal_bytes{instance=~"${node.Status?.Addr}.*"}) * 100`, unit: '%' },
  { title: 'Disk I/O', query: `rate(node_disk_read_bytes_total{instance=~"${node.Status?.Addr}.*"}[5m])`, unit: 'bytes/s' },
  { title: 'Network I/O', query: `rate(node_network_receive_bytes_total{instance=~"${node.Status?.Addr}.*"}[5m])`, unit: 'bytes/s' },
]} />
```

**Step 4: Add charts to ServiceDetail**

Similar pattern, using cAdvisor metrics filtered by service name:

```tsx
<MetricsPanel charts={[
  { title: 'CPU Usage', query: `rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${service.Spec?.Name}"}[5m])`, unit: 'cores' },
  { title: 'Memory Usage', query: `container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${service.Spec?.Name}"}`, unit: 'bytes' },
]} />
```

**Step 5: Build and verify**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build
```

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: Prometheus time-series charts with uPlot"
```

---

### Task 13: Dockerfile & Docker Compose Stack

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `prometheus.yml`

**Step 1: Create Dockerfile**

Create `Dockerfile`:

```dockerfile
# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -o cetacean .

# Stage 3: Minimal runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=backend /app/cetacean /usr/local/bin/cetacean
EXPOSE 9000
ENTRYPOINT ["cetacean"]
```

**Step 2: Create Prometheus config**

Create `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'cadvisor'
    dockerswarm_sd_configs:
      - host: unix:///var/run/docker.sock
        role: tasks
    relabel_configs:
      - source_labels: [__meta_dockerswarm_service_name]
        regex: .*cadvisor.*
        action: keep
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: instance

  - job_name: 'node-exporter'
    dockerswarm_sd_configs:
      - host: unix:///var/run/docker.sock
        role: tasks
    relabel_configs:
      - source_labels: [__meta_dockerswarm_service_name]
        regex: .*node-exporter.*
        action: keep
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: instance
```

**Step 3: Create Docker Compose stack**

Create `docker-compose.yml`:

```yaml
version: "3.8"

services:
  cetacean:
    image: cetacean:latest
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - CETACEAN_PROMETHEUS_URL=http://prometheus:9090
    deploy:
      placement:
        constraints:
          - node.role == manager

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - prometheus_data:/prometheus
    deploy:
      placement:
        constraints:
          - node.role == manager

  cadvisor:
    image: gcr.io/cadvisor/cadvisor:latest
    volumes:
      - /:/rootfs:ro
      - /var/run:/var/run:ro
      - /sys:/sys:ro
      - /var/lib/docker/:/var/lib/docker:ro
    deploy:
      mode: global

  node-exporter:
    image: prom/node-exporter:latest
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/rootfs:ro
    command:
      - '--path.procfs=/host/proc'
      - '--path.sysfs=/host/sys'
      - '--path.rootfs=/rootfs'
      - '--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)'
    deploy:
      mode: global

volumes:
  prometheus_data:
```

**Step 4: Verify Docker build**

```bash
docker build -t cetacean:latest .
```

Expected: builds successfully.

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: Dockerfile and batteries-included Docker Compose stack"
```

---

### Task 14: Integration Test & Polish

**Files:**
- Modify: `internal/api/handlers_test.go` (add more coverage)
- Create: `.gitignore`

**Step 1: Create .gitignore**

Create `.gitignore`:

```
# Go
cetacean
*.exe

# Frontend
frontend/node_modules/
frontend/dist/

# IDE
.idea/

# OS
.DS_Store
```

**Step 2: Add handler tests for filtering and remaining endpoints**

Add tests for `HandleListServices`, `HandleListStacks`, `HandleGetStack`, etc. following the same pattern as Task 6.

**Step 3: Run all tests**

```bash
cd /Users/moritz/GolandProjects/cetacean && go test ./... -v
```

Expected: all tests pass.

**Step 4: Run frontend build**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend && npm run build
```

**Step 5: Build final binary**

```bash
cd /Users/moritz/GolandProjects/cetacean && go build .
```

**Step 6: Commit**

```bash
git add -A && git commit -m "chore: gitignore, additional test coverage, build verification"
```

---

## Dependency Order

```
Task 1 (scaffolding)
  → Task 2 (cache)
    → Task 3 (docker client)
      → Task 4 (watcher)
  → Task 5 (SSE)
  → Task 6 (REST handlers)
  → Task 7 (prometheus proxy)
    → Task 8 (SPA embed + wiring) — depends on 2-7
      → Task 9 (frontend scaffolding)
        → Task 10 (node pages)
        → Task 11 (remaining pages)
        → Task 12 (charts)
          → Task 13 (Dockerfile + compose)
            → Task 14 (integration + polish)
```

Tasks 2, 5, 6, 7 can be parallelized after Task 1.
Tasks 10, 11 can be parallelized after Task 9.
