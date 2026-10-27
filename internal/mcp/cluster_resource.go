package mcp

import (
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/quorum"
)

// clusterOverview is the shape cetacean://cluster serves. cache.ClusterSnapshot
// cannot be: it reports TotalCPU in cores and ReservedCPU in nanoCPUs under
// adjacent names carrying no unit. Its field names stay, since the web API
// publishes them, so the correction is made here and every field names its unit.
type clusterOverview struct {
	NodeCount    int            `json:"nodeCount"`
	ServiceCount int            `json:"serviceCount"`
	TaskCount    int            `json:"taskCount"`
	StackCount   int            `json:"stackCount"`
	TasksByState map[string]int `json:"tasksByState"`

	NodesReady    int `json:"nodesReady"`
	NodesDown     int `json:"nodesDown"`
	NodesDraining int `json:"nodesDraining"`

	ServicesConverged int `json:"servicesConverged"`
	ServicesDegraded  int `json:"servicesDegraded"`

	cluster.ClusterCapacity

	MaxNodeCPUCores    float64 `json:"maxNodeCPUCores"`
	MaxNodeMemoryBytes int64   `json:"maxNodeMemoryBytes"`

	Quorum quorum.Quorum `json:"quorum"`

	LastSync time.Time `json:"lastSync"`
}

// newClusterOverview converts a snapshot into the served shape. The per-node
// maxima the snapshot has already divided down, so they are only widened. The
// service counts all come from services, so the parts never exceed the total.
func newClusterOverview(snap cache.ClusterSnapshot, services []swarm.Service) clusterOverview {
	converged, degraded := cluster.CountServiceStates(snap, services)

	return clusterOverview{
		NodeCount:    snap.NodeCount,
		ServiceCount: len(services),
		TaskCount:    snap.TaskCount,
		StackCount:   snap.StackCount,
		TasksByState: snap.TasksByState,

		NodesReady:    snap.NodesReady,
		NodesDown:     snap.NodesDown,
		NodesDraining: snap.NodesDraining,

		ServicesConverged: converged,
		ServicesDegraded:  degraded,

		ClusterCapacity: cluster.CapacityOf(snap),

		MaxNodeCPUCores:    float64(snap.MaxNodeCPU),
		MaxNodeMemoryBytes: snap.MaxNodeMemory,

		Quorum: snap.Quorum,

		LastSync: snap.LastSync,
	}
}
