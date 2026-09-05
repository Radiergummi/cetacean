package mcp

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// resolveOne finds a resource by the identifier a caller has in hand, whether
// that is its ID or its name.
//
// The cache keys six of the eight resource types by ID (only stacks and
// volumes are keyed by name), so an ID lookup is the fast path and a name
// lookup is the fallback — which also settles the collision: a resource whose
// name happens to be another's ID never shadows the ID holder.
//
// The fallback scans, since there is no name index to consult. That cost is
// paid only when an identifier is not an ID, and the alternative — a second
// index the watcher would have to keep in step on every event — buys a scan of
// an in-memory slice that is already the size the caller is browsing.
func resolveOne[T any](
	get func(string) (T, bool),
	list func() []T,
	nameOf func(T) string,
	idOf func(T) string,
	identifier string,
) (T, bool, error) {
	if found, ok := get(identifier); ok {
		return found, true, nil
	}

	var (
		match   T
		matched []string
	)

	for _, item := range list() {
		if nameOf(item) != identifier {
			continue
		}

		match = item
		matched = append(matched, idOf(item))
	}

	switch len(matched) {
	case 0:
		var zero T

		return zero, false, nil

	case 1:
		return match, true, nil

	default:
		var zero T

		// Swarm does not enforce unique node hostnames, and two hosts built
		// from one image routinely share one. Answering with either would
		// describe the wrong machine on a read a human acts on, so the caller
		// is told to disambiguate instead.
		return zero, false, fmt.Errorf(
			"%q is ambiguous: it names %d resources (%s); use an ID instead",
			identifier, len(matched), strings.Join(matched, ", "),
		)
	}
}

// taskName is cluster.TaskName with the parent service resolved from the
// cache, so a task can be addressed the way find and describe render it —
// "<service>.<slot>" — rather than only by its ID. A task whose service is
// gone falls back to its own ID, which is what TaskName does with a nil
// service.
func (s *Server) taskName(task swarm.Task) string {
	var parent *swarm.Service

	if svc, ok := s.cache.GetService(task.ServiceID); ok {
		parent = &svc
	}

	return cluster.TaskName(task, parent)
}
