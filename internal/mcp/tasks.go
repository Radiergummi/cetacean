package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/cluster"
)

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

// awaitServiceConvergence waits for a mutated service to actually reach the
// state it was asked for, but only when the caller issued the mutation as a
// task. A plain tools/call keeps returning the moment Docker accepts the
// change, which is what every existing client expects — so the predicate is not
// even built on that path.
//
// tasks/cancel cannot interrupt the wait: awaitServiceConvergenceFor detaches
// the context, so a cancelled task is marked cancelled for the client while
// this goroutine keeps polling until it converges or times out.
// cluster.ConvergenceTimeout is the real bound.
func (s *Server) awaitServiceConvergence(
	ctx context.Context,
	req mcplib.CallToolRequest,
	svc swarm.Service,
) error {
	if req.Params.Task == nil {
		return nil
	}

	// svc is Docker's post-mutation view, so its version is the one the cache
	// has to catch up to before the predicate means anything — see
	// awaitServiceConvergenceFor.
	return s.awaitServiceConvergenceFor(
		ctx,
		svc.ID,
		svc.Version.Index,
		cluster.ConvergenceTimeout,
		nil,
	)
}

// awaitServiceConvergenceFor waits up to timeout for svcID to settle, writing
// the last progress line into observed. It is the core awaitServiceConvergence
// wraps; watch needs the same wait with a caller-chosen bound and without the
// task-augmentation early return, and one wait rather than two is what keeps
// the two from drifting on the detachment above.
//
// The convergence wait itself lives in internal/cluster so the REST and MCP
// transports cannot drift on what "settled" means — see cluster.AwaitService
// for the minVersion gate this relies on.
func (s *Server) awaitServiceConvergenceFor(
	ctx context.Context,
	svcID string,
	minVersion uint64,
	timeout time.Duration,
	observed *string,
) error {
	progress, err := cluster.AwaitService(
		// Detached so timeout is the bound rather than the caller's
		// connection. registerTools already detaches the task path; this is
		// what makes watch's own non-cancellability true on a plain call.
		context.WithoutCancel(ctx),
		s.cache,
		svcID,
		minVersion,
		cluster.ConvergencePollInterval,
		timeout,
	)
	if observed != nil {
		*observed = progress
	}

	return err
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
