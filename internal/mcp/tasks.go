package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
)

const (
	// convergencePollInterval is how often a running task re-checks the cache.
	// The cache is in-memory and the predicate is a cheap read, so this is
	// bounded by how quickly we want to notice convergence, not by cost.
	convergencePollInterval = 500 * time.Millisecond

	// convergenceTimeout bounds how long a task waits for the cluster to reach
	// the requested state before failing. A mutation that has not converged by
	// then is not going to on its own — an unsatisfiable placement constraint,
	// an image that will not pull — and an agent is better told so than left
	// polling a task that never finishes.
	convergenceTimeout = 5 * time.Minute
)

// convergenceFunc reports whether the cluster has reached the state a mutation
// asked for. status is a human-readable progress line describing what is still
// outstanding.
type convergenceFunc func(ctx context.Context, c *cache.Cache) (done bool, status string)

// awaitConvergence blocks until fn reports done or ctx expires. Docker's write
// APIs return as soon as the swarm accepts a spec change, long before the new
// state is real; this is what turns "accepted" into "actually running".
func (s *Server) awaitConvergence(ctx context.Context, fn convergenceFunc) error {
	// Check once before waiting: an already-satisfied mutation should not pay
	// a full tick.
	if done, _ := fn(ctx, s.cache); done {
		return nil
	}

	ticker := time.NewTicker(convergencePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if done, _ := fn(ctx, s.cache); done {
				return nil
			}
		}
	}
}

// awaitIfTask waits for convergence, but only when the caller asked for the
// mutation as a task. A plain tools/call keeps returning the moment Docker
// accepts the change, which is what every existing client expects.
//
// The context is deliberately detached first. mcp-go runs a task-augmented
// tool on a goroutine holding the *HTTP request* context, and net/http cancels
// that as soon as the create-task response is written — so by the time this
// runs, ctx is almost always already cancelled. Honouring it would fail every
// task within microseconds of starting it. convergenceTimeout is the real
// bound instead.
//
// The cost is that tasks/cancel cannot interrupt the wait: mcp-go cancels the
// same context the transport does, so the two are indistinguishable here. A
// cancelled task is still marked cancelled for the client; this goroutine
// keeps polling an in-memory map until it converges or times out.
func (s *Server) awaitIfTask(
	ctx context.Context,
	req mcplib.CallToolRequest,
	fn convergenceFunc,
) error {
	if req.Params.Task == nil {
		return nil
	}

	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), convergenceTimeout)
	defer cancel()

	return s.awaitConvergence(detached, fn)
}

// serviceConverged reports a replicated service as converged once its running
// task count matches its desired replica count and no update is in progress.
//
// A global service has no replica count to compare against, so it converges as
// soon as no update is in flight: the desired count is whatever the scheduler
// decides the cluster's nodes can carry.
func serviceConverged(serviceID string) convergenceFunc {
	return func(_ context.Context, c *cache.Cache) (bool, string) {
		svc, ok := c.GetService(serviceID)
		if !ok {
			return false, "service not in the cache yet"
		}

		running := c.RunningTaskCount(svc.ID)

		// An in-flight rolling update means tasks are still being replaced;
		// wait it out rather than reporting a transient match as success.
		if svc.UpdateStatus != nil && svc.UpdateStatus.State == swarm.UpdateStateUpdating {
			return false, fmt.Sprintf("rolling update in progress (%d running)", running)
		}

		if svc.Spec.Mode.Replicated == nil || svc.Spec.Mode.Replicated.Replicas == nil {
			return true, fmt.Sprintf("converged: %d tasks running", running)
		}

		desired := int(*svc.Spec.Mode.Replicated.Replicas)
		if running == desired {
			return true, fmt.Sprintf("converged: %d/%d replicas running", running, desired)
		}

		return false, fmt.Sprintf("waiting: %d/%d replicas running", running, desired)
	}
}

// convergenceTarget picks the service ID to watch. Tools accept an ID or a
// name, but the cache is keyed by ID, so the ID the write returned is the
// reliable one; the caller's argument is the fallback for a writer that does
// not echo it back.
func convergenceTarget(svc swarm.Service, fallback string) string {
	if svc.ID != "" {
		return svc.ID
	}

	return fallback
}
