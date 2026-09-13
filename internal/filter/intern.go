package filter

import "github.com/docker/docker/api/types/swarm"

// Putting a string into a map[string]any boxes it, and boxing allocates. A
// filtered list does that for every field of every item, which was most of what
// filtering a thousand resources cost.
//
// The fields whose values come from a closed set are boxed once, at startup,
// and the environment is handed the same interface every time. The rest — ids,
// names, images — are unique per item and have to be boxed as they come.
//
// A value outside the set still works; it is boxed on the spot, which is what
// every value did before. That is the case for a Docker release adding a state
// this list has not caught up with.
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

	// The two values ServiceEnv reports for mode, boxed rather than rebuilt.
	modeReplicated any = "replicated"
	modeGlobal     any = "global"

	// An absent optional field, boxed once rather than per item.
	emptyString any = ""
)
