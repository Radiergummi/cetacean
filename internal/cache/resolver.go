package cache

// StackOf returns the stack name for a resource, or "" if it doesn't belong
// to a stack. Reads the com.docker.stack.namespace label directly.
func (c *Cache) StackOf(resourceType, resourceID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	const label = "com.docker.stack.namespace"

	switch resourceType {
	case "service":
		if s, ok := c.services[resourceID]; ok {
			return s.Spec.Labels[label]
		}
	case "config":
		if cfg, ok := c.configs[resourceID]; ok {
			return cfg.Spec.Labels[label]
		}
	case "secret":
		if s, ok := c.secrets[resourceID]; ok {
			return s.Spec.Labels[label]
		}
	case "network":
		if n, ok := c.networks[resourceID]; ok {
			return n.Labels[label]
		}
	case "volume":
		if v, ok := c.volumes[resourceID]; ok {
			return v.Labels[label]
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
