package filter

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Program is a compiled filter expression.
type Program = *vm.Program

// Compile parses and compiles a filter expression string.
// Returns an error if the expression is syntactically invalid.
func Compile(expression string) (Program, error) {
	if prog, ok := compileCache.get(expression); ok {
		return prog, nil
	}
	prog, err := expr.Compile(expression, expr.AsBool())
	if err != nil {
		return nil, err
	}
	compileCache.put(expression, prog)
	return prog, nil
}

const compileCacheSize = 64

var compileCache = newProgramCache(compileCacheSize)

type programCache struct {
	mu      sync.RWMutex
	entries map[string]Program
	cap     int
}

func newProgramCache(cap int) *programCache {
	return &programCache{entries: make(map[string]Program, cap), cap: cap}
}

func (c *programCache) get(key string) (Program, bool) {
	c.mu.RLock()
	p, ok := c.entries[key]
	c.mu.RUnlock()
	return p, ok
}

func (c *programCache) put(key string, prog Program) {
	c.mu.Lock()
	if len(c.entries) >= c.cap {
		// Evict one random entry.
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = prog
	c.mu.Unlock()
}

// Evaluate runs a compiled program against an environment map.
func Evaluate(prog Program, env map[string]any) (bool, error) {
	out, err := expr.Run(prog, env)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("filter expression must return bool, got %T", out)
	}
	return b, nil
}

// NodeEnv builds an expression environment from a swarm.Node.
func NodeEnv(n swarm.Node, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 5)
	}
	m["id"] = n.ID
	m["name"] = n.Description.Hostname
	m["state"] = string(n.Status.State)
	m["role"] = string(n.Spec.Role)
	m["availability"] = string(n.Spec.Availability)
	return m
}

// ServiceEnv builds an expression environment from a swarm.Service.
func ServiceEnv(s swarm.Service, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 5)
	}
	mode := "replicated"
	if s.Spec.Mode.Global != nil {
		mode = "global"
	}
	var image string
	if s.Spec.TaskTemplate.ContainerSpec != nil {
		image = s.Spec.TaskTemplate.ContainerSpec.Image
	}
	m["id"] = s.ID
	m["name"] = s.Spec.Name
	m["image"] = image
	m["mode"] = mode
	m["stack"] = s.Spec.Labels["com.docker.stack.namespace"]
	return m
}

// TaskEnv builds an expression environment from a swarm.Task.
func TaskEnv(t swarm.Task, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 9)
	}
	var image string
	if t.Spec.ContainerSpec != nil {
		image = t.Spec.ContainerSpec.Image
	}
	var exitCode string
	if t.Status.ContainerStatus != nil {
		exitCode = strconv.Itoa(t.Status.ContainerStatus.ExitCode)
	}
	m["id"] = t.ID
	m["state"] = string(t.Status.State)
	m["desired_state"] = string(t.DesiredState)
	m["image"] = image
	m["exit_code"] = exitCode
	m["error"] = t.Status.Err
	m["service"] = t.ServiceID
	m["node"] = t.NodeID
	m["slot"] = t.Slot
	return m
}

// ConfigEnv builds an expression environment from a swarm.Config.
func ConfigEnv(c swarm.Config, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 2)
	}
	m["id"] = c.ID
	m["name"] = c.Spec.Name
	return m
}

// SecretEnv builds an expression environment from a swarm.Secret.
func SecretEnv(s swarm.Secret, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 2)
	}
	m["id"] = s.ID
	m["name"] = s.Spec.Name
	return m
}

// NetworkEnv builds an expression environment from a network.Summary.
func NetworkEnv(n network.Summary, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 4)
	}
	m["id"] = n.ID
	m["name"] = n.Name
	m["driver"] = n.Driver
	m["scope"] = n.Scope
	return m
}

// VolumeEnv builds an expression environment from a volume.Volume.
func VolumeEnv(v volume.Volume, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 3)
	}
	m["name"] = v.Name
	m["driver"] = v.Driver
	m["scope"] = v.Scope
	return m
}

// StackEnv builds an expression environment from a cache.Stack.
func StackEnv(s cache.Stack, m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any, 6)
	}
	m["name"] = s.Name
	m["services"] = len(s.Services)
	m["configs"] = len(s.Configs)
	m["secrets"] = len(s.Secrets)
	m["networks"] = len(s.Networks)
	m["volumes"] = len(s.Volumes)
	return m
}

// ResourceEnv builds an expression environment from an interface{} resource,
// as stored in cache.Event.Resource. Returns nil for unrecognized types.
func ResourceEnv(resource any) map[string]any {
	switch r := resource.(type) {
	case swarm.Node:
		return NodeEnv(r, nil)
	case swarm.Service:
		return ServiceEnv(r, nil)
	case swarm.Task:
		return TaskEnv(r, nil)
	case swarm.Config:
		return ConfigEnv(r, nil)
	case swarm.Secret:
		return SecretEnv(r, nil)
	case network.Summary:
		return NetworkEnv(r, nil)
	case volume.Volume:
		return VolumeEnv(r, nil)
	default:
		return nil
	}
}
