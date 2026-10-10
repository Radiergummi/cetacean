package cluster

import (
	"fmt"

	"github.com/docker/docker/api/types/swarm"
)

// serviceUpdateInFlight reports whether a service's desired spec is still in
// flux — a rolling update, or an unfinished rollback. The single definition of
// "not settled yet": DeriveServiceState and ServiceConverged both read it, so
// the state shown and the moment a mutation is done cannot disagree.
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

// ServiceConverged reports whether a service has reached its desired state, and
// what is outstanding — Docker's writes return as soon as a spec change is
// accepted. The count must *equal* the desired one, since `>=` holds on the
// first look of every scale-down, and the cache must have caught up first.
func ServiceConverged(svc swarm.Service, runningCount, completedCount int) (bool, string) {
	// An in-flight rolling update means tasks are still being replaced; wait it
	// out rather than reporting a transient count match as success.
	if serviceUpdateInFlight(svc) {
		return false, fmt.Sprintf("update in progress (%d running)", runningCount)
	}

	// A job has nothing to keep running: it converges once its run completes.
	if isJob(svc) {
		if jobState(svc, runningCount, completedCount) == stateCompleted {
			return true, fmt.Sprintf("converged: job completed (%d tasks)", completedCount)
		}

		return false, fmt.Sprintf(
			"waiting: %d tasks completed, %d running", completedCount, runningCount,
		)
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

// stateCompleted is a job whose current run has finished; no other mode has it.
const stateCompleted = "completed"

// DeriveServiceState returns a human-readable state for a service — "running",
// "pending", "failed", "updating", or for a job "completed" — and is read by
// both transports so they cannot report differently. completedCount is what
// cache.CompletedJobTaskCount reports, and only a job reads it.
func DeriveServiceState(svc swarm.Service, runningCount, completedCount int) string {
	if serviceUpdateInFlight(svc) {
		return "updating"
	}

	if isJob(svc) {
		return jobState(svc, runningCount, completedCount)
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

func isJob(svc swarm.Service) bool {
	return svc.Spec.Mode.ReplicatedJob != nil || svc.Spec.Mode.GlobalJob != nil
}

// jobState applies the replicated rule — none running and not done is a
// failure — to a run's completions. A global job's target is one completion
// per eligible node, which the spec does not say, so any completion with
// nothing left running counts as done.
func jobState(svc swarm.Service, runningCount, completedCount int) string {
	switch {
	case runningCount > 0:
		return "running"
	case completedCount >= max(jobTotalCompletions(svc), 1):
		return stateCompleted
	default:
		return "failed"
	}
}

// jobTotalCompletions is a replicated job's target, defaulted as Swarm does:
// TotalCompletions, else MaxConcurrent, else 1. A global job has none.
func jobTotalCompletions(svc swarm.Service) int {
	job := svc.Spec.Mode.ReplicatedJob
	if job == nil {
		return 0
	}

	if job.TotalCompletions != nil {
		return int(*job.TotalCompletions)
	}

	if job.MaxConcurrent != nil {
		return int(*job.MaxConcurrent)
	}

	return 1
}

// deriveNodeState reports a node's practical condition, and is what both
// RowsForNodes and NodeDigest call. Status.State is Swarm's read on whether the
// node is reachable, Spec.Availability the operator's decision about scheduling
// there. Unreachable is worse, so Availability only answers once it is "ready".
func deriveNodeState(node swarm.Node) string {
	if node.Status.State != swarm.NodeStateReady {
		return string(node.Status.State)
	}

	if node.Spec.Availability != swarm.NodeAvailabilityActive {
		return string(node.Spec.Availability)
	}

	return string(swarm.NodeStateReady)
}
