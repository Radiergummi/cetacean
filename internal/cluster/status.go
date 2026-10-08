package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ClusterStatus is the landing read: everything needed to decide whether to
// look further, and where. It names the unhealthy resources rather than
// counting them, since a count guarantees a second call. Every unhealthy entry
// is a Row, so it carries the id and name the next describe needs.
type ClusterStatus struct {
	// Healthy is the one-line answer: nothing degraded, unreachable, or
	// stalled mid-rollout. A node deliberately drained is the cluster doing
	// what it was told, so it lands in DrainingNodes and leaves this true; an
	// update still rolling likewise, and only one Swarm has paused counts.
	Healthy bool `json:"healthy"`

	NodeCount    int `json:"nodeCount"`
	ServiceCount int `json:"serviceCount"`
	TaskCount    int `json:"taskCount"`
	StackCount   int `json:"stackCount"`

	// UnhealthyServices are the services not in their desired state, and
	// Rollouts those mid-update, and DrainingNodes those deliberately made
	// unavailable. All are always arrays, never null.
	UnhealthyServices []Row `json:"unhealthyServices"`
	UnhealthyNodes    []Row `json:"unhealthyNodes"`
	DrainingNodes     []Row `json:"drainingNodes"`
	Rollouts          []Row `json:"rollouts"`

	Capacity ClusterCapacity `json:"capacity"`
}

// ClusterCapacity is the reserved-versus-available picture, in named units.
// Both CPU figures are cores: the snapshot behind them holds one in cores and
// one in nanoCPUs, so dividing them as spelled is wrong by a factor of 1e9.
type ClusterCapacity struct {
	TotalCPUCores       float64 `json:"totalCPUCores"`
	ReservedCPUCores    float64 `json:"reservedCPUCores"`
	TotalMemoryBytes    int64   `json:"totalMemoryBytes"`
	ReservedMemoryBytes int64   `json:"reservedMemoryBytes"`
}

// nanoCPUsPerCore is Docker's fixed-point scale: NanoCPUs of 1e9 is one core.
const nanoCPUsPerCore = 1e9

// CapacityOf puts a snapshot's two CPU figures in the same unit, and is the
// one place that correction is made: TotalCPU is whole cores and ReservedCPU
// nanoCPUs, under adjacent names carrying neither unit. Total and per-node CPU
// are already divided down, so they are widened rather than rescaled.
func CapacityOf(snap cache.ClusterSnapshot) ClusterCapacity {
	return ClusterCapacity{
		TotalCPUCores:       float64(snap.TotalCPU),
		ReservedCPUCores:    float64(snap.ReservedCPU) / nanoCPUsPerCore,
		TotalMemoryBytes:    snap.TotalMemory,
		ReservedMemoryBytes: snap.ReservedMemory,
	}
}

// BuildClusterStatus assembles the landing view. services and nodes are the
// caller's already-ACL-filtered slices and running the per-service task count,
// so a caller granted one stack is told about their cluster, not everyone's.
func BuildClusterStatus(
	snap cache.ClusterSnapshot,
	services []swarm.Service,
	nodes []swarm.Node,
	running map[string]int,
) ClusterStatus {
	status := ClusterStatus{
		NodeCount:         snap.NodeCount,
		ServiceCount:      snap.ServiceCount,
		TaskCount:         snap.TaskCount,
		StackCount:        snap.StackCount,
		UnhealthyServices: []Row{},
		UnhealthyNodes:    []Row{},
		DrainingNodes:     []Row{},
		Rollouts:          []Row{},
		Capacity:          CapacityOf(snap),
	}

	// Rows are sorted, so the raw services are indexed rather than walked
	// alongside them: a rollout's state lives on the Docker record, not on the
	// projection.
	byID := make(map[string]swarm.Service, len(services))
	for _, svc := range services {
		byID[svc.ID] = svc
	}

	stalled := 0

	for _, row := range RowsForServices(services, running) {
		svc := byID[row.ID]

		// A stalled rollout is a rollout even though the service does not read
		// as "updating": a paused update is settled as far as the replica count
		// goes, so a service whose deploy Swarm gave up on sits at "running" on
		// its old spec and says nothing about the change that never landed.
		if isStalled := rolloutStalled(svc); isStalled || row.State == "updating" {
			status.Rollouts = append(status.Rollouts, row)

			if isStalled {
				stalled++
			}
		}

		// Not exclusive with the above: a service failing mid-deploy is both a
		// rollout and something wrong.
		if row.State == "failed" || row.State == "pending" {
			status.UnhealthyServices = append(status.UnhealthyServices, row)
		}
	}

	for _, row := range RowsForNodes(nodes) {
		switch row.State {
		case string(swarm.NodeStateReady):
			// Nominal: ready and accepting work.

		case string(swarm.NodeAvailabilityDrain), string(swarm.NodeAvailabilityPause):
			status.DrainingNodes = append(status.DrainingNodes, row)

		default:
			status.UnhealthyNodes = append(status.UnhealthyNodes, row)
		}
	}

	status.Healthy = len(status.UnhealthyServices) == 0 &&
		len(status.UnhealthyNodes) == 0 &&
		stalled == 0

	return status
}

// rolloutStalled reports whether a spec change stopped short rather than
// merely being in progress: Swarm pauses an update whose tasks keep failing.
// It reads the Docker record, not the derived state, where a paused update
// counts as settled and the service reads as "running".
func rolloutStalled(svc swarm.Service) bool {
	if svc.UpdateStatus == nil {
		return false
	}

	switch svc.UpdateStatus.State {
	case swarm.UpdateStatePaused, swarm.UpdateStateRollbackPaused:
		return true

	default:
		return false
	}
}
