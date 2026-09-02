package mcp

import (
	"context"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// toolGetTopology projects the cluster as a graph for the topology widget.
//
// The graph builders live in internal/cluster because a topology is a fact
// about the cluster, not about a transport — the REST handlers in internal/api
// read the same cache through the same rules, and a second implementation here
// is exactly how the two would drift.
//
// Every slice handed to a builder is ACL-filtered first: the builders drop any
// task whose node or service is missing from what they were given, so filtering
// the inputs is what keeps an unreadable resource out of the graph entirely
// rather than leaking it as a dangling edge.
func (s *Server) toolGetTopology(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	view := req.GetString("view", cluster.TopologyViewNetwork)

	switch view {
	case cluster.TopologyViewNetwork:
		return marshalResult(cluster.NetworkGraph(
			s.filterServices(ctx, s.cache.ListServices()),
			s.filterNetworks(ctx, s.cache.ListNetworks()),
		))

	case cluster.TopologyViewPlacement:
		return marshalResult(cluster.PlacementGraph(
			s.filterNodes(ctx, s.cache.ListNodes()),
			s.cache.ListTasks(),
			s.filterServices(ctx, s.cache.ListServices()),
		))

	default:
		return "", fmt.Errorf(
			"unknown view %q; expected %q or %q",
			view, cluster.TopologyViewNetwork, cluster.TopologyViewPlacement,
		)
	}
}
