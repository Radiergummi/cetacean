package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// maxWatchTimeout bounds a wait. convergenceTimeout (5m) is the ceiling the
// converging mutations already use; a read has no reason to exceed it.
const (
	maxWatchTimeout     = 5 * time.Minute
	defaultWatchTimeout = 60 * time.Second
)

// watchResult reports how a wait ended.
type watchResult struct {
	// Outcome is "converged" or "timeout".
	Outcome string `json:"outcome"`

	// Observed is the last progress line the convergence check produced —
	// "waiting: 1/3 replicas running". On a timeout it is the whole answer:
	// it says how far the rollout actually got.
	Observed string `json:"observed"`

	ElapsedSeconds float64 `json:"elapsedSeconds"`
}

// toolWatch waits until a service has settled, and reports what it saw.
//
// cluster.ServiceConverged is the rule for "settled", and REST and MCP already
// share it — but it was reachable only as a side effect of a mutation, so an
// agent could not ask "has it deployed yet?" without deploying something.
// Making it a read turns a poll loop of twenty describe calls into one call.
//
// The wait detaches from the request context, exactly as the converging
// mutations do, because a task-augmented call runs on a goroutine holding an
// already-cancelled HTTP context. tasks/cancel therefore cannot interrupt it;
// `timeout` is the real bound, which is why it is capped.
func (s *Server) toolWatch(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name := req.GetString("service", "")
	if name == "" {
		return "", fmt.Errorf("service: required")
	}

	svc, found, err := s.cache.ResolveService(name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no such service %q", name)
	}

	if err := s.checkRead(ctx, "service", svc.Spec.Name); err != nil {
		return "", err
	}

	started := time.Now()

	result := watchResult{Outcome: "converged"}

	if waitErr := s.awaitServiceConvergenceFor(
		ctx,
		svc.ID,
		watchTimeout(req),
		&result.Observed,
	); waitErr != nil {
		result.Outcome = "timeout"
	}

	result.ElapsedSeconds = time.Since(started).Seconds()

	return marshalResult(result)
}

// watchTimeout resolves how long a wait may run.
//
// Zero or negative means "no preference", not "wait as long as you are
// allowed to": the wait is detached from the request context, so tasks/cancel
// cannot interrupt it, and a caller spelling it that way would be held for the
// full ceiling instead of the documented minute.
func watchTimeout(req mcplib.CallToolRequest) time.Duration {
	timeout := time.Duration(
		req.GetInt("timeout", int(defaultWatchTimeout.Seconds())),
	) * time.Second

	if timeout <= 0 {
		return defaultWatchTimeout
	}

	if timeout > maxWatchTimeout {
		return maxWatchTimeout
	}

	return timeout
}
