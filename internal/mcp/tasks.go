package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
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
// outstanding; it becomes the failure message when the wait times out.
type convergenceFunc func() (done bool, status string)

// taskSupportOptional marks a service mutation as pollable. Convergence takes
// far longer than the Docker call that starts it, so a client may ask for the
// mutation as a task and poll it. Optional, not required: a plain call still
// returns as soon as Swarm accepts the change.
//
// Every tool declaring this must call awaitServiceConvergence in its handler,
// or it reports a task complete while the cluster is still catching up.
// TestEveryTaskToolAwaitsConvergence enforces the pairing.
func taskSupportOptional() mcplib.ToolOption {
	return mcplib.WithTaskSupport(mcplib.TaskSupportOptional)
}

// awaitConvergence blocks until fn reports done or ctx expires. Docker's write
// APIs return as soon as the swarm accepts a spec change, long before the new
// state is real; this is what turns "accepted" into "actually running".
//
// The predicate runs before the first tick, so an already-satisfied mutation
// returns without waiting.
func (s *Server) awaitConvergence(ctx context.Context, fn convergenceFunc) error {
	ticker := time.NewTicker(convergencePollInterval)
	defer ticker.Stop()

	for {
		done, status := fn()
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			// Report what was still outstanding rather than a bare deadline:
			// "waiting: 2/5 replicas running" tells an agent why its task
			// failed, where "context deadline exceeded" does not.
			return fmt.Errorf("service did not converge (%s): %w", status, ctx.Err())

		case <-ticker.C:
		}
	}
}

// awaitServiceConvergence waits for a mutated service to actually reach the
// state it was asked for, but only when the caller issued the mutation as a
// task. A plain tools/call keeps returning the moment Docker accepts the
// change, which is what every existing client expects — so the predicate is not
// even built on that path.
//
// The context is deliberately detached first. mcp-go runs a task-augmented tool
// on a goroutine holding the *HTTP request* context, and net/http cancels that
// as soon as the create-task response is written — so by the time this runs,
// ctx is almost always already cancelled. Honouring it would fail every task
// within microseconds of starting it. convergenceTimeout is the real bound.
//
// The cost is that tasks/cancel cannot interrupt the wait: mcp-go cancels the
// same context the transport does, so the two are indistinguishable here. A
// cancelled task is still marked cancelled for the client; this goroutine keeps
// polling an in-memory map until it converges or times out.
func (s *Server) awaitServiceConvergence(
	ctx context.Context,
	req mcplib.CallToolRequest,
	svc swarm.Service,
) error {
	if req.Params.Task == nil {
		return nil
	}

	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), convergenceTimeout)
	defer cancel()

	return s.awaitConvergence(detached, serviceConverged(s.cache, svc.ID))
}

// serviceConverged watches one service by ID. The convergence rule itself lives
// in internal/cluster so the REST and MCP transports cannot drift on what
// "settled" means; this only supplies the cache reads.
func serviceConverged(c *cache.Cache, serviceID string) convergenceFunc {
	return func() (bool, string) {
		svc, ok := c.GetService(serviceID)
		if !ok {
			return false, "service not in the cache yet"
		}

		return cluster.ServiceConverged(svc, c.RunningTaskCount(svc.ID))
	}
}
