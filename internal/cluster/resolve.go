package cluster

import (
	"errors"
	"slices"
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
	if before, _, ok := strings.CutLast(identifier, "."); ok {
		serviceName = before
	}

	service, ok, err := c.ResolveService(serviceName)
	if err != nil || !ok {
		return swarm.Task{}, false, err
	}

	// Swarm keeps a record for every replica it has replaced, rendering under
	// the name of the one that succeeded it. A caller means the live one, and
	// where none is live there is nothing to name. A start-first update keeps
	// two live, and the name means the newer.
	var (
		match swarm.Task
		found bool
	)

	for _, task := range c.ListTasksByService(service.ID) {
		if TaskIsLive(task) && TaskName(task, &service) == identifier &&
			(!found || task.CreatedAt.After(match.CreatedAt)) {
			match, found = task, true
		}
	}

	return match, found, nil
}

// ResolveTaskAs is ResolveTask for a caller reading through canRead; nil reads
// all. A rendered name resolves only through a service the caller can read,
// since the name is the service's.
func ResolveTaskAs(
	c *cache.Cache,
	identifier string,
	canRead func(resource string) bool,
) (swarm.Task, bool, error) {
	task, ok, err := ResolveTask(c, identifier)
	if err != nil || !ok || canRead == nil || task.ID == identifier {
		return task, ok, err
	}

	if svc, ok := c.GetService(task.ServiceID); !ok || !canRead("service:"+svc.Spec.Name) {
		return swarm.Task{}, false, nil
	}

	return task, true, nil
}

// ResolveIdentifier returns the canonical ID the history ring keys a resource
// of this type by, given either that ID or a name, as a caller reading through
// canRead sees it. An empty type searches every type and answers only when
// exactly one matches, so a name shared across types resolves to nothing. An
// unknown type, or an identifier nothing matches, comes back as it went in:
// the ring holds IDs the cache has since forgotten, and those must still match.
func ResolveIdentifier(
	c *cache.Cache,
	resourceType, identifier string,
	canRead func(resource string) bool,
) (string, error) {
	if identifier == "" {
		return identifier, nil
	}

	if resourceType == "" {
		return resolveAcrossTypes(c, identifier, canRead)
	}

	if !slices.Contains(identifierKinds, resourceType) {
		return identifier, nil
	}

	record, found, err := LookupAs(c, resourceType, identifier, canRead)
	if err != nil || !found {
		return identifier, err
	}

	return record.ID, nil
}

// identifierKinds are the history event types whose identifiers resolve to the
// ID the ring stores. Volumes and stacks are absent because both are keyed by
// name already, so their identifier is canonical as it stands.
var identifierKinds = []string{
	string(cache.EventService),
	string(cache.EventNode),
	string(cache.EventConfig),
	string(cache.EventSecret),
	string(cache.EventNetwork),
	string(cache.EventTask),
}

// resolveAcrossTypes answers only an identifier that is unambiguous over every
// type. An *cache.AmbiguousNameError within one type is the same answer as a
// name matching two types: the caller has not said enough to be served, and
// without a type there is no ACL prefix to report the candidates under.
func resolveAcrossTypes(
	c *cache.Cache,
	identifier string,
	canRead func(resource string) bool,
) (string, error) {
	var resolved string

	for _, kind := range identifierKinds {
		record, found, err := LookupAs(c, kind, identifier, canRead)
		if _, ambiguous := errors.AsType[*cache.AmbiguousNameError](err); ambiguous {
			return identifier, nil
		}

		if err != nil {
			return identifier, err
		}

		if !found {
			continue
		}

		if resolved != "" && resolved != record.ID {
			return identifier, nil
		}

		resolved = record.ID
	}

	if resolved == "" {
		return identifier, nil
	}

	return resolved, nil
}
