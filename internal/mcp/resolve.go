package mcp

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// resolveTask returns the task with this ID or rendered name. The rule lives
// in internal/cluster so REST cannot disagree about what a task name denotes.
func (s *Server) resolveTask(identifier string) (swarm.Task, bool, error) {
	return cluster.ResolveTask(s.cache, identifier)
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
