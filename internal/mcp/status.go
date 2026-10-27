package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetClusterStatus is the landing call. It reads through the same
// ACL-filtered listings find uses, so a caller is told about the cluster they
// can see. The counts still come from the cluster-wide snapshot: they are
// already readable at cetacean://cluster, and withholding them would disagree.
func (s *Server) toolGetClusterStatus(
	ctx context.Context,
	_ mcplib.CallToolRequest,
) (string, error) {
	if err := s.requireAnyGrant(ctx); err != nil {
		return "", err
	}

	services := s.filterServices(ctx, s.cache.ListServices())
	nodes := s.filterNodes(ctx, s.cache.ListNodes())

	// The running counts come off the snapshot rather than from a second
	// RunningTaskCounts call, which would walk the whole task table again.
	snap := s.cache.Snapshot()

	return marshalResult(cluster.BuildClusterStatus(
		snap,
		services,
		nodes,
		snap.RunningByService,
	))
}
