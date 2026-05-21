package cluster

import "github.com/docker/docker/api/types/swarm"

// DeriveServiceState returns a human-readable state for a service given its
// current running-task count. Used by both REST (internal/api/search_handlers.go)
// and MCP (internal/mcp/tools.go) search so the two transports report
// identical state.
//
// States: "running", "pending", "failed", "updating". The "updating" branch
// also covers in-progress rollbacks — paused rollbacks and freshly-started
// rollbacks both still represent a service whose desired spec is in flux.
func DeriveServiceState(svc swarm.Service, runningCount int) string {
	if svc.UpdateStatus != nil {
		switch svc.UpdateStatus.State {
		case swarm.UpdateStateUpdating,
			swarm.UpdateStateRollbackStarted,
			swarm.UpdateStateRollbackPaused:
			return "updating"
		case swarm.UpdateStatePaused,
			swarm.UpdateStateCompleted,
			swarm.UpdateStateRollbackCompleted:
			// fall through to replica-count derivation
		}
	}

	if svc.Spec.Mode.Global != nil {
		if runningCount == 0 {
			return "pending"
		}
		return "running"
	}

	// Replicated mode (or unset — treat desired as 0).
	desired := 0
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		desired = int(*svc.Spec.Mode.Replicated.Replicas)
	}

	if desired > 0 && runningCount == 0 {
		return "failed"
	}
	if runningCount < desired {
		return "pending"
	}
	return "running"
}
