package mcp

import (
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
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
// never implied — the same rule the CPU defect in e25089e2 produced.
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

	TotalCPUCores       float64 `json:"totalCPUCores"`
	ReservedCPUCores    float64 `json:"reservedCPUCores"`
	MaxNodeCPUCores     float64 `json:"maxNodeCPUCores"`
	TotalMemoryBytes    int64   `json:"totalMemoryBytes"`
	ReservedMemoryBytes int64   `json:"reservedMemoryBytes"`
	MaxNodeMemoryBytes  int64   `json:"maxNodeMemoryBytes"`

	LastSync time.Time `json:"lastSync"`
}

// nanoCPUsPerCore is Docker's fixed-point scale for CPU quantities: a
// swarm.Resources NanoCPUs of 1e9 is one core.
const nanoCPUsPerCore = 1e9

// newClusterOverview converts a snapshot into the served shape, putting both
// CPU figures in cores. Reserved CPU is the one the snapshot holds in
// nanoCPUs; total and per-node CPU it has already divided down, so they are
// only widened, not rescaled.
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

		TotalCPUCores:       float64(snap.TotalCPU),
		ReservedCPUCores:    float64(snap.ReservedCPU) / nanoCPUsPerCore,
		MaxNodeCPUCores:     float64(snap.MaxNodeCPU),
		TotalMemoryBytes:    snap.TotalMemory,
		ReservedMemoryBytes: snap.ReservedMemory,
		MaxNodeMemoryBytes:  snap.MaxNodeMemory,

		LastSync: snap.LastSync,
	}
}
