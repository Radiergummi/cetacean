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

	// maxConcurrentWatches bounds how many waits may be in flight at once. A
	// wait detaches from its request context, so a disconnected client leaves
	// the poll running to its ceiling, and `watch` is a tier-0 read reachable
	// on a read-only deployment. Not configurable: it stops accumulation.
	maxConcurrentWatches = 16
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

// toolWatch waits until a service has settled, and reports what it saw:
// cluster.ServiceConverged as a read, so an agent can ask "has it deployed
// yet?" without deploying something. The wait detaches from the request
// context, as the converging mutations do, so `timeout` is the real bound.
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

	// Claimed after the read check, so a call the caller was never allowed to
	// make cannot occupy a slot.
	if release, ok := s.claimWatch(); ok {
		defer release()
	} else {
		return "", fmt.Errorf(
			"too many waits in flight (limit %d); a wait cannot be cancelled once "+
				"started, so retry once one has settled or timed out",
			maxConcurrentWatches,
		)
	}

	started := time.Now()

	result := watchResult{Outcome: "converged"}

	if waitErr := s.awaitServiceConvergenceFor(
		ctx,
		svc.ID,
		// No version gate: watch follows no write of its own, so whatever the
		// cache holds is the state it was asked about.
		0,
		watchTimeout(req),
		&result.Observed,
	); waitErr != nil {
		result.Outcome = "timeout"
	}

	result.ElapsedSeconds = time.Since(started).Seconds()

	return marshalResult(result)
}

// watchTimeout resolves how long a wait may run. Zero or negative means "no
// preference", not "as long as allowed": the wait is detached, so tasks/cancel
// cannot interrupt it and such a caller would be held for the full ceiling
// instead of the documented minute.
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

// claimWatch takes one of the concurrent-wait slots, reporting whether it got
// one; the release function returns it. A Server without the channel — built
// in a test rather than by New — is unbounded, since the bound protects a
// process serving untrusted callers.
func (s *Server) claimWatch() (release func(), ok bool) {
	if s.watches == nil {
		return func() {}, true
	}

	select {
	case s.watches <- struct{}{}:
		return func() { <-s.watches }, true

	default:
		return nil, false
	}
}
