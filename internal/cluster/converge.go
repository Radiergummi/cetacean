package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

const (
	// ConvergencePollInterval is how often a wait re-reads the cache.
	ConvergencePollInterval = 500 * time.Millisecond

	// ConvergenceTimeout bounds how long a caller waits before giving up.
	ConvergenceTimeout = 5 * time.Minute
)

// AwaitService blocks until serviceID has settled at or above minVersion, ctx
// expires, or timeout elapses, returning the last progress line either way.
// minVersion is what the mutation produced, and anything older is refused,
// since the cache still holds the pre-write spec. The context is never detached.
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
			// Wrap bounded's error, not ctx's, so callers can tell a timeout
			// (DeadlineExceeded) from a cancellation (Canceled): only the
			// former should still hand back the mutation result.
			return progress, fmt.Errorf(
				"service %s did not converge (%s): %w", serviceID, progress, bounded.Err(),
			)
		case <-ticker.C:
		}
	}
}

// serviceConvergedAt supplies the cache reads and the minVersion gate; the
// convergence rule itself lives in ServiceConverged, so the REST and MCP
// transports cannot drift on what "settled" means.
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
