package cache

import (
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// StackOf returns the stack name for a resource identified by its display name,
// or "" if the resource doesn't exist or isn't in a stack.
// Resources are looked up by name (not Docker ID) because the ACL evaluator
// works with "type:name" resource strings.
func (c *Cache) StackOf(resourceType, name string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	const label = "com.docker.stack.namespace"

	switch resourceType {
	case "service":
		for _, s := range c.services {
			if s.Spec.Name == name {
				return s.Spec.Labels[label]
			}
		}
	case "config":
		for _, cfg := range c.configs.items {
			if cfg.Spec.Name == name {
				return cfg.Spec.Labels[label]
			}
		}
	case "secret":
		for _, s := range c.secrets.items {
			if s.Spec.Name == name {
				return s.Spec.Labels[label]
			}
		}
	case "network":
		for _, n := range c.networks.items {
			if n.Name == name {
				return n.Labels[label]
			}
		}
	case "volume":
		for _, v := range c.volumes.items {
			if v.Name == name {
				return v.Labels[label]
			}
		}
	}
	return ""
}

// ServiceOfTask returns the service name for a task, or "" if unknown.
func (c *Cache) ServiceOfTask(taskID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	t, ok := c.tasks[taskID]
	if !ok {
		return ""
	}
	if svc, ok := c.services[t.ServiceID]; ok {
		return svc.Spec.Name
	}
	return ""
}

// removedFrom names the service and stack a removed resource belonged to.
// Swarm removes a task after its service, so a removed service is kept while
// its tasks remain; failing that, a task's container labels name the stack.
func (c *Cache) removedFrom(resource any) (service, stack string) {
	switch r := resource.(type) {
	case swarm.Service:
		return "", r.Spec.Labels[stackLabel]
	case swarm.Config:
		return "", r.Spec.Labels[stackLabel]
	case swarm.Secret:
		return "", r.Spec.Labels[stackLabel]
	case network.Summary:
		return "", r.Labels[stackLabel]
	case volume.Volume:
		return "", r.Labels[stackLabel]
	case swarm.Task:
		c.mu.RLock()
		service, stack = c.removedTaskFromLocked(r)
		c.mu.RUnlock()
		return service, stack
	}

	return "", ""
}

// removedTaskFromLocked is removedFrom's swarm.Task case, for a caller that
// already holds c.mu (for reading or writing).
func (c *Cache) removedTaskFromLocked(t swarm.Task) (service, stack string) {
	svc, ok := c.services[t.ServiceID]
	if !ok {
		svc, ok = c.goneServices[t.ServiceID]
	}

	if ok {
		return svc.Spec.Name, svc.Spec.Labels[stackLabel]
	}

	if t.Spec.ContainerSpec != nil {
		return "", t.Spec.ContainerSpec.Labels[stackLabel]
	}

	return "", ""
}
