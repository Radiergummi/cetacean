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
	Healthy bool `json:"healthy"`

	NodeCount    int `json:"nodeCount"`
	ServiceCount int `json:"serviceCount"`
	TaskCount    int `json:"taskCount"`
	StackCount   int `json:"stackCount"`

	// UnhealthyServices are the services not in their desired state, and
	// Rollouts those mid-update. Both are always arrays, never null.
	UnhealthyServices []Row `json:"unhealthyServices"`
	UnhealthyNodes    []Row `json:"unhealthyNodes"`
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
		Rollouts:          []Row{},
		Capacity:          CapacityOf(snap),
	}

	for _, row := range RowsForServices(services, running) {
		switch row.State {
		case "failed", "pending":
			status.UnhealthyServices = append(status.UnhealthyServices, row)

		case "updating":
			status.Rollouts = append(status.Rollouts, row)
		}
	}

	for _, row := range RowsForNodes(nodes) {
		if row.State != "ready" {
			status.UnhealthyNodes = append(status.UnhealthyNodes, row)
		}
	}

	status.Healthy = len(status.UnhealthyServices) == 0 && len(status.UnhealthyNodes) == 0

	return status
}
