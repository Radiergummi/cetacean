package filter

import "github.com/docker/docker/api/types/swarm"

// intern boxes each value of a closed set once, so the filter environment gets
// the same interface every time instead of allocating per field per item. A
// value outside the set (e.g. a state a newer Docker adds) still works; it is
// boxed on the spot.
func intern[T ~string](values ...T) map[T]any {
	boxed := make(map[T]any, len(values))
	for _, v := range values {
		boxed[v] = string(v)
	}

	return boxed
}

func boxedOr[T ~string](boxed map[T]any, v T) any {
	if b, ok := boxed[v]; ok {
		return b
	}

	return string(v)
}

var (
	nodeStates = intern(
		swarm.NodeStateUnknown,
		swarm.NodeStateDown,
		swarm.NodeStateReady,
		swarm.NodeStateDisconnected,
	)
	nodeRoles = intern(
		swarm.NodeRoleWorker,
		swarm.NodeRoleManager,
	)
	nodeAvailabilities = intern(
		swarm.NodeAvailabilityActive,
		swarm.NodeAvailabilityPause,
		swarm.NodeAvailabilityDrain,
	)
	taskStates = intern(
		swarm.TaskStateNew,
		swarm.TaskStateAllocated,
		swarm.TaskStatePending,
		swarm.TaskStateAssigned,
		swarm.TaskStateAccepted,
		swarm.TaskStatePreparing,
		swarm.TaskStateReady,
		swarm.TaskStateStarting,
		swarm.TaskStateRunning,
		swarm.TaskStateComplete,
		swarm.TaskStateShutdown,
		swarm.TaskStateFailed,
		swarm.TaskStateRejected,
		swarm.TaskStateRemove,
		swarm.TaskStateOrphaned,
	)

	serviceModes = intern("replicated", "global", "replicated-job", "global-job")

	// An absent optional field, boxed once rather than per item.
	emptyString any = ""
)
