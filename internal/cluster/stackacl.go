package cluster

import (
	"slices"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// FilterStackDetail drops the members the identity may not read. A resource
// label narrows per resource, and the stack itself carries none, so each
// member is checked on its own rather than covered by the stack's grant.
func FilterStackDetail(
	e *acl.Evaluator,
	id *auth.Identity,
	d cache.StackDetail,
) cache.StackDetail {
	d.Services = acl.Filter(e, id, "read", d.Services,
		func(s swarm.Service) string { return "service:" + s.Spec.Name })
	d.Configs = acl.Filter(e, id, "read", d.Configs,
		func(c swarm.Config) string { return "config:" + c.Spec.Name })
	d.Secrets = acl.Filter(e, id, "read", d.Secrets,
		func(s swarm.Secret) string { return "secret:" + s.Spec.Name })
	d.Networks = acl.Filter(e, id, "read", d.Networks,
		func(n network.Summary) string { return "network:" + n.Name })
	d.Volumes = acl.Filter(e, id, "read", d.Volumes,
		func(v volume.Volume) string { return "volume:" + v.Name })

	return d
}

// WithheldStackMembers names the stack members the identity may not read, as
// "type:id" keys (a volume's ID is its name), or nil when it may read them all.
// It reads the cache item by item rather than under one lock, because the
// check itself reads labels back through the same cache.
func WithheldStackMembers(e *acl.Evaluator, id *auth.Identity, c *cache.Cache) map[string]bool {
	allows := e.Allows(id, "read")

	var withheld map[string]bool
	withhold := func(resType, resID, name string, found bool) {
		if found && !allows(resType, name) {
			if withheld == nil {
				withheld = map[string]bool{}
			}
			withheld[resType+":"+resID] = true
		}
	}

	for _, stack := range c.ListStacks() {
		for _, resID := range stack.Services {
			s, ok := c.GetService(resID)
			withhold("service", resID, s.Spec.Name, ok)
		}
		for _, resID := range stack.Configs {
			cfg, ok := c.GetConfig(resID)
			withhold("config", resID, cfg.Spec.Name, ok)
		}
		for _, resID := range stack.Secrets {
			s, ok := c.GetSecret(resID)
			withhold("secret", resID, s.Spec.Name, ok)
		}
		for _, resID := range stack.Networks {
			n, ok := c.GetNetwork(resID)
			withhold("network", resID, n.Name, ok)
		}
		for _, name := range stack.Volumes {
			withhold("volume", name, name, true)
		}
	}

	return withheld
}

// FilterStacks drops the withheld members from each stack's member lists.
func FilterStacks(stacks []cache.Stack, withheld map[string]bool) []cache.Stack {
	if len(withheld) == 0 {
		return stacks
	}

	keep := func(resType string, ids []string) []string {
		return slices.DeleteFunc(
			ids,
			func(resID string) bool { return withheld[resType+":"+resID] },
		)
	}
	for i := range stacks {
		stacks[i].Services = keep("service", stacks[i].Services)
		stacks[i].Configs = keep("config", stacks[i].Configs)
		stacks[i].Secrets = keep("secret", stacks[i].Secrets)
		stacks[i].Networks = keep("network", stacks[i].Networks)
		stacks[i].Volumes = keep("volume", stacks[i].Volumes)
	}

	return stacks
}
