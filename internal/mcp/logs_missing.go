package mcp

import (
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
)

// isNotFound reports whether the daemon refused a read because the record is
// gone.
//
// The classification is the SDK's own: every non-2xx response the Docker
// client returns is wrapped by httpErrorFromStatusCode, so a 404 from the log
// endpoints carries cerrdefs.ErrNotFound exactly as one from any other call
// does — the same predicate internal/api's writeDockerError already uses.
// Matching the daemon's message text instead would also claim any unrelated
// failure whose wording happens to contain the phrase, and the caller would be
// told, confidently, that Swarm had retired a record it never lost.
func isNotFound(err error) bool {
	return cerrdefs.IsNotFound(err)
}

// explainMissingTaskLogs turns the daemon's "task not found" into something a
// caller can act on.
//
// get_logs offers a task read as the only way to reach a replica that has
// already exited, and the moment that is most worth doing — just after a
// replica died — is precisely when Swarm has retired the record: it keeps
// task-history-limit entries per slot, five by default, which on a service
// restarting every few seconds is a window seconds deep. The daemon's own
// message says only "not found", which reads identically to a mistyped ID and
// invites a retry that cannot succeed.
//
// The cache is the tiebreaker. It outlives the daemon's task record, so a task
// it still knows is a real one whose output has simply been discarded — and it
// also knows the parent service, which is where the caller should look next.
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
