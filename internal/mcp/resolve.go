package mcp

import (
	"context"
	"errors"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// visibleNodeErr withholds an ambiguous hostname from a caller who cannot read
// it, since the error lists every candidate ID; the caller then answers not
// found. Only node hostnames can be ambiguous, and all candidates share one key.
func (s *Server) visibleNodeErr(ctx context.Context, err error) error {
	if ambiguous, ok := errors.AsType[*cache.AmbiguousNameError](err); ok &&
		s.checkRead(ctx, "node", ambiguous.Name) != nil {
		return nil
	}

	return err
}

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
