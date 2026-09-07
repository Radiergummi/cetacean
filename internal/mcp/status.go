package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetClusterStatus is the landing call.
//
// It reads through the same ACL-filtered listings find uses rather than the
// cache directly, so a caller is told about the cluster they can see. The
// counts still come from the snapshot, which is cluster-wide — they are
// aggregates a caller with partial grants can already read at
// cetacean://cluster, and withholding them would make the two disagree.
func (s *Server) toolGetClusterStatus(
	ctx context.Context,
	_ mcplib.CallToolRequest,
) (string, error) {
	services := s.filterServices(ctx, s.cache.ListServices())
	nodes := s.filterNodes(ctx, s.cache.ListNodes())

	// The running counts come off the snapshot rather than from a second
	// RunningTaskCounts call: Snapshot builds the identical map to decide
	// converged versus degraded, so asking again walked the whole task table a
	// third time, under a third lock, for the landing call.
	snap := s.cache.Snapshot()

	return marshalResult(cluster.BuildClusterStatus(
		snap,
		services,
		nodes,
		snap.RunningByService,
	))
}
