package mcp

import (
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// resolveTask returns the task with this ID or rendered name. The other seven
// types resolve inside the cache, which owns the ID keying; a task cannot,
// since its name is derived by internal/cluster, which imports the cache.
// Splitting first turns a scan of every task into one service's replicas.
func (s *Server) resolveTask(identifier string) (swarm.Task, bool, error) {
	if task, ok := s.cache.GetTask(identifier); ok {
		return task, true, nil
	}

	// Cut at the *last* separator: Docker permits a dot in a service name,
	// while neither half of the suffix cluster.TaskName appends can hold one.
	// No separator at all is not a miss either — an unassigned global task
	// renders as the bare service name — so the scan below decides.
	serviceName := identifier
	if dot := strings.LastIndex(identifier, "."); dot >= 0 {
		serviceName = identifier[:dot]
	}

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
// every lookupResource branch wants, spelling the not-found once. It returns a
// function taking the URI because Go forwards a multi-valued call only when it
// is the sole argument.
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
