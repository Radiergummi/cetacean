package cluster

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"
)

// serviceUpdateInFlight reports whether a service's desired spec is still in
// flux — a rolling update, or a rollback that has started but not finished.
//
// This is the single definition of "not settled yet". Both DeriveServiceState
// and ServiceConverged read it, so the state a caller is shown and the moment a
// mutation is called done cannot disagree about which Swarm update states count.
func serviceUpdateInFlight(svc swarm.Service) bool {
	if svc.UpdateStatus == nil {
		return false
	}

	switch svc.UpdateStatus.State {
	case swarm.UpdateStateUpdating,
		swarm.UpdateStateRollbackStarted,
		swarm.UpdateStateRollbackPaused:
		return true

	default:
		// Paused, Completed and RollbackCompleted are all settled states; the
		// caller derives from the replica count instead.
		return false
	}
}

// ServiceConverged reports whether a service has actually reached its desired
// state, and a human-readable line describing what is still outstanding.
//
// Docker's write APIs return as soon as the swarm accepts a spec change, long
// before the new state is real. This is what turns "accepted" into "running",
// and it is deliberately stricter than DeriveServiceState: it requires the
// running count to *equal* the desired one, so a scale-down is not called done
// while surplus tasks are still being reaped.
func ServiceConverged(svc swarm.Service, runningCount int) (bool, string) {
	// An in-flight rolling update means tasks are still being replaced; wait it
	// out rather than reporting a transient count match as success.
	if serviceUpdateInFlight(svc) {
		return false, fmt.Sprintf("update in progress (%d running)", runningCount)
	}

	// A global service has no replica count to compare against, so it converges
	// as soon as no update is in flight: the desired count is whatever the
	// scheduler decides the cluster's nodes can carry.
	if svc.Spec.Mode.Replicated == nil || svc.Spec.Mode.Replicated.Replicas == nil {
		return true, fmt.Sprintf("converged: %d tasks running", runningCount)
	}

	desired := int(*svc.Spec.Mode.Replicated.Replicas)
	if runningCount == desired {
		return true, fmt.Sprintf("converged: %d/%d replicas running", runningCount, desired)
	}

	return false, fmt.Sprintf("waiting: %d/%d replicas running", runningCount, desired)
}

// DeriveServiceState returns a human-readable state for a service given its
// current running-task count. Used by both REST (internal/api/search_handlers.go)
// and MCP (internal/mcp/tools.go) search so the two transports report
// identical state.
//
// States: "running", "pending", "failed", "updating". The "updating" branch
// also covers in-progress rollbacks — paused rollbacks and freshly-started
// rollbacks both still represent a service whose desired spec is in flux.
func DeriveServiceState(svc swarm.Service, runningCount int) string {
	if serviceUpdateInFlight(svc) {
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

// deriveNodeState reports a node's practical condition, and is the single
// definition both RowsForNodes and NodeDigest call — a list and a detail view
// of the same node must never disagree about it.
//
// Status.State ("ready", "down", "disconnected") is Swarm's own read on
// whether the node is reachable at all; Spec.Availability ("active", "pause",
// "drain") is the operator's separate decision about whether the scheduler
// may still place work there. Unreachable is the strictly worse fact, so it
// takes precedence: only once Status.State says "ready" does a paused or
// drained node's Availability become the more useful answer, because "ready"
// alone would mislead a caller into thinking the scheduler could still use
// it.
func deriveNodeState(node swarm.Node) string {
	if node.Status.State != swarm.NodeStateReady {
		return string(node.Status.State)
	}

	if node.Spec.Availability != swarm.NodeAvailabilityActive {
		return string(node.Spec.Availability)
	}

	return string(swarm.NodeStateReady)
}
