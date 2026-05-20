package cluster

import "github.com/docker/docker/api/types/swarm"

// DeriveServiceState returns a human-readable state for a service given its
// current running-task count. Mirrors the inlined logic in
// internal/api/search_handlers.go so REST and MCP report identical state.
//
// States: "running", "pending", "failed", "updating".
func DeriveServiceState(svc swarm.Service, runningCount int) string {
	if svc.UpdateStatus != nil && svc.UpdateStatus.State == swarm.UpdateStateUpdating {
		return "updating"
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
