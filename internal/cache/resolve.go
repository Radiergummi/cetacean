package cache

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

// AmbiguousNameError reports a name that identifies more than one resource.
//
// Swarm enforces unique names for services, configs, secrets and networks, but
// not for node hostnames — two hosts built from one image routinely share one.
// Answering with either would describe the wrong machine on a read a human
// acts on, so the caller is asked to disambiguate instead.
type AmbiguousNameError struct {
	Name string
	IDs  []string
}

func (e *AmbiguousNameError) Error() string {
	return fmt.Sprintf(
		"%q is ambiguous: it names %d resources (%s); use an ID instead",
		e.Name, len(e.IDs), strings.Join(e.IDs, ", "),
	)
}

// resolveIn finds a resource by ID or by name in one pass over items.
//
// The ID lookup comes first, which is both the fast path and the rule that
// settles a collision: a resource whose name happens to be another's ID never
// shadows the ID holder. The name fallback scans, because the alternative — a
// name index the watcher keeps in step on every event — would cover the wrong
// types anyway (services and tasks, the two callers actually address by name,
// are not ResourceMaps) and buys nothing at the size these maps reach.
//
// The map key is the resource's ID, so the ambiguity report needs no separate
// accessor for it. Callers hold the read lock.
func resolveIn[T any](
	items map[string]T,
	identifier string,
	nameOf func(T) string,
) (T, bool, error) {
	if found, ok := items[identifier]; ok {
		return found, true, nil
	}

	var (
		match   T
		matched []string
	)

	for id, item := range items {
		if nameOf(item) == identifier {
			match = item
			matched = append(matched, id)
		}
	}

	if len(matched) == 1 {
		return match, true, nil
	}

	var zero T

	if len(matched) == 0 {
		return zero, false, nil
	}

	return zero, false, &AmbiguousNameError{Name: identifier, IDs: matched}
}

// resolve finds a resource in this map by ID or by the name its nameFunc
// extracts. A map registered without a nameFunc resolves by ID alone.
func (r *ResourceMap[T]) resolve(identifier string) (T, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.nameFunc == nil {
		found, ok := r.get(identifier)

		return found, ok, nil
	}

	return resolveIn(r.items, identifier, r.nameFunc)
}

// ResolveNode returns the node with this ID or hostname.
func (c *Cache) ResolveNode(identifier string) (swarm.Node, bool, error) {
	return c.nodes.resolve(identifier)
}

// ResolveConfig returns the config with this ID or name.
func (c *Cache) ResolveConfig(identifier string) (swarm.Config, bool, error) {
	return c.configs.resolve(identifier)
}

// ResolveSecret returns the secret with this ID or name.
func (c *Cache) ResolveSecret(identifier string) (swarm.Secret, bool, error) {
	return c.secrets.resolve(identifier)
}

// ResolveNetwork returns the network with this ID or name.
func (c *Cache) ResolveNetwork(identifier string) (network.Summary, bool, error) {
	return c.networks.resolve(identifier)
}

// ResolveService returns the service with this ID or name. Services are held
// in a plain map rather than a ResourceMap, so the name accessor is supplied
// here rather than coming from a registered nameFunc.
func (c *Cache) ResolveService(identifier string) (swarm.Service, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return resolveIn(c.services, identifier, func(s swarm.Service) string {
		return s.Spec.Name
	})
}
