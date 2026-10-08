package cache

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

// LabelsOf returns the labels for a resource, or nil if unknown.
func (c *Cache) LabelsOf(resourceType, name string) map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch resourceType {
	case "service":
		for _, s := range c.services {
			if s.Spec.Name == name {
				return s.Spec.Labels
			}
		}
	case "config":
		for _, cfg := range c.configs.items {
			if cfg.Spec.Name == name {
				return cfg.Spec.Labels
			}
		}
	case "secret":
		for _, s := range c.secrets.items {
			if s.Spec.Name == name {
				return s.Spec.Labels
			}
		}
	case "network":
		for _, n := range c.networks.items {
			if n.Name == name {
				return n.Labels
			}
		}
	case "volume":
		for _, v := range c.volumes.items {
			if v.Name == name {
				return v.Labels
			}
		}
	case "node":
		return c.nodeLabels()[name]
	}
	return nil
}

// LabelsByType returns the labels of every resource of a type, keyed by the
// name an ACL resource expression uses for it, in one pass under one read lock.
func (c *Cache) LabelsByType(resourceType string) map[string]map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	labels := map[string]map[string]string{}
	switch resourceType {
	case "service":
		for _, s := range c.services {
			labels[s.Spec.Name] = s.Spec.Labels
		}
	case "config":
		for _, cfg := range c.configs.items {
			labels[cfg.Spec.Name] = cfg.Spec.Labels
		}
	case "secret":
		for _, s := range c.secrets.items {
			labels[s.Spec.Name] = s.Spec.Labels
		}
	case "network":
		for _, n := range c.networks.items {
			labels[n.Name] = n.Labels
		}
	case "volume":
		for _, v := range c.volumes.items {
			labels[v.Name] = v.Labels
		}
	case "node":
		return c.nodeLabels()
	}
	return labels
}

// labelledForNobody stands in for the labels of a hostname two nodes share. It
// carries acl.LabelRead, spelled out because acl's tests import this package,
// so an absent policy's allow-all does not apply, and names no audience.
var labelledForNobody = map[string]string{"cetacean.acl.read": ""}

// nodeLabels maps every name a node answers to onto its labels, resolved the
// way resolveIn resolves them: an ID is always its own node's, and a hostname
// is a node's only when no ID spells it and no other node shares it. Callers
// hold the lock.
func (c *Cache) nodeLabels() map[string]map[string]string {
	labels := make(map[string]map[string]string, 2*len(c.nodes.items))
	for id, n := range c.nodes.items {
		labels[id] = n.Spec.Labels
	}

	byHostname := map[string]bool{}
	for _, n := range c.nodes.items {
		hostname := n.Description.Hostname
		if _, isID := c.nodes.items[hostname]; hostname == "" || isID {
			continue
		}

		if byHostname[hostname] {
			labels[hostname] = labelledForNobody
		} else {
			byHostname[hostname] = true
			labels[hostname] = n.Spec.Labels
		}
	}

	return labels
}
