package mcp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// taskSupportOptional marks a service mutation as pollable: convergence takes
// far longer than the Docker call that starts it. Optional, so a plain call
// still returns as soon as Swarm accepts. Every tool declaring it must call
// awaitServiceConvergence, or it reports complete while the cluster catches up.
func taskSupportOptional() mcplib.ToolOption {
	return mcplib.WithTaskSupport(mcplib.TaskSupportOptional)
}

// awaitServiceConvergence waits for a mutated service to reach the state it was
// asked for, but only when the mutation was issued as a task — a plain call
// returns as soon as Docker accepts, and the predicate is not even built.
// tasks/cancel cannot interrupt it: the context is detached.
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
// the last progress line into observed. watch needs the same wait with its own
// bound and no task early-return. The rule lives in cluster.AwaitService, so
// REST and MCP cannot disagree on what "settled" means.
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
// reporting whether it cut the request down: mcp-go starts its cleanup only
// when the client supplied a TTL. A zero default or ceiling disables that half,
// and the fill-in is clamped too, so it cannot escape the ceiling.
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

// installTaskTTLHook bounds the retention of every task-augmented tool call:
// mcp-go has no server-side default TTL and starts its cleanup only for a TTL
// the client sent. This hook gets a pointer to the request handleToolCall
// receives on the next line — an adjacency nothing documents.
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

// taskOwners records which identity created each task. mcp-go isolates tasks
// by session, and a stateless request has none, so it would serve any task to
// whoever names its ID.
type taskOwners struct {
	mu     sync.Mutex
	owners map[string]string
}

func (o *taskOwners) record(taskID, owner string, retention time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.owners == nil {
		o.owners = make(map[string]string)
	}

	o.owners[taskID] = owner

	if retention > 0 {
		time.AfterFunc(retention, func() {
			o.mu.Lock()
			defer o.mu.Unlock()

			delete(o.owners, taskID)
		})
	}
}

// owns fails closed: a task with no record belongs to nobody.
func (o *taskOwners) owns(taskID, caller string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	owner, ok := o.owners[taskID]

	return ok && owner == caller
}

// taskOwner keys a task to its creator. The provider is part of the key: the
// same subject from two providers need not be the same principal.
func taskOwner(ctx context.Context) string {
	id := auth.IdentityFromContext(ctx)
	if id == nil {
		return ""
	}

	return id.Provider + "\x00" + id.Subject
}

// taskOwnerRetention is the longest retention installTaskTTLHook can leave a
// task with, which its ownership record must outlast. Zero: some task may live
// as long as the process, and so must its record.
func (s *Server) taskOwnerRetention() time.Duration {
	if s.config.TaskTTL <= 0 || s.config.MaxTaskTTL <= 0 {
		return 0
	}

	return s.config.MaxTaskTTL
}

// taskCreationHooks records a task's creator. mcp-go calls it holding its task
// lock, so it must not call back into the server.
func (s *Server) taskCreationHooks() *mcpserver.TaskHooks {
	h := &mcpserver.TaskHooks{}
	h.AddOnTaskCreated(func(ctx context.Context, metrics mcpserver.TaskMetrics) {
		s.taskOwners.record(metrics.TaskID, taskOwner(ctx), s.taskOwnerRetention())
	})

	return h
}

// installTaskOwnerHooks points a request for another identity's task at an ID
// mcp-go never issues, so the caller gets the answer an unknown ID gets. Like
// installTaskTTLHook, it relies on the handler receiving the hooked request.
func (s *Server) installTaskOwnerHooks(h *mcpserver.Hooks) {
	hide := func(ctx context.Context, taskID *string) {
		if !s.taskOwners.owns(*taskID, taskOwner(ctx)) {
			*taskID = ""
		}
	}

	h.AddBeforeGetTask(func(ctx context.Context, _ any, msg *mcplib.GetTaskRequest) {
		hide(ctx, &msg.Params.TaskId)
	})
	h.AddBeforeTaskResult(func(ctx context.Context, _ any, msg *mcplib.TaskResultRequest) {
		hide(ctx, &msg.Params.TaskId)
	})
	h.AddBeforeCancelTask(func(ctx context.Context, _ any, msg *mcplib.CancelTaskRequest) {
		hide(ctx, &msg.Params.TaskId)
	})
}
