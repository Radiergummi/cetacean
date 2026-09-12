package mcp

import (
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
)

// isNotFound reports whether the daemon refused a read because the record is
// gone. The classification is the SDK's own, so a 404 from the log endpoints
// carries cerrdefs.ErrNotFound like any other call. Matching the message text
// instead would claim any unrelated failure whose wording contains the phrase.
func isNotFound(err error) bool {
	return cerrdefs.IsNotFound(err)
}

// explainMissingTaskLogs turns the daemon's "task not found" into something a
// caller can act on: a task read is the only way to reach an exited replica,
// and "not found" reads identically to a mistyped ID. The cache is the
// tiebreaker — it outlives the record, and knows the parent service.
func (s *Server) explainMissingTaskLogs(taskID string, err error) error {
	task, known := s.cache.GetTask(taskID)
	if !known {
		return fmt.Errorf(
			"no such task %q: it is not in the cluster state. If this ID came from an "+
				"earlier read, Swarm has since retired the record. Use find with "+
				"type \"tasks\" to list the ones that currently exist",
			taskID,
		)
	}

	// Naming the service beats naming its ID: it is what the caller passes to
	// the very next call. The ID is the fallback for a service the cache has
	// already dropped, which get_logs still resolves.
	target := task.ServiceID
	if svc, ok := s.cache.GetService(task.ServiceID); ok && svc.Spec.Name != "" {
		target = svc.Spec.Name
	}

	return fmt.Errorf(
		"task %s still exists but its log output has been discarded: Swarm keeps only "+
			"task-history-limit records per slot (five by default), so a replica that "+
			"restarts often loses its output within seconds and it is not retrievable "+
			"again. Read the logs of service %q instead, or describe that service for "+
			"the recent task failures and the reason behind them (daemon said: %v)",
		taskID, target, err,
	)
}
