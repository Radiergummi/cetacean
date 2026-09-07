package cluster

import (
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ClusterStatus is the landing read: everything needed to decide whether to
// look further, and where.
//
// It names the unhealthy resources rather than counting them.
// cache.ClusterSnapshot reports "servicesDegraded": 3, which is an answer that
// guarantees a second call — and follow-up calls are where a caller's budget
// actually goes. Every unhealthy entry is a Row, so it carries the id and name
// the next describe needs.
type ClusterStatus struct {
	// Healthy is the one-line answer: nothing degraded, nothing unreachable,
	// nothing mid-rollout that has stalled.
	//
	// A node deliberately drained or paused is not a fault — it is the cluster
	// doing what it was told — so it lands in DrainingNodes and leaves this
	// true. Draining a node is the one operation whose own follow-up check
	// would otherwise report the cluster broken for as long as the maintenance
	// lasted. An update still rolling is likewise the system working; only one
	// Swarm has paused counts against it.
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
//
// Both CPU figures are cores because the snapshot behind them holds one in
// cores and one in nanoCPUs, and a caller dividing them as the snapshot spells
// them is wrong by nine orders of magnitude.
type ClusterCapacity struct {
	TotalCPUCores       float64 `json:"totalCPUCores"`
	ReservedCPUCores    float64 `json:"reservedCPUCores"`
	TotalMemoryBytes    int64   `json:"totalMemoryBytes"`
	ReservedMemoryBytes int64   `json:"reservedMemoryBytes"`
}

// nanoCPUsPerCore is Docker's fixed-point scale: NanoCPUs of 1e9 is one core.
const nanoCPUsPerCore = 1e9

// CapacityOf puts a snapshot's two CPU figures in the same unit.
//
// It is the one place that correction is made. cache.ClusterSnapshot reports
// TotalCPU in whole cores and ReservedCPU in nanoCPUs under two adjacent names
// that carry neither unit, so every reader has to know the quirk; stating it
// twice is how the two readers come to disagree by nine orders of magnitude.
// Total and per-node CPU the snapshot has already divided down, so they are
// only widened, not rescaled.
func CapacityOf(snap cache.ClusterSnapshot) ClusterCapacity {
	return ClusterCapacity{
		TotalCPUCores:       float64(snap.TotalCPU),
		ReservedCPUCores:    float64(snap.ReservedCPU) / nanoCPUsPerCore,
		TotalMemoryBytes:    snap.TotalMemory,
		ReservedMemoryBytes: snap.ReservedMemory,
	}
}

// BuildClusterStatus assembles the landing view.
//
// services and nodes are the caller's already-ACL-filtered slices, and running
// the running-task count per service, so a caller with grants over one stack is
// told about their cluster rather than everyone's — the same rule every builder
// in this package follows.
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

// rolloutStalled reports whether a spec change has stopped short rather than
// merely being in progress.
//
// Swarm pauses an update whose tasks keep failing, and a rollback likewise;
// those are the states that need someone to look. An update still rolling is
// the system working, and counting it as unhealthy would report every ordinary
// deploy as a fault.
//
// This reads the Docker record rather than the derived state on purpose.
// serviceUpdateInFlight treats a paused update as settled — correctly, since
// waiting on it would never finish — so such a service derives its state from
// its replica count and reads as "running" on the spec it failed to leave.
// That is precisely the case a health check must not miss.
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
