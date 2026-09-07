package mcp

import (
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// clusterOverview is the shape cetacean://cluster serves.
//
// It exists because cache.ClusterSnapshot cannot be served as-is: it reports
// TotalCPU in whole cores and ReservedCPU in nanoCPUs, under two adjacent
// names that carry no unit. The dashboard knows to divide one by 1e9 before
// comparing them (frontend/src/components/metrics/CapacitySection.tsx), but
// that knowledge lives in the reader, not the payload, and an MCP caller
// asking "how much of the cluster is reserved?" divides the two as spelled and
// is wrong by nine orders of magnitude.
//
// The snapshot's field names stay as they are because the REST API publishes
// them and the dashboard string-matches on that contract; the correction is
// made here, at the one boundary where the reader is a model rather than code
// that was written against the quirk.
//
// Every numeric field names its unit, per the house rule that a quantity is
// never implied — the same rule the CPU defect in e25089e2 produced. The four
// shared with get_cluster_status are embedded from cluster.ClusterCapacity
// rather than restated, so the two reads cannot correct the snapshot
// differently; embedding is anonymous, so they stay flat in the JSON.
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

	LastSync time.Time `json:"lastSync"`
}

// newClusterOverview converts a snapshot into the served shape. The per-node
// maxima the snapshot has already divided down, so they are only widened.
func newClusterOverview(snap cache.ClusterSnapshot) clusterOverview {
	return clusterOverview{
		NodeCount:    snap.NodeCount,
		ServiceCount: snap.ServiceCount,
		TaskCount:    snap.TaskCount,
		StackCount:   snap.StackCount,
		TasksByState: snap.TasksByState,

		NodesReady:    snap.NodesReady,
		NodesDown:     snap.NodesDown,
		NodesDraining: snap.NodesDraining,

		ServicesConverged: snap.ServicesConverged,
		ServicesDegraded:  snap.ServicesDegraded,

		ClusterCapacity: cluster.CapacityOf(snap),

		MaxNodeCPUCores:    float64(snap.MaxNodeCPU),
		MaxNodeMemoryBytes: snap.MaxNodeMemory,

		LastSync: snap.LastSync,
	}
}
