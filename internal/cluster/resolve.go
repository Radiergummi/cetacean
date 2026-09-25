package cluster

import (
	"errors"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ResolveTask returns the task with this ID or rendered name. It cannot live
// in the cache beside the other seven: the name is derived here, and this
// package imports the cache.
func ResolveTask(c *cache.Cache, identifier string) (swarm.Task, bool, error) {
	if task, ok := c.GetTask(identifier); ok {
		return task, true, nil
	}

	// Cut at the *last* separator: Docker permits a dot in a service name,
	// while neither half of the suffix TaskName appends can hold one. No
	// separator at all is not a miss either — an unassigned global task
	// renders as the bare service name — so the scan below decides.
	serviceName := identifier
	if dot := strings.LastIndex(identifier, "."); dot >= 0 {
		serviceName = identifier[:dot]
	}

	service, ok, err := c.ResolveService(serviceName)
	if err != nil || !ok {
		return swarm.Task{}, false, err
	}

	// Swarm keeps a record for every replica it has replaced, rendering under
	// the name of the one that succeeded it. A caller means the live one, and
	// where none is live there is nothing to name.
	for _, task := range c.ListTasksByService(service.ID) {
		if TaskIsLive(task) && TaskName(task, &service) == identifier {
			return task, true, nil
		}
	}

	return swarm.Task{}, false, nil
}

// ResolveIdentifier returns the canonical ID the history ring keys a resource
// of this type by, given either that ID or a name. An empty type searches
// every type and answers only when exactly one matches, so a name shared
// across types resolves to nothing rather than to whichever was tried first.
// An unknown type, or an identifier nothing matches, comes back as it went in:
// the ring holds IDs the cache has since forgotten, and those must still match.
func ResolveIdentifier(c *cache.Cache, resourceType, identifier string) (string, error) {
	if identifier == "" {
		return identifier, nil
	}

	if resourceType == "" {
		return resolveAcrossTypes(c, identifier)
	}

	resolve, ok := identifierResolvers[resourceType]
	if !ok {
		return identifier, nil
	}

	id, found, err := resolve(c, identifier)
	if err != nil || !found {
		return identifier, err
	}

	return id, nil
}

// identifierResolvers maps a history event type to the lookup that turns one
// of its identifiers into the ID the ring stores. Volumes and stacks are
// absent because both are keyed by name already, so their identifier is
// canonical as it stands.
var identifierResolvers = map[string]func(*cache.Cache, string) (string, bool, error){
	string(cache.EventService): func(c *cache.Cache, id string) (string, bool, error) {
		svc, found, err := c.ResolveService(id)

		return svc.ID, found, err
	},
	string(cache.EventNode): func(c *cache.Cache, id string) (string, bool, error) {
		node, found, err := c.ResolveNode(id)

		return node.ID, found, err
	},
	string(cache.EventConfig): func(c *cache.Cache, id string) (string, bool, error) {
		cfg, found, err := c.ResolveConfig(id)

		return cfg.ID, found, err
	},
	string(cache.EventSecret): func(c *cache.Cache, id string) (string, bool, error) {
		sec, found, err := c.ResolveSecret(id)

		return sec.ID, found, err
	},
	string(cache.EventNetwork): func(c *cache.Cache, id string) (string, bool, error) {
		net, found, err := c.ResolveNetwork(id)

		return net.ID, found, err
	},
	string(cache.EventTask): func(c *cache.Cache, id string) (string, bool, error) {
		task, found, err := ResolveTask(c, id)

		return task.ID, found, err
	},
}

// resolveAcrossTypes answers only an identifier that is unambiguous over every
// type. An *cache.AmbiguousNameError within one type is the same answer as a
// name matching two types: the caller has not said enough to be served, and
// without a type there is no ACL prefix to report the candidates under.
func resolveAcrossTypes(c *cache.Cache, identifier string) (string, error) {
	var resolved string

	for _, resolve := range identifierResolvers {
		id, found, err := resolve(c, identifier)
		if _, ambiguous := errors.AsType[*cache.AmbiguousNameError](err); ambiguous {
			return identifier, nil
		}

		if err != nil {
			return identifier, err
		}

		if !found {
			continue
		}

		if resolved != "" && resolved != id {
			return identifier, nil
		}

		resolved = id
	}

	if resolved == "" {
		return identifier, nil
	}

	return resolved, nil
}
