package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

const (
	// ConvergencePollInterval is how often a wait re-reads the cache. Cheap:
	// it is bounded by how quickly we want to notice convergence, not by cost.
	ConvergencePollInterval = 500 * time.Millisecond

	// ConvergenceTimeout bounds how long a caller waits before giving up. A
	// mutation that has not converged by then is not going to on its own — an
	// unsatisfiable placement constraint, an image that will not pull.
	ConvergenceTimeout = 5 * time.Minute
)

// AwaitService blocks until serviceID has settled at or above minVersion, ctx
// expires, or timeout elapses. It returns the last progress line either way.
//
// minVersion is the service version the mutation produced, and the wait
// refuses to judge anything older. The cache is filled asynchronously by the
// event watcher, so at the moment a write returns it still holds the spec and
// the tasks from *before* it: a scale from two to five asks five running
// against a desired two and settles instantly, and a scale from five to two
// asks two against a desired five and does the same. Neither is a
// convergence, and no predicate over those numbers could tell. Zero disables
// the gate, for callers that follow no write of their own.
//
// The context is honoured as given and never detached. REST passes a live
// request context so a disconnecting client cancels the wait; MCP detaches
// before calling, because mcp-go runs tasks on a goroutine holding an
// already-cancelled request context.
//
// The returned progress line is only ever the last one observed, not a
// running feed — a caller wanting to report progress *during* the wait has to
// poll the cache itself.
func AwaitService(
	ctx context.Context,
	c *cache.Cache,
	serviceID string,
	minVersion uint64,
	poll, timeout time.Duration,
) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var progress string
	for {
		done, status := serviceConvergedAt(c, serviceID, minVersion)
		progress = status
		if done {
			return progress, nil
		}

		select {
		case <-bounded.Done():
			// Wrap bounded's own error, not ctx's: bounded is the WithTimeout
			// child, so a parent cancellation surfaces as context.Canceled and
			// a genuine timeout as context.DeadlineExceeded, and callers can
			// tell the two apart — a timed-out wait should still hand back the
			// mutation result, a cancelled or otherwise failed one should not.
			return progress, fmt.Errorf(
				"service %s did not converge (%s): %w", serviceID, progress, bounded.Err(),
			)
		case <-ticker.C:
		}
	}
}

// serviceConvergedAt watches one service by ID. The convergence rule itself
// lives in ServiceConverged so the REST and MCP transports cannot drift on
// what "settled" means; this only supplies the cache reads and the
// minVersion gate documented on AwaitService.
func serviceConvergedAt(c *cache.Cache, serviceID string, minVersion uint64) (bool, string) {
	svc, ok := c.GetService(serviceID)
	if !ok {
		return false, "service not in the cache yet"
	}

	if svc.Version.Index < minVersion {
		return false, fmt.Sprintf(
			"waiting: the mutation is not visible yet (cache at version %d, wrote %d)",
			svc.Version.Index, minVersion,
		)
	}

	return ServiceConverged(svc, c.RunningTaskCount(svc.ID))
}
