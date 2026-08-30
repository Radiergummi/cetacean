package cache

import "slices"

const stackLabel = "com.docker.stack.namespace"

// addToStack incrementally adds a resource to the appropriate stack. Must be called with c.mu held for writing.
func (c *Cache) addToStack(resource EventType, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		if resource != EventService {
			return // never create a stack entry without at least one service
		}
		s = Stack{Name: ns}
	}
	switch resource {
	case EventService:
		s.Services = appendUnique(s.Services, id)
	case EventConfig:
		s.Configs = appendUnique(s.Configs, id)
	case EventSecret:
		s.Secrets = appendUnique(s.Secrets, id)
	case EventNetwork:
		s.Networks = appendUnique(s.Networks, id)
	case EventVolume:
		s.Volumes = appendUnique(s.Volumes, id)
	default:
		// Nodes, tasks, stacks, and sync events do not belong to a stack.
	}
	c.stacks[ns] = s
}

// removeFromStack incrementally removes a resource from its stack. Must be called with c.mu held for writing.
func (c *Cache) removeFromStack(resource EventType, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		return
	}
	switch resource {
	case EventService:
		s.Services = removeStr(s.Services, id)
	case EventConfig:
		s.Configs = removeStr(s.Configs, id)
	case EventSecret:
		s.Secrets = removeStr(s.Secrets, id)
	case EventNetwork:
		s.Networks = removeStr(s.Networks, id)
	case EventVolume:
		s.Volumes = removeStr(s.Volumes, id)
	default:
		// Nodes, tasks, stacks, and sync events do not belong to a stack.
	}
	if len(s.Services) == 0 {
		delete(c.stacks, ns)
	} else {
		c.stacks[ns] = s
	}
}

// rebuildStacks rebuilds all stacks from the current resource maps. Must be called with c.mu held for writing.
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

	// IDs come from map keys, so they are unique within each resource type.
	// No need for appendUnique during a full rebuild.
	for id, svc := range c.services {
		if ns, ok := svc.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Services = append(s.Services, id)
		}
	}

	for id, cfg := range c.configs.items {
		if ns, ok := cfg.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Configs = append(s.Configs, id)
		}
	}

	for id, sec := range c.secrets.items {
		if ns, ok := sec.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Secrets = append(s.Secrets, id)
		}
	}

	for id, net := range c.networks.items {
		if ns, ok := net.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Networks = append(s.Networks, id)
		}
	}

	for name, vol := range c.volumes.items {
		if ns, ok := vol.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Volumes = append(s.Volumes, name)
		}
	}

	result := make(map[string]Stack, len(stacks))
	for name, s := range stacks {
		// Only include stacks that have at least one service; stacks with
		// only leftover volumes/configs/secrets/networks are ghost stacks
		// from removed deployments and should not appear in the stacks list.
		if len(s.Services) == 0 {
			continue
		}

		// Member IDs were collected by ranging over maps. Sort them so a stack's
		// membership — and everything derived from it — is stable between calls.
		slices.Sort(s.Services)
		slices.Sort(s.Configs)
		slices.Sort(s.Secrets)
		slices.Sort(s.Networks)
		slices.Sort(s.Volumes)

		result[name] = *s
	}
	c.stacks = result
}

// appendUnique inserts v in sorted position, keeping stack membership ordered
// as resources arrive one event at a time. rebuildStacks sorts the same slices
// after a full rebuild; between them the invariant holds for every read.
func appendUnique(sl []string, v string) []string {
	i, found := slices.BinarySearch(sl, v)
	if found {
		return sl
	}
	return slices.Insert(sl, i, v)
}

func removeStr(sl []string, v string) []string {
	return slices.DeleteFunc(sl, func(s string) bool { return s == v })
}
