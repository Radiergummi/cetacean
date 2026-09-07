package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

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

	return s.awaitServiceConvergenceFor(ctx, svc.ID, convergenceTimeout, nil)
}

// awaitServiceConvergenceFor waits up to timeout for svcID to settle, writing
// the last progress line into observed. It is the core awaitServiceConvergence
// wraps; watch needs the same wait with a caller-chosen bound and without the
// task-augmentation early return, and one wait rather than two is what keeps
// the two from drifting on the detachment above.
func (s *Server) awaitServiceConvergenceFor(
	ctx context.Context,
	svcID string,
	timeout time.Duration,
	observed *string,
) error {
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	converged := serviceConverged(s.cache, svcID)

	return s.awaitConvergence(detached, func() (bool, string) {
		done, progress := converged()
		if observed != nil {
			*observed = progress
		}

		return done, progress
	})
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

// boundTaskTTL fills in and caps how long mcp-go retains a task's result,
// reporting whether it had to cut a client's request down.
//
// mcp-go deletes a task record only from scheduleTaskCleanup, which it starts
// solely when the client supplied params.task.ttl — so an omitted TTL pins a
// full CallToolResult for the life of the process, and a large one pins it for
// as long as the client cared to name. Supplying the number mcp-go already
// knows how to honour is the whole mechanism; nothing here reimplements
// retention.
//
// A zero default leaves an absent TTL absent, and a zero ceiling clamps
// nothing, so an operator can disable either half. The fill-in is clamped
// along with everything else, so a default configured above the ceiling cannot
// escape it.
func boundTaskTTL(task *mcplib.TaskParams, def, ceiling time.Duration) bool {
	// A call that carried no task augmentation must stay that way. Inventing
	// params here would turn every ordinary synchronous tools/call into a
	// task, and mcp-go would answer it with a handle instead of the result.
	if task == nil {
		return false
	}

	// Non-positive is not a shorter retention, it is no cleanup at all — the
	// same leak as an absent field, so it gets the same treatment.
	if def > 0 && (task.TTL == nil || *task.TTL <= 0) {
		task.TTL = new(def.Milliseconds())
	}

	if ceiling <= 0 || task.TTL == nil || *task.TTL <= ceiling.Milliseconds() {
		return false
	}

	task.TTL = new(ceiling.Milliseconds())

	return true
}

// installTaskTTLHook bounds the retention of every task-augmented tool call.
//
// mcp-go has no server-side default TTL and no exported way into its task map:
// scheduleTaskCleanup is private and starts only when the client supplied
// params.task.ttl, so a client that omits it pins a full CallToolResult for the
// life of the process. What mcp-go does give us is this hook, called with a
// pointer to the request it passes to handleToolCall on the very next line
// (server/request_handler.go:520-521) — so filling the field in here is
// indistinguishable, to everything downstream, from the client having sent it.
//
// That adjacency is the assumption the whole mechanism rests on, and it is not
// a documented contract. TestTaskWithoutTTLIsStillReleased drives a real
// tools/call and waits for the record to go, so a future bump that reorders or
// copies between those two lines fails the build rather than quietly restoring
// the leak.
func (s *Server) installTaskTTLHook(h *mcpserver.Hooks) {
	h.AddBeforeCallTool(func(_ context.Context, _ any, msg *mcplib.CallToolRequest) {
		if !boundTaskTTL(msg.Params.Task, s.config.TaskTTL, s.config.MaxTaskTTL) {
			return
		}

		// Served, not refused: the caller asked for a cluster mutation, and
		// failing it over a retention preference would be the wrong trade. Debug
		// rather than warn — a client naming a long TTL is being optimistic,
		// not hostile.
		slog.Debug("clamped MCP task TTL to the configured maximum",
			"tool", msg.Params.Name,
			"max", s.config.MaxTaskTTL,
		)
	})
}
