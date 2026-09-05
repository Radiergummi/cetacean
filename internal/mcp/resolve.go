package mcp

import (
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// resolveTask returns the task with this ID or rendered name.
//
// A task is shown as "<service>.<slot>" (or "<service>.<node>" when the
// service is global) everywhere a caller sees one, so that is the identifier
// they paste back and the one a completion offers. The other seven types
// resolve inside the cache, which owns the ID keying; a task cannot, because
// its name is not its own — it is derived from its parent, and the rule that
// derives it lives in internal/cluster, which imports the cache rather than
// the other way round.
//
// Splitting the identifier first is what keeps this cheap: resolving the
// parent by name and scanning only that service's tasks turns a scan of every
// task in the cluster into a scan of one service's replicas.
func (s *Server) resolveTask(identifier string) (swarm.Task, bool, error) {
	if task, ok := s.cache.GetTask(identifier); ok {
		return task, true, nil
	}

	// Cut at the *last* separator, not the first: Docker permits a dot in a
	// service name, while neither half of the suffix cluster.TaskName appends
	// can hold one — a slot is a number and a node ID is hex. Splitting at the
	// first dot resolved "api.example.com.2" against a service called "api"
	// and found nothing, so a name a completion had just offered read back as
	// not found.
	dot := strings.LastIndex(identifier, ".")
	if dot < 0 {
		return swarm.Task{}, false, nil
	}

	serviceName := identifier[:dot]

	service, ok, err := s.cache.ResolveService(serviceName)
	if err != nil || !ok {
		return swarm.Task{}, false, err
	}

	for _, task := range s.cache.ListTasksByService(service.ID) {
		if cluster.TaskName(task, &service) == identifier {
			return task, true, nil
		}
	}

	return swarm.Task{}, false, nil
}

// resolved turns a resolver's (value, found, error) into the (value, error)
// every lookupResource branch actually wants, spelling the not-found once
// instead of six times.
//
// It returns a function taking the URI so the three results of a resolver call
// can be passed straight through — Go forwards a multi-valued call only when
// it is the sole argument, so the URI has to arrive separately.
func resolved[T any](value T, found bool, err error) func(uri string) (T, error) {
	return func(uri string) (T, error) {
		var zero T

		if err != nil {
			return zero, err
		}

		if !found {
			return zero, notFound(uri)
		}

		return value, nil
	}
}
