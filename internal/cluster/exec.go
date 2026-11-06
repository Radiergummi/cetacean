package cluster

import "github.com/docker/docker/api/types/swarm"

// ExecIneligibility names why a task cannot take a shell; empty means it can.
type ExecIneligibility string

const (
	ExecEligible        ExecIneligibility = ""
	ExecTaskNotRunning  ExecIneligibility = "task_not_running"
	ExecNodeUnavailable ExecIneligibility = "node_unavailable"
	ExecNodeUnsupported ExecIneligibility = "node_unsupported"
	ExecSelf            ExecIneligibility = "cetacean"
	ExecSelfUnknown     ExecIneligibility = "self_unknown"
)

// ExecEligibility is the rule every transport applies before a shell is opened.
// A paused or drained node is refused even though its containers may still run:
// Cetacean is not an SSH gateway for nodes taken out of service.
func ExecEligibility(
	task swarm.Task, node swarm.Node, nodeFound bool, self func(swarm.Task) ExecIneligibility,
) ExecIneligibility {
	switch {
	case task.Status.State != swarm.TaskStateRunning,
		task.Status.ContainerStatus == nil,
		task.Status.ContainerStatus.ContainerID == "":
		return ExecTaskNotRunning
	case !nodeFound,
		node.Status.State != swarm.NodeStateReady,
		node.Spec.Availability != swarm.NodeAvailabilityActive:
		return ExecNodeUnavailable
	case node.Description.Platform.OS != "linux":
		return ExecNodeUnsupported
	}

	return self(task)
}
