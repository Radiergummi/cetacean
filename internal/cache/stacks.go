package cache


const stackLabel = "com.docker.stack.namespace"

// addToStack incrementally adds a resource to the appropriate stack. Must be called with c.mu held for writing.
func (c *Cache) addToStack(resource, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		s = Stack{Name: ns}
	}
	switch resource {
	case "service":
		s.Services = appendUnique(s.Services, id)
	case "config":
		s.Configs = appendUnique(s.Configs, id)
	case "secret":
		s.Secrets = appendUnique(s.Secrets, id)
	case "network":
		s.Networks = appendUnique(s.Networks, id)
	case "volume":
		s.Volumes = appendUnique(s.Volumes, id)
	}
	c.stacks[ns] = s
}

// removeFromStack incrementally removes a resource from its stack. Must be called with c.mu held for writing.
func (c *Cache) removeFromStack(resource, id string, labels map[string]string) {
	ns, ok := labels[stackLabel]
	if !ok {
		return
	}
	s, exists := c.stacks[ns]
	if !exists {
		return
	}
	switch resource {
	case "service":
		s.Services = removeStr(s.Services, id)
	case "config":
		s.Configs = removeStr(s.Configs, id)
	case "secret":
		s.Secrets = removeStr(s.Secrets, id)
	case "network":
		s.Networks = removeStr(s.Networks, id)
	case "volume":
		s.Volumes = removeStr(s.Volumes, id)
	}
	if len(s.Services)+len(s.Configs)+len(s.Secrets)+len(s.Networks)+len(s.Volumes) == 0 {
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

	for id, svc := range c.services {
		if ns, ok := svc.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Services = appendUnique(s.Services, id)
		}
	}

	for id, cfg := range c.configs {
		if ns, ok := cfg.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Configs = appendUnique(s.Configs, id)
		}
	}

	for id, sec := range c.secrets {
		if ns, ok := sec.Spec.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Secrets = appendUnique(s.Secrets, id)
		}
	}

	for id, net := range c.networks {
		if ns, ok := net.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Networks = appendUnique(s.Networks, id)
		}
	}

	for name, vol := range c.volumes {
		if ns, ok := vol.Labels[stackLabel]; ok {
			s := ensure(ns)
			s.Volumes = appendUnique(s.Volumes, name)
		}
	}

	result := make(map[string]Stack, len(stacks))
	for name, s := range stacks {
		result[name] = *s
	}
	c.stacks = result
}


func appendUnique(sl []string, v string) []string {
	for _, s := range sl {
		if s == v {
			return sl
		}
	}
	return append(sl, v)
}

func removeStr(sl []string, v string) []string {
	for i, s := range sl {
		if s == v {
			return append(sl[:i], sl[i+1:]...)
		}
	}
	return sl
}
